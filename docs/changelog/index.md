# 更新日志
- 2026-11-11  v0.26.108
  - 新增：
    - 管理面板新增「登录失败告警」：同一来源 IP 在 60 秒内登录失败达到阈值（全局参数 → 安全设置 → 登录失败告警阈值，默认 5，填 0 表示不告警）时推送一条 Bark 告警；同一轮窗口内只告警一次，登录成功或窗口过期后重新计数
    - 管理面板新增「异地登录告警」：开启「仅允许指定省份访问」后，若面板被一个不在允许省份列表里的来源成功登录，推送一条 Bark 告警；查不到省份归属的情况（内网地址、IPv6、境外地址）不判定也不告警
  - 优化：
    - 面板登录失败计数改为加锁读写：原先用裸字段累加，并发登录会互相覆盖，临时锁定阈值形同虚设
    - 只有显式提交密码的失败才计入失败次数，打开登录页本身不再计入
    - 面板是直接对外暴露的、不经过隧道代理，因此不受「访问地区限制」拦截；上述两类告警只做提醒，不会阻断登录

- 2026-11-10  v0.26.107
  - 优化：
    - 全局黑名单查询由「复制整表 + 就地排序 + 二分查找」改为 map 集合查找：`lib/file` 内部维护一份与列表同步的集合视图，查询入口改为 `DbUtils.IsGlobalBlackIp(ip)`，读锁下一次查找即可
    - 1 万条黑名单时单次查询从约 0.9ms 降到约 21ns（快约 4 万倍）。被扫描时每个连接都要查一次黑名单，UDP 链路更是每包查一次，这一项直接决定了高并发下的 CPU 开销
    - 自动拉黑的去重改为查集合视图，由 O(n) 线性扫描降为 O(1)
    - 查询走读锁，多个连接可以并发查黑名单，不再被一把全局互斥锁串行化
  - 修复：
    - `JsonDb.Global` 指针的替换与读取没有加锁：Web 保存全局参数、自动拉黑与每个连接的读取会并发发生，可能读到撕裂的指针；现由 `JsonDb.globalPtrLock` 保护
  - 测试：
    - 新增全局黑名单的功能测试与并发测试：集合视图与列表的一致性、整体替换 / 保存 / 重启加载后立即生效、并发读写与并发去重
    - 新增基准 `BenchmarkIsGlobalBlackIp` 与对照基准 `BenchmarkIsGlobalBlackIpLegacy`，防止查询路径被改回慢实现

- 2026-11-10  v0.26.106
  - 修复：
    - 拦截日志洪水：访问地区限制的拦截面比「禁止国外 IP」大得多（未勾选省份的国内 IP、全部境外 IP、IP 库未覆盖的段都会命中），UDP 链路更是每收到一个包就判定一次，被扫描时每秒能刷出几千条日志，撑爆日志文件并拖慢日志页轮询
    - 新增拦截日志限流器 `server/proxy/blocklog.go`：按来源 IP 冷却 5 分钟只记一条，冷却期内被拦的次数会累计并在下一条日志里补上（信息不丢）；再加一层全局令牌桶，所有来源加起来每分钟最多记 10 条，被扫描时日志速率彻底封顶
    - 黑名单命中日志同样接入限流（同一 IP 5 分钟一条），避免黑名单被反复命中的时候刷爆日志
    - 境外 IP 自动拉黑的 Bark 推送只在日志真正写出去时才发，避免被扫描时把通知服务打爆
    - 境外 IP 自动加入黑名单增加速率上限（每分钟 60 条）：被扫描时每个新 IP 都要全量写一次 `global.json`，不限制会把磁盘写满并把全局锁拖住；超出上限的 IP 不再进黑名单，但境外判定本身已经拦住了它们，拦截效果不受影响

