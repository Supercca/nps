package file

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// TestIsGlobalBlackIpBasic 集合视图的基本查询行为。
func TestIsGlobalBlackIpBasic(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}

	if db.IsGlobalBlackIp("8.8.8.8") {
		t.Error("空黑名单不应命中任何 IP")
	}
	if db.IsGlobalBlackIp("") {
		t.Error("空 IP 不应命中")
	}

	if !db.AddGlobalBlackIp("8.8.8.8") {
		t.Fatal("AddGlobalBlackIp should report a new entry")
	}
	if !db.IsGlobalBlackIp("8.8.8.8") {
		t.Error("刚加入黑名单的 IP 应能查到")
	}
	if db.IsGlobalBlackIp("1.1.1.1") {
		t.Error("未加入黑名单的 IP 不应命中")
	}
}

// TestIsGlobalBlackIpAfterSetList 整体替换黑名单后，查询结果要立刻跟着变。
func TestIsGlobalBlackIpAfterSetList(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}

	global := db.GetGlobal()
	db.SetGlobalBlackIpList(global, []string{"1.1.1.1", "2.2.2.2"})

	if !db.IsGlobalBlackIp("1.1.1.1") || !db.IsGlobalBlackIp("2.2.2.2") {
		t.Error("替换后的名单应立刻可查")
	}
	if db.IsGlobalBlackIp("3.3.3.3") {
		t.Error("不在新名单里的 IP 不应命中")
	}

	// 再替换一次，旧名单里的 IP 应立即失效。
	db.SetGlobalBlackIpList(db.GetGlobal(), []string{"4.4.4.4"})
	if db.IsGlobalBlackIp("1.1.1.1") {
		t.Error("旧名单里的 IP 应失效")
	}
	if !db.IsGlobalBlackIp("4.4.4.4") {
		t.Error("新名单里的 IP 应命中")
	}
}

// TestIsGlobalBlackIpAfterLoadFromFile 重启场景：从磁盘加载后集合视图要重建，
// 否则重启后黑名单会「看起来是空的」。
func TestIsGlobalBlackIpAfterLoadFromFile(t *testing.T) {
	db := newTestDb(t)
	global := new(Glob)
	global.BlackIpList = []string{"5.5.5.5", "6.6.6.6"}
	if err := db.SaveGlobal(global); err != nil {
		t.Fatal(err)
	}

	// 模拟重启：新建一个 JsonDb 从磁盘加载。
	other := &DbUtils{JsonDb: NewJsonDb(db.JsonDb.RunPath)}
	other.JsonDb.LoadGlobalFromJsonFile()

	if !other.IsGlobalBlackIp("5.5.5.5") || !other.IsGlobalBlackIp("6.6.6.6") {
		t.Error("从磁盘加载后黑名单应可查")
	}
	if other.IsGlobalBlackIp("7.7.7.7") {
		t.Error("未在黑名单里的 IP 不应命中")
	}
}

// TestIsGlobalBlackIpAfterSaveGlobal 用 SaveGlobal 换掉整份配置时，
// 集合视图要跟着重建，不能留着旧名单。
func TestIsGlobalBlackIpAfterSaveGlobal(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}
	db.AddGlobalBlackIp("8.8.8.8")

	global := new(Glob)
	global.BlackIpList = []string{"9.9.9.9"}
	if err := db.SaveGlobal(global); err != nil {
		t.Fatal(err)
	}

	if db.IsGlobalBlackIp("8.8.8.8") {
		t.Error("SaveGlobal 换掉配置后，旧黑名单不应再命中")
	}
	if !db.IsGlobalBlackIp("9.9.9.9") {
		t.Error("SaveGlobal 换掉配置后，新黑名单应命中")
	}
}

// TestGlobalBlackIpSetMatchesList 集合视图与列表必须始终一致：
// 列表里的每一条都要能查到，查不到的不能被误判为在名单里。
func TestGlobalBlackIpSetMatchesList(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		db.AddGlobalBlackIp(fmt.Sprintf("10.0.0.%d", i))
	}

	list := db.GetGlobalBlackIpList()
	for _, ip := range list {
		if !db.IsGlobalBlackIp(ip) {
			t.Fatalf("名单里的 %s 应能查到", ip)
		}
	}
	if db.IsGlobalBlackIp("10.1.1.1") {
		t.Error("名单外的 IP 不应命中")
	}
}

