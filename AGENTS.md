# NPS 内网穿透 — 代码库导览

基于 `ehang-io/nps` 0.26.41 二次开发（github.com/yisier/nps）。Go 1.24，Web 面板用 Beego v1.12。

## 分层结构

| 目录 | 职责 |
|---|---|
| `cmd/nps/` | 服务端入口：系统服务安装/启停、`-server` 交互式菜单、默认配置生成 |
| `cmd/npc/` | 客户端入口：`npc.go`（CLI/服务）、`sdk.go`（嵌入式 SDK）、`npc-gui/`（Wails3 桌面版） |
| `gui/npc/` | Fyne 版简易 GUI 客户端（Android 可用） |
| `bridge/` | 握手校验与按连接类型分发（`WORK_MAIN`/`WORK_CHAN`/`WORK_CONFIG`/`WORK_REGISTER`/`WORK_SECRET`/`WORK_FILE`/`WORK_P2P`） |
| `server/` | 服务端编排 |
| `server/proxy/` | 各代理模式：`tcp.go` `http.go` `udp.go` `socks5.go` `p2p.go` `https.go` `websocket.go` `base.go` |
| `server/connection/` | 各类监听器（bridge / web / http / https） |
| `server/tool/` | 端口检测、系统信息采集、IO 速率采集 |
| `client/` | 客户端核心：`client.go`（TRPClient）、`control.go`（握手+配置推送）、`local.go`、`register.go`、`health.go` |
| `lib/` | `nps_mux` 多路复用 · `pmux` 端口复用 · `conn` 连接封装/加密/压缩/限速 · `crypt` TLS+摘要 · `file` 数据模型与 JSON 持久化 · `config` 客户端 INI 解析 · `rate` 限速 · `cache` LRU · `goroutine` 协程池 · `install` `daemon` `common` |
| `web/` | Beego 管理面板：`controllers/` `routers/` `views/` `static/`（静态资源经 embed 打包） |

## 连接与转发流程

**握手**（`bridge/bridge.go:cliProcess` → `client/control.go:NewConn`）：

```
CONN_TEST(3B) → 核心版本(len32+content) → 客户端版本 → 服务端回 md5(核心版本)
→ vkey(32B) → 服务端校验(verifyError/verifySuccess) → 连接类型 flag → typeDeal
```

握手超时 10s；版本不匹配仍兼容放行。

**数据转发**：

```
外部请求 → server/proxy 各模式 → bridge.SendLinkInfo(link) → 客户端 mux stream
→ 客户端 handleChan → Dial 目标地址 → 双向拷贝
```

加密（TLS）/压缩（snappy）/限速统一在 `lib/conn/conn.go:GetConn` 包装层实现。

## 禁止国外 IP 访问

Web 后台「全局参数 → 安全设置 → 禁止国外 IP 访问」开关（`global.json` 的 `BlockForeignIp`），保存后立即生效，无需重启。

- **判定库**：`lib/geo/china_ip_list.txt`（内置 6217 条中国大陆 CIDR，约 96KB，随二进制打包）。
  数据来源 `github.com/gaoyifan/china-operator-ip`，仅含 IPv4。
- **匹配算法**：`lib/geo/geo.go` 启动时把 CIDR 归一化为 uint32 区间并按 start 升序排列，查询走二分查找（O(log n)）。
- **判定规则**：
  - IPv4 命中中国 CIDR → 境内，放行；否则 → 境外，拦截。
  - 私有/保留地址（10.x、172.16-31.x、192.168.x、127.x、169.254.x、100.64-127.x、0.x、224.x+）→ 一律放行。
  - IPv6 → 一律放行（内置库不含 IPv6，避免误杀）。
- **拦截点**（`server/proxy/`）：
  - `base.go:IsForeignIp()` 为统一判定入口；`DealClient` 覆盖 tcp / httpProxy / socks5 / tcpTrans。
  - `http.go:handleHttp` 覆盖域名模式（http/https）。
  - `udp.go` 覆盖 UDP 转发。
  - `websocket.go:HttpReverseProxy.ServeHTTP` 覆盖 WebSocket（该链路不经过 `DealClient`，需单独判定）。
- **注意**：`net.ParseIP` / `net.IPv4` 返回 16 字节表示，IPv4 字节在 12-15 位。
  自行取字节时必须先 `To4()`，否则会读到全零前缀。掩码计算也不能写成 `^uint32(0) << n`
  （常量溢出），必须先赋给变量再移位。

## 流量统计与重置

- 三层计数器：`client.Flow`（客户端总量）、`task.Flow`（隧道）、`host.Flow`（域名解析），
  由 `lib/goroutine/pool.go:CopyBuffer` 在转发时同时累加到三层。
- `lib/file/obj.go:Flow` 内嵌 `sync.RWMutex`，`Add()` / `Reset()` 用同一把写锁，避免与运行中的累加产生数据竞争。
- **重置流量**：Web 面板客户端列表「重置流量」按钮 → `web/controllers/client.go:ResetFlow()`
  → `client.Flow.Reset()`（清零 `ExportFlow`/`InletFlow`，保留 `FlowLimit`）→ 立即
  `StoreClientsToJsonFile()` 落盘。该接口**仅管理员可用**：客户端配额是按 `client.Flow` 累计的
  （`server/proxy/base.go:CheckFlowAndConnNum` 检查 `FlowLimit<<20 < ExportFlow+InletFlow`），
  若允许普通客户端自行重置即可绕过流量上限。