- 2026-11-10  v0.26.105
  - 优化：
    - IP 库不再打包进服务端二进制：ip2region 离线库改为独立文件 `conf/ip2region_v4.xdb`，服务端二进制体积回到正常水平，官方更新 IP 库也不用重新编译
    - 发布包（`build.sh` 与 `build_*_server.bat`）随包携带一份默认 IP 库，开箱即可使用「仅允许指定省份访问」
  - 新增：
    - 后台新增「上传 IP 库」：全局参数 → 安全设置 → IP 库，可直接上传新的 `ip2region_v4.xdb` 更新本机 IP 库，上传后校验通过即原子落盘并热加载，**无需重启服务**
    - 上传前会在内存里解析校验，文件不是有效的 ip2region 库时直接拒绝，磁盘上的旧库与当前生效的省份表都不受影响；上传接口仅管理员可用
    - 全局参数页展示 IP 库状态：是否已加载、库文件路径、收录的省份数与 IP 区间数、加载时间
  - 修复：
    - 打包脚本 `build_windows_amd64_server.bat` / `build_linux_amd64_server.bat` 里的中文注释会被 cmd.exe 按系统 ANSI 代码页（GBK）解析成乱码，打断脚本解析导致整个脚本跑不起来；注释改为纯 ASCII 英文
    - 打包后的归档内容校验改用 gzip 解压后管道给 tar，避免 GNU tar 在 cmd.exe 下找不到 gzip 而报错

- 2026-11-10  v0.26.104
  - 新增：
    - 「禁止国外 IP 访问」升级为三选一的「访问地区限制」：不限制 / 仅禁止境外 IP / 仅允许指定省份访问，配置在「全局参数 → 安全设置」
    - 支持按省份勾选访问范围：ip2region 离线库（IPv4，34 个省级行政区），勾选的省份以大区（华北 / 华东 / 华南 / 华中 / 西南 / 西北 / 东北）分组展示，支持全选与反选；未勾选任何省份将拒绝全部公网访问，内网地址始终放行
    - 地区限制统一由 `server/proxy/base.go:IsRegionRestricted()` 判定，覆盖 TCP / HTTP / SOCKS5 / UDP / WebSocket 全部分发链路
  - 优化：
    - IP 库在启动后惰性解析并预计算成「IP 段 → 省份」有序表，运行时只做一次二分查找，与原有 CIDR 判定同级，且无共享状态、天然并发安全
    - 地区限制配置使用原子快照下发，避免每来一个连接都复制切片、构造 map
    - 历史配置里的 `BlockForeignIp` 会自动迁移成「仅禁止境外 IP」模式，升级后行为保持不变
    - 前端文案遵循既有 i18n 机制（`langtag` + `languages.xml`），补齐 34 个省级行政区与 7 个大区的中英文词条
  - 注意：
    - IP 库只覆盖 IPv4，IPv6 地址不受省份规则限制；境外 IP 自动加入黑名单仍只在「仅禁止境外 IP」模式下生效

- 2026-11-09  v0.26.103
  - 新增：
    - 全局参数新增「配置备份与恢复」：一键把客户端、隧道、域名解析与全局参数打包成 zip 下载，迁移服务器不必再手工拷贝 `conf` 目录，误删的客户端也能从备份找回
    - 支持上传备份包一键恢复数据：恢复前会自动把当前数据备份到 `conf/backup/` 目录（保留最近 10 份），恢复完成后自动重新加载内存数据
    - 备份包内含 `manifest.json`，记录备份格式版本与生成时的 nps 版本；恢复时校验版本（主版本 / 次版本必须一致）与数据结构（四个数据文件齐全、记录格式合法、隧道与域名引用的客户端存在），校验不通过直接拒绝覆盖，当前数据不受影响
  - 优化：
    - 备份 / 恢复接口仅管理员可用；上传大小与解压条目大小均有限制，防止超大包与压缩炸弹
    - 恢复流程与提示文案遵循既有 i18n 机制（`langtag` + `languages.xml`）

- 2026-11-09  v0.26.102
  - 修复：
    - 点击「重置流量」后，客户端名下的隧道（TCP / UDP）与域名流量未被清零：流量转发时客户端级与隧道 / 域名级是两份独立计数器，此前仅重置了客户端级，导致隧道列表等页面仍显示旧的流量；现在一并重置并落盘，`FlowLimit` 流量上限保持不变

