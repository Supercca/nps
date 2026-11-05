package common

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	logFilePathLock sync.RWMutex
	logFilePath     string
)

// SetLogFilePath 记录当前进程实际使用的日志文件路径。
// 由 nps 启动时根据 -log_path 参数或 log_path 配置确定，
// 供 Web 端的日志清理功能定位文件。
func SetLogFilePath(path string) {
	logFilePathLock.Lock()
	defer logFilePathLock.Unlock()
	logFilePath = path
}

// GetLogFilePath 返回当前进程实际使用的日志文件路径。
// 没有启用文件日志时返回空串。
func GetLogFilePath() string {
	logFilePathLock.RLock()
	defer logFilePathLock.RUnlock()
	return logFilePath
}

// LogFileFamily 返回日志文件家族中的全部文件路径：
// 正在写入的日志文件，以及 beego 轮转出来的历史文件。
//
// beego 的轮转命名规则见 logs/file.go：轮转文件名为
// fileNameOnly + "." + 日期格式 + "." + %03d + 后缀，
// 例如 nps.log 会轮转成 nps..001.log、nps.2026-11-05.001.log 等。
// 这里不硬编码具体格式，而是按「同目录 + 同 basename 前缀 + 同后缀」匹配，
// 以覆盖所有轮转变体，同时不会误伤目录下的其他文件。
func LogFileFamily(currentPath string) []string {
	if currentPath == "" {
		return nil
	}
	abs, err := filepath.Abs(currentPath)
	if err != nil {
		abs = currentPath
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	ext := filepath.Ext(base)
	nameOnly := base
	if ext != "" {
		nameOnly = base[:len(base)-len(ext)]
	}

	files := []string{abs}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == base {
			continue
		}
		// 形如 nps.001.log / nps..001.log / nps.2026-11-05.001.log
		if !strings.HasPrefix(name, nameOnly+".") {
			continue
		}
		if ext != "" && !strings.HasSuffix(name, ext) {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	return files
}
