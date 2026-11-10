package proxy

import (
	"testing"

	"ehang.io/nps/lib/file"
)

// setRegionLimit 设置「访问地区限制」的模式与允许访问的省份。
func setRegionLimit(t *testing.T, mode int, provinces []string) {
	t.Helper()
	global := file.GetDb().GetGlobal()
	if global == nil {
		global = new(file.Glob)
	}
	m := mode
	global.RegionLimitMode = &m
	global.BlockForeignIp = &[]bool{mode == file.RegionLimitModeBlockForeign}[0]
	global.AllowedProvinces = provinces
	if err := file.GetDb().SaveGlobal(global); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		g := file.GetDb().GetGlobal()
		if g != nil {
			zero := file.RegionLimitModeNone
			g.RegionLimitMode = &zero
			off := false
			g.BlockForeignIp = &off
			g.AllowedProvinces = nil
			_ = file.GetDb().SaveGlobal(g)
		}
	})
}

// TestRegionLimitDisabled 未开启地区限制时，任何来源都放行。
func TestRegionLimitDisabled(t *testing.T) {
	setupForeignIpTestConf(t)
	setRegionLimit(t, file.RegionLimitModeNone, nil)

	if IsRegionRestricted("8.8.8.8:12345") {
		t.Error("未开启地区限制时不应拦截境外 IP")
	}
}

// TestRegionLimitProvinces 省份模式下，只有允许省份的 IP 才能通过。
func TestRegionLimitProvinces(t *testing.T) {
	setupForeignIpTestConf(t)
	setRegionLimit(t, file.RegionLimitModeAllowProvinces, []string{"广东", "浙江"})

	cases := []struct {
		addr string
		want bool
	}{
		{"202.96.128.86:80", false},           // 广东，允许
		{"223.5.5.5:53", false},               // 浙江，允许
		{"114.114.114.114:80", true},          // 江苏，未允许
		{"8.8.8.8:12345", true},               // 境外
		{"127.0.0.1:8080", false},             // 回环，放行
		{"192.168.1.10:8080", false},          // 内网，放行
		{"[2001:4860:4860::8888]:443", false}, // IPv6 不在库覆盖范围，放行
		{"", false},
	}
	for _, c := range cases {
		if got := IsRegionRestricted(c.addr); got != c.want {
			t.Errorf("IsRegionRestricted(%q) = %v, 期望 %v", c.addr, got, c.want)
		}
	}
}

// TestRegionLimitProvincesEmpty 未勾选任何省份时，公网访问一律拦截。
func TestRegionLimitProvincesEmpty(t *testing.T) {
	setupForeignIpTestConf(t)
	setRegionLimit(t, file.RegionLimitModeAllowProvinces, nil)

	if !IsRegionRestricted("202.96.128.86:80") {
		t.Error("未勾选省份时公网访问应被拦截")
	}
	if IsRegionRestricted("127.0.0.1:8080") {
		t.Error("未勾选省份时内网访问仍应放行")
	}
}
