# S-UI 前端 API 调用文档

> 版本：v1.6.7 ｜ 前端框架：Vue 3 + Vuetify 4 + TypeScript + Pinia + axios
>
> 本文从**前端视角**描述 S-UI 面板如何调用后端接口，覆盖 HTTP 层封装、全局状态、鉴权流程、各视图的 API 调用场景与数据契约。
> 后端接口的完整定义见同目录 [backend.md](./backend.md)。

---

## 一、HTTP 层架构

前端 HTTP 调用分两层：底层 axios 实例（`plugins/api.ts`）+ 业务封装（`plugins/httputil.ts`）。

### 1.1 axios 实例 — `frontend/src/plugins/api.ts`

| 配置项 | 值 | 说明 |
|--------|----|------|
| `baseURL` | `"./"` | 相对路径，自动拼上 `webPath`（默认 `/app/`） |
| POST 默认 Content-Type | `application/x-www-form-urlencoded; charset=UTF-8` | 与后端表单接口对齐 |
| 通用头 | `X-Requested-With: XMLHttpRequest` | 让后端识别 XHR 请求（未登录时返回 JSON 而非 302） |
| FormData | 自动切换 `multipart/form-data` | 用于文件上传（`importdb`） |

**请求去重拦截器**：以 `${method}:${url}` 为 key，相同 key 的新请求会取消前一个未完成请求。避免快速点击触发的重复提交。

### 1.2 HttpUtils 封装 — `frontend/src/plugins/httputil.ts`

```ts
interface Msg {
  success: boolean
  msg: string
  obj: any | null
}
```

| 方法 | 签名 | 说明 |
|------|------|------|
| `HttpUtils.get(url, data?, options?)` | `→ Promise<Msg>` | GET，`data` 作为 query params |
| `HttpUtils.post(url, data?, options?)` | `→ Promise<Msg>` | POST，`data` 作为请求体 |
| `HttpUtils.logout()` | `→ Promise<void>` | 调 `GET api/logout` 并跳转 `/login` |

**统一响应处理**（`_handleMsg`）：
- 后端返回 `msg == "Invalid login"` → 弹错误通知 + 自动调 `logout()` 跳转登录页。
- `success=true` 且 `msg` 非空 → 弹成功通知（`i18n: success: actions.<msg>`）。
- `success=false` 且 `msg` 非空 → 弹错误通知（标题 `failed`，内容 `msg`）。

> **所有 API 调用都返回 `Msg`，前端组件通过 `msg.success` 判断成败、`msg.obj` 取数据。** 文件下载类接口（`getdb`、`singbox-config`）绕过 HttpUtils，直接用 `window.location.href` 触发浏览器下载。

---

## 二、鉴权流程

### 2.1 登录

```
Login.vue → HttpUtil.post('api/login', { user, pass })
  ↓ 成功 → Set-Cookie: s-ui=... → router.push('/')
  ↓ 失败 → 弹错误通知，停留在登录页
```

> `Login.vue` 中用的是 `HttpUtil`（非 `HttpUtils`），但底层是同一个 axios 实例。

### 2.2 会话维持

- 登录成功后浏览器自动持有 `s-ui` Cookie，后续所有 axios 请求自动携带。
- **路由守卫**（`router/index.ts`）：通过 `document.cookie` 是否含 `s-ui=` 判断登录态；未登录访问受保护路由 → 重定向 `/login`。
- **会话过期**：任意 API 返回 `"Invalid login"` → HttpUtils 自动调 `logout()`。
- **Session 有效期**：由后端 setting `sessionMaxAge`（分钟）控制，0=浏览器关闭即失效。

### 2.3 自动轮询

```
router/index.ts → loadDataInterval()
  → 立即调 Data().loadData()
  → setInterval 每 10 秒调 Data().loadData()
```

- 登录态下持续轮询 `/api/load?lu=<timestamp>` 做增量同步。
- 进入 `/login` 时清除轮询定时器。

---

## 三、全局状态管理 — Pinia Store

文件：`frontend/src/store/modules/data.ts`，Store 名 `Data`。

### 3.1 State

