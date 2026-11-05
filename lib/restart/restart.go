package restart

import (
	"os"

	"github.com/kardianos/service"
)

// Do 重启当前 nps 服务端进程，按运行方式自动选择策略：
//
//  1. 系统服务模式（命令行带 service 参数，由 SCM / systemd 拉起）：
//     交给服务管理器重启（service.Control "restart"），停旧起新由管理器保证顺序；
//  2. 控制台模式：原地自重启（见各平台 selfRestart 实现）。
//
// 注意：调用方应先把 JSON 数据落盘，并在 HTTP 响应送达浏览器之后再调用本函数，
// 否则进程退出会导致响应中断。
func Do() error {
	if isServiceProcess() {
		return restartViaService()
	}
	return selfRestart()
}

// isServiceProcess 判断当前进程是否作为系统服务运行。
// cmd/nps 安装服务时会给服务命令行追加 "service" 参数（svcConfig.Arguments），
// 因此只要参数里出现该 token，即可认定本进程由服务管理器拉起。
func isServiceProcess() bool {
	for _, arg := range os.Args[1:] {
		if arg == "service" {
			return true
		}
	}
	return false
}

// stubProgram 满足 service.Interface 的空实现，
// 仅用于构造 service.Service 以便向服务管理器发送控制指令。
type stubProgram struct{}

func (p *stubProgram) Start(s service.Service) error { return nil }
func (p *stubProgram) Stop(s service.Service) error  { return nil }

// restartViaService 通过系统服务管理器重启 Nps 服务。
// 服务未安装时退化为原地自重启（例如手动运行 `nps service` 的情况）。
func restartViaService() error {
	svc, err := service.New(&stubProgram{}, &service.Config{Name: "Nps"})
	if err != nil {
		return err
	}
	if _, err := svc.Status(); err != nil {
		return selfRestart()
	}
	return service.Control(svc, "restart")
}
