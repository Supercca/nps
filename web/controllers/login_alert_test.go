package controllers

import (
	"sync"
	"testing"
	"time"

	"ehang.io/nps/lib/file"
)

// TestLoginFailRecordCounting 登录失败计数的累加与窗口过期清零。
func TestLoginFailRecordCounting(t *testing.T) {
	r := &loginFailRecord{}
	now := time.Now()

	for i := 1; i <= 3; i++ {
		times, alert := r.fail(now, 5)
		if times != i {
			t.Fatalf("第 %d 次失败后计数 = %d, 期望 %d", i, times, i)
		}
		if alert {
			t.Fatalf("未到阈值时不应告警（第 %d 次）", i)
		}
	}

	// 超过窗口没有新失败 → 计数清零。
	r.expire(now.Add(loginFailWindow+time.Second), loginFailWindow)
	times, _ := r.fail(now.Add(loginFailWindow+time.Second), 5)
	if times != 1 {
		t.Errorf("窗口过期后计数应重新开始，实际 = %d", times)
	}
}

// TestLoginFailRecordAlertOnce 达到阈值时只告警一次，避免同一个 IP 反复刷推送。
func TestLoginFailRecordAlertOnce(t *testing.T) {
	r := &loginFailRecord{}
	now := time.Now()

	alerts := 0
	for i := 0; i < 20; i++ {
		if _, alert := r.fail(now, 5); alert {
			alerts++
		}
	}
	if alerts != 1 {
		t.Fatalf("告警次数 = %d, 期望 1", alerts)
	}

	// 窗口过期后重新计数，达到阈值要能再次告警。
	later := now.Add(loginFailWindow + time.Second)
	r.expire(later, loginFailWindow)
	alerts = 0
	for i := 0; i < 5; i++ {
		if _, alert := r.fail(later, 5); alert {
			alerts++
		}
	}
	if alerts != 1 {
		t.Fatalf("新一轮窗口的告警次数 = %d, 期望 1", alerts)
	}
}

// TestLoginFailRecordThresholdDisabled 阈值为 0 表示不告警。
func TestLoginFailRecordThresholdDisabled(t *testing.T) {
	r := &loginFailRecord{}
	now := time.Now()
	for i := 0; i < 50; i++ {
		if _, alert := r.fail(now, 0); alert {
			t.Fatal("阈值为 0 时不应告警")
		}
	}
	// 计数照常累加，只是不告警。
	if r.failTimes != 50 {
		t.Errorf("失败计数 = %d, 期望 50", r.failTimes)
	}
}

// TestLoginFailRecordLockout 失败次数达到上限后应判定为锁定。
func TestLoginFailRecordLockout(t *testing.T) {
	r := &loginFailRecord{}
	now := time.Now()
	for i := 0; i < loginFailLockoutTimes; i++ {
		r.fail(now, 0)
	}
	if !r.locked(loginFailLockoutTimes) {
		t.Fatalf("失败 %d 次后应被锁定", loginFailLockoutTimes)
	}
}

// TestLoginFailRecordConcurrent 并发失败计数不能丢：
// 原来的实现用裸字段累加，并发登录会互相覆盖。
func TestLoginFailRecordConcurrent(t *testing.T) {
	r := &loginFailRecord{}
	now := time.Now()

	const goroutines = 16
	const each = 50

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				r.fail(now, 0)
			}
		}()
	}
	wg.Wait()

	if got := r.failTimes; got != goroutines*each {
		t.Errorf("并发失败计数 = %d, 期望 %d", got, goroutines*each)
	}
}

// TestShouldAlertLoginProvince 登录来源省份的告警判定。
func TestShouldAlertLoginProvince(t *testing.T) {
	allowed := map[string]bool{"广东": true, "浙江": true}

	cases := []struct {
		name     string
		mode     int
		allowed  map[string]bool
		province string
		want     bool
	}{
		{"省份不在允许列表", file.RegionLimitModeAllowProvinces, allowed, "江苏", true},
		{"省份在允许列表", file.RegionLimitModeAllowProvinces, allowed, "广东", false},
		{"查不到省份归属", file.RegionLimitModeAllowProvinces, allowed, "", false},
		{"未开启省份模式", file.RegionLimitModeBlockForeign, allowed, "江苏", false},
		{"不限制模式", file.RegionLimitModeNone, allowed, "江苏", false},
		{"允许列表为空", file.RegionLimitModeAllowProvinces, map[string]bool{}, "江苏", true},
	}
	for _, c := range cases {
		got := shouldAlertLoginProvince(c.mode, c.allowed, c.province)
		if got != c.want {
			t.Errorf("%s: shouldAlertLoginProvince() = %v, 期望 %v", c.name, got, c.want)
		}
	}
}

// TestFilterLoginFailThreshold 告警阈值的规整。
func TestFilterLoginFailThreshold(t *testing.T) {
	cases := map[int]int{
		-1:   0,
		0:    0,
		5:    5,
		1000: file.MaxLoginFailAlertThreshold,
	}
	for in, want := range cases {
		if got := filterLoginFailThreshold(in); got != want {
			t.Errorf("filterLoginFailThreshold(%d) = %d, 期望 %d", in, got, want)
		}
	}
}
