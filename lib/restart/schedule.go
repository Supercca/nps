package restart

import (
	"strings"
	"time"
)

// TimeFormat 是定时重启时刻的配置格式（24 小时制 HH:MM）。
const TimeFormat = "15:04"

// Schedule 描述「每天固定时刻 + 可选星期」的定时重启计划。
//
// Weekdays 为空表示每天都重启；否则只在列出的星期执行（0=周日 … 6=周六）。
type Schedule struct {
	// Enabled 为 true 时定时重启生效。
	Enabled bool
	// Time 是每天执行的时刻，格式 HH:MM。
	Time string
	// Weekdays 是生效的星期，取值 0-6（周日=0，周六=6）。
	Weekdays []int
}

// ParseSchedule 解析全局参数中的定时重启配置。
// 时刻格式非法或星期越界时返回 false，调用方应视为未启用。
func ParseSchedule(enabled bool, timeStr string, weekdays []int) (Schedule, bool) {
	s := Schedule{Enabled: enabled}
	if !enabled {
		return s, false
	}
	s.Time = strings.TrimSpace(timeStr)
	if _, err := time.Parse(TimeFormat, s.Time); err != nil {
		return Schedule{}, false
	}
	for _, w := range weekdays {
		if w < 0 || w > 6 {
			continue
		}
		s.Weekdays = append(s.Weekdays, w)
	}
	return s, true
}

// matchesWeekday 判断某个星期是否在计划的生效范围内。
func (s Schedule) matchesWeekday(w time.Weekday) bool {
	if len(s.Weekdays) == 0 {
		return true
	}
	for _, d := range s.Weekdays {
		if d == int(w) {
			return true
		}
	}
	return false
}

// NextRun 计算 now 之后最近一次应该执行重启的时刻。
//
// 逐日向后查找（最多 7 天），跳过未选中的星期。
// 返回值一定严格晚于 now，避免同一分钟内重复触发。
func (s Schedule) NextRun(now time.Time) (time.Time, bool) {
	if !s.Enabled {
		return time.Time{}, false
	}
	hour, minute, err := parseClock(s.Time)
	if err != nil {
		return time.Time{}, false
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	for i := 0; i < 8; i++ {
		if !day.After(now) {
			day = day.AddDate(0, 0, 1)
			continue
		}
		if s.matchesWeekday(day.Weekday()) {
			return day, true
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// TargetBetween 找出落在 (from, to] 区间内的第一个计划时刻。
//
// 调度器用它判断「是否到点了」：用区间匹配而不是精确相等，
// 可以容忍轮询的抖动（几秒到几十秒的偏差），同时保证同一时刻只触发一次。
// 区间内没有计划时刻时返回 false。
func (s Schedule) TargetBetween(from, to time.Time) (time.Time, bool) {
	if !s.Enabled {
		return time.Time{}, false
	}
	hour, minute, err := parseClock(s.Time)
	if err != nil {
		return time.Time{}, false
	}
	// 从 from 的当天开始逐日查找，最多向后找 8 天。
	day := time.Date(from.Year(), from.Month(), from.Day(), hour, minute, 0, 0, from.Location())
	for i := 0; i < 8; i++ {
		if s.matchesWeekday(day.Weekday()) && day.After(from) && !day.After(to) {
			return day, true
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// NotifyBefore 是重启前推送提醒的提前量。
const NotifyBefore = 5 * time.Minute

// NotifyTime 返回本次重启的提醒时刻（重启前 5 分钟）。
func (s Schedule) NotifyTime(runAt time.Time) time.Time {
	return runAt.Add(-NotifyBefore)
}

// parseClock 解析 HH:MM，返回小时与分钟。
func parseClock(v string) (int, int, error) {
	t, err := time.Parse(TimeFormat, strings.TrimSpace(v))
	if err != nil {
		return 0, 0, err
	}
	return t.Hour(), t.Minute(), nil
}
