package restart

import (
	"sync"
	"time"
)

// tickerInterval 是调度器检查的间隔。
//
// 定时重启的精度要求是「分钟级」，用 10 秒的间隔轮询既能及时触发，
// 又不会因为进程刚启动、宿主机时间漂移而错过当天的执行时刻。
const tickerInterval = 10 * time.Second

// Scheduler 按全局配置在指定时刻定时重启服务端进程。
//
// 调度器只负责「到点」这件事：算出下一次执行时间、到点前提醒、到点重启。
// 配置本身来自全局参数（热更新），因此每次 tick 都重新读取，
// 用户在 Web 后台改完配置立即生效，无需重启进程。
type Scheduler struct {
	// getSchedule 返回当前的定时重启配置。
	getSchedule func() Schedule
	// notify 在重启前触发，用于推送提醒。允许为 nil。
	notify func(runAt time.Time)
	// do 执行真正的重启。允许为 nil（仅提醒不重启）。
	do func(runAt time.Time)

	mu sync.Mutex
	// prev 是上一次检查的时刻，用于判断是否有计划时刻落在 (prev, now] 区间内。
	prev time.Time
	// notified 是最近一次已经发过提醒的计划时刻，用于避免重复提醒。
	notified time.Time
	// stop 关闭调度循环。
	stop chan struct{}
	// once 保证 Stop 只生效一次。
	once sync.Once
}

// NewScheduler 创建定时重启调度器。
// getSchedule 用于读取最新配置，notify 用于到点前提醒，do 用于执行重启。
func NewScheduler(getSchedule func() Schedule, notify func(runAt time.Time), do func(runAt time.Time)) *Scheduler {
	return &Scheduler{
		getSchedule: getSchedule,
		notify:      notify,
		do:          do,
		// 初始 prev 取启动时刻：服务刚起来时不会立刻补执行已经错过的计划时刻。
		prev: time.Now(),
		stop: make(chan struct{}),
	}
}

// Start 启动调度循环（后台协程）。
func (s *Scheduler) Start() {
	go s.loop()
}

// Stop 停止调度循环。
func (s *Scheduler) Stop() {
	s.once.Do(func() {
		close(s.stop)
	})
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

// tick 执行一次调度检查。
func (s *Scheduler) tick(now time.Time) {
	if s.getSchedule == nil {
		return
	}
	schedule := s.getSchedule()
	if !schedule.Enabled {
		s.mu.Lock()
		s.prev = now
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	prev := s.prev
	s.prev = now
	s.mu.Unlock()

	// 落在 (prev, now] 区间内的计划时刻即为本次要触发的时刻。
	// 用区间而不是「当前是否等于计划时刻」来判断，可以容忍 tick 的抖动，
	// 同时保证同一个计划时刻只会被触发一次。
	if target, ok := schedule.TargetBetween(prev, now); ok {
		if s.do != nil {
			s.do(target)
		}
		return
	}

	// 到点前 NotifyBefore 分钟提醒一次，同一个计划时刻只提醒一次。
	if s.notify == nil {
		return
	}
	next, ok := schedule.NextRun(now)
	if !ok {
		return
	}
	s.mu.Lock()
	notified := s.notified
	s.mu.Unlock()
	if !notified.IsZero() && !next.After(notified) {
		return
	}
	if !now.Before(next) || now.Before(next.Add(-NotifyBefore)) {
		return
	}
	s.mu.Lock()
	s.notified = next
	s.mu.Unlock()
	s.notify(next)
}
