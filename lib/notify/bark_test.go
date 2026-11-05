package notify

import (
	"testing"
	"time"
)

func TestInQuietPeriod(t *testing.T) {
	day := func(h, m int) time.Time {
		return time.Date(2026, 11, 5, h, m, 0, 0, time.Local)
	}

	cases := []struct {
		name  string
		now   time.Time
		start string
		end   string
		want  bool
	}{
		{"未配置时段", day(23, 30), "", "", false},
		{"只配置开始", day(23, 30), "23:00", "", false},
		{"只配置结束", day(23, 30), "", "07:00", false},
		{"格式非法", day(23, 30), "abc", "def", false},
		{"普通时段内", day(23, 30), "23:00", "07:00", true},
		{"普通时段边界起点", day(23, 0), "23:00", "07:00", true},
		{"普通时段边界终点", day(7, 0), "23:00", "07:00", false},
		{"跨零点凌晨", day(3, 15), "23:00", "07:00", true},
		{"跨零点晚上", day(23, 30), "23:00", "07:00", true},
		{"跨零点白天", day(12, 0), "23:00", "07:00", false},
		{"不跨零点时段内", day(13, 0), "12:00", "14:00", true},
		{"不跨零点时段外", day(15, 0), "12:00", "14:00", false},
		{"起止相同视为空区间", day(13, 0), "13:00", "13:00", false},
	}

	for _, c := range cases {
		if got := inQuietPeriod(c.now, c.start, c.end); got != c.want {
			t.Errorf("%s: inQuietPeriod(%s, %q, %q) = %v, 期望 %v",
				c.name, c.now.Format("15:04"), c.start, c.end, got, c.want)
		}
	}
}
