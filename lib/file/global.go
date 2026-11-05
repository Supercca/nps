package file

import (
	"sync"

	"github.com/astaxie/beego"
)

// GlobalSettings 是 Glob 的只读快照：把尚未迁移的指针字段解析成带默认值的普通值，
// 方便在请求/连接处理中直接读取，保证 Web 保存后立即生效。
type GlobalSettings struct {
	AllowPorts              string
	IpLimit                 bool
	ShowHttpProxyPort       bool
	AllowFlowLimit          bool
	AllowRateLimit          bool
	AllowConnectionNumLimit bool
	AllowTunnelNumLimit     bool
	AllowMultiIp            bool
	AllowLocalProxy         bool
	AllowUserLogin          bool
	AllowUserRegister       bool
	AllowUserChangeUsername bool
	OpenCaptcha             bool
	SystemInfoDisplay       bool
	HttpAddOriginHeader     bool
	// BlockForeignIp 为 true 时拒绝境外 IP 访问（通过内置中国大陆 CIDR 段库判定）。
	BlockForeignIp bool
	// AutoBlockForeignIp 为 true 时，把判定为境外的 IP 自动追加到全局黑名单。
	// 仅在 BlockForeignIp 开启时生效。
	AutoBlockForeignIp bool
	// BarkEnabled 为 true 时，IP 加入全局黑名单的事件会通过 Bark 推送。
	BarkEnabled bool
	// BarkUrl 是 Bark 推送地址，形如 https://api.day.app/<deviceKey>。
	BarkUrl string
	// BarkQuietStart / BarkQuietEnd 是免打扰时段，格式 HH:MM，均为空表示不启用。
	BarkQuietStart string
	BarkQuietEnd   string
	// HiddenMenus 是左侧导航需要隐藏的菜单项 key 列表。
	HiddenMenus []string
}

// MaxGlobalBlackIpCount 是全局黑名单的最大条目数。
// 自动拉黑时超过该上限就不再追加，避免被大量境外 IP 撑爆内存与 global.json。
const MaxGlobalBlackIpCount = 10000

// globalBlackIpLock 保护全局黑名单的读写。
// 代理层每来一个连接都要在黑名单里查找一次，而查找用的 in() 会先就地排序
// 入参切片（sort.Strings）；不加锁的话并发连接会同时排序同一份切片，
// 自动拉黑时更会与 append 并发，导致切片损坏。
var globalBlackIpLock sync.Mutex

// GetGlobalBlackIpList 返回全局黑名单的副本。
// 调用方拿到的是快照，不会与后台的自动拉黑产生数据竞争。
func (s *DbUtils) GetGlobalBlackIpList() []string {
	globalBlackIpLock.Lock()
	defer globalBlackIpLock.Unlock()
	global := s.GetGlobal()
	if global == nil {
		return nil
	}
	list := make([]string, len(global.BlackIpList))
	copy(list, global.BlackIpList)
	return list
}

// AddGlobalBlackIp 把 ip 追加到全局黑名单并立即落盘。
// 返回 true 表示确实新增了一条；ip 为空、已在黑名单中或黑名单已达上限时返回 false。
func (s *DbUtils) AddGlobalBlackIp(ip string) bool {
	if ip == "" {
		return false
	}

	globalBlackIpLock.Lock()
	defer globalBlackIpLock.Unlock()

	global := s.GetGlobal()
	if global == nil {
		global = new(Glob)
	}
	for _, item := range global.BlackIpList {
		if item == ip {
			return false
		}
	}
	if len(global.BlackIpList) >= MaxGlobalBlackIpCount {
		return false
	}
	global.BlackIpList = append(global.BlackIpList, ip)
	s.JsonDb.Global = global
	s.JsonDb.StoreGlobalToJsonFile()
	return true
}

// SetGlobalBlackIpList 用 list 替换全局黑名单（Web 后台保存时使用）。
// global 传入调用方当前持有的全局配置对象，保证黑名单与其他设置落在同一份数据上；
// 落盘由调用方随后执行的 SaveGlobal 负责。
func (s *DbUtils) SetGlobalBlackIpList(global *Glob, list []string) {
	globalBlackIpLock.Lock()
	defer globalBlackIpLock.Unlock()
	if global == nil {
		global = s.GetGlobal()
		if global == nil {
			global = new(Glob)
		}
	}
	global.BlackIpList = list
	s.JsonDb.Global = global
}

