package file

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/version"
)

const (
	// BackupFormatVersion 是备份包的结构版本。
	//
	// 备份包里除了业务数据，还带一份 manifest.json 记录这个版本号。
	// 只有在备份包结构发生不兼容变更（例如字段含义调整、文件拆分方式变化）时才递增，
	// 恢复时版本不一致直接拒绝，避免把旧结构的包覆盖到新版本的数据上。
	BackupFormatVersion = 1

	// backupManifestName 是备份包内清单文件的名称。
	backupManifestName = "manifest.json"

	// backupKeepCount 是本地自动备份保留的份数。
	//
	// 恢复前会自动把当前数据打包存到 conf/backup/ 目录下，
	// 超过这个数量时删除最旧的，避免目录无限增长。
	backupKeepCount = 10
)

// backupDataFiles 是参与备份的数据文件，顺序固定，便于人工比对。
var backupDataFiles = []string{"clients.json", "tasks.json", "hosts.json", "global.json"}

// BackupManifest 描述一个备份包的元信息。
type BackupManifest struct {
	// FormatVersion 是备份包的结构版本，见 BackupFormatVersion。
	FormatVersion int `json:"formatVersion"`
	// NPSVersion 是生成该备份时的 nps 版本号。
	NPSVersion string `json:"npsVersion"`
	// CreatedAt 是备份生成时间。
	CreatedAt string `json:"createdAt"`
	// Files 是备份包内包含的数据文件。
	Files []string `json:"files"`
}

// BackupResult 描述一次恢复操作的结果。
type BackupResult struct {
	// Manifest 是备份包的清单信息。
	Manifest BackupManifest
	// BackupPath 是覆盖前自动备份当前数据生成的文件路径。
	// 用户可以用它回滚这次恢复。
	BackupPath string
	// ClientCount / TaskCount / HostCount 是恢复后的记录数。
	ClientCount int
	TaskCount   int
	HostCount   int
}

// backupFileNames 返回当前实例参与备份的数据文件路径。
func (s *JsonDb) backupFilePaths() map[string]string {
	return map[string]string{
		"clients.json": s.ClientFilePath,
		"tasks.json":   s.TaskFilePath,
		"hosts.json":   s.HostFilePath,
		"global.json":  s.GlobalFilePath,
	}
}

// backupDir 返回本地自动备份目录（runPath/conf/backup）。
func (s *JsonDb) backupDir() string {
	return filepath.Join(filepath.Dir(s.ClientFilePath), "backup")
}

