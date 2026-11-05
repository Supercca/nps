package common

import (
	"testing"
	"time"

	"github.com/astaxie/beego/logs"
)

func newTestStore(cap int) *LogStore {
	return &LogStore{cap: cap}
}

func TestLogStoreAppendAndSince(t *testing.T) {
	s := newTestStore(100)
	now := time.Now()
	for i := 0; i < 10; i++ {
		s.Append(now, "message", logs.LevelInformational)
	}

	entries, latest := s.Since(0, 1000)
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}
	if latest != 10 {
		t.Fatalf("expected latest seq 10, got %d", latest)
	}
	if entries[0].Seq != 1 || entries[9].Seq != 10 {
		t.Fatalf("unexpected seq range: %d..%d", entries[0].Seq, entries[9].Seq)
	}
	if entries[0].Level != "info" {
		t.Fatalf("unexpected level: %s", entries[0].Level)
	}
	if entries[0].Time == "" {
		t.Fatal("time should be filled")
	}
}

func TestLogStoreSinceWithOffset(t *testing.T) {
	s := newTestStore(100)
	now := time.Now()
	for i := 0; i < 10; i++ {
		s.Append(now, "message", logs.LevelInformational)
	}

	entries, _ := s.Since(7, 1000)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after offset, got %d", len(entries))
	}
	if entries[0].Seq != 8 {
		t.Fatalf("expected first seq 8, got %d", entries[0].Seq)
	}
}

func TestLogStoreSinceRespectsLimit(t *testing.T) {
	s := newTestStore(100)
	now := time.Now()
	for i := 0; i < 50; i++ {
		s.Append(now, "message", logs.LevelInformational)
	}
	entries, _ := s.Since(0, 10)
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}
}

func TestLogStoreEvictsOldest(t *testing.T) {
	s := newTestStore(5)
	now := time.Now()
	for i := 0; i < 12; i++ {
		s.Append(now, "message", logs.LevelInformational)
	}
	if s.Len() != 5 {
		t.Fatalf("expected 5 entries after eviction, got %d", s.Len())
	}
	entries, latest := s.Since(0, 1000)
	// 最旧的 7 条应被淘汰，最新 seq 仍然是 12。
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if entries[0].Seq != 8 {
		t.Fatalf("expected oldest kept seq 8, got %d", entries[0].Seq)
	}
	if latest != 12 {
		t.Fatalf("expected latest seq 12, got %d", latest)
	}
}

func TestLogStoreClearKeepsSeq(t *testing.T) {
	s := newTestStore(100)
	now := time.Now()
	s.Append(now, "message", logs.LevelInformational)
	s.Append(now, "message", logs.LevelInformational)

	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("expected empty store, got %d", s.Len())
	}
	// 清空后序号继续递增，避免前端把「清空」误判成「有新日志」。
	s.Append(now, "message", logs.LevelInformational)
	entries, latest := s.Since(0, 1000)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Seq != 3 {
		t.Fatalf("expected seq to keep increasing, got %d", entries[0].Seq)
	}
	if latest != 3 {
		t.Fatalf("expected latest 3, got %d", latest)
	}
}

func TestLogStoreLevelNames(t *testing.T) {
	s := newTestStore(100)
	now := time.Now()
	s.Append(now, "boom", logs.LevelError)
	s.Append(now, "careful", logs.LevelWarning)
	entries, _ := s.Since(0, 10)
	if entries[0].Level != "error" || entries[1].Level != "warning" {
		t.Fatalf("unexpected levels: %s, %s", entries[0].Level, entries[1].Level)
	}
}

// TestLogStoreConcurrent 覆盖多 goroutine 并发写日志的场景。
// 实际运行时 beego 会从多个连接 goroutine 调用 WriteMsg。
func TestLogStoreConcurrent(t *testing.T) {
	s := newTestStore(500)
	now := time.Now()
	done := make(chan bool)
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				s.Append(now, "message", logs.LevelInformational)
			}
			done <- true
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if s.Len() != 500 {
		t.Fatalf("expected buffer capped at 500, got %d", s.Len())
	}
}
