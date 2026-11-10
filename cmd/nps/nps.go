package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/daemon"
	"ehang.io/nps/server"
	"github.com/fatih/color"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/install"
	"ehang.io/nps/lib/notify"
	"ehang.io/nps/lib/restart"
	"ehang.io/nps/lib/version"
	"ehang.io/nps/server/connection"
	"ehang.io/nps/server/tool"
	"ehang.io/nps/web/routers"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"

	"github.com/kardianos/service"
)

var (
	level      string
	ver        = flag.Bool("version", false, "show current version")
	confPath   = flag.String("conf_path", "", "set current confPath")
	serverCmd  = flag.Bool("server", false, "NPS管理脚本")
	npsLogPath = flag.String("log_path", "", "nps log path")
)

func main() {

	debug.SetMaxThreads(1000000)

	flag.Parse()
	// init log
	if *ver {
		common.PrintVersion()
		return
	}

	var logPath string
	// *confPath why get null value ?
	for _, v := range os.Args[1:] {
		switch v {
		case "install", "start", "stop", "uninstall", "restart":
			continue
		}
		if strings.Contains(v, "-conf_path=") {
			common.ConfPath = strings.Replace(v, "-conf_path=", "", -1)
		}

		if strings.Contains(v, "-log_path=") {
			logPath = strings.Replace(v, "-log_path=", "", -1)
		}
	}

	if *serverCmd {
		_ = logs.SetLogger(logs.AdapterConsole, `{"level":7,"color":true}`)
		printSlogan()
		inputCmd()
		return
	}

	// auto-generate default config files if not exist
	initConfig(filepath.Join(common.GetRunPath(), "conf"))

	if err := beego.LoadAppConfig("ini", filepath.Join(common.GetRunPath(), "conf", "nps.conf")); err != nil {
		log.Fatalln("load config file error", err.Error())
	}

	// 首次启动/升级时，把 nps.conf 中可热更新的参数迁移到 global.json。
	// 已通过 Web 全局参数页面保存过的值不会被覆盖。
	file.GetDb().MigrateGlobalFromAppConfig()

	common.InitPProfFromFile()
	if level = beego.AppConfig.String("log_level"); level == "" {
		level = "7"
	}
	logs.Reset()
	logs.EnableFuncCallDepth(true)
	logs.SetLogFuncCallDepth(3)

	if logPath == "" {
		logPath = beego.AppConfig.String("log_path")
		if logPath == "" {
			logPath = common.GetLogPath()
		}
	}

	// 加载「访问地区限制 → 仅允许指定省份」用的 IP 库（conf/ip2region_v4.xdb）。
	// 库文件独立于二进制，可通过后台上传更新；缺失时只有省份模式不可用。
	if err := file.GetDb().LoadProvinceDb(); err != nil {
		logs.Warn("加载 IP 库失败，「仅允许指定省份访问」模式暂不可用:", err.Error())
	}

	// init service
	options := make(service.KeyValue)

	svcConfig := &service.Config{
		Name:        "Nps",
		DisplayName: "nps内网穿透代理服务器",
		Description: "一款轻量级、功能强大的内网穿透代理服务器。支持tcp、udp流量转发，支持内网http代理、内网socks5代理，同时支持snappy压缩、站点保护、加密传输、多路复用、header修改等。支持web图形化管理，集成多用户模式。",
		Option:      options,
	}

	bridge.ServerTlsEnable = beego.AppConfig.DefaultBool("tls_enable", false)

	for _, v := range os.Args[1:] {
		switch v {
		case "install", "start", "stop", "uninstall", "restart":
			continue
		}
		svcConfig.Arguments = append(svcConfig.Arguments, v)
	}

	svcConfig.Arguments = append(svcConfig.Arguments, "service")
	if len(os.Args) > 1 && os.Args[1] == "service" {
		_ = logs.SetLogger(logs.AdapterFile, fileLoggerConfig(level, logPath))
	} else {
		_ = logs.SetLogger(logs.AdapterConsole, `{"level":`+level+`,"color":true}`)
	}
	// 记录日志文件路径，供 Web 端日志页面读取与清理。
	common.SetLogFilePath(logPath)
	if !common.IsWindows() {
		svcConfig.Dependencies = []string{
			"Requires=network.target",
			"After=network-online.target syslog.target"}
		svcConfig.Option["SystemdScript"] = install.SystemdScript
		svcConfig.Option["SysvScript"] = install.SysvScript
	}
	prg := &nps{}
	prg.exit = make(chan struct{})
	s, err := service.New(prg, svcConfig)
	if err != nil {
		logs.Error(err, "service function disabled")
		run()
		// run without service
		wg := sync.WaitGroup{}
		wg.Add(1)
		wg.Wait()
		return
	}

	if len(os.Args) > 1 && os.Args[1] != "service" {
		switch os.Args[1] {
		case "reload":
			daemon.InitDaemon("nps", common.GetRunPath(), common.GetTmpPath())
			return
		case "install":
			// uninstall before
			_ = service.Control(s, "stop")
			_ = service.Control(s, "uninstall")

			binPath := install.InstallNps()
			svcConfig.Executable = binPath
			s, err := service.New(prg, svcConfig)
			if err != nil {
				logs.Error(err)
				return
			}
			err = service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}
			return
		case "start", "restart", "stop":
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				cmd := exec.Command("/etc/init.d/"+svcConfig.Name, os.Args[1])
				err := cmd.Run()
				if err != nil {
					logs.Error(err)
				}
				return
			}
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			return
		case "uninstall":
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}
			return
		case "update":
			if updated, err := install.UpdateNps(); err != nil {
				logs.Error("更新失败：", err)
			} else if updated {
				restartNpsService(s)
			}
			return
			//default:
			//	logs.Error("command is not support")
			//	return
		}
	}

	_ = s.Run()
}

