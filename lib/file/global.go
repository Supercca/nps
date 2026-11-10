package file

import (
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/geo"
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
	// 该字段已被 RegionLimitMode 取代，仅作为历史兼容保留。
	BlockForeignIp bool
	// RegionLimitMode 是「访问地区限制」的模式：
	// 0 = 不限制；1 = 仅禁止境外 IP；2 = 仅允许所选省份访问。
	RegionLimitMode int
	// AllowedProvinces 是省份模式下允许访问的省级行政区（规范中文简称）。
	AllowedProvinces []string
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
	// AutoRestartEnabled 为 true 时启用定时重启服务端进程。
	AutoRestartEnabled bool
	// AutoRestartTime 是定时重启的时刻，格式 HH:MM。为空表示未配置。
	AutoRestartTime string
	// AutoRestartWeekdays 是定时重启生效的星期（0=周日，6=周六），为空表示每天。
	AutoRestartWeekdays []int
}

// MaxGlobalBlackIpCount 是全局黑名单的最大条目数。
// 自动拉黑时超过该上限就不再追加，避免被大量境外 IP 撑爆内存与 global.json。
const MaxGlobalBlackIpCount = 10000

// 「访问地区限制」的三个模式。
const (
	// RegionLimitModeNone 不限制访问地区（默认）。
	RegionLimitModeNone = 0
	// RegionLimitModeBlockForeign 仅禁止境外 IP 访问。
	// 使用内置的中国大陆 CIDR 段库判定，不依赖 ip2region 离线库。
	RegionLimitModeBlockForeign = 1
	// RegionLimitModeAllowProvinces 仅允许所选省级行政区访问。
	// 使用 ip2region 离线库定位 IP 所属省份。
	RegionLimitModeAllowProvinces = 2
)

// RegionLimit 是「访问地区限制」在热路径上的只读快照。
//
// 代理层每来一个连接都要读一次限制配置，这里用 atomic 缓存一份快照，
// 避免每次连接都复制切片、构造 map。配置变更时由 refreshRegionLimit 重建快照。
type RegionLimit struct {
	// Mode 是限制模式，取值为 RegionLimitModeXxx。
	Mode int
	// AutoBlockForeign 为 true 时，判定为境外的 IP 会自动加入全局黑名单。
	// 仅在 RegionLimitModeBlockForeign 模式下生效。
	AutoBlockForeign bool
	// Allowed 是省份模式下允许访问的省级行政区集合（规范中文简称）。
	// Mode 不为 RegionLimitModeAllowProvinces 时为 nil。
	Allowed map[string]bool
}

// regionLimitCache 缓存当前的地区限制快照。
var regionLimitCache atomic.Value

// refreshRegionLimit 依据最新的全局配置重建地区限制快照。
// 配置的写入路径（保存、加载、迁移）都要调用它，保证快照与配置一致。
func refreshRegionLimit(global *Glob) {
	limit := &RegionLimit{Mode: RegionLimitModeNone}
	if global != nil {
		mode := RegionLimitModeNone
		switch {
		case global.RegionLimitMode != nil:
			mode = *global.RegionLimitMode
		case global.BlockForeignIp != nil && *global.BlockForeignIp:
			// 兼容历史配置：只有 BlockForeignIp 时等价于「仅禁止境外 IP」。
			mode = RegionLimitModeBlockForeign
		}
		if mode != RegionLimitModeNone && mode != RegionLimitModeBlockForeign && mode != RegionLimitModeAllowProvinces {
			mode = RegionLimitModeNone
		}
		limit.Mode = mode
		limit.AutoBlockForeign = boolPtrValue(global.AutoBlockForeignIp)
		if mode == RegionLimitModeAllowProvinces && len(global.AllowedProvinces) > 0 {
			limit.Allowed = make(map[string]bool, len(global.AllowedProvinces))
			for _, item := range global.AllowedProvinces {
				name := geo.NormalizeProvinceName(item)
				if name != "" {
					limit.Allowed[name] = true
				}
			}
		}
	}
	regionLimitCache.Store(limit)
}

// GetRegionLimit 返回当前生效的「访问地区限制」快照。
// 返回值只读，调用方不应修改其中的 map。
func (s *DbUtils) GetRegionLimit() RegionLimit {
	if v, ok := regionLimitCache.Load().(*RegionLimit); ok && v != nil {
		return *v
	}
	return RegionLimit{Mode: RegionLimitModeNone}
}