// CreateBackup 把当前数据打包成 zip 字节流。
//
// 先调用四个 Store*ToJsonFile 落盘，保证备份内容与 conf 目录下的文件一致
// （内存里可能有尚未落盘的流量统计），再把这四个文件连同 manifest.json 一起打包。
func (s *JsonDb) CreateBackup() ([]byte, error) {
	s.StoreClientsToJsonFile()
	s.StoreTasksToJsonFile()
	s.StoreHostToJsonFile()
	s.StoreGlobalToJsonFile()

	paths := s.backupFilePaths()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	for _, name := range backupDataFiles {
		content, err := os.ReadFile(paths[name])
		if err != nil {
			return nil, fmt.Errorf("read %s: %v", name, err)
		}
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(content); err != nil {
			return nil, err
		}
	}

	manifest := BackupManifest{
		FormatVersion: BackupFormatVersion,
		NPSVersion:    version.VERSION,
		CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
		Files:         append([]string(nil), backupDataFiles...),
	}
	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	w, err := zw.Create(backupManifestName)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(mb); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// maxBackupEntrySize 是单个备份包条目的解压上限，防止压缩炸弹。
const maxBackupEntrySize = 64 << 20

// RestoreBackup 从 zip 字节流恢复数据。
//
// 步骤：校验（版本 + 结构）-> 自动备份当前数据 -> 覆盖数据文件 -> 重新加载内存。
// 任何一步失败都不会破坏当前数据：覆盖前会先在 conf/backup/ 留一份可回滚的备份。
func (s *JsonDb) RestoreBackup(data []byte) (*BackupResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("backup file is empty")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip file: %v", err)
	}

	entries := make(map[string][]byte, len(backupDataFiles)+1)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// 只读取约定的文件名，避免包内出现同名目录穿越条目。
		if f.Name != backupManifestName && !isBackupDataFile(f.Name) {
			continue
		}
		if f.UncompressedSize64 > maxBackupEntrySize {
			return nil, fmt.Errorf("%s is too large", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxBackupEntrySize+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %v", f.Name, err)
		}
		if len(content) > maxBackupEntrySize {
			return nil, fmt.Errorf("%s is too large", f.Name)
		}
		entries[f.Name] = content
	}

	manifest, err := parseBackupManifest(entries[backupManifestName])
	if err != nil {
		return nil, err
	}
	if manifest.FormatVersion != BackupFormatVersion {
		return nil, fmt.Errorf("backup format version %d is not supported (current %d)",
			manifest.FormatVersion, BackupFormatVersion)
	}
	if err := checkBackupVersion(manifest.NPSVersion); err != nil {
		return nil, err
	}

	// 结构校验：四个数据文件必须齐全，且内容必须能解析成对应的数据结构。
	for _, name := range backupDataFiles {
		if _, ok := entries[name]; !ok {
			return nil, fmt.Errorf("%s is missing in backup", name)
		}
	}
	clientCount, taskCount, hostCount, err := validateBackupContent(entries)
	if err != nil {
		return nil, err
	}

	// 覆盖前先把当前数据打包留底，出问题可以手动换回去。
	backupPath, err := s.saveLocalBackup()
	if err != nil {
		return nil, fmt.Errorf("backup current data failed: %v", err)
	}

	paths := s.backupFilePaths()
	for _, name := range backupDataFiles {
		if err := writeFileAtomic(paths[name], entries[name]); err != nil {
			return nil, fmt.Errorf("write %s: %v", name, err)
		}
	}

	// 内存里的客户端/隧道/域名对象被正在运行的连接和代理引用，
	// 覆盖文件不会让它们立刻生效，因此这里重新从磁盘加载一份。
	s.reloadFromJsonFile()

	return &BackupResult{
		Manifest:    manifest,
		BackupPath:  backupPath,
		ClientCount: clientCount,
		TaskCount:   taskCount,
		HostCount:   hostCount,
	}, nil
}

// isBackupDataFile 判断文件名是否是参与备份的数据文件。
func isBackupDataFile(name string) bool {
	for _, f := range backupDataFiles {
		if f == name {
			return true
		}
	}
	return false
}

// parseBackupManifest 解析备份包里的 manifest.json。
func parseBackupManifest(content []byte) (BackupManifest, error) {
	var manifest BackupManifest
	if len(content) == 0 {
		return manifest, fmt.Errorf("%s is missing in backup", backupManifestName)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid %s: %v", backupManifestName, err)
	}
	return manifest, nil
}

// checkBackupVersion 校验备份来源的 nps 版本与当前版本是否兼容。
//
// 主版本或次版本不同即认为结构可能不兼容，直接拒绝；补丁版本差异（第三段）
// 不影响数据结构，允许恢复。
func checkBackupVersion(backupVersion string) error {
	if backupVersion == "" {
		return fmt.Errorf("backup version is empty")
	}
	bmajor, bminor, ok := parseVersionParts(backupVersion)
	if !ok {
		return fmt.Errorf("invalid backup version %q", backupVersion)
	}
	cmajor, cminor, _ := parseVersionParts(version.VERSION)
	if bmajor != cmajor || bminor != cminor {
		return fmt.Errorf("backup was created by nps v%s, current version is v%s",
			backupVersion, version.VERSION)
	}
	return nil
}

