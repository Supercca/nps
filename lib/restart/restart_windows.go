//go:build windows
// +build windows

package restart

import (
	"os"
	"os/exec"
)

// selfRestart Windows 没有 exec 系统调用，改为启动一个同路径的子进程接管服务，
// 然后当前进程立即退出释放端口。
//
// 时序：cmd.Start() 返回到子进程真正开始绑定端口之间有几百毫秒的初始化间隔
// （加载配置、JSON 数据库等），而父进程退出只需几毫秒，端口冲突概率可以忽略。
func selfRestart() error {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	// 继承控制台句柄，重启后日志输出行为与重启前一致
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
