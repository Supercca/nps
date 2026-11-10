package proxy

import (
	"testing"
	"time"
)

// TestBlockLogLimiterCooldown 同一个来源在冷却期内只应记录一条。
func TestBlockLogLimiterCooldown(t *testing.T) {
	l := NewBlockLogLimiter(time.Minute, 100)

	if !l.Log("province:1.2.3.4", "第一次") {
		t.Fatal("首次记录应写入日志")
	}
	for i := 0; i < 100; i++ {
		if l.Log("province:1.2.3.4", "重复") {
			t.Fatalf("冷却期内的第 %d 次重复不应写入日志", i+1)
		}
	}
	// 不同来源不受影响
	if !l.Log("province:5.6.7.8", "另一个来源") {
		t.Error("不同来源不应被冷却影响")
	}
}

// TestBlockLogLimiterGlobalRate 全局额度用完后应停止记录，
// 保证被扫描时日志速率有上限。
func TestBlockLogLimiterGlobalRate(t *testing.T) {
	l := NewBlockLogLimiter(time.Minute, 2)

	if !l.Log("province:1.1.1.1", "一") {
		t.Fatal("第 1 条应写入")
	}
	if !l.Log("province:2.2.2.2", "二") {
		t.Fatal("第 2 条应写入")
	}
	if l.Log("province:3.3.3.3", "三") {
		t.Error("超出全局额度后不应再写入日志")
	}
}

// TestBlockLogLimiterSkipCount 被省略的条数要累计起来，不能静默丢失。
func TestBlockLogLimiterSkipCount(t *testing.T) {
	l := NewBlockLogLimiter(20*time.Millisecond, 100)

	l.Log("province:9.9.9.9", "第一次")
	l.Log("province:9.9.9.9", "第二次")
	l.Log("province:9.9.9.9", "第三次")

	_, pending := l.BlockLogStats()
	if pending != 2 {
		t.Errorf("待补记的条数 = %d, 期望 2", pending)
	}

	// 冷却期过后再记一条，之前的省略条数应被结算。
	time.Sleep(40 * time.Millisecond)
	l.Log("province:9.9.9.9", "第四次")
	_, pending = l.BlockLogStats()
	if pending != 0 {
		t.Errorf("结算后待补记的条数 = %d, 期望 0", pending)
	}
}

// TestBlockLogLimiterEvict 长时间运行后记录的来源数量不能无限增长。
func TestBlockLogLimiterEvict(t *testing.T) {
	l := NewBlockLogLimiter(time.Millisecond, 10000)
	l.maxKeys = 16

	for i := 0; i < 200; i++ {
		l.Log("province:"+string(rune('a'+i%26))+string(rune('a'+i/26)), "记录")
	}
	tracked, _ := l.BlockLogStats()
	if tracked > l.maxKeys {
		t.Errorf("记录的来源数 = %d, 期望不超过 %d", tracked, l.maxKeys)
	}
}
