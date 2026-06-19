# S-UI 后端 REST API 文档

> 版本：v1.4.2 ｜ 后端框架：Gin ｜ 数据格式：JSON（除特殊说明外）

本文档覆盖 S-UI 面板后端的全部可用接口。所有业务接口分为两套：

| 接口组 | 路径前缀 | 鉴权方式 | 适用场景 |
|--------|----------|----------|----------|
| 面板接口 | `{基础路径}api/` | **Session Cookie**（登录后下发） | Web 面板内部调用 |
| 开放接口 | `{基础路径}apiv2/` | **Token 请求头** | 第三方/脚本对接 |

- `{基础路径}` 即面板设置里的 `webPath`，默认为 `/`，下文示例统一用 `/`。
- 两套接口共用同一套业务逻辑，差别仅在鉴权与可用动作范围（详见末尾「接口组差异」）。

---

## 一、通用约定

### 1.1 统一响应结构

除「文件下载类」和「sing-box 配置导出」外，所有接口都返回如下 JSON 结构（HTTP 状态码恒为 `200`，成功与否看 `success` 字段）：

```json
{
  "success": true,        // 是否成功
  "msg": "",              // 提示信息；失败时为 "动作: 错误详情"
  "obj": {}               // 业务数据；失败或无数据时为 null
}
```

### 1.2 鉴权说明

- **面板接口（/api）**：先调用 `POST /api/login`，服务端通过名为 `s-ui` 的 Cookie 维持会话。后续请求需携带该 Cookie。未登录时：XHR 请求返回 `{"success":false,"msg":"Invalid login"}`，普通请求 302 跳转 `/login`。
- **开放接口（/apiv2）**：在请求头加 `Token: <你的令牌>`。令牌通过面板接口 `POST /api/addToken` 创建。令牌过期或无效时返回失败。

### 1.3 请求格式约定

- 大部分 POST 接口使用 **`application/x-www-form-urlencoded`**（表单），而非 JSON body。
- 表单中若有 `data` 字段，其值是一段 **JSON 字符串**（需自行序列化后填入）。
- 唯一例外：`importRules` 接收 **原始 JSON body**（见对应章节）。

---

## 二、认证与会话

### 2.1 登录

`POST /api/login`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user | string | 是 | 用户名 |
| pass | string | 是 | 密码 |

- 表单提交（`x-www-form-urlencoded`）。
- 成功后通过 `Set-Cookie: s-ui=...` 下发会话。
- 会话有效期取设置项 `sessionMaxAge`（分钟），为 0 时浏览器关闭即失效。
- 响应：`{"success":true,"msg":"","obj":null}`

### 2.2 登出

`GET /api/logout` — 清除当前会话。无参数。

### 2.3 修改账户凭据

`POST /api/changePass`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | string | 是 | 用户 ID |
| oldPass | string | 是 | 旧密码 |
| newUsername | string | 是 | 新用户名 |
| newPass | string | 是 | 新密码 |

> 仅面板接口（/api）提供，/apiv2 无此动作。

---

## 三、数据加载

### 3.1 全量/增量加载

`GET /api/load?lu=<时间戳>`

- `lu`（可选）：上一次拉取的 `LastUpdate` 时间戳（秒）。
- 服务端据此判断数据是否变化：
  - **有变化或不传 lu**：返回完整数据集（见下）。
  - **无变化**：仅返回 `onlines`（在线列表），节省带宽。
- 返回 `obj` 字段（有变化时）：

| 字段 | 说明 |
|------|------|
| config | sing-box 基础配置（JSON） |
| clients | 客户端列表 |
| tls | TLS 模板列表 |
| inbounds / outbounds / endpoints / services | 各资源列表 |
| subURI | 订阅基础地址 |
| enableTraffic | 是否启用流量统计存储 |
| onlines | 在线资源（含 user/inbound/outbound 三类 tag） |
| lastLog | 核心未运行时附带的最近一条日志 |

### 3.2 按资源单独加载