| 字段 | 类型 | 说明 |
|------|------|------|
| `lastLoad` | `number` | 上次全量加载的时间戳（秒），用作增量拉取的 `lu` 参数 |
| `reloadItems` | `string[]` | 仪表盘需要轮询的指标项（持久化到 localStorage） |
| `subURI` | `string` | 订阅基础地址 |
| `enableTraffic` | `boolean` | 是否启用流量统计存储（`trafficAge > 0`） |
| `onlines` | `{ inbound: string[], outbound: string[], user: string[] }` | 在线资源 tag |
| `config` | `any` | sing-box 基础配置（含 route/dns/log/experimental） |
| `inbounds` | `any[]` | 入站列表 |
| `outbounds` | `any[]` | 出站列表 |
| `services` | `any[]` | sing-box service 列表 |
| `endpoints` | `any[]` | Endpoint 列表 |
| `clients` | `any[]` | 客户端列表 |
| `tlsConfigs` | `any[]` | TLS 模板列表 |

### 3.2 Actions — API 调用

#### `loadData()` — 全量/增量加载

```ts
const msg = await HttpUtils.get('api/load', this.lastLoad > 0 ? { lu: this.lastLoad } : {})
```

- 首次调用（`lastLoad=0`）：不传 `lu`，后端返回全量数据。
- 后续调用：传 `lu=lastLoad`，后端判断是否变化：
  - **有变化** → `msg.obj` 含 `config/clients/inbounds/outbounds/endpoints/services/tls/subURI/enableTraffic/onlines` → 调 `setNewData()` 更新全部 state。
  - **无变化** → `msg.obj` 仅含 `onlines` → 只更新 `onlines`。
- 若 `msg.obj.lastLog` 存在（核心未运行），弹错误通知展示最近日志。

#### `loadInbounds(ids: number[]) → Promise<Inbound[]>`

```ts
const msg = await HttpUtils.get('api/inbounds', ids.length > 0 ? { id: ids.join(",") } : {})
```

- 不传 `id`：返回精简列表。
- 传 `id`：返回完整字段（含 `options`/`addrs`/`out_json`）。

#### `loadClients(id: number) → Promise<Client>`

```ts
const msg = await HttpUtils.get('api/clients', id > 0 ? { id } : {})
```

- 传 `id`：返回单条完整配置（含 `config` 各协议认证信息）。

#### `save(object, action, data, initUsers?) → Promise<boolean>`

```ts
const msg = await HttpUtils.post('api/save', {
  object, action,
  data: JSON.stringify(data, null, 2),
  initUsers: initUsers?.join(',') ?? undefined
})
```

- 成功后调 `setNewData(msg.obj)` 更新受影响的 state（后端回读返回）。
- 弹成功通知：`actions.<action> objects.<objectName>`。

#### `checkClientName(id, newName) → boolean`

本地校验客户端名唯一性（不调 API，查 `this.clients`），重复时弹错误并返回 `true`。

#### `checkBulkClientNames(names) → boolean`

本地校验批量客户端名唯一性 + 批内不重复。

#### `checkTag(object, id, tag) → boolean`

本地校验 tag 唯一性（查 `inbounds`/`outbounds`/`services`/`endpoints`），重复时弹错误并返回 `true`。

### 3.3 `setNewData(data)` — State 更新规则

```ts
this.lastLoad = Math.floor(Date.now() / 1000)
if (data.subURI)       this.subURI = data.subURI
if (data.enableTraffic) this.enableTraffic = data.enableTraffic
if (data.config)       this.config = data.config
if (Object.hasOwn(data, 'clients'))   this.clients = data.clients ?? []
if (Object.hasOwn(data, 'inbounds'))  this.inbounds = data.inbounds ?? []
// ... outbounds / services / endpoints / tls 同理
```

> 使用 `Object.hasOwn` 判断字段是否存在（区分"未返回"与"返回空数组"），避免增量响应误清空 state。

---

## 四、各视图 API 调用清单

### 4.1 登录页 — `views/Login.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/login` | POST | `{ user, pass }` | 登录提交 |

### 4.2 仪表盘 — `components/Main.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/status` | GET | `{ r: "cpu,mem,dsk,dio,swp,net,sys,sbd,db" }` | 加载仪表盘指标（按 `reloadItems` 动态拼接） |
| `api/status` | GET | `{ r: "sys" }` | 单独刷新系统信息 |
| `api/restartSb` | POST | `{}` | 重启 sing-box 内核 |

> 仪表盘有独立轮询：`reloadItems` 非空时每 **2 秒** 调 `api/status`（比全局 10 秒轮询更密集）。