func boolPtrValue(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// GetGlobalSettings 返回全局参数的只读快照，未初始化的字段使用历史默认值。
func (s *DbUtils) GetGlobalSettings() GlobalSettings {
	settings := GlobalSettings{
		// 与原来的 DefaultBool / 默认 nps.conf 保持一致
		ShowHttpProxyPort:       true,
		AllowFlowLimit:          true,
		AllowRateLimit:          true,
		AllowConnectionNumLimit: true,
		AllowTunnelNumLimit:     true,
		AllowMultiIp:            true,
		AllowUserLogin:          true,
		AllowUserChangeUsername: true,
		SystemInfoDisplay:       true,
		HttpAddOriginHeader:     true,
	}
	global := s.GetGlobal()
	if global == nil {
		return settings
	}
	settings.AllowPorts = stringPtrValue(global.AllowPorts)
	settings.IpLimit = boolPtrValue(global.IpLimit)
	settings.ShowHttpProxyPort = boolPtrValue(global.ShowHttpProxyPort)
	settings.AllowFlowLimit = boolPtrValue(global.AllowFlowLimit)
	settings.AllowRateLimit = boolPtrValue(global.AllowRateLimit)
	settings.AllowConnectionNumLimit = boolPtrValue(global.AllowConnectionNumLimit)
	settings.AllowTunnelNumLimit = boolPtrValue(global.AllowTunnelNumLimit)
	settings.AllowMultiIp = boolPtrValue(global.AllowMultiIp)
	settings.AllowLocalProxy = boolPtrValue(global.AllowLocalProxy)
	settings.AllowUserLogin = boolPtrValue(global.AllowUserLogin)
	settings.AllowUserRegister = boolPtrValue(global.AllowUserRegister)
	settings.AllowUserChangeUsername = boolPtrValue(global.AllowUserChangeUsername)
	settings.OpenCaptcha = boolPtrValue(global.OpenCaptcha)
	settings.SystemInfoDisplay = boolPtrValue(global.SystemInfoDisplay)
	settings.HttpAddOriginHeader = boolPtrValue(global.HttpAddOriginHeader)
	settings.BlockForeignIp = boolPtrValue(global.BlockForeignIp)
	settings.AutoBlockForeignIp = boolPtrValue(global.AutoBlockForeignIp)
	settings.BarkEnabled = boolPtrValue(global.BarkEnabled)
	settings.BarkUrl = stringPtrValue(global.BarkUrl)
	settings.BarkQuietStart = stringPtrValue(global.BarkQuietStart)
	settings.BarkQuietEnd = stringPtrValue(global.BarkQuietEnd)
	if len(global.HiddenMenus) > 0 {
		settings.HiddenMenus = append([]string(nil), global.HiddenMenus...)
	}
	return settings
}

func appConfigBool(key string, defaultValue bool) bool {
	value, err := beego.AppConfig.Bool(key)
	if err != nil {
		return defaultValue
	}
	return value
}

func boolPtr(value bool) *bool {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

// MigrateGlobalFromAppConfig 在启动时把 nps.conf 中可热更新的参数补齐到 global.json。
// 只填充仍未初始化的字段：已经通过 Web 页面保存过的值不会被 nps.conf 覆盖。
func (s *DbUtils) MigrateGlobalFromAppConfig() {
	global := s.GetGlobal()
	if global == nil {
		global = new(Glob)
	}
	if global.BlackIpList == nil {
		global.BlackIpList = []string{}
	}
	changed := false

	if global.AllowPorts == nil {
		global.AllowPorts = stringPtr(beego.AppConfig.String("allow_ports"))
		changed = true
	}
	if global.IpLimit == nil {
		global.IpLimit = boolPtr(appConfigBool("ip_limit", false))
		changed = true
	}
	if global.ShowHttpProxyPort == nil {
		global.ShowHttpProxyPort = boolPtr(beego.AppConfig.DefaultBool("show_http_proxy_port", true))
		changed = true
	}
	if global.AllowFlowLimit == nil {
		global.AllowFlowLimit = boolPtr(appConfigBool("allow_flow_limit", true))
		changed = true
	}
	if global.AllowRateLimit == nil {
		global.AllowRateLimit = boolPtr(appConfigBool("allow_rate_limit", true))
		changed = true
	}
	if global.AllowConnectionNumLimit == nil {
		global.AllowConnectionNumLimit = boolPtr(appConfigBool("allow_connection_num_limit", true))
		changed = true
	}
	if global.AllowTunnelNumLimit == nil {
		global.AllowTunnelNumLimit = boolPtr(appConfigBool("allow_tunnel_num_limit", true))
		changed = true
	}
	if global.AllowMultiIp == nil {
		global.AllowMultiIp = boolPtr(appConfigBool("allow_multi_ip", true))
		changed = true
	}
	if global.AllowLocalProxy == nil {
		global.AllowLocalProxy = boolPtr(appConfigBool("allow_local_proxy", false))
		changed = true
	}
	if global.AllowUserLogin == nil {
		global.AllowUserLogin = boolPtr(appConfigBool("allow_user_login", true))
		changed = true
	}
	if global.AllowUserRegister == nil {
		global.AllowUserRegister = boolPtr(appConfigBool("allow_user_register", false))
		changed = true
	}
	if global.AllowUserChangeUsername == nil {
		global.AllowUserChangeUsername = boolPtr(appConfigBool("allow_user_change_username", true))
		changed = true
	}
	if global.OpenCaptcha == nil {
		global.OpenCaptcha = boolPtr(appConfigBool("open_captcha", false))
		changed = true
	}
	if global.SystemInfoDisplay == nil {
		global.SystemInfoDisplay = boolPtr(appConfigBool("system_info_display", true))
		changed = true
	}
	if global.HttpAddOriginHeader == nil {
		global.HttpAddOriginHeader = boolPtr(appConfigBool("http_add_origin_header", true))
		changed = true
	}
	if global.BlockForeignIp == nil {
		global.BlockForeignIp = boolPtr(appConfigBool("block_foreign_ip", false))
		changed = true
	}
	if global.AutoBlockForeignIp == nil {
		global.AutoBlockForeignIp = boolPtr(appConfigBool("auto_block_foreign_ip", false))
		changed = true
	}
	if global.BarkEnabled == nil {
		global.BarkEnabled = boolPtr(appConfigBool("bark_enabled", false))
		changed = true
	}
	if global.BarkUrl == nil {
		global.BarkUrl = stringPtr(beego.AppConfig.String("bark_url"))
		changed = true
	}
	if global.BarkQuietStart == nil {
		global.BarkQuietStart = stringPtr(beego.AppConfig.String("bark_quiet_start"))
		changed = true
	}
	if global.BarkQuietEnd == nil {
		global.BarkQuietEnd = stringPtr(beego.AppConfig.String("bark_quiet_end"))
		changed = true
	}

	if changed {
		_ = s.SaveGlobal(global)
	}
}
