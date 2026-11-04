package geo

import "testing"

// TestPublicIpClassification 用真实公网 IP 校验判定结果（境外日本 IP）。
func TestPublicIpClassification(t *testing.T) {
	// 13.231.141.236 是本机出口公网 IP（日本 AWS 段），应判定为境外。
	if !IsForeignIPStr("13.231.141.236") {
		t.Error("13.231.141.236 应判定为境外")
	}
	// 对照：若干境内 IP 应判定为境内。
	for _, s := range []string{"114.114.114.114", "223.5.5.5", "119.29.29.29", "180.101.49.11"} {
		if IsForeignIPStr(s) {
			t.Errorf("%s 应判定为境内", s)
		}
	}
}
