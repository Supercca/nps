package proxy

import (
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
)

// setGlobalSwitches 同时设置「禁止国外 IP 访问」与「境外 IP 自动加入黑名单」两个开关。
func setGlobalSwitches(t *testing.T, block, auto bool) {
	t.Helper()
	global := file.GetDb().GetGlobal()
	if global == nil {
		global = new(file.Glob)
	}
	b, a := block, auto
	global.BlockForeignIp = &b
	global.AutoBlockForeignIp = &a
	// 全局黑名单是进程内共享的，测试之间需要重置，避免互相污染。
	file.GetDb().SetGlobalBlackIpList(global, nil)
	if err := file.GetDb().SaveGlobal(global); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		g := file.GetDb().GetGlobal()
		if g != nil {
			off := false
			g.BlockForeignIp = &off
			g.AutoBlockForeignIp = &off
			file.GetDb().SetGlobalBlackIpList(g, nil)
			_ = file.GetDb().SaveGlobal(g)
		}
	})
}

// TestAutoBlackIpRequiresMainSwitch 主开关关闭时，副开关不应产生任何效果：
// 既不拦截，也不把 IP 加进黑名单。
func TestAutoBlackIpRequiresMainSwitch(t *testing.T) {
	setupForeignIpTestConf(t)
	setGlobalSwitches(t, false, true)

	if IsForeignIp("8.8.8.8:12345") {
		t.Error("主开关关闭时不应拦截境外 IP")
	}
	if list := file.GetDb().GetGlobalBlackIpList(); len(list) != 0 {
		t.Errorf("主开关关闭时不应写入黑名单: %v", list)
	}
}

// TestAutoBlackIpDisabled 主开关开启、副开关关闭时：拦截但不写入黑名单。
func TestAutoBlackIpDisabled(t *testing.T) {
	setupForeignIpTestConf(t)
	setGlobalSwitches(t, true, false)

	if !IsForeignIp("8.8.8.8:12345") {
		t.Fatal("开启主开关后境外 IP 应被拦截")
	}
	if list := file.GetDb().GetGlobalBlackIpList(); len(list) != 0 {
		t.Errorf("副开关关闭时不应写入黑名单: %v", list)
	}
}

// TestAutoBlackIpEnabled 两个开关都开启时：境外 IP 被拦截并自动加入全局黑名单。
func TestAutoBlackIpEnabled(t *testing.T) {
	setupForeignIpTestConf(t)
	setGlobalSwitches(t, true, true)

	if !IsForeignIp("8.8.8.8:12345") {
		t.Fatal("开启主开关后境外 IP 应被拦截")
	}

	list := file.GetDb().GetGlobalBlackIpList()
	if len(list) != 1 || list[0] != "8.8.8.8" {
		t.Fatalf("境外 IP 应自动加入黑名单: %v", list)
	}
	// 重复命中同一个 IP 不应重复入黑名单。
	IsForeignIp("8.8.8.8:12345")
	if list := file.GetDb().GetGlobalBlackIpList(); len(list) != 1 {
		t.Fatalf("重复命中不应重复入黑名单: %v", list)
	}

	// 境内 IP 不应被加入黑名单。
	IsForeignIp("114.114.114.114:80")
	if list := file.GetDb().GetGlobalBlackIpList(); len(list) != 1 {
		t.Fatalf("境内 IP 不应加入黑名单: %v", list)
	}
}

// TestAutoBlackIpPersisted 自动拉黑应立即落盘到 global.json。
func TestAutoBlackIpPersisted(t *testing.T) {
	setupForeignIpTestConf(t)
	setGlobalSwitches(t, true, true)

	IsForeignIp("1.1.1.1:9999")

	b, err := os.ReadFile(file.GetDb().JsonDb.GlobalFilePath)
	if err != nil {
		t.Skipf("global.json 不可读，跳过落盘断言: %v", err)
	}
	global := new(file.Glob)
	if err := json.Unmarshal(b, global); err != nil {
		t.Fatalf("global.json 应为合法 json: %v", err)
	}
	found := false
	for _, item := range global.BlackIpList {
		if item == "1.1.1.1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("黑名单应已落盘: %v", global.BlackIpList)
	}
}

// TestDealClientAutoAddsForeignIp 走真实的 DealClient 链路：
// 来自境外地址的连接被拦截，同时该 IP 被自动加入全局黑名单。
func TestDealClientAutoAddsForeignIp(t *testing.T) {
	setupForeignIpTestConf(t)
	setGlobalSwitches(t, true, true)

	clientConn, rawServerConn := net.Pipe()
	defer clientConn.Close()
	defer rawServerConn.Close()
	// 伪装成来自境外地址 8.8.8.8 的连接。
	serverConn := &fakeRemoteAddrConn{Conn: rawServerConn, remote: staticAddr{"8.8.8.8:12345"}}

	targetClient, targetServer := net.Pipe()
	defer targetClient.Close()
	defer targetServer.Close()

	client := file.NewClient("test", true, true)
	client.Id = 1
	task := &file.Tunnel{
		Id:     1,
		Client: client,
		Target: &file.Target{TargetStr: "127.0.0.1:22"},
	}
	server := NewTunnelModeServer(ProcessTunnel, &tunnelTestBridge{target: targetServer}, task)

	done := make(chan error, 1)
	go func() {
		done <- server.DealClient(conn.NewConn(serverConn), client, "127.0.0.1:22", nil,
			common.CONN_TCP, nil, client.Flow, false, task, nil)
	}()

	// 服务端会立即关闭连接，写入可能返回 broken pipe，属于预期行为。
	_, _ = clientConn.Write([]byte("SSH-2.0-OpenSSH_9.0\r\n"))

	clientConn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DealClient did not exit")
	}

	list := file.GetDb().GetGlobalBlackIpList()
	if len(list) != 1 || list[0] != "8.8.8.8" {
		t.Fatalf("境外 IP 应自动加入全局黑名单: %v", list)
	}

	// 拉黑后，同一 IP 的后续连接会被全局黑名单直接拦下。
	if !IsGlobalBlackIp("8.8.8.8:12345") {
		t.Fatal("自动拉黑的 IP 应命中全局黑名单")
	}
}
