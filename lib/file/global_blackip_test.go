package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/astaxie/beego"
)

// readGlobalJson 读取落盘的 global.json，验证黑名单确实写入了文件。
func readGlobalJson(t *testing.T, db *DbUtils) *Glob {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(db.JsonDb.RunPath, "conf", "global.json"))
	if err != nil {
		t.Fatalf("global.json should exist: %v", err)
	}
	global := new(Glob)
	if err := json.Unmarshal(b, global); err != nil {
		t.Fatalf("global.json should be valid json: %v", err)
	}
	return global
}

func TestAddGlobalBlackIp(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}

	if !db.AddGlobalBlackIp("8.8.8.8") {
		t.Fatal("AddGlobalBlackIp should report a new entry")
	}
	list := db.GetGlobalBlackIpList()
	if len(list) != 1 || list[0] != "8.8.8.8" {
		t.Fatalf("unexpected blacklist: %v", list)
	}

	// 重复添加同一个 IP 应当被忽略。
	if db.AddGlobalBlackIp("8.8.8.8") {
		t.Fatal("duplicate ip should not be added again")
	}
	if list := db.GetGlobalBlackIpList(); len(list) != 1 {
		t.Fatalf("duplicate ip must not grow the blacklist: %v", list)
	}

	// 立即落盘。
	saved := readGlobalJson(t, db)
	if len(saved.BlackIpList) != 1 || saved.BlackIpList[0] != "8.8.8.8" {
		t.Fatalf("blacklist should be persisted: %v", saved.BlackIpList)
	}

	if db.AddGlobalBlackIp("") {
		t.Fatal("empty ip should not be added")
	}
}

func TestAddGlobalBlackIpRespectsLimit(t *testing.T) {
	db := newTestDb(t)
	global := new(Glob)
	global.BlackIpList = make([]string, MaxGlobalBlackIpCount)
	for i := range global.BlackIpList {
		global.BlackIpList[i] = "10.0.0.1"
	}
	if err := db.SaveGlobal(global); err != nil {
		t.Fatal(err)
	}

	if db.AddGlobalBlackIp("8.8.8.8") {
		t.Fatal("blacklist must not grow beyond MaxGlobalBlackIpCount")
	}
	if got := len(db.GetGlobalBlackIpList()); got != MaxGlobalBlackIpCount {
		t.Fatalf("unexpected blacklist size: %d", got)
	}
}

func TestGetGlobalBlackIpListReturnsCopy(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}
	db.AddGlobalBlackIp("8.8.8.8")

	list := db.GetGlobalBlackIpList()
	list[0] = "9.9.9.9"

	if got := db.GetGlobalBlackIpList(); got[0] != "8.8.8.8" {
		t.Fatalf("caller must not be able to mutate the internal list: %v", got)
	}
}

func TestSetGlobalBlackIpList(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}

	global := db.GetGlobal()
	db.SetGlobalBlackIpList(global, []string{"1.1.1.1", "2.2.2.2"})

	list := db.GetGlobalBlackIpList()
	if len(list) != 2 || list[0] != "1.1.1.1" || list[1] != "2.2.2.2" {
		t.Fatalf("unexpected blacklist: %v", list)
	}
}

// TestAddGlobalBlackIpConcurrent 覆盖自动拉黑的并发场景：
// 多个连接同时命中同一个境外 IP 时只能入黑名单一次，且不能产生数据竞争。
func TestAddGlobalBlackIpConcurrent(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				db.AddGlobalBlackIp("8.8.8.8")
				db.GetGlobalBlackIpList()
			}
		}()
	}
	wg.Wait()

	if got := len(db.GetGlobalBlackIpList()); got != 1 {
		t.Fatalf("concurrent add should keep one entry, got %d", got)
	}
}

func TestMigrateAutoBlockForeignIp(t *testing.T) {
	db := newTestDb(t)
	_ = beego.AppConfig.Set("auto_block_foreign_ip", "true")

	db.MigrateGlobalFromAppConfig()

	if !db.GetGlobalSettings().AutoBlockForeignIp {
		t.Fatal("auto_block_foreign_ip should be migrated as true")
	}
	if db.GetGlobalSettings().BlockForeignIp {
		t.Fatal("block_foreign_ip should stay disabled by default")
	}
}
