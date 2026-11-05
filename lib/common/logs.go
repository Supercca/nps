package common

import (
	"strings"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
)

// MaxMsgLen 是内存日志缓冲的默认容量（条）。
const MaxMsgLen = 2000

// logLevelNames 对应 beego 的日志级别常量。
var logLevelNames = map[int]string{
	logs.LevelEmergency:     "emergency",
	logs.LevelAlert:         "alert",
	logs.LevelCritical:      "critical",
	logs.LevelError:         "error",
	logs.LevelWarning:       "warning",
	logs.LevelNotice:        "notice",
	logs.LevelInformational: "info",
	logs.LevelDebug:         "debug",
}

// LogEntry 是一条日志记录。
// Seq 是全局单调递增的序号，前端用它做增量拉取；
// 环形缓冲滚动时行号会变，但序号不会回退。
type LogEntry struct {
	Seq   uint64 `json:"seq"`
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// LogStore 是线程安全的环形日志缓冲。
// beego 的 logs.SetLogger 是「追加」语义，每条日志会写入所有已注册的 logger，
// 因此把它注册成一个额外的输出端即可，无需改动既有的 console / file 输出。
type LogStore struct {
	mu      sync.RWMutex
	entries []LogEntry
	cap     int
	seq     uint64
}

var logStore = &LogStore{cap: MaxMsgLen}

// LogStoreInstance 返回全局日志缓冲。
func LogStoreInstance() *LogStore {
	return logStore
}

// Append 追加一条日志，超过容量时淘汰最旧的一条。
func (s *LogStore) Append(when time.Time, msg string, level int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	levelName, ok := logLevelNames[level]
	if !ok {
		levelName = "info"
	}
	entry := LogEntry{
		Seq:   s.seq,
		Time:  when.Format("2006-01-02 15:04:05"),
		Level: levelName,
		Msg:   strings.TrimRight(msg, "\r\n"),
	}
	if s.cap <= 0 {
		s.cap = MaxMsgLen
	}
	if len(s.entries) >= s.cap {
		// 环形淘汰最旧的一条。
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:len(s.entries)-1]
	}
	s.entries = append(s.entries, entry)
}

// Since 返回 Seq 大于 offset 的日志条目，最多 limit 条。
// offset 通常是前端已经收到的最后一条的 Seq。
func (s *LogStore) Since(offset uint64, limit int) (entries []LogEntry, latest uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	latest = s.seq
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	for _, entry := range s.entries {
		if entry.Seq <= offset {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= limit {
			break
		}
	}
	return entries, latest
}

// Len 返回当前缓冲中的日志条数。
func (s *LogStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Clear 清空内存缓冲。序号不重置，避免前端误判为「有增量」。
func (s *LogStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = s.entries[:0]
}

func init() {
	logs.Register("store", func() logs.Logger {
		return new(StoreMsg)
	})
}

// GetLogMsg 返回内存中最近的日志文本（兼容旧接口）。
func GetLogMsg() string {
	s := logStore
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sb strings.Builder
	for _, entry := range s.entries {
		sb.WriteString(entry.Time)
		sb.WriteString(" ")
		sb.WriteString(entry.Msg)
		sb.WriteString("\r\n")
	}
	return sb.String()
}

// StoreMsg 是 beego 日志适配器，把日志写入内存环形缓冲。
type StoreMsg struct {
}

func (lg *StoreMsg) Init(config string) error {
	return nil
}

func (lg *StoreMsg) WriteMsg(when time.Time, msg string, level int) error {
	logStore.Append(when, msg, level)
	return nil
}

func (lg *StoreMsg) Destroy() {
	return
}

func (lg *StoreMsg) Flush() {
	return
}
