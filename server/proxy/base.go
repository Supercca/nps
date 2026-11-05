package proxy

import (
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/geo"
	"ehang.io/nps/lib/notify"
	"github.com/astaxie/beego/logs"
)

type Service interface {
	Start() error
	Close() error
}

type NetBridge interface {
	SendLinkInfo(clientId int, link *conn.Link, t *file.Tunnel) (target net.Conn, err error)
}

// BaseServer struct
type BaseServer struct {
	id           int
	bridge       NetBridge
	task         *file.Tunnel
	errorContent []byte
	errorCode    int
	sync.Mutex
}

func NewBaseServer(bridge *bridge.Bridge, task *file.Tunnel) *BaseServer {
	return &BaseServer{
		bridge:       bridge,
		task:         task,
		errorContent: nil,
		Mutex:        sync.Mutex{},
	}
}

// add the flow
func (s *BaseServer) FlowAdd(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.task.Flow.ExportFlow += out
	s.task.Flow.InletFlow += in
}

// change the flow
func (s *BaseServer) FlowAddHost(host *file.Host, in, out int64) {
	s.Lock()
	defer s.Unlock()
	host.Flow.ExportFlow += out
	host.Flow.InletFlow += in
}

// write fail bytes to the connection
func (s *BaseServer) writeConnFail(c net.Conn) {
	c.Write([]byte(common.ConnectionFailBytes))
	c.Write(s.errorContent)
}

// auth check for reverse-proxy hosts (401 + WWW-Authenticate).
func (s *BaseServer) auth(r *http.Request, c *conn.Conn, u, p string) error {
	return s.doAuth(r, c, u, p, common.UnauthorizedBytes, "401 Unauthorized")
}

// proxyAuth check for HTTP forward proxy (407 + Proxy-Authenticate).
func (s *BaseServer) proxyAuth(r *http.Request, c *conn.Conn, u, p string) error {
	return s.doAuth(r, c, u, p, common.ProxyAuthRequiredBytes, "407 Proxy Authentication Required")
}

func (s *BaseServer) doAuth(r *http.Request, c *conn.Conn, u, p, failBytes, errMsg string) error {
	if u != "" && p != "" && !common.CheckAuth(r, u, p) {
		c.Write([]byte(failBytes))
		c.Close()
		return errors.New(errMsg)
	}
	return nil
}

// check flow limit of the client ,and decrease the allow num of client
func (s *BaseServer) CheckFlowAndConnNum(client *file.Client) error {
	if client.Flow.FlowLimit > 0 && (client.Flow.FlowLimit<<20) < (client.Flow.ExportFlow+client.Flow.InletFlow) {
		return errors.New("Traffic exceeded")
	}
	if !client.GetConn() {
		return errors.New("Connections exceed the current client limit")
	}
	return nil
}

func in(target string, str_array []string) bool {
	sort.Strings(str_array)
	index := sort.SearchStrings(str_array, target)
	if index < len(str_array) && str_array[index] == target {
		return true
	}
	return false
}

// create a new connection and start bytes copying
func (s *BaseServer) DealClient(c *conn.Conn, client *file.Client, addr string,
	rb []byte, tp string, f func(), flow *file.Flow, localProxy bool, task *file.Tunnel, host *file.Host) error {

	// 判断访问地址是否在全局黑名单内
	if IsGlobalBlackIp(c.RemoteAddr().String()) {
		c.Close()
		return nil
	}

	// 判断是否开启了「禁止国外 IP 访问」
	if IsForeignIp(c.RemoteAddr().String()) {
		c.Close()
		return nil
	}

	// 判断访问地址是否在黑名单内
	if common.IsBlackIp(c.RemoteAddr().String(), client.VerifyKey, client.BlackIpList) {
		c.Close()
		return nil
	}

	protoVersion := ""
	if task != nil {
		protoVersion = task.ProtoVersion
	}

	link := conn.NewLink(tp, addr, client.Cnf.Crypt, client.Cnf.Compress, c.Conn.RemoteAddr().String(), localProxy, protoVersion)
	if target, err := s.bridge.SendLinkInfo(client.Id, link, s.task); err != nil {
		logs.Warn("get connection from client id %d  error %s", client.Id, err.Error())
		c.Close()
		return err
	} else {
		if f != nil {
			f()
		}
		conn.CopyWaitGroup(target, c.Conn, link.Crypt, link.Compress, client.Rate, flow, true, rb, task, host)
	}
	return nil
}

// 判断访问地址是否在全局黑名单内
func IsGlobalBlackIp(ipPort string) bool {
	// 判断访问地址是否在全局黑名单内
	ip := common.GetIpByAddr(ipPort)
	// 传入副本：in() 会先就地排序入参切片，直接传共享切片会造成数据竞争。
	list := file.GetDb().GetGlobalBlackIpList()
	if in(ip, list) {
		logs.Error("IP地址[" + ip + "]在全局黑名单列表内")
		return true
	}

	return false
}

// IsForeignIp 判断访问来源是否为境外 IP。
// 仅在全局参数「禁止国外 IP 访问」开启时生效：命中中国大陆 IP 段库之外的公网地址
// 视为境外并拒绝；私有/保留地址与 IPv6 一律放行。
//
// 若同时开启了「境外 IP 自动加入黑名单」，确认是境外地址后会把该 IP 追加到
// 全局黑名单（global.BlackIpList）并立即落盘。
func IsForeignIp(ipPort string) bool {
	settings := file.GetDb().GetGlobalSettings()
	if !settings.BlockForeignIp {
		return false
	}
	ip := common.GetIpByAddr(ipPort)
	if ip == "" {
		return false
	}
	if geo.IsForeignIPStr(ip) {
		logs.Warn("IP地址[" + ip + "]属于境外地址，已按「禁止国外IP访问」策略拦截")
		if settings.AutoBlockForeignIp {
			if file.GetDb().AddGlobalBlackIp(ip) {
				logs.Warn("IP地址[" + ip + "]已自动加入全局黑名单")
				notify.NotifyBlackIpAdded(ip, "境外 IP 自动拦截")
			}
		}
		return true
	}
	return false
}