- 2026-11-09  v0.26.101
  - 新增：
    - 全局参数新增「定时重启服务」：可设置每天固定时刻（`HH:MM`）并可选星期（不勾选即每天）；重启前 5 分钟通过 Bark 推送提醒；重启前先落盘统计数据再重启，避免流量统计丢失；配置在 Web 保存后立即生效，无需重启进程
    - Web 面板新增深色主题：顶栏新增主题切换按钮，亮色 / 暗色即时切换并记忆在浏览器本地；首次访问跟随操作系统的深色偏好；登录页、注册页同样支持；首页图表在暗色模式下自动调整坐标轴与图例配色
  - 优化：
    - 全局参数页的定时重启、主题等配置项均遵循既有 i18n 机制（`langtag` + `languages.xml`）

- 2026-11-04  v0.26.100
  - 新增：
    - 全局参数新增「禁止国外IP访问」开关：内置中国境内 IP 段库（6217 条 CIDR），开启后境外 IP 的 TCP / HTTP / SOCKS5 / UDP / WebSocket 反代连接将被拦截；内网地址与 IPv6 一律放行；开关位于「全局参数 → 安全设置」
    - 全局参数新增「境外 IP 自动加入黑名单」开关：需与「禁止国外IP访问」同时开启，判定为境外的 IP 会被自动追加到全局黑名单并立即落盘；黑名单上限 10000 条
    - 全局参数新增「Bark 通知」：每次有 IP 加入全局黑名单时推送一条 Bark 通知，可配置推送地址与免打扰时段（`HH:MM`，跨零点已支持，时段内通知直接丢弃）
    - 全局参数新增「隐藏左侧菜单」：按菜单项勾选隐藏左侧导航中不需要的入口；仅隐藏入口，页面仍可通过网址访问；「全局参数」不可隐藏
    - Web 面板新增「服务端日志」页面：直接读取服务器上的日志文件（`nps.log` 及 beego 轮转出来的历史文件）进行显示，支持关键词/级别过滤、暂停刷新与下载；日志含客户端 vkey，该页面仅管理员可见
    - 服务端日志支持一键清理：物理删除磁盘上的日志文件；无关文件不受影响
    - 客户端列表新增「重置流量」按钮：点击后可将所选客户端 `clients.json` 中的 `ExportFlow`（出口流量）与 `InletFlow`（入口流量）清零，`FlowLimit` 流量上限保持不变；该操作仅管理员可用
  - 优化：
    - `Flow.Reset()` 与 `Flow.Add()` 共用同一把锁，避免与运行中的流量累加产生数据竞争
    - 全局黑名单的读写改由 `lib/file` 统一加锁并传递副本，修复黑名单查找时就地排序共享切片造成的数据竞争
  - 修复：
    - Windows 上以绝对路径指定日志文件时，路径里的反斜杠未做 JSON 转义，导致 beego 的文件日志初始化静默失败、`nps.log` 根本不生成；`log_path` 配置为空时因变量遮蔽同样读不到配置值

