package controllers

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/geo"
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
	s.Data["regionLimitMode"] = settings.RegionLimitMode
	s.Data["loginFailAlertThreshold"] = settings.LoginFailAlertThreshold
	s.Data["loginSuccessNotify"] = settings.LoginSuccessNotify
	s.Data["provinceGroups"] = provinceOptionsWithState(settings.AllowedProvinces)
	provinceDb := geo.ProvinceDBStatus()
	if provinceDb.Path == "" {
		// 库尚未加载时也要把预期路径展示出来，方便用户知道该把文件放哪。
		provinceDb.Path = file.GetDb().JsonDb.ProvinceDbPath()
	}
	s.Data["provinceDb"] = provinceDb
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
	// 「访问地区限制」是三选一，保存时以单选框的值为准；
	// 历史字段 BlockForeignIp 同步成「仅禁止境外 IP」，保证旧版本读取时行为一致。
	regionLimitMode := s.GetIntNoErr("regionLimitMode")
	switch regionLimitMode {
	case file.RegionLimitModeBlockForeign, file.RegionLimitModeAllowProvinces:
	default:
		regionLimitMode = file.RegionLimitModeNone
	}
	global.RegionLimitMode = globalIntPtr(regionLimitMode)
	global.BlockForeignIp = globalBoolPtr(regionLimitMode == file.RegionLimitModeBlockForeign)
	global.AllowedProvinces = filterProvinces(s.provincesParam())
	global.LoginFailAlertThreshold = globalIntPtr(filterLoginFailThreshold(s.GetIntNoErr("loginFailAlertThreshold")))
	global.LoginSuccessNotify = globalBoolPtr(s.GetBoolNoErr("loginSuccessNotify"))
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

// maxBackupUploadSize 是允许上传的备份包大小上限。
//
// 备份包本质上就是几份 JSON 的压缩，体积很小；这里限制得比较紧，
// 避免超大上传把服务内存撑爆。
const maxBackupUploadSize = 32 << 20

// backupAdminOnly 校验管理员身份。
//
// 备份包里含有全部客户端的 vkey 与 Web 登录口令，且恢复会整体覆盖业务数据，
// 因此这两个接口必须只对管理员开放。
func (s *GlobalController) backupAdminOnly() bool {
	return s.adminOnly("only admin can backup data")
}

// adminOnly 校验管理员身份，denyMsg 是被拒绝时返回的提示语。
func (s *GlobalController) adminOnly(denyMsg string) bool {
	if isAdmin, ok := s.GetSession("isAdmin").(bool); ok && isAdmin {
		return true
	}
	s.AjaxErr(denyMsg)
	return false
}

// backupFail 统一返回恢复失败的结果。
//
// detail 里放具体原因（例如版本不兼容、缺少文件），由前端直接展示。
func (s *GlobalController) backupFail(detail string) {
	s.Data["json"] = map[string]interface{}{
		"status": 0,
		"msg":    "restore failed",
		"detail": detail,
	}
	s.ServeJSON()
	s.StopRun()
}

// Backup 把当前全部业务数据（客户端、隧道、域名解析、全局参数）打包成 zip 供下载。
//
// GET /global/backup
//
// 下载前会先把内存中的数据落盘，保证备份内容与 conf 目录一致。
func (s *GlobalController) Backup() {
	if !s.backupAdminOnly() {
		return
	}
	data, err := file.GetDb().JsonDb.CreateBackup()
	if err != nil {
		logs.Error("create backup failed:", err)
		s.AjaxErr("backup failed: " + err.Error())
		return
	}
	filename := fmt.Sprintf("nps-backup-%s.zip", time.Now().Format("20060102-150405"))
	s.Ctx.Output.Header("Content-Type", "application/zip")
	s.Ctx.Output.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	// Body 会直接写出响应并标记 ResponseWriter 已开始，beego 不会再渲染模板。
	s.Ctx.Output.Body(data)
}