// fileLoggerConfig 生成 beego 文件日志的 JSON 配置。
//
// 文件名必须用 strconv.Quote 转义：Windows 上的绝对路径含有反斜杠，
// 直接拼进 JSON 会让反序列化失败，beego 初始化文件 logger 静默失败
// （错误被上层丢弃），表现为日志文件根本不生成。
func fileLoggerConfig(level, filename string) string {
	return `{"level":` + level + `,"filename":` + strconv.Quote(filename) +
		`,"daily":false,"maxlines":100000,"color":true}`
}

func printSlogan() {
	confFile := ""
	for _, dir := range []string{common.GetAppPath(), common.GetRunPath()} {
		if p := filepath.Join(dir, "conf", "nps.conf"); common.FileExists(p) {
			confFile = p
			break
		}
	}
	configLoaded := false
	if confFile != "" {
		if err := beego.LoadAppConfig("ini", confFile); err == nil {
			configLoaded = true
		}
	}

	green := color.New(color.FgGreen).SprintFunc()
	w := color.Output

	fmt.Fprintf(w, "%s", green(""))

	fmt.Fprintf(w, "\033[32;0m###########################################################\n")
	fmt.Fprintf(w, "\033[32;0m#                   \033[31mNPS内网穿透服务端\033[0m                     #\n")
	fmt.Fprintf(w, "\033[32;0m#                                                         #\n")
	fmt.Fprintf(w, "\033[32;0m#\033[32m 地址：\033[31;0mhttps://github.com/yisier/nps\033[0m                     #\n")
	fmt.Fprintf(w, "\033[32;0m#\033[32m 提示：\033[32;0m1、涉及到系统服务的需要以管理员/root身份运行\033[0m      #\n")
	fmt.Fprintf(w, "\033[32;0m#\033[32m       \033[32;0m2、安装/卸载/更新请选择对应的菜单项\033[0m               #\n")
	fmt.Fprintf(w, "\033[32;0m#\033[32m       \033[32;0m3、启动/停止/重启操作的是系统服务\033[0m                 #\n")
	fmt.Fprintf(w, "\033[32;0m###########################################################\n")
	fmt.Fprintf(w, "\033[0m") // 重置颜色

	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "\u001B[32m输入 [1]\u001B[0m - 安装 NPS\n")
	fmt.Fprintf(w, "\u001B[32m输入 [2]\u001B[0m - 卸载 NPS\n")
	fmt.Fprintf(w, "\u001B[32m输入 [3]\u001B[0m - 更新 NPS\n")
	fmt.Fprintf(w, "---------------------\n")
	fmt.Fprintf(w, "\u001B[32m输入 [4]\u001B[0m - 启动 NPS\n")
	fmt.Fprintf(w, "\u001B[32m输入 [5]\u001B[0m - 停止 NPS\n")
	fmt.Fprintf(w, "\u001B[32m输入 [6]\u001B[0m - 重启 NPS\n")
	fmt.Fprintf(w, "---------------------\n")
	fmt.Fprintf(w, "\u001B[32m输入 [0]\u001B[0m - 退出脚本\n")
	fmt.Fprintf(w, "---------------------\n")

	status := npsServiceStatus()
	fmt.Fprintf(w, "\u001B[32m当前版本：v%s\u001B[0m\n", version.VERSION)
	fmt.Fprintf(w, "\u001B[32m服务状态：%s\u001B[0m\n", status)
	// 管理面板地址仅当服务已启动时显示
	if status == "运行中" {
		fmt.Fprintf(w, "\u001B[32m管理面板：%s\u001B[0m\n", npsPanelAddr(configLoaded))
	}
	fmt.Fprintf(w, "\n")
}

