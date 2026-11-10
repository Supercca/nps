package geo

import (
	"net"
	"os"
	"testing"
)

// testXDBPath 是仓库内的 IP 库路径。
// 测试运行时的工作目录是包目录（lib/geo），因此要往上两级。
const testXDBPath = "../../conf/ip2region_v4.xdb"

// loadTestProvinceIndex 加载测试用 IP 库。
func loadTestProvinceIndex(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testXDBPath); err != nil {
		t.Skipf("测试用 IP 库不存在: %s", err)
	}
	if err := LoadProvinceIndex(testXDBPath); err != nil {
		t.Fatalf("加载 IP 库失败: %s", err)
	}
}

// TestProvinceIndex 校验 IP 库能被正确解析，并且几个典型 IP 能落到正确省份。
func TestProvinceIndex(t *testing.T) {
	loadTestProvinceIndex(t)

	if ProvinceRangeCount() == 0 {
		t.Fatal("省份区间为空")
	}

	cases := []struct {
		ip   string
		want string
	}{
		{"202.96.128.86", "广东"},
		{"223.5.5.5", "浙江"},
		{"114.114.114.114", "江苏"},
		{"202.106.0.20", "北京"},
		{"183.60.83.19", "广东"},
		{"116.1.1.1", "广西"},
		{"120.1.1.1", "河北"},
	}
	for _, c := range cases {
		got := LookupProvince(net.ParseIP(c.ip))
		if got != c.want {
			t.Errorf("LookupProvince(%s) = %q, 期望 %q", c.ip, got, c.want)
		}
	}

	// 境外地址不应归属任何中国省份。
	if got := LookupProvince(net.ParseIP("8.8.8.8")); got != "" {
		t.Errorf("LookupProvince(8.8.8.8) = %q, 期望空串", got)
	}
	// IPv6 不在库覆盖范围内。
	if got := LookupProvince(net.ParseIP("2400:3200::1")); got != "" {
		t.Errorf("LookupProvince(IPv6) = %q, 期望空串", got)
	}
}

// TestProvinceDBStatus 校验状态信息能正确反映已加载的库。
func TestProvinceDBStatus(t *testing.T) {
	loadTestProvinceIndex(t)

	status := ProvinceDBStatus()
	if !status.Ready {
		t.Fatal("状态应为已加载")
	}
	if status.ProvinceCount != 34 {
		t.Errorf("省份数 = %d, 期望 34", status.ProvinceCount)
	}
	if status.RangeCount == 0 {
		t.Error("IP 区间数不应为 0")
	}
	if status.Path == "" {
		t.Error("状态里应带上库文件路径")
	}
}

// TestParseProvinceIndexInvalid 校验非法文件会被拒绝（上传前的校验）。
func TestParseProvinceIndexInvalid(t *testing.T) {
	if _, err := ParseProvinceIndex([]byte("not an xdb file")); err == nil {
		t.Error("非法内容应解析失败")
	}
	if _, err := ParseProvinceIndex(nil); err == nil {
		t.Error("空内容应解析失败")
	}
}

// TestNormalizeProvinceName 校验各种写法的省份名都能归一化。
func TestNormalizeProvinceName(t *testing.T) {
	cases := map[string]string{
		"广东省":      "广东",
		"广东":       "广东",
		"北京市":      "北京",
		"内蒙古":      "内蒙古",
		"内蒙古自治区":   "内蒙古",
		"广西壮族自治区":  "广西",
		"新疆维吾尔自治区": "新疆",
		"宁夏回族自治区":  "宁夏",
		"西藏自治区":    "西藏",
		"香港特别行政区":  "香港",
		"澳门特别行政区":  "澳门",
		"台湾省":      "台湾",
		"上海市":      "上海",
		"0":        "",
		"Unknown":  "",
		"":         "",
	}
	for in, want := range cases {
		if got := NormalizeProvinceName(in); got != want {
			t.Errorf("NormalizeProvinceName(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestProvincesByGroup 校验 34 个省级行政区都被正确归入 7 个大区。
func TestProvincesByGroup(t *testing.T) {
	groups := ProvincesByGroup()
	if len(groups) != 7 {
		t.Fatalf("大区数量 = %d, 期望 7", len(groups))
	}
	total := 0
	for _, g := range groups {
		if len(g.Provinces) == 0 {
			t.Errorf("大区 %s 下没有省份", g.Name)
		}
		total += len(g.Provinces)
	}
	if total != 34 {
		t.Errorf("省份总数 = %d, 期望 34", total)
	}
}
