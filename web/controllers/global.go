package controllers

import (
	"os"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/notify"
	"ehang.io/nps/lib/restart"
	"github.com/astaxie/beego/logs"
)

type GlobalController struct {
	BaseController
}

func (s *GlobalController) Index() {
	s.Data["menu"] = "global"
	s.SetInfo("global")

	global := file.GetDb().GetGlobal()
	settings := file.GetDb().GetGlobalSettings()
	blackIpList := ""
	serverUrl := ""
	if global != nil {
		// 取快照副本，避免与后台的自动拉黑并发读写同一个切片。
		blackIpList = strings.Join(file.GetDb().GetGlobalBlackIpList(), "\r\n")
		serverUrl = global.ServerUrl
	}
	s.Data["globalBlackIpList"] = blackIpList
	s.Data["serverUrl"] = serverUrl
	s.Data["allowPorts"] = settings.AllowPorts
	s.Data["ipLimit"] = settings.IpLimit
	s.Data["showHttpProxyPort"] = settings.ShowHttpProxyPort
	s.Data["allowFlowLimit"] = settings.AllowFlowLimit
	s.Data["allowRateLimit"] = settings.AllowRateLimit
	s.Data["allowConnectionNumLimit"] = settings.AllowConnectionNumLimit
	s.Data["allowTunnelNumLimit"] = settings.AllowTunnelNumLimit
	s.Data["allowMultiIp"] = settings.AllowMultiIp
	s.Data["allowLocalProxy"] = settings.AllowLocalProxy
	s.Data["allowUserLogin"] = settings.AllowUserLogin
	s.Data["allowUserRegister"] = settings.AllowUserRegister
	s.Data["allowUserChangeUsername"] = settings.AllowUserChangeUsername
	s.Data["openCaptcha"] = settings.OpenCaptcha
	s.Data["systemInfoDisplay"] = settings.SystemInfoDisplay
	s.Data["httpAddOriginHeader"] = settings.HttpAddOriginHeader
	s.Data["blockForeignIp"] = settings.BlockForeignIp
	s.Data["autoBlockForeignIp"] = settings.AutoBlockForeignIp
	s.Data["barkEnabled"] = settings.BarkEnabled
	s.Data["barkUrl"] = settings.BarkUrl
	s.Data["barkQuietStart"] = settings.BarkQuietStart
	s.Data["barkQuietEnd"] = settings.BarkQuietEnd
	s.Data["menuOptions"] = menuOptionsWithState(settings.HiddenMenus)
	s.Data["autoRestartEnabled"] = settings.AutoRestartEnabled
	s.Data["autoRestartTime"] = settings.AutoRestartTime
	s.Data["autoRestartWeekdays"] = weekdayOptionsWithState(settings.AutoRestartWeekdays)

	s.display("global/index")
}

// Save 保存全局参数。global.json 会同时更新内存和文件，保存后相关配置立即生效。
func (s *GlobalController) Save() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["menu"] = "global"
		s.SetInfo("save global")
		s.display()
		return
	}

	global := file.GetDb().GetGlobal()
	if global == nil {
		global = new(file.Glob)
	}
	// 保存前先记下旧黑名单，用于事后判断哪些 IP 是这次新增的。
	oldBlackIpList := file.GetDb().GetGlobalBlackIpList()
	newBlackIpList := RemoveRepeatedElement(strings.Split(s.getEscapeString("globalBlackIpList"), "\r\n"))
	file.GetDb().SetGlobalBlackIpList(global, newBlackIpList)
	global.ServerUrl = s.getEscapeString("serverUrl")
	global.AllowPorts = globalStringPtr(strings.TrimSpace(s.getEscapeString("allowPorts")))
	global.IpLimit = globalBoolPtr(s.GetBoolNoErr("ipLimit"))
	global.ShowHttpProxyPort = globalBoolPtr(s.GetBoolNoErr("showHttpProxyPort"))
	global.AllowFlowLimit = globalBoolPtr(s.GetBoolNoErr("allowFlowLimit"))
	global.AllowRateLimit = globalBoolPtr(s.GetBoolNoErr("allowRateLimit"))
	global.AllowConnectionNumLimit = globalBoolPtr(s.GetBoolNoErr("allowConnectionNumLimit"))
	global.AllowTunnelNumLimit = globalBoolPtr(s.GetBoolNoErr("allowTunnelNumLimit"))
	global.AllowMultiIp = globalBoolPtr(s.GetBoolNoErr("allowMultiIp"))
	global.AllowLocalProxy = globalBoolPtr(s.GetBoolNoErr("allowLocalProxy"))
	global.AllowUserLogin = globalBoolPtr(s.GetBoolNoErr("allowUserLogin"))
	global.AllowUserRegister = globalBoolPtr(s.GetBoolNoErr("allowUserRegister"))
	global.AllowUserChangeUsername = globalBoolPtr(s.GetBoolNoErr("allowUserChangeUsername"))
	global.OpenCaptcha = globalBoolPtr(s.GetBoolNoErr("openCaptcha"))
	global.SystemInfoDisplay = globalBoolPtr(s.GetBoolNoErr("systemInfoDisplay"))
	global.HttpAddOriginHeader = globalBoolPtr(s.GetBoolNoErr("httpAddOriginHeader"))
	global.BlockForeignIp = globalBoolPtr(s.GetBoolNoErr("blockForeignIp"))
	global.AutoBlockForeignIp = globalBoolPtr(s.GetBoolNoErr("autoBlockForeignIp"))
	global.BarkEnabled = globalBoolPtr(s.GetBoolNoErr("barkEnabled"))
	global.BarkUrl = globalStringPtr(strings.TrimSpace(s.getEscapeString("barkUrl")))
	global.BarkQuietStart = globalStringPtr(strings.TrimSpace(s.getEscapeString("barkQuietStart")))
	global.BarkQuietEnd = globalStringPtr(strings.TrimSpace(s.getEscapeString("barkQuietEnd")))
	global.HiddenMenus = filterHiddenMenus(s.hiddenMenusParam())
	global.AutoRestartEnabled = globalBoolPtr(s.GetBoolNoErr("autoRestartEnabled"))
	global.AutoRestartTime = globalStringPtr(strings.TrimSpace(s.getEscapeString("autoRestartTime")))
	global.AutoRestartWeekdays = filterWeekdays(s.autoRestartWeekdaysParam())

	if err := file.GetDb().SaveGlobal(global); err != nil {
		s.AjaxErr(err.Error())
		return
	}
	// 通知本次新增到全局黑名单的 IP。
	if added := diffBlackIpList(oldBlackIpList, newBlackIpList); len(added) > 0 {
		notify.NotifyBlackIpBatchAdded(added, "Web 后台手动添加")
	}
	s.AjaxOk("save success")
}