// npsServiceStatus 返回 Nps 系统服务的运行状态描述。
func npsServiceStatus() string {
	prg := &nps{exit: make(chan struct{})}
	s, err := service.New(prg, &service.Config{Name: "Nps"})
	if err != nil {
		return "未知"
	}
	st, err := s.Status()
	if err != nil {
		return "未安装"
	}
	switch st {
	case service.StatusRunning:
		return "运行中"
	case service.StatusStopped:
		return "已停止"
	}
	return "未知"
}

// npsPanelAddr 返回管理面板的访问地址（基于当前配置）。
// configLoaded 为 false 表示未找到配置文件，无法判断面板地址。
func npsPanelAddr(configLoaded bool) string {
	if !configLoaded {
		return "未找到配置文件 conf/nps.conf"
	}
	port := strings.TrimSpace(beego.AppConfig.String("web_port"))
	if port == "" {
		return "未配置 web_port（管理面板已关闭）"
	}
	scheme := "http"
	if beego.AppConfig.DefaultBool("web_open_ssl", false) {
		scheme = "https"
	}
	host := strings.TrimSpace(beego.AppConfig.String("web_host"))
	if host == "" || host == "a.o.com" {
		host = "127.0.0.1"
	}
	base := strings.TrimSuffix(beego.AppConfig.String("web_base_url"), "/")
	return scheme + "://" + host + ":" + port + base
}

// restartNpsService 在更新完成后自动重启已安装的 Nps 系统服务，使新版本生效。
// 未安装为系统服务时不改动当前进程，仅提示手动重启。
func restartNpsService(s service.Service) {
	if _, err := s.Status(); err != nil {
		fmt.Println("未检测到已安装的 Nps 服务，请手动重启 nps 使新版本生效")
		return
	}
	if err := service.Control(s, "restart"); err != nil {
		fmt.Println("Nps 服务自动重启失败，请手动重启：", err)
		return
	}
	fmt.Println("Nps 服务已自动重启，新版本已生效")
}

