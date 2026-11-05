package common

import (
	"os"
	"path/filepath"
	"sort"
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

// ReadLogTail 读取日志文件末尾的若干行。
// 文件可能很大，只从末尾向前读取有限字节再按行切分，避免把整个文件读进内存。
func ReadLogTail(path string, maxLines int) ([]string, error) {
	if maxLines <= 0 {
		maxLines = 500
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// 最多回读 512KB，足以覆盖远超 maxLines 的内容。
	const maxRead = 512 << 10
	readSize := int64(maxRead)
	if info.Size() < readSize {
		readSize = info.Size()
	}
	if readSize <= 0 {
		return nil, nil
	}
	buf := make([]byte, readSize)
	n, err := f.ReadAt(buf, info.Size()-readSize)
	if err != nil && n == 0 {
		return nil, err
	}
	buf = buf[:n]

	lines := strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n")
	// 回读起点可能在行中间，丢弃第一行残缺内容。
	if readSize < info.Size() && len(lines) > 0 {
		lines = lines[1:]
	}
	// 去掉末尾的空行（文件通常以换行结尾）。
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
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
	var rotated []string
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
		rotated = append(rotated, filepath.Join(dir, name))
	}
	// 轮转文件按名称倒序：编号越大越新，排在前面。
	sort.Sort(sort.Reverse(sort.StringSlice(rotated)))
	return append(files, rotated...)
}
