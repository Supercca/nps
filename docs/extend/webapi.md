# Web API

> 除 `AuthController` 和 `LoginController` 外，所有接口均需通过鉴权，详见 [API 鉴权说明](api.html)。

## 通用响应格式

| 响应类型 | 格式 |
| --- | --- |
| 成功 | `{"status": 1, "msg": "success"}` |
| 成功（含 id） | `{"status": 1, "msg": "success", "id": 123}` |
| 失败 | `{"status": 0, "msg": "error message"}` |
| 列表 | `{"rows": [...], "total": 100}` |

> 单个查询接口（`GetClient`、`GetOneTunnel`、`GetHost`）返回 `{"code": 1, "data": {...}}` 或 `{"code": 0}`。

---

## Client 客户端管理

### 客户端列表

```
POST /client/list/
```

| 参数 | 含义 |
| --- | --- |
| search | 搜索关键词 |
| sort | 排序字段 |
| order | asc 正序 / desc 倒序 |
| offset | 分页起始 |
| limit | 每页条数 |

返回 `AjaxTable` 格式，额外含 `ip`、`bridgeType`、`bridgePort` 字段。

---

### 获取单个客户端

```
POST /client/getclient/
```

| 参数 | 含义 |
| --- | --- |
| id | 客户端 id |

---

### 添加客户端

```
POST /client/add/
```

| 参数 | 含义 |
| --- | --- |
| remark | 备注 |
| vkey | 客户端验证密钥 |
| u | basic 权限认证用户名 |
| p | basic 权限认证密码 |
| compress | 是否压缩传输，`true` / `false` |
| crypt | 是否加密传输，`true` / `false` |
| config_conn_allow | 是否允许客户端以配置文件模式连接，`true` / `false` |
| rate_limit | 带宽限制，单位 KB/s，留空不限制 |
| flow_limit | 流量限制，单位 M，留空不限制 |
| max_conn | 最大连接数，留空不限制 |
| max_tunnel | 最大隧道数，留空不限制 |
| web_username | web 用户登录用户名 |
| web_password | web 用户登录密码 |
| blackiplist | 黑名单 IP 列表，`\r\n` 分隔 |
| ipwhite | 是否开启 IP 白名单，`true` / `false` |
| ipwhitepass | IP 白名单授权密码 |
| ipwhitelist | 白名单 IP 列表，`\r\n` 分隔 |
| expire_time | 到期时间，留空表示永不过期。支持格式：`2006-01-02 15:04:05`、`2006-01-02 15:04`、`2006-01-02T15:04:05`、`2006-01-02T15:04`、`2006-01-02` |

---

### 修改客户端

```
POST /client/edit/
```

| 参数 | 含义 |
| --- | --- |
| id | 要修改的客户端 id |
| remark | 备注 |
| vkey | 客户端验证密钥（仅管理员可修改） |
| u | basic 权限认证用户名 |
| p | basic 权限认证密码 |
| compress | 是否压缩传输，`true` / `false` |
| crypt | 是否加密传输，`true` / `false` |
| config_conn_allow | 是否允许客户端以配置文件模式连接，`true` / `false` |
| rate_limit | 带宽限制，单位 KB/s（仅管理员可修改） |
| flow_limit | 流量限制，单位 M（仅管理员可修改） |
| max_conn | 最大连接数（仅管理员可修改） |
| max_tunnel | 最大隧道数（仅管理员可修改） |
| web_username | web 用户登录用户名 |
| web_password | web 用户登录密码 |
| blackiplist | 黑名单 IP 列表，`\r\n` 分隔 |
| ipwhite | 是否开启 IP 白名单，`true` / `false` |
| ipwhitepass | IP 白名单授权密码 |
| ipwhitelist | 白名单 IP 列表，`\r\n` 分隔 |
| expire_time | 到期时间，格式同新增接口 |
| flow_inlet | 入口流量，单位字节，传入则覆盖原值（留空不修改） |
| flow_export | 出口流量，单位字节，传入则覆盖原值（留空不修改） |

---

### 更改客户端状态

```
POST /client/changestatus/
```

