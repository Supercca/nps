package file

import (
	"testing"
	"time"
)

// resetAutoBlackIpWindow 复位自动拉黑的速率窗口状态。
func resetAutoBlackIpWindow() {
	autoBlackIpWindowLock.Lock()
	autoBlackIpWindowStart = time.Time{}
	autoBlackIpWindowCount = 0
	autoBlackIpWindowLock.Unlock()
}

// TestAllowAutoBlackIpRateLimit 校验自动拉黑的速率上限生效：
// 超过每分钟上限后不再新增，避免被扫描时把磁盘写满。
func TestAllowAutoBlackIpRateLimit(t *testing.T) {
	resetAutoBlackIpWindow()
	t.Cleanup(resetAutoBlackIpWindow)

	for i := 0; i < MaxAutoBlackIpPerMinute; i++ {
		if !allowAutoBlackIp() {
			t.Fatalf("第 %d 次应在速率上限内", i+1)
		}
	}
	if allowAutoBlackIp() {
		t.Error("超出速率上限后应拒绝新增")
	}
}
