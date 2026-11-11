package file

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/rate"
	"github.com/pkg/errors"
)

type Flow struct {
	ExportFlow int64
	InletFlow  int64
	FlowLimit  int64
	sync.RWMutex
}

func (s *Flow) Add(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.InletFlow += int64(in)
	s.ExportFlow += int64(out)
}

// Reset 将入口/出口流量统计清零，流量上限 FlowLimit 保持不变。
// 用于在 Web 后台手动重置某个客户端的流量计数。
func (s *Flow) Reset() {
	s.Lock()
	defer s.Unlock()
	s.InletFlow = 0
	s.ExportFlow = 0
}

type Config struct {
	U        string
	P        string
	Compress bool
	Crypt    bool
}

type Client struct {
	Cnf             *Config
	Id              int        //id
	VerifyKey       string     //verify key
	Addr            string     // client egress address as seen by server (RemoteAddr)
	LocalAddr       string     // client private/LAN addresses reported by npc
	Remark          string     //remark
	Status          bool       //is allow connect
	IsConnect       bool       //is the client connect
	RateLimit       int        //rate /kb
	Flow            *Flow      //flow setting
	Rate            *rate.Rate //rate limit
	NoStore         bool       //no store to file
	NoDisplay       bool       //no display on web
	MaxConn         int        //the max connection num of client allow
	NowConn         int32      //the connection num of now
	WebUserName     string     //the username of web login
	WebPassword     string     //the password of web login
	ConfigConnAllow bool       //is allow connected by config file
	MaxTunnelNum    int
	Version         string
	BlackIpList     []string
	CreateTime      string
	LastOnlineTime  string
	IpWhite         bool     // 是否启用ip白名单
	IpWhitePass     string   // ip授权密码
	IpWhiteList     []string // ip白名单
	ExpireTime      string   // 到期时间,留空表示永不过期,格式 2006-01-02 15:04:05
	sync.RWMutex
}

func NewClient(vKey string, noStore bool, noDisplay bool) *Client {
	return &Client{
		Cnf:       new(Config),
		Id:        0,
		VerifyKey: vKey,
		Addr:      "",
		LocalAddr: "",
		Remark:    "",
		Status:    true,
		IsConnect: false,
		RateLimit: 0,
		Flow:      new(Flow),
		Rate:      nil,
		NoStore:   noStore,
		RWMutex:   sync.RWMutex{},
		NoDisplay: noDisplay,
	}
}

func (s *Client) CutConn() {
	atomic.AddInt32(&s.NowConn, 1)
}

func (s *Client) AddConn() {
	atomic.AddInt32(&s.NowConn, -1)
}

func (s *Client) GetConn() bool {
	if s.MaxConn == 0 || int(s.NowConn) < s.MaxConn {
		s.CutConn()
		return true
	}
	return false
}

func (s *Client) HasTunnel(t *Tunnel) (exist bool) {
	GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if v.Client.Id == s.Id && v.Port == t.Port && t.Port != 0 {
			exist = true
			return false
		}
		return true
	})
	return
}

func (s *Client) GetTunnelNum() (num int) {
	GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if v.Client.Id == s.Id {
			num++
		}
		return true
	})

	GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		if v.Client.Id == s.Id {
			num++
		}
		return true
	})
	return
}

func (s *Client) HasHost(h *Host) bool {
	var has bool
	GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		if v.Client.Id == s.Id && v.Host == h.Host && h.Location == v.Location {
			has = true
			return false
		}
		return true
	})
	return has
}

type Tunnel struct {
	Id           int
	Port         int
	ServerIp     string
	Mode         string
	Status       bool
	RunStatus    bool
	Client       *Client
	Ports        string
	Flow         *Flow
	Password     string
	Remark       string
	TargetAddr   string
	NoStore      bool
	LocalPath    string
	StripPre     string
	ProtoVersion string
	Target       *Target
	MultiAccount *MultiAccount
	Health
	sync.RWMutex
}

type Health struct {
	HealthCheckTimeout  int
	HealthMaxFail       int
	HealthCheckInterval int
	HealthNextTime      time.Time
	HealthMap           map[string]int
	HttpHealthUrl       string
	HealthRemoveArr     []string
	HealthCheckType     string
	HealthCheckTarget   string
	sync.RWMutex
}

type Host struct {
	Id           int
	Host         string //host
	HeaderChange string //header change
	HostChange   string //host change
	Location     string //url router
	Remark       string //remark
	Scheme       string //http https all
	CertFilePath string
	KeyFilePath  string
	NoStore      bool
	IsClose      bool
	AutoHttps    bool // 自动https
	Flow         *Flow
	Client       *Client
	Target       *Target //目标
	Health       `json:"-"`
	sync.RWMutex
}

type Target struct {
	nowIndex   int
	TargetStr  string
	TargetArr  []string
	LocalProxy bool
	sync.RWMutex
}

type MultiAccount struct {
	AccountMap map[string]string // multi account and pwd
}

func (s *Target) GetRandomTarget() (string, error) {
	s.Lock()
	if s.TargetArr == nil {
		arr := strings.Split(s.TargetStr, "\n")
		s.TargetArr = make([]string, 0, len(arr))
		for _, v := range arr {
			v = strings.TrimRight(v, "\r")
			if v != "" {
				s.TargetArr = append(s.TargetArr, v)
			}
		}
	}
	if len(s.TargetArr) == 0 {
		s.Unlock()
		return "", errors.New("all inward-bending targets are offline")
	}
	if s.nowIndex >= len(s.TargetArr)-1 {
		s.nowIndex = -1
	}
	s.nowIndex++
	addr := s.TargetArr[s.nowIndex]
	s.Unlock()
	return addr, nil
}