| 参数 | 含义 |
| --- | --- |
| id | 客户端 id |
| status | `true` 启用 / `false` 禁用 |

---

### 删除客户端

```
POST /client/del/
```

| 参数 | 含义 |
| --- | --- |
| id | 要删除的客户端 id |

---

## Index 隧道管理

### 隧道列表

```
POST /index/gettunnel/
```

| 参数 | 含义 |
| --- | --- |
| client_id | 客户端 id |
| type | 隧道类型：`tcp`、`udp`、`httpProxy`、`socks5`、`secret`、`p2p`、`file` |
| search | 搜索关键词 |
| sort | 排序字段 |
| order | asc 正序 / desc 倒序 |
| offset | 分页起始 |
| limit | 每页条数 |

---

### 获取单条隧道

```
POST /index/getonetunnel/
```

| 参数 | 含义 |
| --- | --- |
| id | 隧道 id |

---

### 添加隧道

```
POST /index/add/
```

| 参数 | 含义 |
| --- | --- |
| client_id | 客户端 id |
| type | 隧道类型：`tcp`、`udp`、`httpProxy`、`socks5`、`secret`、`p2p`、`file` |
| remark | 备注 |
| port | 服务端端口（端口为 0 或留空时自动分配） |
| server_ip | 绑定的服务端 IP（多 IP 场景） |
| target | 内网目标，格式 `ip:端口` |
| local_proxy | 是否转发到 nps 服务器本地，`true` / `false` |
| password | 隧道密码（secret 模式） |
| local_path | 本地文件路径（file 模式） |
| strip_pre | URL 前缀去除（httpProxy 模式） |
| proto_version | 协议版本 |

---

### 复制隧道

```
POST /index/copy/
```

| 参数 | 含义 |
| --- | --- |
| id | 要复制的源隧道 id |

复制后自动分配新端口和新 id，其他配置沿用源隧道。

---

### 修改隧道

```
POST /index/edit/
```

| 参数 | 含义 |
| --- | --- |
| id | 隧道 id |
| client_id | 客户端 id |
| type | 隧道类型 |
| port | 服务端端口 |
| server_ip | 绑定的服务端 IP |
| target | 内网目标 |
| local_proxy | 是否转发到 nps 服务器本地 |
| remark | 备注 |
| password | 隧道密码 |
| local_path | 本地文件路径 |
| strip_pre | URL 前缀去除 |
| proto_version | 协议版本 |
| flow_inlet | 入口流量，单位字节，传入则覆盖原值（留空不修改） |
| flow_export | 出口流量，单位字节，传入则覆盖原值（留空不修改） |

---

### 停止隧道

```
POST /index/stop/
```

| 参数 | 含义 |
| --- | --- |
| id | 隧道 id |

---

### 启动隧道

```
POST /index/start/
```

| 参数 | 含义 |
| --- | --- |
| id | 隧道 id |

---

### 删除隧道

```
POST /index/del/
```

| 参数 | 含义 |
| --- | --- |
| id | 隧道 id |

---

## Host 域名解析管理

### 域名列表

```
POST /index/hostlist/
```

| 参数 | 含义 |
| --- | --- |
| client_id | 客户端 id |
| search | 搜索关键词（域名/备注） |
| offset | 分页起始 |
| limit | 每页条数 |

---

### 获取单条域名解析

```
POST /index/gethost/
```

| 参数 | 含义 |
| --- | --- |
| id | 域名解析 id |

---

### 添加域名解析

```
POST /index/addhost/
```

| 参数 | 含义 |
| --- | --- |
| client_id | 客户端 id |
| remark | 备注 |
| host | 域名 |
| scheme | 协议类型：`all`、`http`、`https` |
| location | URL 路由，留空不限制 |
| target | 内网目标，格式 `ip:端口` |
| local_proxy | 是否转发到 nps 服务器本地，`true` / `false` |
| header | 自定义 request header |
| hostchange | 修改 request host |
| key_file_path | HTTPS 证书私钥文本或路径 |
| cert_file_path | HTTPS 证书文件文本或路径 |
| AutoHttps | 是否自动 HTTPS（仅 scheme 非 `http` 时生效） |

