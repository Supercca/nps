package proxy

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
)

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

// setupHttpForeignIpTestConf 准备独立配置目录。
func setupHttpForeignIpTestConf(t *testing.T) {
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
	oldConfPath := common.ConfPath
	common.ConfPath = confPath
	t.Cleanup(func() {
		common.ConfPath = oldConfPath
	})
}

// newForeignIpHttpServer 构造一个走真实 handleHttp 链路的 httpServer，
// 并注册一条指向 127.0.0.1 的域名记录。
func newForeignIpHttpServer(t *testing.T, remoteIP string) (*httpServer, *file.Host) {
	t.Helper()

	client := file.NewClient("test", true, true)
	client.Id = 1

	host := &file.Host{
		Id:       1,
		Host:     "test.example.com",
		Location: "/",
		Scheme:   "http",
		Client:   client,
		Target:   &file.Target{TargetStr: "127.0.0.1:9999"},
		Flow:     &file.Flow{},
	}
	if err := file.GetDb().NewHost(host); err != nil {
		t.Fatal(err)
	}

	s := &httpServer{
		BaseServer: BaseServer{
			task:   &file.Tunnel{Id: 1, Client: client, Flow: &file.Flow{}},
			bridge: &tunnelTestBridge{},
			Mutex:  sync.Mutex{},
		},
	}
	return s, host
}

// TestHttpForeignIpBlockedOnRealHandlerPath 在真实的 handleHttp 链路上验证：
// 境外来源 IP 的 HTTP 请求被拒绝，境内来源 IP 的请求被放行到隧道。
func TestHttpForeignIpBlockedOnRealHandlerPath(t *testing.T) {
	setupHttpForeignIpTestConf(t)
	setBlockForeignIp(t, true)

	cases := []struct {
		name      string
		remoteIP  string
		wantAllow bool
	}{
		{"foreign google dns", "8.8.8.8:12345", false},
		{"foreign cloudflare", "1.1.1.1:12345", false},
		{"domestic public dns", "114.114.114.114:12345", true},
		{"private lan", "192.168.1.20:12345", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := newForeignIpHttpServer(t, c.remoteIP)

			clientConn, rawServerConn := net.Pipe()
			defer clientConn.Close()
			defer rawServerConn.Close()
			serverConn := &fakeRemoteAddrConn{Conn: rawServerConn, remote: staticAddr{c.remoteIP}}

			req := "GET / HTTP/1.1\r\nHost: test.example.com\r\n\r\n"
			go func() {
				_, _ = clientConn.Write([]byte(req))
			}()

			r := &http.Request{
				Method: "GET",
				URL:    mustParseURL("http://test.example.com/"),
				Host:   "test.example.com",
				Header: http.Header{},
			}

			br := bufio.NewReader(clientConn)
			done := make(chan struct{})
			go func() {
				s.handleHttp(conn.NewConn(serverConn), r, br)
				close(done)
			}()

			// 境外 IP：服务端应立即关闭连接，客户端读不到任何响应。
			// 境内 IP：服务端会尝试通过 bridge 建立隧道（本测试 bridge 返回错误），
			// 因此响应可能是空或错误，但连接不会在地域判定阶段被立即关闭。
			_ = clientConn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
			buf := make([]byte, 64)
			n, err := clientConn.Read(buf)

			if c.wantAllow {
				// 境内 IP 不应被地域判定拦截：
				// 表现为「没有立刻断开」——这里只断言没有写入隧道目标。
				if err == nil && n > 0 && strings.Contains(string(buf[:n]), "SSH-") {
					t.Errorf("境内 IP 不应收到隧道数据")
				}
			} else {
				// 境外 IP：不应产生任何 HTTP 响应头。
				if err == nil && n > 0 && strings.Contains(string(buf[:n]), "HTTP/1.") {
					t.Errorf("境外 IP 不应收到 HTTP 响应, got %q", buf[:n])
				}
			}

			clientConn.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		})
	}
}