// Glob 保存管理端「全局参数」，可通过 Web 页面热更新。
// 指针字段用于区分「尚未初始化」和「显式设置为 false/空」：
// 首次启动时会从 nps.conf 迁移，之后以 global.json 为准。
type Glob struct {
	BlackIpList []string
	ServerUrl   string

	AllowPorts              *string `json:"AllowPorts,omitempty"`
	IpLimit                 *bool   `json:"IpLimit,omitempty"`
	ShowHttpProxyPort       *bool   `json:"ShowHttpProxyPort,omitempty"`
	AllowFlowLimit          *bool   `json:"AllowFlowLimit,omitempty"`
	AllowRateLimit          *bool   `json:"AllowRateLimit,omitempty"`
	AllowConnectionNumLimit *bool   `json:"AllowConnectionNumLimit,omitempty"`
	AllowTunnelNumLimit     *bool   `json:"AllowTunnelNumLimit,omitempty"`
	AllowMultiIp            *bool   `json:"AllowMultiIp,omitempty"`
	AllowLocalProxy         *bool   `json:"AllowLocalProxy,omitempty"`
	AllowUserLogin          *bool   `json:"AllowUserLogin,omitempty"`
	AllowUserRegister       *bool   `json:"AllowUserRegister,omitempty"`
	AllowUserChangeUsername *bool   `json:"AllowUserChangeUsername,omitempty"`
	OpenCaptcha             *bool   `json:"OpenCaptcha,omitempty"`
	SystemInfoDisplay       *bool   `json:"SystemInfoDisplay,omitempty"`
	HttpAddOriginHeader     *bool   `json:"HttpAddOriginHeader,omitempty"`
	// BlockForeignIp 为 true 时，服务端拒绝来自中国大陆 IP 段以外的访问。
	// 该字段已被 RegionLimitMode 取代，仅作为历史兼容保留：
	// 读取时 RegionLimitMode 为空时会回退读取它，保存时由 RegionLimitMode 同步写回。
	BlockForeignIp *bool `json:"BlockForeignIp,omitempty"`
	// RegionLimitMode 控制「访问地区限制」的强度：
	//   0 = 不限制；1 = 仅禁止境外 IP；2 = 仅允许所选省份访问。
	RegionLimitMode *int `json:"RegionLimitMode,omitempty"`
	// AllowedProvinces 保存省份模式（RegionLimitMode = 2）下允许访问的
	// 省级行政区，使用规范中文简称，例如 ["广东", "浙江"]。
	AllowedProvinces []string `json:"AllowedProvinces,omitempty"`
	// LoginFailAlertThreshold 是面板登录失败告警阈值：
	// 同一来源 IP 在统计窗口内登录失败达到该次数时推送一条告警。
	// 0 表示不告警。
	LoginFailAlertThreshold *int `json:"LoginFailAlertThreshold,omitempty"`
	// LoginSuccessNotify 为 true 时，面板登录成功会推送一条 Bark 通知，
	// 通知内容包含本次登录的来源 IP。默认关闭，避免频繁登录刷屏。
	LoginSuccessNotify *bool `json:"LoginSuccessNotify,omitempty"`
	// AutoBlockForeignIp 为 true 时，把被判定为境外的 IP 自动追加到全局黑名单。
	// 仅在 BlockForeignIp 开启时生效。
	AutoBlockForeignIp *bool `json:"AutoBlockForeignIp,omitempty"`
	// BarkEnabled 为 true 时，把 IP 加入全局黑名单的事件通过 Bark 推送。
	BarkEnabled *bool `json:"BarkEnabled,omitempty"`
	// BarkUrl 是 Bark 推送地址，形如 https://api.day.app/<deviceKey>，
	// 自建 Bark 服务器填写自己的前缀即可。
	BarkUrl *string `json:"BarkUrl,omitempty"`
	// BarkQuietStart / BarkQuietEnd 是免打扰时段，格式 HH:MM。
	// 两者都填写且当前时间落在该区间内时，通知会被直接丢弃。
	BarkQuietStart *string `json:"BarkQuietStart,omitempty"`
	BarkQuietEnd   *string `json:"BarkQuietEnd,omitempty"`
	// HiddenMenus 是左侧导航需要隐藏的菜单项 key 列表。
	// 可选值：index/client/host/tcp/udp/http/socks5/secret/p2p/file/global/help。
	HiddenMenus []string `json:"HiddenMenus,omitempty"`
	// AutoRestartEnabled 为 true 时启用定时重启服务端进程。
	AutoRestartEnabled *bool `json:"AutoRestartEnabled,omitempty"`
	// AutoRestartTime 是定时重启的时刻，格式 HH:MM（24 小时制），如 04:00。
	AutoRestartTime *string `json:"AutoRestartTime,omitempty"`
	// AutoRestartWeekdays 是定时重启生效的星期，取值 0-6（周日=0，周六=6）。
	// 为空表示每天都重启。
	AutoRestartWeekdays []int `json:"AutoRestartWeekdays,omitempty"`

	sync.RWMutex
}