// Restore 从上传的 zip 备份包恢复数据。
//
// POST /global/restore   （multipart/form-data，字段名 file）
//
// 恢复流程：校验版本与结构 -> 自动备份当前数据 -> 覆盖数据文件 -> 重新加载内存。
// 覆盖前会在 conf/backup/ 留一份当前数据的备份，出问题可以手工换回去。
// 恢复后需要重启服务，正在运行的隧道才会按新数据重建。
func (s *GlobalController) Restore() {
	if !s.backupAdminOnly() {
		return
	}
	f, fh, err := s.GetFile("file")
	if err != nil {
		s.backupFail("no backup file uploaded")
		return
	}
	defer f.Close()
	if fh.Size > maxBackupUploadSize {
		s.backupFail("backup file is too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBackupUploadSize+1))
	if err != nil {
		s.backupFail("read backup file failed: " + err.Error())
		return
	}
	if len(data) > maxBackupUploadSize {
		s.backupFail("backup file is too large")
		return
	}

	result, err := file.GetDb().JsonDb.RestoreBackup(data)
	if err != nil {
		logs.Error("restore backup failed:", err)
		s.backupFail(err.Error())
		return
	}
	logs.Info("restore backup success, previous data saved to", result.BackupPath)
	s.Data["json"] = map[string]interface{}{
		"status":     1,
		"msg":        "restore success",
		"detail":     result.BackupPath,
		"clients":    result.ClientCount,
		"tasks":      result.TaskCount,
		"hosts":      result.HostCount,
		"createdAt":  result.Manifest.CreatedAt,
		"npsVersion": result.Manifest.NPSVersion,
	}
	s.ServeJSON()
	s.StopRun()
}

// maxProvinceDbUploadSize 是允许上传的 IP 库大小上限（字节）。
//
// 官方 ip2region v4 库约 10.6MB，这里留足余量给后续版本。
const maxProvinceDbUploadSize = 128 << 20

// provinceUploadFail 统一返回 IP 库上传失败的结果。
func (s *GlobalController) provinceUploadFail(detail string) {
	s.Data["json"] = map[string]interface{}{
		"status": 0,
		"msg":    "province database upload failed",
		"detail": detail,
	}
	s.ServeJSON()
	s.StopRun()
}

// ProvinceUpload 上传并更新「访问地区限制 → 仅允许指定省份」使用的 IP 库。
//
// POST /global/province/upload   （multipart/form-data，字段名 file）
//
// 流程：读取上传文件 -> 在内存中校验是不是有效的 ip2region 库 ->
// 原子写入 conf/ip2region_v4.xdb -> 重新加载并立即生效。
// 校验不通过时磁盘上的旧库保持不变，当前生效的省份表也不受影响。
func (s *GlobalController) ProvinceUpload() {
	if !s.adminOnly("only admin can upload data") {
		return
	}
	f, fh, err := s.GetFile("file")
	if err != nil {
		s.provinceUploadFail("no province database uploaded")
		return
	}
	defer f.Close()
	if fh.Size > maxProvinceDbUploadSize {
		s.provinceUploadFail("province database file is too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxProvinceDbUploadSize+1))
	if err != nil {
		s.provinceUploadFail("read province database failed: " + err.Error())
		return
	}
	if len(data) > maxProvinceDbUploadSize {
		s.provinceUploadFail("province database file is too large")
		return
	}

	if err := file.GetDb().SaveProvinceDb(data); err != nil {
		logs.Error("update province database failed:", err)
		s.provinceUploadFail(err.Error())
		return
	}

	status := geo.ProvinceDBStatus()
	s.Data["json"] = map[string]interface{}{
		"status":        1,
		"msg":           "province database updated",
		"detail":        status.Path,
		"provinceCount": status.ProvinceCount,
		"rangeCount":    status.RangeCount,
		"updatedAt":     status.LoadedAt,
	}
	s.ServeJSON()
	s.StopRun()
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

// provincesParam 读取勾选的省份列表。
// 同 hiddenMenusParam，需要兼容 name 与 name[] 两种写法。
func (s *GlobalController) provincesParam() []string {
	values := s.GetStrings("allowedProvinces")
	values = append(values, s.GetStrings("allowedProvinces[]")...)
	return values
}

// provinceOption 是省份勾选项的视图模型。
type provinceOption struct {
	// Name 是省份规范名，同时是表单提交的值。
	Name string
	// LangKey 是前端 i18n 词条 key。
	LangKey string
	// Checked 表示该省份当前是否在允许列表中。
	Checked bool
}

// provinceGroupOption 是大区分组的视图模型。
type provinceGroupOption struct {
	// LangKey 是大区的 i18n 词条 key。
	LangKey string
	// Provinces 是该大区下的省份勾选项。
	Provinces []provinceOption
}

// provinceOptionsWithState 返回带勾选状态的省份分组列表，供全局参数页渲染。
func provinceOptionsWithState(selected []string) []provinceGroupOption {
	state := make(map[string]bool, len(selected))
	for _, item := range selected {
		if name := geo.NormalizeProvinceName(item); name != "" {
			state[name] = true
		}
	}
	groups := geo.ProvincesByGroup()
	result := make([]provinceGroupOption, 0, len(groups))
	for _, group := range groups {
		item := provinceGroupOption{LangKey: group.LangKey}
		for _, province := range group.Provinces {
			item.Provinces = append(item.Provinces, provinceOption{
				Name:    province.Name,
				LangKey: province.LangKey,
				Checked: state[province.Name],
			})
		}
		result = append(result, item)
	}
	return result
}

// filterLoginFailThreshold 规整登录失败告警阈值：
// 负数按 0（不告警）处理，超过上限按上限截断。
func filterLoginFailThreshold(value int) int {
	if value < 0 {
		return 0
	}
	if value > file.MaxLoginFailAlertThreshold {
		return file.MaxLoginFailAlertThreshold
	}
	return value
}

// filterProvinces 只保留合法且去重后的省份名，避免写入任意值。
func filterProvinces(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := geo.NormalizeProvinceName(strings.TrimSpace(value))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
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

func globalIntPtr(value int) *int {
	return &value
}