以下 GET 接口返回单类资源，部分支持 `id` 过滤：

| 接口 | 说明 | 查询参数 |
|------|------|----------|
| `GET /api/inbounds` | 入站列表/详情 | `id`（可选，指定则返回单条及其用户） |
| `GET /api/clients` | 客户端列表/详情 | `id`（可选，指定则返回单条完整配置） |
| `GET /api/outbounds` | 出站列表 | 无 |
| `GET /api/endpoints` | Endpoint 列表 | 无 |
| `GET /api/services` | Service 列表 | 无 |
| `GET /api/tls` | TLS 模板列表 | 无 |
| `GET /api/config` | sing-box 基础配置 | 无 |

> 注意：`inbounds` 与 `clients` 不带 `id` 时返回精简列表；带 `id` 时返回完整字段。

### 3.3 用户与设置

| 接口 | 说明 |
|------|------|
| `GET /api/users` | 面板登录用户列表 |
| `GET /api/settings` | 全部面板设置项（键值对） |

### 3.4 统计与监控

**流量统计** `GET /api/stats`

| 参数 | 类型 | 说明 |
|------|------|------|
| resource | string | 资源类型：`user` / `inbound` / `outbound` |
| tag | string | 资源标识（用户名 / 入站 tag / 出站 tag） |
| limit | int | 返回条数，默认 100 |

**系统状态** `GET /api/status?r=<指标列表>`

- `r` 为逗号分隔的指标组合，可选值：

| 值 | 含义 |
|----|------|
| cpu | CPU 占用率 |
| mem | 内存 |
| dsk | 磁盘 |
| dio | 磁盘 IO |
| swp | swap |
| net | 网络吞吐 |
| sys | 系统信息（CPU 型号、核数等） |
| sbd | sing-box 运行状态 |
| db | 数据库信息 |

示例：`GET /api/status?r=cpu,mem,sbd`

**在线列表** `GET /api/onlines` — 返回当前在线的 user/inbound/outbound。

**运行日志** `GET /api/logs?c=<条数>&l=<级别>`
- `c`：返回条数；`l`：级别（`debug`/`info`/`warning`/`error`）。

**变更记录** `GET /api/changes?a=<操作者>&k=<键>&c=<条数>`
- `a`：操作者用户名（可选）；`k`：变更对象键（可选）；`c`：条数。

**用户流量排行** `GET /api/topUsers`

按流量返回 Top N 客户端，支持累计排行与时段排行。**被禁用客户端也参与排行**（不过滤 enable）。

| 参数 | 取值 | 默认 | 说明 |
|------|------|------|------|
| period | `total` / `1h` / `24h` / `7d` / `30d` | total | `total`=读 clients 表累计；其余=聚合 stats 表对应时间窗口 |
| direction | `both` / `up` / `down` | both | 服务端排序字段：both→合计、up→上行、down→下行 |
| limit | 整数 1..100 | 10 | 返回条数；越界自动夹紧到 [1,100]，非数字回落 10 |

- 非法 `period`/`direction` 返回 `{"success":false,"msg":"invalid period: xxx"}`。
- 时段排行依赖 `stats` 表，其保留天数由设置项 `trafficAge` 决定（接口本身不读/不改）；若 `trafficAge=7`，则 `period=30d` 实际只有 7 天数据。
- `period=total` 口径为 `clients.up/down`（周期重置时会归零），非自创建以来的真累计。

响应 `obj` 为数组，已按 direction 服务端排序：

```json
{
  "success": true,
  "msg": "",
  "obj": [
    { "name": "alice", "up": 12345678, "down": 87654321, "total": 100000000 },
    { "name": "bob",   "up": 123456,   "down": 4567890,  "total": 4691346 }
  ]
}
```

| 字段 | 说明 |
|------|------|
| name | 客户端名（即 sing-box 用户 tag） |
| up | 上行字节 |
| down | 下行字节 |
| total | up + down，服务端预算 |