// rebuildBlackIpSet 依据全局配置里的黑名单列表重建集合视图。
//
// 集合视图是查询路径（每个连接都要查一次）的依据，必须与 Global.BlackIpList 一致：
// 任何替换黑名单的入口（加载磁盘数据、Web 保存、备份恢复）都要调用它。
func (s *JsonDb) rebuildBlackIpSet(global *Glob) {
	s.blackIpLock.Lock()
	defer s.blackIpLock.Unlock()
	s.rebuildBlackIpSetLocked(global)
}

// rebuildBlackIpSetLocked 重建集合视图，调用方必须持有写锁。
func (s *JsonDb) rebuildBlackIpSetLocked(global *Glob) {
	set := make(map[string]struct{})
	if global != nil {
		for _, ip := range global.BlackIpList {
			set[ip] = struct{}{}
		}
	}
	s.blackIpSet = set
}

// IsGlobalBlackIp 判断 ip 是否在全局黑名单内。
//
// 走的是 lib/file 内部维护的集合视图（map），一次查找即可，O(1)：
// 不再像以前那样每个连接都复制整表 + 就地排序 + 二分查找，
// 也就不会有「查找过程把共享切片就地排序」的数据竞争。
func (s *DbUtils) IsGlobalBlackIp(ip string) bool {
	if ip == "" {
		return false
	}
	s.JsonDb.blackIpLock.RLock()
	defer s.JsonDb.blackIpLock.RUnlock()
	_, ok := s.JsonDb.blackIpSet[ip]
	return ok
}

// GetGlobalBlackIpList 返回全局黑名单的副本。
// 调用方拿到的是快照，不会与后台的自动拉黑产生数据竞争。
func (s *DbUtils) GetGlobalBlackIpList() []string {
	s.JsonDb.blackIpLock.RLock()
	defer s.JsonDb.blackIpLock.RUnlock()
	global := s.GetGlobal()
	if global == nil {
		return nil
	}
	list := make([]string, len(global.BlackIpList))
	copy(list, global.BlackIpList)
	return list
}

// MaxAutoBlackIpPerMinute 是自动拉黑每分钟最多新增的条数。
//
// 被扫描时会有大量不同的境外 IP 自动进黑名单，若不限制：
//   - 黑名单会迅速写满上限（MaxGlobalBlackIpCount），后面的 IP 白扫；
//   - 每个新 IP 都要全量写一次 conf/global.json，写放大会把磁盘和锁都拖垮。
//
// 超出速率上限的 IP 不再进黑名单，但境外判定本身已经拦住了它们，
// 不影响拦截效果，只是少了一条「事后拉黑」的记录。
const MaxAutoBlackIpPerMinute = 60

var (
	// autoBlackIpWindowLock 保护自动拉黑的速率窗口。
	autoBlackIpWindowLock sync.Mutex
	// autoBlackIpWindowStart 是当前速率窗口的起点。
	autoBlackIpWindowStart time.Time
	// autoBlackIpWindowCount 是当前窗口内新增的条数。
	autoBlackIpWindowCount int
)

// allowAutoBlackIp 判断当前是否还在自动拉黑的速率上限内。
func allowAutoBlackIp() bool {
	autoBlackIpWindowLock.Lock()
	defer autoBlackIpWindowLock.Unlock()

	now := time.Now()
	if autoBlackIpWindowStart.IsZero() || now.Sub(autoBlackIpWindowStart) >= time.Minute {
		autoBlackIpWindowStart = now
		autoBlackIpWindowCount = 0
	}
	if autoBlackIpWindowCount >= MaxAutoBlackIpPerMinute {
		return false
	}
	autoBlackIpWindowCount++
	return true
}