---

### 修改域名解析

```
POST /index/edithost/
```

| 参数 | 含义 |
| --- | --- |
| id | 域名解析 id |
| client_id | 客户端 id |
| remark | 备注 |
| host | 域名 |
| scheme | 协议类型 |
| location | URL 路由 |
| target | 内网目标 |
| local_proxy | 是否转发到 nps 服务器本地 |
| header | 自定义 request header |
| hostchange | 修改 request host |
| key_file_path | HTTPS 证书私钥文本或路径 |
| cert_file_path | HTTPS 证书文件文本或路径 |
| AutoHttps | 是否自动 HTTPS |
| flow_inlet | 入口流量，单位字节，传入则覆盖原值（留空不修改） |
| flow_export | 出口流量，单位字节，传入则覆盖原值（留空不修改） |

```
POST /index/hoststop/
```

| 参数 | 含义 |
| --- | --- |
| id | 域名解析 id |

---

### 启动域名解析

```
POST /index/hoststart/
```

| 参数 | 含义 |
| --- | --- |
| id | 域名解析 id |

---

### 删除域名解析

```
POST /index/delhost/
```

| 参数 | 含义 |
| --- | --- |
| id | 域名解析 id |

---

## Global 全局设置

### 查看全局设置

```
GET /global/index/
```

返回全局黑名单 IP 列表和服务端 URL。

---

### 保存全局设置

```
POST /global/save/
```

| 参数 | 含义 |
| --- | --- |
| globalBlackIpList | 全局黑名单 IP 列表，`\r\n` 分隔 |
| serverUrl | 服务端访问地址（用于更正显示 IP） |
| blockForeignIp | `1` 开启「禁止国外 IP 访问」；不传或 `0` 表示关闭 |
| autoBlockForeignIp | `1` 开启「境外 IP 自动加入黑名单」；不传或 `0` 表示关闭。仅在 `blockForeignIp` 开启时生效：被判定为境外的 IP 会自动追加到全局黑名单并立即落盘 |
| barkEnabled | `1` 开启 Bark 通知；不传或 `0` 表示关闭。开启后每次有 IP 加入全局黑名单都会推送一条通知 |
| barkUrl | Bark 推送地址，形如 `https://api.day.app/<deviceKey>`；自建服务器填写自己的地址前缀 |
| barkQuietStart | 免打扰时段开始时间，格式 `HH:MM`；为空表示不启用 |
| barkQuietEnd | 免打扰时段结束时间，格式 `HH:MM`；与开始时间都填写且当前时间落在区间内时，通知会被直接丢弃、不再推送 |
| hiddenMenus | 需要隐藏的左侧菜单项，可重复传；可选值 `index`/`client`/`host`/`tcp`/`udp`/`http`/`socks5`/`secret`/`p2p`/`file`/`global`/`log`/`help`。`global` 不可隐藏 |

---

### 获取服务端日志（增量）

```
GET /log/list/?offset=0&limit=500
```

| 参数 | 含义 |
| --- | --- |
| offset | 已收到的最后一条日志的 `seq`，不传视为 0 |
| limit | 本次最多返回条数，默认 500、上限 2000 |

返回：

```json
{
  "status": 1,
  "entries": [
    { "seq": 12, "time": "2026-11-05 14:48:50", "level": "info", "msg": "the version of server is ..." }
  ],
  "latest": 12,
  "total": 12
}
```

`seq` 单调递增，前端带上它即可只拉取新增部分。日志来自服务端的内存环形缓冲（默认保留最近 2000 条），
进程重启后清空。**该接口仅管理员可用**：日志内容包含客户端 vkey。

---

### 清理服务端日志

```
POST /log/clear/
```

清空内存日志缓冲，并截断磁盘上的日志文件（含 beego 轮转出来的历史文件）。
当前正在写入的日志文件只做截断而非删除，清理后日志可以继续正常写入。

返回 `removed`（已处理的文件名列表）与 `failed`（处理失败的文件及原因）。**仅管理员可用**。

---