func inputCmd() {
	var flag string
	fmt.Print("请输入[0-6]：")

	stdin := bufio.NewReader(os.Stdin)
	_, err := fmt.Fscanln(stdin, &flag)
	if err != nil {
		fmt.Println("输入有误")
	} else {
		if flag == "0" {
			os.Exit(0)
		}

		// init service

		prg := &nps{
			exit: make(chan struct{}),
		}
		options := make(service.KeyValue)
		svcConfig := &service.Config{
			Name:        "Nps",
			DisplayName: "nps",
			Description: "这是NPS服务，没有描述...",
			Option:      options,
		}
		s, _ := service.New(prg, svcConfig)

		switch flag {
		case "1":
			// uninstall before
			_ = service.Control(s, "stop")
			_ = service.Control(s, "uninstall")
			binPath := install.InstallNpsToCurrentDir()

			// Ensure conf exists (same as normal startup), then load it for display.
			// Previously LoadAppConfig error was ignored and web_port could be empty,
			// printing "127.0.0.1:" with no port. See #317.
			confDir := filepath.Join(common.GetAppPath(), "conf")
			initConfig(confDir)
			if err := beego.LoadAppConfig("ini", filepath.Join(confDir, "nps.conf")); err != nil {
				fmt.Println("加载配置文件失败：", err)
				break
			}

			logPath := filepath.Join(common.GetAppPath(), "nps.log")
			if common.IsWindows() {
				logPath = strings.Replace(logPath, "\\", "\\\\", -1)
			}
			svcConfig.Arguments = append(svcConfig.Arguments, "service")
			svcConfig.Arguments = append(svcConfig.Arguments, "-conf_path="+common.GetAppPath())
			svcConfig.Arguments = append(svcConfig.Arguments, "-log_path="+logPath)

			fmt.Println("日志文件路径为：", logPath)

			svcConfig.Executable = binPath
			s, err := service.New(prg, svcConfig)

			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}

			err = service.Control(s, "install")
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			} else {
				fmt.Println("NPS服务安装成功")
			}

			err = service.Control(s, "start")
			if err != nil {
				fmt.Println("启动NPS服务失败", err)
			} else {
				webPort := beego.AppConfig.String("web_port")
				if webPort == "" {
					fmt.Println("NPS服务已启动（未配置 web_port，管理面板已关闭）")
				} else {
					scheme := "http"
					if beego.AppConfig.DefaultBool("web_open_ssl", false) {
						scheme = "https"
					}
					fmt.Println("NPS服务已启动，管理面板访问地址：" + scheme + "://127.0.0.1:" + webPort)
				}
			}

			break
		case "2":
			// 卸载系统服务
			err := service.Control(s, "stop")
			if err != nil {
				fmt.Println("NPS服务停止失败", err)
			} else {
				fmt.Println("NPS服务已停止")
			}

			err = service.Control(s, "uninstall")
			if err != nil {
				logs.Error("NPS服务卸载失败")
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}

			if err == nil {
				fmt.Println("NPS服务已卸载成功")
			}
			break
		case "3":
			if updated, err := install.UpdateNps(); err != nil {
				fmt.Println("更新失败：", err)
			} else if updated {
				restartNpsService(s)
			}
			break
		case "4":
			// 启动 NPS
			err := service.Control(s, "start")
			if err != nil {
				fmt.Println("NPS服务启动失败", err)
			} else {
				fmt.Println("NPS服务启动成功")
			}

			break
		case "5":
			// 停止 NPS
			err := service.Control(s, "stop")
			if err != nil {
				fmt.Println("NPS服务停止失败", err)
			} else {
				fmt.Println("NPS服务停止成功")
			}

			break
		case "6":
			// 重启 NPS
			err := service.Control(s, "restart")
			if err != nil {
				fmt.Println("NPS服务重启失败", err)
			} else {
				fmt.Println("NPS服务重启成功")
			}

			break
		}
	}

	inputCmd()
}

func installNps() {

}

type nps struct {
	exit chan struct{}
}

func (p *nps) Start(s service.Service) error {
	_, _ = s.Status()
	go p.run()
	return nil
}
func (p *nps) Stop(s service.Service) error {
	_, _ = s.Status()
	close(p.exit)
	if service.Interactive() {
		os.Exit(0)
	}
	return nil
}

func (p *nps) run() error {
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			logs.Warning("nps: panic serving %v: %v\n%s", err, string(buf))
		}
	}()
	run()
	select {
	case <-p.exit:
		logs.Warning("stop...")
	}
	return nil
}

// startAutoRestartScheduler 启动定时重启调度器。
//
// 配置来自全局参数（热更新），因此用户在 Web 后台改完立即生效。
// 到点前 5 分钟推送 Bark 提醒，到点执行重启：先把内存中的 JSON 数据落盘一次，
// 再复用 Web 面板「重启服务」的同一套重启逻辑。
func startAutoRestartScheduler() {
	scheduler := restart.NewScheduler(
		func() restart.Schedule {
			settings := file.GetDb().GetGlobalSettings()
			schedule, ok := restart.ParseSchedule(settings.AutoRestartEnabled,
				settings.AutoRestartTime, settings.AutoRestartWeekdays)
			if !ok {
				return restart.Schedule{}
			}
			return schedule
		},
		func(runAt time.Time) {
			logs.Info("定时重启将在 %s 执行，提前 5 分钟提醒", runAt.Format("2006-01-02 15:04"))
			notify.NotifyScheduledRestart(runAt)
		},
		func(runAt time.Time) {
			logs.Info("定时重启开始执行：%s", runAt.Format("2006-01-02 15:04"))
			// 重启前把内存中的 JSON 数据落盘一次，
			// 避免 flowSession 周期性持久化间隔内的流量统计丢失。
			file.GetDb().JsonDb.StoreHostToJsonFile()
			file.GetDb().JsonDb.StoreTasksToJsonFile()
			file.GetDb().JsonDb.StoreClientsToJsonFile()
			file.GetDb().JsonDb.StoreGlobalToJsonFile()
			if err := restart.Do(); err != nil {
				logs.Error("定时重启失败:", err)
			}
		},
	)
	scheduler.Start()
}