### 4.3 入站 — `views/Inbounds.vue` + `layouts/modals/Inbound.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/inbounds` | GET | `{ id }` | 编辑/克隆时加载单条完整入站 |
| `api/save` | POST | `{ object: "inbounds", action: "new"\|"edit"\|"del", data, initUsers }` | 通过 `Data().save()` 增删改 |

- 列表数据来自 `Data().inbounds`（全局轮询维护，不单独调 API）。
- `initUsers` 仅新建时传：要绑定的客户端 ID 数组（逗号分隔）。

### 4.4 客户端 — `views/Clients.vue` + `layouts/modals/Client.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/clients` | GET | `{ id }` | 编辑时加载单条完整配置 |
| `api/save` | POST | `{ object: "clients", action: "new"\|"edit"\|"del"\|"addbulk"\|"editbulk"\|"delbulk", data }` | 通过 `Data().save()` 增删改 |
| `api/topUsers` | GET | `{ period: "24h", direction: "both", limit: 10 }` | 流量排行弹窗（固定参数） |
| `api/stats` | GET | `{ resource: "user", tag, limit }` | 单用户流量图表 |

### 4.5 出站 — `views/Outbounds.vue` + `layouts/modals/Outbound.vue` + `layouts/modals/OutboundBulk.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/save` | POST | `{ object: "outbounds", action: "new"\|"newbulk"\|"edit"\|"del", data }` | 增删改 |
| `api/linkConvert` | POST | `{ link }` | 出站编辑器「从链接导入」单条节点 URI |
| `api/subConvert` | POST | `{ link }` | 批量导入：订阅 URL 模式 |
| `api/subConvertText` | POST | `{ content }` | 批量导入：本地粘贴节点文本模式 |
| `api/checkOutbound` | GET | `{ tag }` | 出站连通性测速 |