// parseVersionParts 解析版本号的前两段（主版本、次版本）。
func parseVersionParts(v string) (major, minor int, ok bool) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, false
	}
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// validateBackupContent 校验备份包内各文件的内容结构。
//
// clients.json / tasks.json / hosts.json 是「每行一条 JSON、用分隔符拼接」的结构，
// 这里逐条反序列化确认格式正确，并检查隧道/域名引用的客户端确实存在
// （否则恢复后这些记录会因为找不到客户端而被丢弃）。
func validateBackupContent(entries map[string][]byte) (clientCount, taskCount, hostCount int, err error) {
	clientIDs := make(map[int]bool)
	for _, record := range splitBackupRecords(entries["clients.json"]) {
		var c Client
		if err := json.Unmarshal(record, &c); err != nil {
			return 0, 0, 0, fmt.Errorf("invalid client record: %v", err)
		}
		if c.Id <= 0 {
			return 0, 0, 0, fmt.Errorf("invalid client id %d", c.Id)
		}
		if c.VerifyKey == "" {
			return 0, 0, 0, fmt.Errorf("client %d has empty verify key", c.Id)
		}
		clientIDs[c.Id] = true
		clientCount++
	}

	for _, record := range splitBackupRecords(entries["tasks.json"]) {
		var t Tunnel
		if err := json.Unmarshal(record, &t); err != nil {
			return 0, 0, 0, fmt.Errorf("invalid tunnel record: %v", err)
		}
		if t.Id <= 0 {
			return 0, 0, 0, fmt.Errorf("invalid tunnel id %d", t.Id)
		}
		if t.Client == nil {
			return 0, 0, 0, fmt.Errorf("tunnel %d has no client", t.Id)
		}
		if !clientIDs[t.Client.Id] {
			return 0, 0, 0, fmt.Errorf("tunnel %d refers to unknown client %d", t.Id, t.Client.Id)
		}
		taskCount++
	}

	for _, record := range splitBackupRecords(entries["hosts.json"]) {
		var h Host
		if err := json.Unmarshal(record, &h); err != nil {
			return 0, 0, 0, fmt.Errorf("invalid host record: %v", err)
		}
		if h.Id <= 0 {
			return 0, 0, 0, fmt.Errorf("invalid host id %d", h.Id)
		}
		if h.Client == nil {
			return 0, 0, 0, fmt.Errorf("host %d has no client", h.Id)
		}
		if !clientIDs[h.Client.Id] {
			return 0, 0, 0, fmt.Errorf("host %d refers to unknown client %d", h.Id, h.Client.Id)
		}
		hostCount++
	}

	var g Glob
	if err := json.Unmarshal(entries["global.json"], &g); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid global.json: %v", err)
	}
	return clientCount, taskCount, hostCount, nil
}

// splitBackupRecords 把「分隔符拼接的 JSON 记录流」拆成单条记录。
func splitBackupRecords(content []byte) [][]byte {
	parts := strings.Split(string(content), "\n"+common.CONN_DATA_SEQ)
	records := make([][]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		records = append(records, []byte(p))
	}
	return records
}

// writeFileAtomic 先写临时文件再改名，避免写到一半被读到。
func writeFileAtomic(path string, content []byte) error {
	tmp := path + ".restore.tmp"
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// saveLocalBackup 把当前数据打包存到 conf/backup/ 目录，返回文件路径。
func (s *JsonDb) saveLocalBackup() (string, error) {
	data, err := s.CreateBackup()
	if err != nil {
		return "", err
	}
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("backup-%s.zip", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	s.pruneLocalBackups(dir)
	return path, nil
}

// pruneLocalBackups 只保留最新的若干份本地备份。
func (s *JsonDb) pruneLocalBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		backups = append(backups, e.Name())
	}
	if len(backups) <= backupKeepCount {
		return
	}
	// 文件名里带时间戳，字典序即时间序。
	sort.Strings(backups)
	for _, name := range backups[:len(backups)-backupKeepCount] {
		os.Remove(filepath.Join(dir, name))
	}
}

// reloadFromJsonFile 用磁盘上的数据文件重新加载内存状态。
//
// 恢复备份后必须调用：内存里的客户端/隧道/域名对象被正在运行的连接和代理引用，
// 只覆盖文件不会让它们生效。重新加载会换掉这些对象，旧的限速器也要停掉，
// 否则会残留 goroutine。
func (s *JsonDb) reloadFromJsonFile() {
	// 停掉旧客户端的限速器，避免 goroutine 泄漏。
	s.Clients.Range(func(key, value interface{}) bool {
		if c, ok := value.(*Client); ok && c != nil && c.Rate != nil {
			c.Rate.Stop()
		}
		return true
	})

	clearSyncMap(&s.Clients)
	clearSyncMap(&s.Tasks)
	clearSyncMap(&s.Hosts)

	// 自增 ID 归零后由重新加载的记录重新推高。
	atomic.StoreInt32(&s.ClientIncreaseId, 0)
	atomic.StoreInt32(&s.TaskIncreaseId, 0)
	atomic.StoreInt32(&s.HostIncreaseId, 0)

	s.LoadClientFromJsonFile()
	s.LoadTaskFromJsonFile()
	s.LoadHostFromJsonFile()
	s.LoadGlobalFromJsonFile()
}

// clearSyncMap 清空一个 sync.Map。
func clearSyncMap(m *sync.Map) {
	m.Range(func(key, value interface{}) bool {
		m.Delete(key)
		return true
	})
}
