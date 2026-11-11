package common

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
)

// 拦截日志限流参数。
const (
	// blockLogCooldown 是同一个来源 IP 两次记录之间的最小间隔。
	// 同一 IP 反复连接被拦时，冷却期内只记第一条，其余静默计数。
	blockLogCooldown = 5 * time.Minute
	// blockLogRatePerMinute 是全局每分钟最多记录的拦截日志条数。
	// 被扫描（成千上万个不同来源 IP）时用它给日志速率封顶。
	blockLogRatePerMinute = 10
	// blockLogMaxKeys 是内存里记录的来源 IP 数量上限，超出后清理过期项。
	blockLogMaxKeys = 20000
)

// BlockLogLimiter 给「拦截类」日志加两层保护：
//
//   - 按来源冷却：同一 IP 在冷却期内只记一条，冷却期内被拦的次数会被计数，
//     在该 IP 的下一条日志里以「另有 N 次同类拦截未记录」的形式补上，信息不丢。
//   - 全局令牌桶：所有来源加起来每分钟最多记录 blockLogRatePerMinute 条，
//     防止被扫描时把日志文件写爆、把日志页的轮询拖垮。
//
// 之所以必须限流：访问地区限制的拦截面比「禁止国外 IP」大得多
// （未勾选省份的国内 IP、全部境外 IP、IP 库未覆盖的段都会命中），
// UDP 链路更是每收到一个包就判定一次，不限流的话每秒能刷出几千条日志。
type BlockLogLimiter struct {
	mu sync.Mutex
	// last 记录每个来源最近一次真正写入日志的时间。
	last map[string]time.Time
	// skipped 记录每个来源在冷却期内被省略的条数。
	skipped map[string]int
	// globalSkipped 是因为全局限流被省略的总条数。
	globalSkipped int64
	// tokens 是令牌桶余额，补充速率为 ratePerMinute 每分钟。
	tokens float64
	// lastFill 是上次补充令牌的时间。
	lastFill time.Time
	// cooldown 是同一来源两次记录之间的最小间隔。
	cooldown time.Duration
	// ratePerMinute 是全局每分钟最多记录的条数。
	ratePerMinute float64
	// maxKeys 是内存中记录的来源数量上限。
	maxKeys int
}

// NewBlockLogLimiter 创建限流器。
func NewBlockLogLimiter(cooldown time.Duration, ratePerMinute int) *BlockLogLimiter {
	return &BlockLogLimiter{
		last:          make(map[string]time.Time),
		skipped:       make(map[string]int),
		tokens:        float64(ratePerMinute),
		lastFill:      time.Now(),
		cooldown:      cooldown,
		ratePerMinute: float64(ratePerMinute),
		maxKeys:       blockLogMaxKeys,
	}
}

// BlockLog 是全局唯一的拦截日志限流器。
// 全局预算是共享的，因此各个拦截点必须共用这一个实例。
var BlockLog = NewBlockLogLimiter(blockLogCooldown, blockLogRatePerMinute)

// Log 记录一条拦截日志（Warn 级别），返回是否真正写入了日志。
//
// key 一般是来源 IP（建议带上场景前缀，避免不同拦截点互相抢占冷却额度）；
// msg 是日志正文。被限流省略时不会静默丢信息：
// 省略条数会累计，并在该 key 的下一条日志里补上。
func (l *BlockLogLimiter) Log(key, msg string) bool {
	return l.log("warn", key, msg)
}

// LogError 与 Log 相同，但以 Error 级别记录。
func (l *BlockLogLimiter) LogError(key, msg string) bool {
	return l.log("error", key, msg)
}

// log 是两种级别的公共实现。
func (l *BlockLogLimiter) log(level, key, msg string) bool {
	now := time.Now()

	l.mu.Lock()
	// 补充令牌
	if elapsed := now.Sub(l.lastFill).Minutes(); elapsed > 0 {
		l.tokens += elapsed * l.ratePerMinute
		if l.tokens > l.ratePerMinute {
			l.tokens = l.ratePerMinute
		}
		l.lastFill = now
	}
	// 同一来源的冷却期：直接返回，不消耗全局额度。
	// 这样单个 IP 的反复连接不会把全局预算吃光。
	if last, ok := l.last[key]; ok && now.Sub(last) < l.cooldown {
		l.skipped[key]++
		l.mu.Unlock()
		return false
	}
	// 全局限速
	if l.tokens < 1 {
		l.globalSkipped++
		l.mu.Unlock()
		return false
	}
	l.tokens--
	skipped := l.skipped[key]
	delete(l.skipped, key)
	globalSkipped := l.globalSkipped
	l.globalSkipped = 0
	l.last[key] = now
	if len(l.last) > l.maxKeys {
		l.evictLocked(now)
	}
	l.mu.Unlock()

	if skipped > 0 {
		msg = fmt.Sprintf("%s（另有 %d 次同类拦截未记录）", msg, skipped)
	}
	if globalSkipped > 0 {
		msg = fmt.Sprintf("%s（另有 %d 条拦截日志因全局限流未记录）", msg, globalSkipped)
	}
	switch level {
	case "error":
		logs.Error("%s", msg)
	default:
		logs.Warn("%s", msg)
	}
	return true
}

// evictLocked 清理过期的来源记录，避免长时间运行后 map 无限增长。
func (l *BlockLogLimiter) evictLocked(now time.Time) {
	for key, t := range l.last {
		if now.Sub(t) >= l.cooldown {
			delete(l.last, key)
			delete(l.skipped, key)
		}
	}
	if len(l.last) <= l.maxKeys {
		return
	}
	// 仍然超限就丢掉最老的一半
	type entry struct {
		key string
		at  time.Time
	}
	items := make([]entry, 0, len(l.last))
	for key, at := range l.last {
		items = append(items, entry{key: key, at: at})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })
	drop := len(items) - l.maxKeys/2
	for i := 0; i < drop && i < len(items); i++ {
		delete(l.last, items[i].key)
		delete(l.skipped, items[i].key)
	}
}

// BlockLogStats 返回当前限流器的统计信息，便于诊断。
func (l *BlockLogLimiter) BlockLogStats() (tracked int, pending int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, count := range l.skipped {
		pending += count
	}
	return len(l.last), pending
}
