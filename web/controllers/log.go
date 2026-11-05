package controllers

import (
	"os"
	"path/filepath"

	"ehang.io/nps/lib/common"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// LogController 提供服务端日志的查看与清理。
//
// 日志里包含客户端 vkey（例如「隧道[x]流量已经超出」），因此所有接口
// 都限制为管理员访问。
type LogController struct {
	BaseController
}

// logAdminOnly 校验管理员身份。
func (s *LogController) logAdminOnly() bool {
	if isAdmin, ok := s.GetSession("isAdmin").(bool); ok && isAdmin {
		return true
	}
	s.AjaxErr("only admin can view server logs")
	return false
}

// Index 渲染日志页面。
func (s *LogController) Index() {
	if !s.logPageAdminOnly() {
		return
	}
	s.Data["menu"] = "log"
	s.SetInfo("log")
	s.Data["logFiles"] = LogFileNames()
	s.Data["logFilePath"] = common.GetLogFilePath()
	s.display("log/index")
}

// logPageAdminOnly 校验管理员身份，非管理员重定向到首页。
func (s *LogController) logPageAdminOnly() bool {
	if isAdmin, ok := s.GetSession("isAdmin").(bool); ok && isAdmin {
		return true
	}
	s.Redirect(beego.AppConfig.String("web_base_url")+"/", 302)
	return false
}

// List 返回增量日志。offset 是前端已经收到的最后一条日志的 seq。
//
// GET /log/list?offset=123&limit=500
func (s *LogController) List() {
	if !s.logAdminOnly() {
		return
	}
	offset := uint64(s.GetIntNoErr("offset"))
	limit := s.GetIntNoErr("limit")
	entries, latest := common.LogStoreInstance().Since(offset, limit)
	s.Data["json"] = map[string]interface{}{
		"status":  1,
		"entries": entries,
		"latest":  latest,
		"total":   common.LogStoreInstance().Len(),
	}
	s.ServeJSON()
	s.StopRun()
}

// Clear 清空内存日志缓冲，并清理磁盘上的日志文件。
//
// 磁盘上的所有日志文件（含 beego 轮转出来的历史文件）统一做「截断」而不是删除：
// beego 持有当前日志文件的句柄，删除会让后续日志写进已删除的 inode
// （磁盘空间不释放），在 Windows 上更是会直接失败。截断既能立即释放空间，
// 又能让文件本身继续被正常写入。
//
// POST /log/clear
func (s *LogController) Clear() {
	if !s.logAdminOnly() {
		return
	}
	common.LogStoreInstance().Clear()

	removed := []string{}
	failed := []string{}
	current := common.GetLogFilePath()
	for _, path := range common.LogFileFamily(current) {
		if err := clearLogFile(path); err != nil {
			failed = append(failed, filepath.Base(path)+": "+err.Error())
			continue
		}
		removed = append(removed, filepath.Base(path))
	}
	logs.Info("日志已清理: 清空内存缓冲，磁盘文件处理 %d 个", len(removed))

	s.Data["json"] = map[string]interface{}{
		"status":  1,
		"msg":     "clean success",
		"removed": removed,
		"failed":  failed,
	}
	s.ServeJSON()
	s.StopRun()
}

// clearLogFile 清空单个日志文件的内容，保留文件本身。
func clearLogFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	// O_TRUNC 会把文件长度截断为 0，beego 持有的句柄依然有效，
	// 后续日志会从文件开头继续写入。
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

// LogFileNames 返回当前日志文件家族的名称，供页面展示。
func LogFileNames() []string {
	current := common.GetLogFilePath()
	if current == "" {
		return nil
	}
	paths := common.LogFileFamily(current)
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}
