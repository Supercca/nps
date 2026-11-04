package file

import (
	"sync"
	"testing"
)

// TestFlowReset 验证 Reset 会把入口/出口流量清零，且不影响流量上限。
func TestFlowReset(t *testing.T) {
	f := &Flow{FlowLimit: 100}
	f.Add(30, 40)
	if f.InletFlow != 30 || f.ExportFlow != 40 {
		t.Fatalf("Add 后流量不正确: inlet=%d export=%d", f.InletFlow, f.ExportFlow)
	}

	f.Reset()
	if f.InletFlow != 0 {
		t.Errorf("Reset 后 InletFlow 应为 0, 实际 %d", f.InletFlow)
	}
	if f.ExportFlow != 0 {
		t.Errorf("Reset 后 ExportFlow 应为 0, 实际 %d", f.ExportFlow)
	}
	if f.FlowLimit != 100 {
		t.Errorf("Reset 不应修改 FlowLimit, 实际 %d", f.FlowLimit)
	}
}

// TestFlowResetConcurrent 与 Add 并发时不应触发数据竞争（配合 -race 运行）。
func TestFlowResetConcurrent(t *testing.T) {
	f := &Flow{FlowLimit: 1000}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				f.Add(1, 1)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			f.Reset()
		}
	}()
	wg.Wait()
}