- 前端：`web/views/client/list.html` 的 option 列 + `web/static/js/language.js:submitform()`
  的 `case 'resetflow'`（走 confirm 确认弹窗，成功后 reload）；文案在 `web/static/page/languages.xml`
  的 `word-resetflow` / `confirm/resetflow` / `reply/*` 中维护。


**连接类型**：`main`（信号/心跳）、`chan`（mux 数据隧道）、`conf`（配置下发）、`rgst`（IP 注册）、`sert`（secret 直连）、`file`（文件传输）、`p2pm`（P2P 打洞）。

## 代理模式

`tcp` · `udp` · `httpProxy`（正向代理）· `socks5` · `file` · `secret` · `p2p` · `httpHostServer`（域名解析，内置 Web 面板）· `tcpTrans`

- `httpHostServer` 的路由匹配在 `lib/file/db.go:GetInfoByHost`：Host 精确/泛解析匹配 + `Location` 最长前缀优先。
- UDP 转发（`server/proxy/udp.go`）用「原子占位」模式：同一 src addr 只建一条 mux stream，输家阻塞在 `ready` 通道复用会话，避免 `NowConn` 配额泄漏。
- P2P（`server/proxy/p2p.go` + `client/control.go`）三端口探测 + 端口区间打洞。

## 配置与存储

- `conf/nps.conf`（INI，beego 读取）：bridge/http/p2p/web/日志等启动参数。
- `conf/global.json`：可热更新的全局参数（黑名单、限流开关、用户注册开关等）。首次启动自动生成；升级时由 `file.GetDb().MigrateGlobalFromAppConfig()` 从 nps.conf 迁移，不覆盖用户已保存值。
- `conf/clients.json` / `tasks.json` / `hosts.json`：业务数据持久化。
- ID 分配：持久化记录用自增 ID；`NoStore` 临时记录使用独立的**负数 ID 空间**（`getTransientClientId` 等），不污染自增序列。分配与删除分别由 `clientIdLock`/`taskIdLock`/`hostIdLock` 保护。

## 端口约定

`80`/`443` 域名解析 · `8024` 网桥 · `8025` TLS 网桥 · `8081` Web 面板 · `6000+` P2P

## 构建与测试

```bash
make build   # go build cmd/nps/nps.go && go build cmd/npc/npc.go
make test    # go test -race -coverpkg=./... ./...
make lint    # golangci-lint + misspell
```

`server/proxy/tcp.go` 带 `//go:build !npcgui` 标签，`tcp_npcgui.go` 为桌面版分支。

### 本机 Go 环境（Windows）

- 安装：winget `GoLang.Go` 版本 `1.24.6`，装到 `C:\Program Files\Go`，已注册到系统 PATH。
- `go.mod` 声明 `toolchain go1.24.9`，`GOTOOLCHAIN=auto` 会自动下载该工具链到 `~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.24.9.windows-amd64`，无需手动装多个版本。
- 持久化配置在 `C:\Users\gjwl\AppData\Roaming\go\env`：
  `GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct`、`GOSUMDB=sum.golang.google.cn`、`GOTOOLCHAIN=auto`。
- 修改后可用 `go env -w KEY=VALUE` 持久化。

### 已知构建/测试限制（非本次改动引入）

| 问题 | 原因 | 规避 |
|---|---|---|
| `go build ./gui/npc/` 失败 | Fyne 依赖 OpenGL（`go-gl/gl`），需 CGO；本机 `CGO_ENABLED=0` 且无 C 编译器 | 安装 MinGW-w64 并 `set CGO_ENABLED=1`；或跳过该 GUI 包 |
| `cmd/npc/npc-gui` 构建失败 | 独立模块（`go 1.25.0`），需先构建前端产出 `frontend/dist` | 先在 `cmd/npc/npc-gui/frontend` 执行前端构建 |
| `TestDealCommon` 失败 | 既有测试缺陷：用 `*c != *f` 比较含 `*file.Client` 指针的结构体，指针永不相等 | 改为逐字段断言 |
| `lib/nps_mux` 的 `TestServer` 等失败 | 测试依赖 Docker 起容器网络，本机未安装 Docker | 安装 Docker 后运行，或跳过该包 |
| `go vet` 报 unkeyed fields / lock by value | 既有代码风格问题（`net.TCPAddr{...}`、`sync.Map` 值传递） | 不影响编译，可后续批量整改 |

## 注意事项

- Web API 鉴权：`md5(auth_key + timestamp)`，时间戳偏差须 ≤ 20 秒（`web/controllers/base.go:Prepare`）。
- 全局/客户端 IP 黑名单在 `server/proxy/base.go` 与 `udp.go` 两处判定，新增代理模式需一并接入。
- 客户端连接配额由 `Client.GetConn()`/`AddConn()` 维护，超限返回 "Connections exceed the current client limit"。
