package geo

import (
	"net"
	"testing"
)

func TestRangeCountLoaded(t *testing.T) {
	if RangeCount() == 0 {
		t.Fatal("内置中国 IP 段库解析结果为空")
	}
	// 参考值：当前内置库约 6200 条网段。
	if n := RangeCount(); n < 5000 {
		t.Fatalf("网段数量异常偏少: %d", n)
	}
}

func TestParseCIDRList(t *testing.T) {
	ranges := parseCIDRList("1.0.1.0/24\n10.0.0.0/8\n\n# comment\nbad-line\n1.2.3.4/33\n")
	if len(ranges) != 2 {
		t.Fatalf("期望 2 条有效网段, 实际 %d", len(ranges))
	}
	// 校验归一化结果。
	for _, r := range ranges {
		if r.start > r.end {
			t.Fatalf("网段 start>end: %+v", r)
		}
	}
}

func TestParseCIDRListMasking(t *testing.T) {
	// 1.2.3.4/24 归一化后应为 1.2.3.0 - 1.2.3.255
	ranges := parseCIDRList("1.2.3.4/24")
	if len(ranges) != 1 {
		t.Fatalf("期望 1 条网段, 实际 %d", len(ranges))
	}
	wantStart := ipv4ToUint32(net.IPv4(1, 2, 3, 0))
	wantEnd := ipv4ToUint32(net.IPv4(1, 2, 3, 255))
	if ranges[0].start != wantStart || ranges[0].end != wantEnd {
		t.Fatalf("掩码归一化错误: got %+v", ranges[0])
	}
}

func TestIsChinaIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		// 中国大陆常见公网地址
		{"1.0.1.0", true},
		{"114.114.114.114", true},
		{"223.5.5.5", true},
		{"119.29.29.29", true},
		{"180.101.49.11", true},
		// 境外公网地址
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"13.107.42.14", false},
		{"52.94.236.248", false},
		{"151.101.65.69", false},
		// 私有 / 保留地址：一律放行
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"100.64.0.1", true},
		{"224.0.0.1", true},
		{"255.255.255.255", true},
	}
	for _, c := range cases {
		if got := IsChinaIP(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("IsChinaIP(%s) = %v, 期望 %v", c.ip, got, c.want)
		}
	}
}

func TestIsForeignIP(t *testing.T) {
	if !IsForeignIPStr("8.8.8.8") {
		t.Error("8.8.8.8 应判定为境外")
	}
	if IsForeignIPStr("114.114.114.114") {
		t.Error("114.114.114.114 应判定为境内")
	}
	if IsForeignIPStr("192.168.1.1") {
		t.Error("内网地址不应判定为境外")
	}
	if IsForeignIPStr("not-an-ip") {
		t.Error("非法 IP 字符串不应判定为境外")
	}
}

func TestIPv6AlwaysAllowed(t *testing.T) {
	// 内置库不含 IPv6，IPv6 一律放行。
	for _, s := range []string{"2001:4860:4860::8888", "::1", "fe80::1"} {
		if IsForeignIPStr(s) {
			t.Errorf("IPv6 %s 不应判定为境外", s)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.20.0.1", "::1"} {
		if !IsPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s 应判定为私有/保留地址", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "114.114.114.114"} {
		if IsPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s 不应判定为私有地址", s)
		}
	}
}

// TestSortedRanges 保证二分查找的前提：网段按 start 升序。
func TestSortedRanges(t *testing.T) {
	for i := 1; i < len(chinaRanges); i++ {
		if chinaRanges[i].start < chinaRanges[i-1].start {
			t.Fatalf("网段未按 start 升序排列, 索引 %d", i)
		}
	}
}

// TestNoOverlappingRanges 保证网段互不重叠，避免重复覆盖导致匹配歧义。
func TestNoOverlappingRanges(t *testing.T) {
	for i := 1; i < len(chinaRanges); i++ {
		if chinaRanges[i].start <= chinaRanges[i-1].end {
			t.Fatalf("网段存在重叠: [%d]=%+v [%d]=%+v", i-1, chinaRanges[i-1], i, chinaRanges[i])
		}
	}
}
