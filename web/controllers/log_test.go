package controllers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearLogFileTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nps.log")
	if err := os.WriteFile(path, []byte("hello log content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := clearLogFile(path); err != nil {
		t.Fatalf("clearLogFile should not fail: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("log file should still exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("log file should be truncated, size=%d", info.Size())
	}

	// 截断后仍可继续写入。
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("new content"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Fatalf("unexpected content after truncate: %q", string(data))
	}
}

func TestClearLogFileMissingFile(t *testing.T) {
	dir := t.TempDir()
	// 文件不存在时不应报错，清理流程要能继续处理后面的文件。
	if err := clearLogFile(filepath.Join(dir, "nope.log")); err != nil {
		t.Fatalf("missing file should be ignored, got %v", err)
	}
}

func TestLogMenuOptionExists(t *testing.T) {
	if _, ok := MenuKeys["log"]; !ok {
		t.Fatal("log menu key should be registered")
	}
	if ProtectedMenus["log"] {
		t.Fatal("log menu should be hideable")
	}
	if !ProtectedMenus["global"] {
		t.Fatal("global menu must stay protected")
	}
}
