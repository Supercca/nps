package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogFileFamily(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "nps.log")

	names := []string{
		"nps.log",                // 当前正在写入的日志文件
		"nps..001.log",           // beego 按行数轮转出来的文件
		"nps..002.log",           //
		"nps.2026-11-05.001.log", // beego 按日期轮转出来的文件
		"other.log",              // 不属于日志家族
		"nps.txt",                // 后缀不同
		"nps.conf",               // 同前缀但后缀不同
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// 一个同前缀的目录，不应被当成日志文件。
	if err := os.Mkdir(filepath.Join(dir, "nps.dir.log"), 0755); err != nil {
		t.Fatal(err)
	}

	family := LogFileFamily(current)
	got := map[string]bool{}
	for _, path := range family {
		got[filepath.Base(path)] = true
	}

	for _, want := range []string{"nps.log", "nps..001.log", "nps..002.log", "nps.2026-11-05.001.log"} {
		if !got[want] {
			t.Errorf("日志家族应包含 %s，实际: %v", want, family)
		}
	}
	for _, notWant := range []string{"other.log", "nps.txt", "nps.conf", "nps.dir.log"} {
		if got[notWant] {
			t.Errorf("日志家族不应包含 %s，实际: %v", notWant, family)
		}
	}
}

func TestLogFileFamilyEmptyPath(t *testing.T) {
	if family := LogFileFamily(""); family != nil {
		t.Fatalf("空路径应返回 nil，实际: %v", family)
	}
}

func TestLogFilePathStore(t *testing.T) {
	old := GetLogFilePath()
	defer SetLogFilePath(old)

	SetLogFilePath("/var/log/nps.log")
	if got := GetLogFilePath(); got != "/var/log/nps.log" {
		t.Fatalf("unexpected log file path: %s", got)
	}
	SetLogFilePath("")
	if got := GetLogFilePath(); got != "" {
		t.Fatalf("expected empty path, got %s", got)
	}
}