### 4.6 服务 / Endpoint / TLS — 各对应视图

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/save` | POST | `{ object: "services"\|"endpoints"\|"tls", action: "new"\|"edit"\|"del", data }` | 增删改 |
| `api/keypairs` | GET | `{ k: "wireguard" }` | Endpoint：生成 WireGuard 密钥对 |
| `api/keypairs` | GET | `{ k: "wireguard", o: private_key }` | Endpoint：由私钥推导公钥 |
| `api/keypairs` | GET | `{ k: "tls", o: server_name }` | TLS 模板：生成自签证书 |
| `api/keypairs` | GET | `{ k: "reality" }` | TLS 模板：生成 Reality 密钥对 |

### 4.7 路由规则 — `views/Rules.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/save` | POST | `{ object: "config", action: "edit", data }` | 保存 `route` 配置（整体替换） |

> Rules 页编辑的是 `config.route` 子树，保存时提交完整 config 对象。规则批量导入的前端弹窗目前走 `api/save` 路径；后端 `POST /api/importRules` 供第三方/脚本调用，前端可选接入。

### 4.8 DNS / 基础项 — `views/Dns.vue` + `views/Basics.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/save` | POST | `{ object: "config", action: "edit", data }` | 保存 `dns`/`log`/`experimental` 等基础配置 |

### 4.9 管理员 — `views/Admins.vue` + `layouts/modals/Token.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/users` | GET | 无 | 加载管理员账户列表（含最近登录 IP） |
| `api/changePass` | POST | `{ id, oldPass, newUsername, newPass }` | 修改账户凭据 |
| `api/tokens` | GET | 无 | 加载当前用户的 API Token 列表 |
| `api/addToken` | POST | `{ desc, expiry }` | 新建 Token（`expiry=0` 永不过期） |
| `api/deleteToken` | POST | `{ id }` | 删除 Token |

### 4.10 设置 — `views/Settings.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/settings` | GET | 无 | 加载全部面板设置项 |
| `api/save` | POST | `{ object: "settings", action: "set", data: JSON.stringify(settings) }` | 保存设置（键值对对象） |
| `api/restartApp` | POST | `{}` | 保存后重启面板自身（3 秒延迟） |

> `Settings.vue` 的保存不走 `Data().save()`，而是直接调 `HttpUtils.post('api/save', ...)`，成功后用 `msg.obj.settings` 回填本地。重启后 `window.location.replace(url)` 跳转到新地址（端口/路径可能已变）。

### 4.11 仪表盘弹窗

| 弹窗 | 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|------|
| 日志 `Logs.vue` | `api/logs` | GET | `{ c: count, l: level }` | 拉取运行日志 |
| 变更记录 `Changes.vue` | `api/changes` | GET | `{ a: actor, k: key, c: count }` | 拉取审计日志 |
| 备份 `Backup.vue` | `api/getdb` | GET（下载） | `?exclude=stats,changes` | 导出数据库（浏览器直接跳转） |
| 备份 `Backup.vue` | `api/singbox-config` | GET（下载） | 无 | 导出 sing-box 配置 JSON（浏览器直接跳转） |
| 备份 `Backup.vue` | `api/importdb` | POST | `FormData(db=file)` | 导入数据库（`multipart/form-data`） |
| 数据库信息 `UsageStats.vue` | `api/status` | GET | `{ r: "db" }` | 数据库统计信息 |

### 4.12 ECH 配置 — `components/tls/Ech.vue`

| 调用 | 方法 | 参数 | 场景 |
|------|------|------|------|
| `api/keypairs` | GET | `{ k: "ech", o: server_name }` | 生成 ECH 配置+密钥对 |

---

## 五、文件下载类接口

以下接口**不经过 HttpUtils**，直接用 `window.location.href` 触发浏览器导航下载（利用 Cookie 鉴权）：

| 接口 | 触发方式 | 返回 |
|------|----------|------|
| `api/getdb?exclude=stats,changes` | `window.location.href = 'api/getdb' + excludeOption` | 二进制 `.db` 文件流 |
| `api/singbox-config` | `window.location.href = 'api/singbox-config'` | JSON 文件流 `config_YYYYMMDD-HHMMSS.json` |

> 这两个接口返回的不是 `{ success, msg, obj }` 结构，所以不能用 HttpUtils 解析。

---

## 六、统一保存接口调用约定

前端所有资源的增删改都通过 `Data().save(object, action, data, initUsers?)` → `POST api/save`。

### 6.1 调用模板

```ts
await Data().save('inbounds', 'new', inboundObject, [1, 2, 3])
// 等价于：
await HttpUtils.post('api/save', {
  object: 'inbounds',
  action: 'new',
  data: JSON.stringify(inboundObject, null, 2),
  initUsers: '1,2,3'
})
```

### 6.2 object × action 矩阵（前端使用场景）

| object | action | 前端触发点 | data 结构 |
|--------|--------|-----------|-----------|
| `inbounds` | `new` | Inbound 弹窗保存 | 入站对象 |
| `inbounds` | `edit` | Inbound 弹窗保存 | 入站对象（含 `id`） |
| `inbounds` | `del` | 列表删除确认 | 入站 ID |
| `clients` | `new` / `edit` / `del` | Client 弹窗 / 列表 | 客户端对象 / ID |
| `clients` | `addbulk` / `editbulk` / `delbulk` | 批量操作 | 对象数组 / ID 数组 |
| `outbounds` | `new` / `edit` / `del` | Outbound 弹窗 | 出站对象 / ID |
| `outbounds` | `newbulk` | 批量导入保存 | 出站对象数组 |
| `services` | `new` / `edit` / `del` | Service 弹窗 | service 对象 / ID |
| `endpoints` | `new` / `edit` / `del` | Endpoint 弹窗 | endpoint 对象 / ID |
| `tls` | `new` / `edit` / `del` | TLS 弹窗 | TLS 模板对象 / ID |
| `config` | （直接传配置） | Rules / Dns / Basics 页 | sing-box 基础配置 JSON |
| `settings` | `set` | Settings 页 | 键值对对象 |

### 6.3 保存成功后的 State 更新

后端 `api/save` 成功后会回读受影响的资源并返回，前端 `Data().save()` 调 `setNewData(msg.obj)` 更新本地。例如：
- 保存 `clients` → 后端返回 `{ clients: [...], inbounds: [...] }`（因为 client 变更会影响 inbound 的 users 列表）。
- 保存 `settings` → 后端返回 `{ settings: {...} }`。

---

## 七、数据契约速查

### 7.1 客户端对象（前端 `types/clients.ts`）

```ts
interface Client {
  id?: number
  enable: boolean
  name: string
  config: { [protocol: string]: { name: string, [key: string]: any } }
  inbounds: number[]
  links: { type: "external" | "sub", uri: string }[]
  volume: number      // 流量配额（字节），0=不限
  expiry: number      // 到期时间戳（秒），0=不限
  up: number          // 已用上行
  down: number        // 已用下行
  desc: string        // 备注
  group: string       // 分组
  delayStart: boolean // 首次连接才开始计时
  autoReset: boolean  // 周期自动重置流量
  resetDays: number   // 重置周期（天）
  upLimit: number     // 上行限速值，0=不限速
  downLimit: number   // 下行限速值，0=不限速
  limitUnit: "mbps" | "kbps" | "bps"  // 限速单位，默认 mbps
}
```

### 7.2 入站对象（前端 `types/inbounds.ts`）

- 公共字段：`id`, `type`, `tag`, `tls_id`, `listen`, `listen_port`
- 客户端侧字段：`addrs`（多地址数组）, `out_json`（客户端用出站 JSON）
- 协议特有字段平铺在同级（如 `method`, `up_mbps`, `version` 等），由 `createInbound(type, json?)` 工厂函数按协议类型生成默认值。

### 7.3 出站对象

- sing-box 原生结构：`{ type, tag, server, server_port, ... }`
- 由 `createOutbound(type, json?)` 工厂函数生成。

### 7.4 TLS 模板

```ts
interface Tls {
  id?: number
  name: string
  server: object  // 服务端 TLS 配置（enabled/server_name/certificate/...）
  client: object  // 客户端 TLS 配置（enabled/server_name/utls/reality/ech/...）
}
```

### 7.5 全局 Config

```ts
interface Config {
  log: { level, output, timestamp }
  dns: { servers, rules, final, strategy, ... }
  route: { rules, rule_set, final, ... }
  experimental: { cache_file, clash_api, v2ray_api }
  // 各子树由对应视图（Basics/Dns/Rules）分别编辑
}
```

---

## 八、前端类型定义文件索引

| 文件 | 定义内容 |
|------|----------|
| `types/inbounds.ts` | `Inbound` 接口 + `InTypes` 常量 + `createInbound()` 工厂 |
| `types/outbounds.ts` | `Outbound` 接口 + `OutTypes` 常量 + `createOutbound()` 工厂 |
| `types/clients.ts` | `Client` 接口 + `defaultClient` 常量 |
| `types/endpoints.ts` | `Endpoint` 接口 + `EpTypes` 常量 |
| `types/services.ts` | `Service` 接口 + `SvcTypes` 常量 |
| `types/tls.ts` | `iTls` / `oTls` 接口 + `defaultInTls` / `defaultOutTls` |
| `types/dns.ts` | DNS Server / Rule 类型 |
| `types/rules.ts` | Route Rule / RuleSet 类型 + `actionKeys` 常量 |
| `types/config.ts` | 全局 `Config` 类型 |
| `types/transport.ts` | `Transport` 接口 + `TrspTypes` 常量 |
| `types/dial.ts` | `Dial` 接口 |
| `types/multiplex.ts` | `Multiplex` 接口 |
| `types/brutal.ts` | `Brutal` 接口 |

---

## 九、API 调用与后端文档的映射

| 前端调用 | 后端文档章节 |
|----------|-------------|
| `api/login` / `api/logout` / `api/changePass` | [backend.md §二](./backend.md#二认证与会话) |
| `api/load` / `api/inbounds` / `api/clients` | [backend.md §三](./backend.md#三数据加载) |
| `api/save` | [backend.md §四](./backend.md#四统一保存接口核心) |
| `api/restartApp` / `api/restartSb` | [backend.md §五](./backend.md#五运维操作) |
| `api/linkConvert` / `api/subConvert` / `api/subConvertText` | [backend.md §六](./backend.md#六链接与订阅转换) |
| `api/importRules` | [backend.md §七](./backend.md#七路由规则导入特殊格式) |
| `api/tokens` / `api/addToken` / `api/deleteToken` | [backend.md §八](./backend.md#八api-令牌管理用于-apiv2) |
| `api/keypairs` | [backend.md §九](./backend.md#九密钥生成) |
| `api/getdb` / `api/importdb` | [backend.md §十](./backend.md#十数据库备份与恢复) |
| `api/singbox-config` | [backend.md §十一](./backend.md#十一sing-box-配置导出) |
| `api/checkOutbound` | [backend.md §十二](./backend.md#十二出站连通性检测) |
| `api/stats` / `api/status` / `api/onlines` / `api/logs` / `api/changes` / `api/topUsers` | [backend.md §三.4](./backend.md#34-统计与监控) |
| `api/users` / `api/settings` | [backend.md §三.3](./backend.md#33-用户与设置) |
