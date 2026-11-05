//go:build !windows
// +build !windows

package restart

import (
	"os"
	"path/filepath"
	"syscall"
)

// selfRestart 原地重启：用 execve 把当前进程替换为同一二进制的新镜像。
//
//  - PID 不变，对 systemd 等管理器完全透明（仍在同一 cgroup）；
//  - Go 创建的监听 socket 默认带 CLOEXEC，exec 时自动关闭，
//    新镜像启动后重新绑定端口，不会出现端口占用冲突；
//  - os.Args / 环境变量 / 工作目录全部原样保留。
func selfRestart() error {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	// /proc/self/exe 等场景下解析出真实路径，避免 exec 一个软链接本身
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
