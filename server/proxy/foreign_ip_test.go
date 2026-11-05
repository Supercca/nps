package proxy

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
)

// setupForeignIpTestConf 准备独立的测试配置目录，并允许设置全局参数。
func setupForeignIpTestConf(t *testing.T) {
	t.Helper()
	confPath := t.TempDir()
	confDir := filepath.Join(confPath, "conf")
	if err := os.MkdirAll(confDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clients.json", "tasks.json", "hosts.json"} {
		if err := os.WriteFile(filepath.Join(confDir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(confDir, "global.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	oldConfPath := common.ConfPath
	common.ConfPath = confPath

	// JsonDb 的路径在首次 GetDb() 时就固定了，这里把它指向测试目录，
	// 这样 global.json 的落盘行为才能在测试中被断言。
	db := file.GetDb()
	oldGlobalFilePath := db.JsonDb.GlobalFilePath
	db.JsonDb.GlobalFilePath = filepath.Join(confDir, "global.json")

	t.Cleanup(func() {
		db.JsonDb.GlobalFilePath = oldGlobalFilePath
		common.ConfPath = oldConfPath
	})
}

// setBlockForeignIp 设置全局「禁止国外 IP 访问」开关。
func setBlockForeignIp(t *testing.T, enabled bool) {
	t.Helper()
	global := file.GetDb().GetGlobal()
	if global == nil {
		global = new(file.Glob)
	}
	v := enabled
	global.BlockForeignIp = &v
	if err := file.GetDb().SaveGlobal(global); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		g := file.GetDb().GetGlobal()
		if g != nil {
			off := false
			g.BlockForeignIp = &off
			_ = file.GetDb().SaveGlobal(g)
		}
	})
}

// TestIsForeignIpDisabledByDefault 未开启开关时，境外 IP 不应被拦截。
func TestIsForeignIpDisabledByDefault(t *testing.T) {
	setupForeignIpTestConf(t)
	setBlockForeignIp(t, false)

	if IsForeignIp("8.8.8.8:12345") {
		t.Error("开关关闭时不应拦截境外 IP")
	}
}

// TestIsForeignIpEnabled 开启开关后，境外公网 IP 应被拦截，境内与内网 IP 应放行。
func TestIsForeignIpEnabled(t *testing.T) {
	setupForeignIpTestConf(t)
	setBlockForeignIp(t, true)

	cases := []struct {
		addr string
		want bool
	}{
		{"8.8.8.8:12345", true},
		{"1.1.1.1:9999", true},
		{"13.107.42.14:80", true},
		{"114.114.114.114:80", false},
		{"223.5.5.5:53", false},
		{"127.0.0.1:8080", false},
		{"192.168.1.10:8080", false},
		{"10.0.0.5:3000", false},
		{"[2001:4860:4860::8888]:443", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsForeignIp(c.addr); got != c.want {
			t.Errorf("IsForeignIp(%q) = %v, 期望 %v", c.addr, got, c.want)
		}
	}
}

// fakeRemoteAddrConn 包装 net.Conn，伪造 RemoteAddr，
// 用于模拟来自指定 IP 的访问（net.Pipe 的 RemoteAddr 固定为 "pipe"）。
type fakeRemoteAddrConn struct {
	net.Conn
	remote net.Addr
}

func (c *fakeRemoteAddrConn) RemoteAddr() net.Addr { return c.remote }

type staticAddr struct{ s string }

func (a staticAddr) Network() string { return "tcp" }
func (a staticAddr) String() string  { return a.s }

// TestDealClientBlocksForeignIp 端到端验证：开启开关后，境外来源的 TCP 连接
// 不会到达目标端，而是被直接关闭。
func TestDealClientBlocksForeignIp(t *testing.T) {
	setupForeignIpTestConf(t)
	setBlockForeignIp(t, true)

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

	payload := []byte("SSH-2.0-OpenSSH_9.0\r\n")
	// 服务端会立即关闭连接，写入可能返回 broken pipe，属于预期行为。
	if _, err := clientConn.Write(payload); err != nil {
		// 连接已被服务端关闭，说明拦截生效。
	} else {
		// 目标端不应收到任何数据（连接被拦截）。
		_ = targetClient.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 64)
		n, err := targetClient.Read(buf)
		if err == nil && n > 0 {
			t.Fatalf("境外 IP 应被拦截，但目标端收到 %q", buf[:n])
		}
	}

	clientConn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DealClient did not exit")
	}
}

// TestDealClientAllowsDomesticIp 境内 IP 的连接应正常透传到目标端。
func TestDealClientAllowsDomesticIp(t *testing.T) {
	setupForeignIpTestConf(t)
	setBlockForeignIp(t, true)

	clientConn, rawServerConn := net.Pipe()
	defer clientConn.Close()
	defer rawServerConn.Close()
	// 伪装成来自境内地址 114.114.114.114 的连接。
	serverConn := &fakeRemoteAddrConn{Conn: rawServerConn, remote: staticAddr{"114.114.114.114:12345"}}

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

	payload := []byte("SSH-2.0-OpenSSH_9.0\r\n")
	if _, err := clientConn.Write(payload); err != nil {
		t.Fatal(err)
	}

	_ = targetClient.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(targetClient, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("期望透传 %q, 实际 %q", payload, got)
	}

	clientConn.Close()
	targetClient.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DealClient did not exit")
	}
}