// AddGlobalBlackIp 把 ip 追加到全局黑名单并立即落盘。
// 返回 true 表示确实新增了一条；ip 为空、已在黑名单中、
// 黑名单已达上限或超出自动拉黑速率上限时返回 false。
func (s *DbUtils) AddGlobalBlackIp(ip string) bool {
	if ip == "" {
		return false
	}

	s.JsonDb.blackIpLock.Lock()
	defer s.JsonDb.blackIpLock.Unlock()

	global := s.GetGlobal()
	if global == nil {
		global = new(Glob)
	}
	// 去重直接查集合视图，O(1)，不用再线性扫整张列表。
	if _, ok := s.JsonDb.blackIpSet[ip]; ok {
		return false
	}
	if len(global.BlackIpList) >= MaxGlobalBlackIpCount {
		return false
	}
	// 速率上限：被扫描时不至于把磁盘写满。
	if !allowAutoBlackIp() {
		return false
	}
	global.BlackIpList = append(global.BlackIpList, ip)
	s.JsonDb.blackIpSet[ip] = struct{}{}
	s.JsonDb.setGlobal(global)
	s.JsonDb.StoreGlobalToJsonFile()
	return true
}

// SetGlobalBlackIpList 用 list 替换全局黑名单（Web 后台保存时使用）。
// global 传入调用方当前持有的全局配置对象，保证黑名单与其他设置落在同一份数据上；
// 落盘由调用方随后执行的 SaveGlobal 负责。
func (s *DbUtils) SetGlobalBlackIpList(global *Glob, list []string) {
	s.JsonDb.blackIpLock.Lock()
	defer s.JsonDb.blackIpLock.Unlock()
	if global == nil {
		global = s.GetGlobal()
		if global == nil {
			global = new(Glob)
		}
	}
	global.BlackIpList = list
	s.JsonDb.setGlobal(global)
	// 整体替换后要重建集合视图，否则查询路径看到的还是旧名单。
	s.JsonDb.rebuildBlackIpSetLocked(global)
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

// regionLimitModeOf 读取配置中的地区限制模式。
// RegionLimitMode 尚未初始化时回退到历史字段 BlockForeignIp，
// 保证升级前保存过「禁止国外 IP 访问」的用户行为不变。
func regionLimitModeOf(global *Glob) int {
	if global == nil {
		return RegionLimitModeNone
	}
	if global.RegionLimitMode != nil {
		mode := *global.RegionLimitMode
		switch mode {
		case RegionLimitModeNone, RegionLimitModeBlockForeign, RegionLimitModeAllowProvinces:
			return mode
		default:
			return RegionLimitModeNone
		}
	}
	if boolPtrValue(global.BlockForeignIp) {
		return RegionLimitModeBlockForeign
	}
	return RegionLimitModeNone
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
	settings.RegionLimitMode = regionLimitModeOf(global)
	if len(global.AllowedProvinces) > 0 {
		settings.AllowedProvinces = append([]string(nil), global.AllowedProvinces...)
	}
	settings.AutoBlockForeignIp = boolPtrValue(global.AutoBlockForeignIp)
	settings.BarkEnabled = boolPtrValue(global.BarkEnabled)
	settings.BarkUrl = stringPtrValue(global.BarkUrl)
	settings.BarkQuietStart = stringPtrValue(global.BarkQuietStart)
	settings.BarkQuietEnd = stringPtrValue(global.BarkQuietEnd)
	if len(global.HiddenMenus) > 0 {
		settings.HiddenMenus = append([]string(nil), global.HiddenMenus...)
	}
	settings.AutoRestartEnabled = boolPtrValue(global.AutoRestartEnabled)
	settings.AutoRestartTime = stringPtrValue(global.AutoRestartTime)
	if len(global.AutoRestartWeekdays) > 0 {
		settings.AutoRestartWeekdays = append([]int(nil), global.AutoRestartWeekdays...)
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

func intPtr(value int) *int {
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
	// 访问地区限制模式：历史配置里只有 BlockForeignIp，迁移成「仅禁止境外 IP」。
	if global.RegionLimitMode == nil {
		global.RegionLimitMode = intPtr(regionLimitModeOf(global))
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
	if global.AutoRestartEnabled == nil {
		global.AutoRestartEnabled = boolPtr(appConfigBool("auto_restart_enabled", false))
		changed = true
	}
	if global.AutoRestartTime == nil {
		global.AutoRestartTime = stringPtr(beego.AppConfig.String("auto_restart_time"))
		changed = true
	}
	// AutoRestartWeekdays 为空即「每天」，无需迁移。

	if changed {
		_ = s.SaveGlobal(global)
	}
	// 无论配置是否有变化都要刷新快照，避免进程启动时快照为空导致限制不生效。
	refreshRegionLimit(global)
}