> 同样提供 `GET /apiv2/topUsers`（Token 鉴权），参数与响应一致。仅 GET，无 POST。

---

## 四、统一保存接口（核心）

`POST /api/save` — **所有资源的增删改都通过此单一接口完成。**

表单字段（`x-www-form-urlencoded`）：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| object | string | 是 | 操作的资源类型，见 4.1 |
| action | string | 是 | 操作动作，见 4.1 |
| data | string(JSON) | 是 | 资源数据，**JSON 字符串**，结构随 object/action 变化 |
| initUsers | string | 否 | 仅新建 inbound 时使用：要绑定的客户端 ID（逗号分隔） |

保存成功后会回读受影响的资源并返回（例如保存 clients 会一并返回更新后的 inbounds）。

### 4.1 object × action 支持矩阵

| object | 支持的 action | 说明 |
|--------|---------------|------|
| `clients` | new / edit / addbulk / editbulk / del / delbulk | 客户端 |
| `inbounds` | new / edit / del | 入站 |
| `outbounds` | newbulk / new / edit / del | 出站（**支持批量新建**） |
| `services` | new / edit / del | sing-box service |
| `endpoints` | new / edit / del | WireGuard/Tailscale/WARP |
| `tls` | new / edit / del | TLS 模板 |
| `config` | （直接传配置） | sing-box 基础配置，保存后异步重启核心 |
| `settings` | （直接传键值） | 面板设置 |

### 4.2 支持多条数据提交的动作（⚠️ 需嵌套结构）

下列动作的 `data` 字段不是单个对象，而是 **数组或对象数组**，需特别注意嵌套：

#### ① clients - addbulk（批量新建客户端）

`data` 为**客户端对象数组**：

```json
[
  { "enable": true, "name": "u1", "config": { ... }, "inbounds": [1,2] },
  { "enable": true, "name": "u2", "config": { ... }, "inbounds": [1] }
]
```

> 数组内所有客户端共用第一个元素的 `inbounds` 作为绑定入站判定。

#### ② clients - editbulk（批量编辑客户端）

`data` 为**客户端对象数组**，每个元素须带 `id`：

```json
[
  { "id": 5, "name": "u1", "upLimit": 10, "limitUnit": "mbps", ... },
  { "id": 6, "name": "u2", "volume": 0, ... }
]
```

#### ③ clients - delbulk（批量删除客户端）

`data` 为 **ID 数组**：

```json
[5, 6, 7]
```

#### ④ outbounds - newbulk（批量新建出站）

`data` 为 **出站对象的 JSON 数组**（每个元素是一个完整 sing-box 出站）：

```json
[
  { "type": "vless", "tag": "node1", "server": "...", ... },
  { "type": "trojan", "tag": "node2", "server": "...", ... }
]
```

> 典型用途：从订阅/链接转换（见 6.x）后批量导入出站。

### 4.3 单条数据提交的 data 结构

#### clients（new / edit / del）

- new/edit：`data` 为单个客户端对象。
- del：`data` 为单个客户端 **ID 数字**（如 `5`）。

客户端对象主要字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int | 编辑时必填 |
| enable | bool | 是否启用 |
| name | string | 用户名（唯一） |
| config | object | 各协议的认证信息（按协议名分组，见下方嵌套说明） |
| inbounds | int[] | 绑定的入站 ID 列表 |
| links | object[] | 外部链接/订阅，元素含 `type`(external/sub) 与 `uri` |
| volume | int64 | 流量配额（字节），0=不限 |
| expiry | int64 | 到期时间戳（秒），0=不限 |
| up / down | int64 | 已用上行/下行（字节） |
| desc / group | string | 备注 / 分组 |
| delayStart | bool | 首次连接才开始计时 |
| autoReset | bool | 周期自动重置流量 |
| resetDays | int | 重置周期（天） |
| **upLimit** | int64 | **上行限速值，0=不限速** |
| **downLimit** | int64 | **下行限速值，0=不限速** |
| **limitUnit** | string | **限速单位：`mbps`/`kbps`/`bps`，默认 mbps** |