// Restart 重启服务端进程。仅管理员可用。
// 先应答浏览器，再延迟 1 秒执行重启：进程退出前必须让响应先送达，
// 否则前端会把它当成网络错误。重启前把内存中的 JSON 数据落盘一次，
// 避免 flowSession 周期性持久化间隔内的流量统计丢失。
func (s *GlobalController) Restart() {
	if s.Data["isAdmin"] != true {
		s.AjaxErr("permission denied")
		return
	}
	go func() {
		time.Sleep(time.Second)
		file.GetDb().JsonDb.StoreHostToJsonFile()
		file.GetDb().JsonDb.StoreTasksToJsonFile()
		file.GetDb().JsonDb.StoreClientsToJsonFile()
		file.GetDb().JsonDb.StoreGlobalToJsonFile()
		if err := restart.Do(); err != nil {
			logs.Error("restart service failed:", err)
		}
	}()
	s.AjaxOkWithId("restart command accepted", os.Getpid())
}

// RestartPid 返回当前进程 PID，供前端在轮询时判断面板是否已由重启后的
// 新进程接管（PID 变化即说明重启完成）。旧进程在等待退出的窗口期内
// 仍会返回旧 PID，前端据此继续等待，避免把未重启误判为已恢复。
func (s *GlobalController) RestartPid() {
	s.Ctx.WriteString(strconv.Itoa(os.Getpid()))
}

// hiddenMenusParam 读取被隐藏的菜单列表。
// 浏览器的表单由 jQuery serializeArray 提交，同名参数会被编码成 name[]，
// 而普通 API 调用可能直接传 name，两种写法都要兼容。
func (s *GlobalController) hiddenMenusParam() []string {
	keys := s.GetStrings("hiddenMenus")
	keys = append(keys, s.GetStrings("hiddenMenus[]")...)
	return keys
}

// autoRestartWeekdaysParam 读取定时重启生效的星期列表。
// 同 hiddenMenusParam，需要兼容 name 与 name[] 两种写法。
func (s *GlobalController) autoRestartWeekdaysParam() []string {
	values := s.GetStrings("autoRestartWeekdays")
	values = append(values, s.GetStrings("autoRestartWeekdays[]")...)
	return values
}

// filterWeekdays 只保留合法且去重后的星期（0-6）。
func filterWeekdays(values []string) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, v := range values {
		day, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || day < 0 || day > 6 || seen[day] {
			continue
		}
		seen[day] = true
		result = append(result, day)
	}
	return result
}

// WeekdayOption 描述一个星期几的勾选项。
type WeekdayOption struct {
	// Value 是提交给后端的星期值（0=周日 … 6=周六）。
	Value int
	// Name 是 i18n 词条 key。
	Name string
	// Checked 表示当前是否已勾选。
	Checked bool
}

// weekdayNames 是星期几的 i18n 词条，顺序为周日到周六。
var weekdayNames = []string{
	"word-weekday-sunday",
	"word-weekday-monday",
	"word-weekday-tuesday",
	"word-weekday-wednesday",
	"word-weekday-thursday",
	"word-weekday-friday",
	"word-weekday-saturday",
}

// weekdayOptionsWithState 返回带勾选状态的星期列表，供全局参数页渲染。
func weekdayOptionsWithState(selected []int) []WeekdayOption {
	state := make(map[int]bool, len(selected))
	for _, day := range selected {
		state[day] = true
	}
	options := make([]WeekdayOption, 0, len(weekdayNames))
	for i, name := range weekdayNames {
		options = append(options, WeekdayOption{
			Value:   i,
			Name:    name,
			Checked: state[i],
		})
	}
	return options
}

// diffBlackIpList 返回在 new 中但不在 old 中的 IP。
func diffBlackIpList(old, new []string) []string {
	exist := make(map[string]bool, len(old))
	for _, item := range old {
		exist[item] = true
	}
	var added []string
	for _, item := range new {
		item = strings.TrimSpace(item)
		if item == "" || exist[item] {
			continue
		}
		exist[item] = true
		added = append(added, item)
	}
	return added
}

// filterHiddenMenus 只保留已知菜单 key，避免写入任意值。
func filterHiddenMenus(keys []string) []string {
	var result []string
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := MenuKeys[key]; !ok {
			continue
		}
		if ProtectedMenus[key] {
			continue
		}
		result = append(result, key)
	}
	return result
}

func globalBoolPtr(value bool) *bool {
	return &value
}

func globalStringPtr(value string) *string {
	return &value
}