// TestIsGlobalBlackIpConcurrentReadWrite 并发读写：一边查（每个连接都会查），
// 一边加（自动拉黑）与整体替换（Web 保存），不能有数据竞争，
// 最终集合视图与列表必须一致。
//
// 用 -race 跑才能检出竞争。
func TestIsGlobalBlackIpConcurrentReadWrite(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}
	resetAutoBlackIpWindow()
	t.Cleanup(resetAutoBlackIpWindow)

	const (
		readers  = 8
		writers  = 4
		rounds   = 50
		distinct = 40
	)

	var wg sync.WaitGroup

	// 读者：模拟每个连接都要查一次黑名单。
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				db.IsGlobalBlackIp("8.8.8.8")
				db.IsGlobalBlackIp("10.0.0.1")
				db.IsGlobalBlackIp("127.0.0.1")
			}
		}()
	}

	// 写者：自动拉黑。
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < distinct; j++ {
				db.AddGlobalBlackIp(fmt.Sprintf("10.1.%d.%d", id, j))
			}
		}(i)
	}

	// 写者：Web 保存整体替换名单。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < rounds; j++ {
			db.SetGlobalBlackIpList(db.GetGlobal(), []string{"172.16.0.1"})
		}
	}()

	wg.Wait()

	// 并发结束后，集合视图与列表仍然要一致。
	list := db.GetGlobalBlackIpList()
	for _, ip := range list {
		if !db.IsGlobalBlackIp(ip) {
			t.Fatalf("并发结束后名单里的 %s 应能查到", ip)
		}
	}
}

// TestAddGlobalBlackIpConcurrentDistinct 并发加入不同的 IP：
// 每条都应只进一次，列表与集合视图要一致。
func TestAddGlobalBlackIpConcurrentDistinct(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}
	resetAutoBlackIpWindow()
	t.Cleanup(resetAutoBlackIpWindow)

	const total = 40
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ip := fmt.Sprintf("192.168.%d.%d", id/256, id%256)
			if !db.AddGlobalBlackIp(ip) {
				t.Errorf("首次加入 %s 应成功", ip)
			}
			// 重复加入必须被拒绝。
			if db.AddGlobalBlackIp(ip) {
				t.Errorf("重复加入 %s 应被拒绝", ip)
			}
		}(i)
	}
	wg.Wait()

	list := db.GetGlobalBlackIpList()
	if len(list) != total {
		t.Fatalf("名单条数 = %d, 期望 %d", len(list), total)
	}
	seen := make(map[string]bool, len(list))
	for _, ip := range list {
		if seen[ip] {
			t.Fatalf("名单里出现了重复项 %s", ip)
		}
		seen[ip] = true
		if !db.IsGlobalBlackIp(ip) {
			t.Fatalf("名单里的 %s 应能查到", ip)
		}
	}
}

// TestGlobalBlackIpPersistedAcrossRestart 落盘后重启仍能查到（回归测试）。
func TestGlobalBlackIpPersistedAcrossRestart(t *testing.T) {
	db := newTestDb(t)
	if err := db.SaveGlobal(new(Glob)); err != nil {
		t.Fatal(err)
	}
	db.AddGlobalBlackIp("8.8.8.8")

	// 确认文件里确实写进去了。
	b, err := os.ReadFile(filepath.Join(db.JsonDb.RunPath, "conf", "global.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved := new(Glob)
	if err := json.Unmarshal(b, saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.BlackIpList) != 1 || saved.BlackIpList[0] != "8.8.8.8" {
		t.Fatalf("黑名单应已落盘: %v", saved.BlackIpList)
	}

	other := &DbUtils{JsonDb: NewJsonDb(db.JsonDb.RunPath)}
	other.JsonDb.LoadGlobalFromJsonFile()
	if !other.IsGlobalBlackIp("8.8.8.8") {
		t.Error("重启后黑名单应仍然可查")
	}
}

// BenchmarkIsGlobalBlackIp 集合视图的查询开销（10000 条黑名单）。
func BenchmarkIsGlobalBlackIp(b *testing.B) {
	db := newTestDb(&testing.T{})
	global := new(Glob)
	global.BlackIpList = make([]string, MaxGlobalBlackIpCount)
	for i := range global.BlackIpList {
		global.BlackIpList[i] = fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)
	}
	if err := db.SaveGlobal(global); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.IsGlobalBlackIp("10.1.2.3")
	}
}

// BenchmarkIsGlobalBlackIpLegacy 对比改造前的实现：
// 每个连接都要复制整张列表 + 就地排序 + 二分查找。
// 留着这个基准是为了防止有人把查询路径改回慢实现。
func BenchmarkIsGlobalBlackIpLegacy(b *testing.B) {
	list := make([]string, MaxGlobalBlackIpCount)
	for i := range list {
		list[i] = fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copied := make([]string, len(list))
		copy(copied, list)
		sort.Strings(copied)
		index := sort.SearchStrings(copied, "10.1.2.3")
		_ = index < len(copied) && copied[index] == "10.1.2.3"
	}
}
