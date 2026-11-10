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

	// 判断访问来源是否被「访问地区限制」拦截
	if IsRegionRestricted(c.RemoteAddr().String()) {
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

// IsRegionRestricted 判断访问来源是否被「访问地区限制」拦截。
//
// 这是访问地区限制的统一判定入口，覆盖三种模式：
//   - 不限制：一律放行。
//   - 仅禁止境外 IP：命中内置中国大陆 CIDR 段库之外的公网地址时拦截。
//   - 仅允许所选省份：IP 必须归属到允许列表中的中国省级行政区才放行。
//
// 内网 / 回环 / 保留地址在所有模式下都放行；IPv6 不在 ip2region 库覆盖范围内，
// 省份模式下同样放行（后台页面会提示这一点）。
func IsRegionRestricted(ipPort string) bool {
	limit := file.GetDb().GetRegionLimit()
	switch limit.Mode {
	case file.RegionLimitModeBlockForeign:
		return blockForeignIp(ipPort, limit)
	case file.RegionLimitModeAllowProvinces:
		return blockNonAllowedProvince(ipPort, limit)
	}
	return false
}

// blockForeignIp 判定 IP 是否属于境外地址。
//
// 若同时开启了「境外 IP 自动加入黑名单」，确认是境外地址后会把该 IP 追加到
// 全局黑名单（global.BlackIpList）并立即落盘。
func blockForeignIp(ipPort string, limit file.RegionLimit) bool {
	ip := common.GetIpByAddr(ipPort)
	if ip == "" {
		return false
	}
	if !geo.IsForeignIPStr(ip) {
		return false
	}
	logs.Warn("IP地址[" + ip + "]属于境外地址，已按「禁止国外IP访问」策略拦截")
	if limit.AutoBlockForeign {
		if file.GetDb().AddGlobalBlackIp(ip) {
			logs.Warn("IP地址[" + ip + "]已自动加入全局黑名单")
			notify.NotifyBlackIpAdded(ip, "境外 IP 自动拦截")
		}
	}
	return true
}

// blockNonAllowedProvince 判定 IP 是否属于未获准访问的省份。
//
// 判定依据是 ip2region 离线库：IP 必须能定位到一个中国省级行政区，
// 且该省份在允许列表里，才放行。境外地址、库中查不到的地址都会被拦截。
func blockNonAllowedProvince(ipPort string, limit file.RegionLimit) bool {
	ip := common.GetIpByAddr(ipPort)
	if ip == "" {
		return false
	}
	netIP := net.ParseIP(ip)
	if netIP == nil {
		return false
	}
	// 内网 / 回环 / 保留地址一律放行，与「禁止国外 IP 访问」保持一致。
	if geo.IsPrivateIP(netIP) {
		return false
	}
	// 内置 IP 库只覆盖 IPv4，IPv6 无法定位省份，放行。
	if netIP.To4() == nil {
		return false
	}
	province := geo.LookupProvince(netIP)
	if province == "" {
		logs.Warn("IP地址[" + ip + "]未归属任何中国省份，已按「访问地区限制」策略拦截")
		return true
	}
	if limit.Allowed[province] {
		return false
	}
	logs.Warn("IP地址[" + ip + "]所属省份[" + province + "]不在允许访问的省份列表内，已按「访问地区限制」策略拦截")
	return true
}

// IsForeignIp 判断访问来源是否为境外 IP。
// 仅对应「仅禁止境外 IP」模式；统一判定入口请使用 IsRegionRestricted。
//
// 若同时开启了「境外 IP 自动加入黑名单」，确认是境外地址后会把该 IP 追加到
// 全局黑名单（global.BlackIpList）并立即落盘。
func IsForeignIp(ipPort string) bool {
	limit := file.GetDb().GetRegionLimit()
	if limit.Mode != file.RegionLimitModeBlockForeign {
		return false
	}
	return blockForeignIp(ipPort, limit)
}
