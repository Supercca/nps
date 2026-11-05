package controllers

import (
	"os"
	"path/filepath"

	"ehang.io/nps/lib/common"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// LogController 提供服务端日志文件的查看与清理。
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

// logPageAdminOnly 校验管理员身份，非管理员重定向到首页。
func (s *LogController) logPageAdminOnly() bool {
	if isAdmin, ok := s.GetSession("isAdmin").(bool); ok && isAdmin {
		return true
	}
	s.Redirect(beego.AppConfig.String("web_base_url")+"/", 302)
	return false
}

// Index 渲染日志页面。
func (s *LogController) Index() {
	if !s.logPageAdminOnly() {
		return
	}
	s.Data["menu"] = "log"
	s.SetInfo("log")
	s.Data["logFilePath"] = common.GetLogFilePath()
	s.display("log/index")
}

// LogFileInfo 是一个日志文件的展示信息。
type LogFileInfo struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Size  int64    `json:"size"`
	Lines []string `json:"lines"`
}

// List 读取服务器上的日志文件并返回内容。
//
// GET /log/list?lines=500
func (s *LogController) List() {
	if !s.logAdminOnly() {
		return
	}
	limit := s.GetIntNoErr("lines")
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	current := common.GetLogFilePath()
	files := []LogFileInfo{}
	for _, path := range common.LogFileFamily(current) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		lines, err := common.ReadLogTail(path, limit)
		if err != nil {
			logs.Error("读取日志文件失败 %s: %v", path, err)
			continue
		}
		files = append(files, LogFileInfo{
			Name:  filepath.Base(path),
			Path:  path,
			Size:  info.Size(),
			Lines: lines,
		})
	}
	s.Data["json"] = map[string]interface{}{
		"status": 1,
		"files":  files,
		"path":   current,
	}
	s.ServeJSON()
	s.StopRun()
}

// Clear 物理删除服务器上的日志文件（当前日志文件 + beego 轮转出来的历史文件）。
//
// 删除当前日志文件时，beego 仍持有它的句柄：
//   - Linux 上删除成功，但后续日志会写进已删除的 inode，需要重启服务才会重新生成文件。
//   - Windows 上文件被占用无法删除，此时退化为清空文件内容。
//
// POST /log/clear
func (s *LogController) Clear() {
	if !s.logAdminOnly() {
		return
	}
	removed := []string{}
	truncated := []string{}
	failed := []string{}
	for _, path := range common.LogFileFamily(common.GetLogFilePath()) {
		name := filepath.Base(path)
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// 删除失败（通常是文件被占用），退化为清空内容。
			if err := truncateFile(path); err != nil {
				failed = append(failed, name+": "+err.Error())
				continue
			}
			truncated = append(truncated, name)
			continue
		}
		removed = append(removed, name)
	}
	logs.Info("日志清理完成: 已删除 %d 个，清空 %d 个，失败 %d 个", len(removed), len(truncated), len(failed))

	s.Data["json"] = map[string]interface{}{
		"status":    1,
		"msg":       "clean success",
		"removed":   removed,
		"truncated": truncated,
		"failed":    failed,
	}
	s.ServeJSON()
	s.StopRun()
}

// truncateFile 清空文件内容但保留文件本身。
func truncateFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}