> **嵌套说明（config 字段）**：`config` 是按协议名分组的对象，键为协议（`vless`/`vmess`/`trojan`/`shadowsocks`/`hysteria2`/`tuic`...），值为该协议的认证字段。示例：
> ```json
> "config": {
>   "vless": { "name": "u1", "uuid": "xxxx-...", "flow": "xtls-rprx-vision" },
>   "trojan": { "name": "u1", "password": "xxxx" }
> }
> ```

#### inbounds（new / edit / del）

- new/edit：`data` 为入站对象。del：`data` 为入站 ID。
- 入站对象采用 **扁平结构**：固定字段 `id`/`type`/`tag`/`tls_id`/`addrs`/`out_json`，其余 sing-box 原生字段平铺在同级（会被收集进 `Options`）。
- 含 `tls_id` 时会自动关联 TLS 模板。
- 新建时配合表单 `initUsers` 绑定客户端。

#### outbounds / services / endpoints（new / edit / del）

- new/edit：`data` 为对应资源对象（sing-box 原生结构，含 `type`/`tag` 等）。
- del：`data` 为资源 ID。

#### tls（new / edit / del）

- new/edit：`data` 含 `name`、`server`（服务端 TLS 配置）、`client`（客户端 TLS 配置）。
- del：`data` 为 TLS 模板 ID。

#### config（基础配置）

- `object=config`，`data` 为 sing-box 基础配置 JSON。保存后**异步重启核心**生效。

#### settings（面板设置）

- `object=settings`，`data` 为**键值对对象**：`{ "webPath": "/", "trafficAge": "30", ... }`。
- 特殊键：`webCertFile`/`webKeyFile`/`subCertFile`/`subKeyFile` 会校验文件存在；`webPath`/`subPath` 自动补全首尾 `/`；`trafficAge=0` 会清空所有流量统计。

---

## 五、运维操作

| 接口 | 方法 | 说明 |
|------|------|------|
| `POST /api/restartApp` | POST | 重启面板自身（延迟 3 秒，通过 SIGHUP） |
| `POST /api/restartSb` | POST | 重启 sing-box 内核 |

均无请求体，返回标准消息结构。

---

## 六、链接与订阅转换

### 6.1 单链接转出站

`POST /api/linkConvert`

| 字段 | 类型 | 说明 |
|------|------|------|
| link | string | **单条节点 URI**（vmess/vless/trojan/hy/hy2/anytls/tuic/ss/naive 等） |

- 返回：1 个 sing-box 出站对象。
- 用途：出站编辑器「从链接导入」。

### 6.2 外部订阅转出站

`POST /api/subConvert`

| 字段 | 类型 | 说明 |
|------|------|------|
| link | string | **订阅 URL**（注意：是可 GET 的地址，不是订阅正文！） |

- 服务端 GET 该 URL（跳过证书校验）→ 自动 base64 解码 → 识别 sing-box JSON 或多行 URI → 返回**出站对象数组**。
- ⚠️ 普通 Clash YAML 订阅不被识别；返回数组可配合 `outbounds/newbulk` 批量导入。

### 6.3 本地订阅文本转出站

`POST /api/subConvertText`

| 字段 | 类型 | 说明 |
|------|------|------|
| content | string | **订阅正文**（多行 URI 或整体 base64） |

- 与 6.2 区别：直接传内容，不发起网络请求。返回出站对象数组。

---

## 七、路由规则导入（特殊格式）

`POST /api/importRules` — ⚠️ **此接口接收原始 JSON body，不是表单！**

请求体（`application/json`）支持多条嵌套数据：