- 2026-11-02  v0.26.40
  - 新增：
    - 全局参数支持热更新：`nps.conf` 中的 `allow_ports`、`ip_limit`、`show_http_proxy_port`、`http_add_origin_header`、`allow_flow_limit`、`allow_rate_limit`、`allow_connection_num_limit`、`allow_tunnel_num_limit`、`allow_multi_ip`、`allow_local_proxy`、`allow_user_login`、`allow_user_register`、`allow_user_change_username`、`open_captcha`、`system_info_display` 迁移到 `conf/global.json`，新生成的 `nps.conf` 不再包含这些参数；升级时首次启动自动迁移，Web 后台保存后立即生效；「全局参数」页面同时按基础设置、安全设置、客户端能力、登录与账号、展示与高级分组重构

      <img src="/image/new/setting.png" alt="全局参数" style="max-width: 620px; width: 100%;" />
    - 新增客户端、隧道、域名解析必填校验，并兼容隧道各模式下的动态字段
    - 客户端交互菜单新增「查看系统服务」，可列出本机已注册的 `nps-client-<vkey>` 服务及运行状态；「更新客户端」自动重启已注册的客户端服务

      <img src="/image/new/npc_service.png" alt="查看系统服务" style="max-width: 420px; width: 100%;" />
    - Web 管理首页自动检测服务端新版本，并提示当前版本与最新版本
    - Web 管理端客户端、隧道、域名列表每页条数支持 `20/50/100/200`，并记住用户选择 [#363](https://github.com/yisier/nps/issues/363)
  - 优化：
    - 新增/编辑客户端、隧道、域名解析表单改为紧凑分组布局，减少页面高度并修复字段对齐
    - 优化 `nps_mux.receiveWindowQueue.Pop`，避免并发释放后持续空转 [#364](https://github.com/yisier/nps/pull/364)
  - 修复：
    - 客户端、隧道、域名解析 ID 在删除后继续增长导致跳跃的问题 [#362](https://github.com/yisier/nps/issues/362)

- 2026-10-10  v0.26.39
  - 修复：
    - 同时开启 `web_base_url` 与 `open_captcha` 时，验证码图片路径未跟随 `web_base_url`，反向代理到子路径后无法登录 [#359](https://github.com/yisier/nps/pull/359)
    - Web 面板语言机制：浏览器语言按前缀归一化匹配（`zh*`→`zh-CN`、`en*`→`en-US`），匹配不到时回退默认 `en-US`；用户手动切换语言后长期持久化，不再因关闭浏览器而失效 [#356](https://github.com/yisier/nps/issues/356)
    - SOCKS5（私密代理）UDP ASSOCIATE 回复的中继地址 `BND.ADDR` 优先使用客户端实际连接到的服务端地址，避免外网 IP 探测被运营商 HTTP 劫持返回错误 IP 导致 UDP 不通；外网探测改为 HTTPS 端点并加超时与校验 [#355](https://github.com/yisier/nps/issues/355)
    - 开启 `web_open_ssl=true` 后以系统服务方式启动时 Web 面板无法访问：证书相对路径按进程工作目录解析失败，改为基于 nps 运行目录解析 `web_cert_file` / `web_key_file` [#353](https://github.com/yisier/nps/issues/353)
    - `nps` 服务端升级后自动重启服务 [#347](https://github.com/yisier/nps/issues/347)

- 2026-08-24  v0.26.38
  - 新增：
    - GUI 客户端（npc-gui）迁移到 Wails v3
    - GUI 客户端新增版本自动更新：启动时自动检查 GitHub 最新 release，支持一键下载并热替换当前可执行文件后自动重启  
    ![img.png](/image/new/gui.png)
  - 修复：
    - 客户端内网地址为 192.169.x.x 等非标准私网段时，Web 面板客户端列表不显示本机 IP [#348](https://github.com/yisier/nps/issues/348)

- 2026-08-08  v0.26.37
  - 修复：
    - NPC 安装到系统服务失败 BUG [#338](https://github.com/yisier/nps/issues/338)  
    - 隧道实际未运行但面板提示「启动成功」、运行状态始终为关闭的问题 [#339](https://github.com/yisier/nps/issues/339)
    - 配置 `allow_ports` 后随机生成端口问题 [#339](https://github.com/yisier/nps/issues/339)
    - Web 面板`languages.xml`使用 NPS 版本号作为缓存标识 [#342](https://github.com/yisier/nps/issues/342)
  - 新增：
     - 客户端、隧道、域名解析修改接口支持修改流量数据 [#341](https://github.com/yisier/nps/issues/341)

- 2026-07-12  v0.26.36
  - 新增：
    - 新增客户端内网地址上报功能,客户端列表展示 npc 内网地址 [#333](https://github.com/yisier/nps/issues/333)
    - Web 管理端客户端/隧道/域名列表业务字段支持服务端排序（备注、状态、在线等）[#254](https://github.com/yisier/nps/issues/254)
  - 优化：
    - 不再依赖外部 `web` 目录的静态文件
  - 修复：
    - 客户端/服务端在线更新失败 BUG
    - `nps -server` 安装成功后 `web_port` 未正常显示BUG [#317](https://github.com/yisier/nps/issues/317)
    - Bridge 握手增加 10s 读超时，校验失败/读失败立即关闭连接，减轻端口扫描导致的连接与 goroutine 堆积 [#281](https://github.com/yisier/nps/issues/281)
    - 解决私密代理和P2P连接的TLS启用问题 [#143](https://github.com/yisier/nps/issues/143)
    - HTTP 正向代理 Basic 认证 BUG[#133](https://github.com/yisier/nps/issues/133)
    - SOCKS5 用户名密码认证 BUG [#133](https://github.com/yisier/nps/issues/133)
    - 客户端建连失败后半连接假死、需手动重启 npc BUG [#115](https://github.com/yisier/nps/issues/115)

- 2026-07-04  v0.26.35
  - 新增：
    - 客户端列表、隧道列表、域名解析列表支持多选批量删除 [#287](https://github.com/yisier/nps/issues/287)
  - 修复：
    - HTTP 代理在 SVN 客户端无法正常检出 [#336](https://github.com/yisier/nps/issues/336)
    - 新增/编辑隧道时无提示且不跳转，重复点击会产生多条记录 [#334](https://github.com/yisier/nps/issues/334)
    - mips/mipsle 架构运行出现 Illegal instruction 错误 [#331](https://github.com/yisier/nps/issues/331)
  - 优化：
    - 隧道、域名解析编辑页的客户端 ID 改为下拉选择 [#334](https://github.com/yisier/nps/issues/334)

- 2026-06-04  v0.26.34
  - 新增：
    - `nps -server` 管理脚本欢迎语显示当前版本号,更新版本时比对版本，已是最新则直接退出，避免无谓覆盖 [#328](https://github.com/yisier/nps/issues/328)
    - `npc` 管理脚本欢迎语显示当前版本号，新增 `[5] 更新客户端` 选项，更新并替换 npc 二进制
    - 客户端新增到期时间字段，创建/修改客户端时可选填写，到期后自动暂停客户端 [#322](https://github.com/yisier/nps/issues/322)
  - 修复：
    - Dashboard IO 速率采集阻塞请求 500ms，改为后台采集缓存，消除 Sleep 等待
    - ServerStatus 切片并发读写不安全，添加 RWMutex 保护
    - flowSession goroutine 泄漏，改为仅在 StartNewServer 启动一次
    - GetTunnel 双重遍历 sync.Map 性能问题，合并为单次 Range
    - Bridge Client 字段并发读写不安全，添加 Mutex 保护
    - TLS ClientHello recordLen 无上限，添加 16KB 限制防止内存耗尽
    - SOCKS5 地址解析未检查 Read 返回值，改用 io.ReadFull
    - 文件存储 panic 改为错误日志，defer 清理临时文件防止泄漏
    - GenerateServerPort 可能无限循环，限制 1000 次重试
    - P2P UDP 无超时永久阻塞，添加 30s ReadDeadline
    - P2P goroutine 退出控制改用 context，修复潜在泄漏
    - 客户端 UDP ReadFrom 错误后未退出，修复无效数据继续处理
    - [#324](https://github.com/yisier/nps/issues/324) 隧道/域名解析/UDP 流量始终为 0
    - muxPackager buffer pool 泄漏：UnPack() 错误路径未归还 windowBuff 导致内存持续增长
    - bridge 客户端重连时旧 WORK_CHAN/WORK_FILE mux 未关闭，底层连接和 goroutine 永不释放
    - pmux 连接超时泄漏：process() channel 发送超时后连接未关闭，且 ACCEPT_TIME_OUT 单位错误（10ns 应为 10s）
    - pmux 关闭流程 panic：加 done channel + WaitGroup 保证 Close() 等所有 process() 退出后再 close conn channel；process() 入口设 ReadDeadline 防止阻塞读导致 wg.Wait() 死锁；PortListener 加 done channel 唤醒 Accept()
  - 优化：ioutil.WriteFile → os.WriteFile、rand.Seed → rand.New 本地随机源

- 2026-05-23  v0.26.33
  - 新增：
    - 配置文件自动生成：启动时若 conf 目录或 nps.conf 不存在，自动创建并写入默认配置，方便 Docker 部署
    - 首次启动默认 web_username/admin、web_password、auth_key、auth_crypt_key 均改为随机生成，并打印到终端，提升安全性
    - Web 静态文件打包进可执行文件 + 静态文件 URL 加入版本号参数，升级后自动刷新缓存，部署无需单独拷贝 web 目录
    - 精简发布包：去除打包时冗余的 conf/nps.conf、web 目录
    - 清理失效配置项：移除 appname、runmode、https_default_cert_file、https_default_key_file、https_just_proxy
  - 修复：
    - [#184](https://github.com/yisier/nps/issues/184) 限速导致隧道中断：UpdateClient 在 RateLimit=0 时创建零速率令牌桶导致永久阻塞
    - [#159](https://github.com/yisier/nps/issues/159) 关闭隧道端口仍可用：StopServer 错误时提前返回未更新状态 + 重复 OpenTask case 导致隧道被自动重启
    - [#170](https://github.com/yisier/nps/issues/170) TCP 负载均衡只有一个后端可达：GetRandomTarget 未处理 \r 换行符和尾部空行
    - [#319](https://github.com/yisier/nps/issues/319) 客户端列表不显示实时网速：NowRate 计算逻辑错误，改为统计每秒实际消耗字节数
    - [#306](https://github.com/yisier/nps/issues/306) [#316](https://github.com/yisier/nps/issues/316) TCP Basic 认证与域名解析冲突：移除 TCP 隧道 Basic 认证探测，域名解析继续使用客户端 Basic 认证

- 2026-03-27  v0.26.32
  - 修复：
    - 客户端注册参数未正确处理[快捷启动命令]和[TLS快捷启动命令] [#303](https://github.com/yisier/nps/issues/303)
    - 修复https反向代理bug，感谢[okawao](https://github.com/okawao) 的 PR

- 2026-03-23  v0.26.31
  - 新增：
    - 域名解析记录开关功能 [#314](https://github.com/yisier/nps/issues/314)
    - TCP隧道增加Basic认证 [306](https://github.com/yisier/nps/issues/306)
  - 修复：
    - 新增隧道出现secret mode keys must be unique [301](https://github.com/yisier/nps/issues/301)

- 2026-03-12  v0.26.30
  - 修复：
    - http 协议 websocket 错误，感谢[xiaozonglong](https://github.com/xiaozonglong) 提供的 home assistant 环境 ,[#268](https://github.com/yisier/nps/issues/268),[#296](https://github.com/yisier/nps/issues/296)
    - 域名解析中，主机url点击无法跳转非80端口 [311](https://github.com/yisier/nps/issues/311)
    - 新增的主机配置页面，客户端ID异常 [#310](https://github.com/yisier/nps/issues/310)
    - 自动 HTTPS(301) 按照 `https_proxy_port` 配置的端口号跳转

- 2026-01-24 v0.26.29
  - 新增：
    - GUI 客户端，基于 Wails 开发，[点击查看了解更多](https://github.com/yisier/nps/blob/master/cmd/npc/npc-gui/README.md)，需要 WebView2 运行时，可通过两种方式添加客户端。
      - 方式1：直接输入快捷启动命令即可
      - 方式2：手动添加客户端，输入-server -vkey等参数

      ![img.png](/image/new/gui.png)
  - 修复：
    - Android 包未构建到 Release [72](https://github.com/yisier/nps/issues/72)

- 2025-12-06 v0.26.28
  - 【全局参数】页面新增`服务地址`配置项，用于客户端命令地址显示、隧道访问地址显示 [293](https://github.com/yisier/nps/issues/293)
  - 【IP授权功能优化】，通过穿透的端口去提交 IP 认证，感谢 [Aqamoe](https://github.com/Aqamoe) 的建议 [291](https://github.com/yisier/nps/issues/291)
  - 【重构限速器实现】提升性能和准确性

- 2025-11-05 v0.26.27
  - 新增：
    - 客户端新增【IP白名单】，可通过两种方式添加IP：
      - 方式1️⃣：在【客户端】新建/修改页面，配置IP授权密码，不在IP白名单的外网访问将被跳转到 IP 授权页面，输入正确的授权密码，即可完成添加 IP。
      - 方式2️⃣：在【客户端】新建/修改页面，直接添加IP到白名单中。

      ![img.png](/image/new/ip.png)
    - 客户端配置移除【压缩】和【加密】功能，此功能会显著提高内存和CPU占用率，"利大于弊"。
    - 客户端默认不再提供配置文件，***强烈推荐使用无配置文件模式启动客户端***

- 2025-08-15  v0.26.26
  - 修复：
    - windows注册服务 使用tls无法连接 [269](https://github.com/yisier/nps/issues/269)
    - 域名解析自动HTTPS逻辑bug [273](https://github.com/yisier/nps/issues/273)

- 2025-05-28  v0.26.25
  - 新增：
    - nps增加`nps(.exe) -server` 命令，用于管理NPS服务，安装和卸载服务在 Linux 下需要有 sudo 权限，Windows 下需要有管理员权限。

      ![img.png](/image/new/server.png)
    - 增加【TLS快捷启动命令】，可用于在tls模式下，快速启动客户端。 [257](https://github.com/yisier/nps/issues/257)

- 2025-04-16  v0.26.24
  - 新增：
    - 隧道支持复制功能，在隧道页面增加复制按钮，除了端口号为随机生成外，其他参数全部拷贝。[251](https://github.com/yisier/nps/issues/251)
  - 修复：
    - 私密代理连接时服务端会报错退出，[250](https://github.com/yisier/nps/issues/250)

- 2025-04-11  v0.26.23
  - 新增：
    - TCP 隧道支持 Proxy Protocol 协议来传递经过请求的真实 IP。

      ![img.png](/image/new/protocol.png)
  - 修复：
    - 客户端较多时，协程增长过快，感谢 [@huanglei288766](https://github.com/yisier/nps/pull/244) 的PR

- 2025-01-23  v0.26.22
  - 优化：
    - 客户端注册系统服务时，（新版客户端注册方式，非 install 命令） 将按照 `npc-[vkey].log` 格式保存日志，每个客户端VKEY单独一个日志文件，windows 日志位于 `npc.exe` 同级目录下，linux 位于 `/var/log/`目录。
    - 域名模式，HTTPS证书支持上传文件和文件路径，系统将自动识别路径还是证书内容。 [#175](https://github.com/yisier/nps/issues/175)
  - 修复：
    - ipv6地址显示不全 [#237](https://github.com/yisier/nps/issues/237)

- 2025-01-07  v0.26.21
  - 新增：
    - 客户端列表页面新增【快捷启动命令】，此命令为连接地址+链接秘钥的base64编码，方便一键启动、一键安装客户端。
    - 域名解析和隧道列表增加访问地址，可以点击隧道链接，直接网页打开。 [#234](https://github.com/yisier/nps/issues/234)
  - 优化：
    - 修改vkey生成方式，由16位缩短至10位，截取uuid前10位，避免重复。
    - 优化客户端启动方式，当npc文件目录下无配置文件时，可直接双击运行客户端，输入命令完成启动、安装、卸载客户端，直接启动和安装服务时需要输入【快捷启动命令】，卸载服务、启动服务、停止服务时，只需输入隧道秘钥【vkey】即可。安装服务以 nps-client-vkey 方式命名 ，实现注册多个服务。

      ![image](/image/new/cmd.png)

  > **注意**：强烈推荐使用无配置文件模式启动客户端，所有数据应该在服务端保存和配置，而客户端只做连接转发。客户端配置文件对小白极不友好，配置繁琐，容易出错。

- 2024-11-07  v0.26.20
  - 新增：
    - 客户端增加创建时间
  - 修复：
    - 客户端限速单位不统一 [#185](https://github.com/yisier/nps/issues/185)
    - 增加从下拉选择客户端,隧道列表排序,新增编辑后不会刷新界面 [#183](https://github.com/yisier/nps/issues/183)
    - 隧道数量限制无法统计域名映射 [#209](https://github.com/yisier/nps/issues/209)

- 2024-06-01  v0.26.19
  - golang 版本升级到 1.22。
  - 增加自动https，自动将http 重定向（301）到 https。
  - 客户端命令行方式启动支持多个隧道ID，使用逗号拼接，示例：`npc -server=xxx:8024 -vkey=ytkpyr0er676m0r7,iwnbjfbvygvzyzzt`。
  - 移除 nps.conf 参数 `https_just_proxy` ，调整 https 处理逻辑，如果上传了 https 证书，则由nps负责SSL（此方式可以获取真实IP），否则走端口转发模式（使用本地证书，nps 获取不到真实IP），如下图所示。

    ![image](/image/new/https.png)

- 2024-02-27  v0.26.18
  - ***新增***：nps.conf 新增 `tls_bridge_port=8025` 参数，当 `tls_enable=true` 时，nps 会监听8025端口，作为 tls 的连接端口。客户端可以选择连接 tls 端口或者非 tls 端口：`npc.exe -server=xxx:8024 -vkey=xxx` 或 `npc.exe -server=xxx:8025 -vkey=xxx -tls_enable=true`

- 2024-01-31  v0.26.17
  - ***说明***：考虑到 npc 历史版本客户端众多，版本号不同旧版本客户端无法连接，为了兼容，仓库版本号将继续沿用 0.26.xx

- 2024-01-02  v0.27.01  (已作废，功能移动到 v0.26.17 版本)
  - ***新增***：tls 流量加密，(客户端忽略证书校验，谨慎使用，客户端与服务端需要同时开启，或同时关闭)，使用方式：
    - 服务端：nps.conf `tls_enable=true`；
    - 客户端：npc.conf `tls_enable=true` 或者 `npc.exe -server=xxx -vkey=xxx -tls_enable=true`

- 2023-06-01  v0.26.16
  - ***修复***：https 流量不统计 Bug 修复。
  - ***新增***：新增全局黑名单IP，用于防止被肉鸡扫描端口或被恶意攻击。
  - ***新增***：新增客户端上次在线时间。

- 2023-02-24  v0.26.15
  - ***修复***：更新程序 url 更改到当前仓库中
  - ***修复***：nps 在外部路径启动时找不到配置文件
  - ***新增***：增加 nps 启动参数，`-conf_path=D:\test\nps`，可用于加载指定nps配置文件和web文件目录。
  - ***window 使用示例：***
    - 直接启动：`nps.exe -conf_path=D:\test\nps`
    - 安装：`nps.exe install -conf_path=D:\test\nps`
    - 安装启动：`nps.exe start`
  - ***linux 使用示例：***
    - 直接启动：`./nps -conf_path=/app/nps`
    - 安装：`./nps install -conf_path=/app/nps`
    - 安装启动：`nps start -conf_path=/app/nps`

- 2022-12-30  v0.26.14
  - ***修复***：API 鉴权漏洞修复

- 2022-12-19
  - ***修复***：某些场景下丢包导致服务端意外退出
  - ***优化***：新增隧道时，不指定服务端口时，将自动生成端口号
  - ***优化***：API返回ID，`/client/add/`, `/index/addhost/`, `/index/add/`
  - ***优化***：域名解析、隧道页面，增加[唯一验证密钥]，方便搜查

- 2022-10-30
  - ***新增***：在管理面板中新增客户端时，可以配置多个黑名单IP，用于防止被肉鸡扫描端口或被恶意攻击。
  - ***优化***：0.26.12 版本还原了注册系统功能，使用方式和以前一样。无论是否注册了系统服务，直接执行 nps 时只会读取当前目录下的配置文件。

- 2022-10-27
  - ***新增***：在管理面板登录时开启验证码校验，开启方式：nps.conf `open_captcha=true`，感谢 [@dongFangTuring](https://github.com/dongFangTuring) 提供的PR

- 2022-10-24
  - ***修复***：HTTP协议支持WebSocket（稳定性待测试）

- 2022-10-21
  - ***修复***：HTTP协议下实时统计流量，能够精准的限制住流量（上下行对等）
  - ***优化***：删除HTTP隧道时，客户端已用流量不再清空

- 2022-10-19
  - ***BUG***：在TCP协议下，流量统计有问题，只有当连接断开时才会统计流量。例如，限制客户端流量20m,当传输100m的文件时，也能传输成功。
  - ***修复***：TCP协议下实时统计流量，能够精准的限制住流量（上下行对等）
  - ***优化***：删除TCP隧道时，客户端已用流量不再清空

    ![image](/image/new/tcp_limit.png)

- 2022-09-14
  - 修改NPS工作目录为当前可执行文件目录（即配置文件和nps可执行文件放在同一目录下，直接执行nps文件即可），去除注册系统服务，启动、停止、升级等命令