func run() {
	routers.Init()
	task := &file.Tunnel{
		Mode: "webServer",
	}
	bridgePort, err := beego.AppConfig.Int("bridge_port")
	if err != nil {
		logs.Error("Getting bridge_port error", err)
		os.Exit(0)
	}

	logs.Info("日志路径：" + *npsLogPath)
	logs.Info("the config path is:" + common.GetRunPath())
	logs.Info("the version of server is %s ,allow client core version to be %s,tls enable is %t", version.VERSION, version.GetVersion(), bridge.ServerTlsEnable)
	connection.InitConnectionService()
	//crypt.InitTls(filepath.Join(common.GetRunPath(), "conf", "server.pem"), filepath.Join(common.GetRunPath(), "conf", "server.key"))
	crypt.InitTls()
	tool.StartSystemInfo()
	timeout, err := beego.AppConfig.Int("disconnect_timeout")
	if err != nil {
		timeout = 60
	}
	go server.StartNewServer(bridgePort, task, beego.AppConfig.String("bridge_type"), timeout)
	// 启动定时重启调度器（配置来自全局参数，可热更新）。
	startAutoRestartScheduler()
}

func initConfig(confDir string) {
	if !common.FileExists(confDir) {
		os.MkdirAll(confDir, 0755)
	}
	confPath := filepath.Join(confDir, "nps.conf")
	confExisted := common.FileExists(confPath)
	if !confExisted {
		webPassword := crypt.GetRandomString(8)
		authKey := crypt.GetRandomString(8)
		authCryptKey := crypt.GetRandomString(16)
		content := strings.Replace(defaultNpsConf, "web_password=123", "web_password="+webPassword, 1)
		content = strings.Replace(content, "auth_key=123", "auth_key="+authKey, 1)
		content = strings.Replace(content, "auth_crypt_key =213", "auth_crypt_key ="+authCryptKey, 1)
		f, err := os.Create(confPath)
		if err != nil {
			return
		}
		defer f.Close()
		f.WriteString(content)
		logs.Info("Auto-generated default config file:", confPath)
		logs.Info("Web login username: admin, password:", webPassword)
		logs.Info("auth_key:", authKey)
		logs.Info("auth_crypt_key:", authCryptKey)
	}
	// 全新安装直接生成默认 global.json；升级场景交给 MigrateGlobalFromAppConfig
	// 从已有的 nps.conf 迁移，避免覆盖用户原有配置。
	if !confExisted {
		initGlobalConfig(confDir)
	}
}

// initGlobalConfig 生成默认的 conf/global.json。
// 可热更新的开关已从 nps.conf 迁移到这里，首次启动即生效。
func initGlobalConfig(confDir string) {
	globalPath := filepath.Join(confDir, "global.json")
	if common.FileExists(globalPath) {
		return
	}
	if err := os.WriteFile(globalPath, []byte(defaultGlobalJson), 0644); err != nil {
		logs.Error("Auto-generated default global config file error:", err)
		return
	}
	logs.Info("Auto-generated default global config file:", globalPath)
}

const defaultGlobalJson = `{"BlackIpList":[],"ServerUrl":"","AllowPorts":"","IpLimit":false,"ShowHttpProxyPort":true,"AllowFlowLimit":true,"AllowRateLimit":true,"AllowConnectionNumLimit":true,"AllowTunnelNumLimit":true,"AllowMultiIp":true,"AllowLocalProxy":false,"AllowUserLogin":true,"AllowUserRegister":false,"AllowUserChangeUsername":true,"OpenCaptcha":false,"SystemInfoDisplay":true,"HttpAddOriginHeader":true,"BlockForeignIp":false,"AutoBlockForeignIp":false,"BarkEnabled":false,"BarkUrl":"","BarkQuietStart":"","BarkQuietEnd":"","HiddenMenus":[]}`

const defaultNpsConf = `http_proxy_ip=0.0.0.0
http_proxy_port=80
https_proxy_port=443

bridge_type=tcp
bridge_port=8024
bridge_ip=0.0.0.0

public_vkey=123

flow_store_interval=1

log_level=6
log_path=nps.log

web_host=a.o.com
web_username=admin
web_password=123
web_port = 8081
web_ip=0.0.0.0
web_base_url=
web_open_ssl=false
web_cert_file=conf/server.pem
web_key_file=conf/server.key

auth_key=123
auth_crypt_key =213

http_cache=false
http_cache_length=100

disconnect_timeout=60

tls_enable=true
tls_bridge_port=8025
`