```json
{
  "rules": [
    { "user": ["u1","u2"], "outbound": "proxy" },
    { "type": "logical", "rules": [ { "auth_user": ["u3"] } ], "outbound": "block" }
  ],
  "rule_set": [
    { "tag": "geosite-cn", "type": "remote", "url": "..." }
  ],
  "final": "proxy"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| rules | object[] | 路由规则数组，支持 `logical` 类型递归嵌套子规则 |
| rule_set | object[] | 规则集数组（按 tag 去重） |
| final | string | 默认出站 |

- 冲突处理：若规则的 `user`/`auth_user` 已被现有路由占用，则跳过该规则。
- 返回 `obj`：`{ added, skipped, skippedRules[], addedRulesets, totalRules, restarted }`。
- 成功导入后异步重启核心。

---

## 八、API 令牌管理（用于 /apiv2）

> 仅面板接口（/api）提供，管理 /apiv2 使用的 Token。

### 8.1 查询令牌

`GET /api/tokens` — 返回当前登录用户的全部令牌。

### 8.2 新建令牌

`POST /api/addToken`

| 字段 | 类型 | 说明 |
|------|------|------|
| expiry | int64 | 过期时间戳（秒），`0` 表示永不过期 |
| desc | string | 备注 |

- 返回新建的令牌（含明文 token，请妥善保存）。

### 8.3 删除令牌

`POST /api/deleteToken`

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 令牌 ID |

> 新增/删除令牌后服务端会自动重载 /apiv2 的令牌缓存。

---

## 九、密钥生成

`GET /api/keypairs?k=<类型>&o=<选项>`

| 参数 | 说明 |
|------|------|
| k | 密钥类型：如 `x25519`（Reality）、`ech`、`tls`(自签) 等 |
| o | 附加选项（按类型不同） |

返回生成的密钥对。

---

## 十、数据库备份与恢复

### 10.1 导出数据库

`GET /api/getdb?exclude=<排除表>`

- `exclude`（可选）：逗号分隔的排除表名。
- 返回 **二进制文件流**（`application/octet-stream`），文件名形如 `s-ui_20060102-150405.db`。

### 10.2 导入数据库

`POST /api/importdb`

- **`multipart/form-data`**，文件字段名为 `db`。
- 导入后覆盖现有数据库。

---

## 十一、sing-box 配置导出

`GET /api/singbox-config`

- 返回当前**完整 sing-box 运行配置**（`application/json` 文件流），文件名形如 `config_20060102-150405.json`。
- 失败时返回 HTTP 400 + 纯文本错误。

---

## 十二、出站连通性检测

`GET /api/checkOutbound?tag=<出站tag>&link=<链接>`

| 参数 | 说明 |
|------|------|
| tag | 已存在出站的 tag（二选一） |
| link | 临时节点 URI（二选一） |

- 返回延迟检测结果。

---

## 十三、接口组差异（/api vs /apiv2）

`/apiv2` 是 `/api` 的子集，**不包含会话与账户管理类动作**。

| 动作 | /api | /apiv2 |
|------|:----:|:------:|
| login / logout / changePass | ✅ | ❌ |
| addToken / deleteToken / tokens | ✅ | ❌ |
| singbox-config | ✅ | ❌ |
| save / restartApp / restartSb | ✅ | ✅ |
| linkConvert / subConvert / subConvertText | ✅ | ✅ |
| importdb / importRules | ✅ | ✅ |
| load / 各资源加载 / users / settings | ✅ | ✅ |
| stats / status / onlines / logs / changes | ✅ | ✅ |
| topUsers | ✅ | ✅ |
| keypairs / getdb / checkOutbound | ✅ | ✅ |

> `/apiv2` 调用方式：在以上共有接口的路径中把 `/api/` 换成 `/apiv2/`，并在请求头加 `Token: <令牌>`。
> 例如：`GET /apiv2/load`、`POST /apiv2/save`。

---

## 十四、错误处理

- HTTP 状态码恒为 `200`（文件流接口和 singbox-config 失败例外）。
- 失败时 `success=false`，`msg` 格式为 `"动作: 错误详情"`。
- 未鉴权：
  - /api：XHR 返回 `{"success":false,"msg":"Invalid login"}`；普通请求 302 → `/login`。
  - /apiv2：Token 无效/过期返回失败结构。
