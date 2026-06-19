# 入站添加/编辑弹窗 — 前端复刻参考

> 本文档描述 S-UI 项目中"入站（Inbound）添加/编辑弹窗"功能的**文件关联关系、数据骨架和业务逻辑**。不涉及具体 UI 框架实现细节，接收方自行决定用 React + Ant Design 或其他框架渲染。

---

## 零、项目路径

**项目根目录**: `/Users/yuzai/Tools/s-ui`

以下所有文件路径均相对于此根目录。前端代码位于 `frontend/src/` 子目录下，后端代码位于根目录下各 Go 包中。

扫描文件时，请以项目根目录为起点，读取下列文件。

---

## 一、完整关联文件清单

```
frontend/src/
├── router/index.ts                         # 路由表（/inbounds → Inbounds 页面）
├── views/Inbounds.vue                      # 列表页面，入口组件
├── layouts/modals/
│   ├── Inbound.vue                         # ★ 核心弹窗（添加/编辑，所有子组件在此拼接）
│   └── Stats.vue                           # 流量统计弹窗（列表页"图表"按钮触发）
├── store/modules/data.ts                   # ★ 全局状态（inbounds 数组 + save/loadInbounds/checkTag）
├── types/
│   ├── inbounds.ts                         # ★ 入站类型定义 + createInbound() 工厂函数
│   ├── multiplex.ts                        # Multiplex / Brutal 接口
│   ├── transport.ts                        # Transport / TrspTypes 接口
│   ├── dial.ts                             # Dial 接口
│   ├── tls.ts                              # iTls / oTls / defaultInTls / defaultOutTls
│   ├── brutal.ts                           # Brutal 接口
│   └── config.ts                           # 全局 Config 类型（引用 Inbound）
├── components/
│   ├── Listen.vue                          # 监听地址/端口 + 可选高级选项
│   ├── OutJson.vue                         # 客户端侧出站 JSON 配置
│   ├── Addr.vue                            # 多地址条目（server + port + TLS）
│   ├── Users.vue                           # 新建入站时的用户分配
│   ├── Multiplex.vue                       # 多路复用配置
│   ├── Transport.vue                       # 传输层配置
│   ├── Dial.vue                            # 拨号（detour/bind/mark/keepalive 等）
│   ├── Network.vue                         # 简单下拉：空 / TCP / UDP
│   ├── UoT.vue                             # UDP over TCP：禁用 / v1 / v2
│   ├── Headers.vue                         # HTTP 请求头 kv 增删
│   ├── tls/
│   │   ├── InTLS.vue                       # 入站 TLS 模板下拉
│   │   └── OutTLS.vue                      # 出站 TLS 完整配置
│   ├── transports/
│   │   ├── Http.vue                        # HTTP 传输配置
│   │   ├── WebSocket.vue                   # WebSocket 传输配置
│   │   ├── gRPC.vue                        # gRPC 传输配置
│   │   └── HttpUpgrade.vue                 # HTTPUpgrade 传输配置
│   └── protocols/
│       ├── Direct.vue                      # Direct（无特有字段）
│       ├── Shadowsocks.vue                 # 加密方法 + managed + 密码生成
│       ├── Hysteria.vue                    # 带宽 / obfs / brutal 等
│       ├── Hysteria2.vue                   # 带宽 / obfs / Masquerade
│       ├── Naive.vue
│       ├── ShadowTls.vue                   # version + handshake + strict_mode
│       ├── Tuic.vue                        # 拥塞控制 / auth_timeout / heartbeat
│       ├── AnyTls.vue                      # padding_scheme
│       ├── Tun.vue                         # 虚拟网卡（interface/mtu/stack/auto_route）
│       └── TProxy.vue                      # network 选择
└── plugins/
    ├── api.ts                              # axios 实例 + 拦截器
    ├── httputil.ts                         # get / post 封装，返回 { success, msg, obj }
    └── randomUtil.ts                       # randomIntRange / randomSeq / randomShadowsocksPassword / randomUUID
```

**源文件路径前缀**: `frontend/src/`

---

## 二、组件嵌套关系

```
views/Inbounds.vue  （列表页）
  ├─ Inbound 弹窗 visible/id → layouts/modals/Inbound.vue （核心弹窗）
  │   │
  │   ├─ 服务端面板（Tab "服务端"）：
  │   │   ├── Listen            (props: data, inTags)
  │   │   ├── Direct / Shadowsocks / Hysteria / Hysteria2 / Naive
  │   │   │     / ShadowTls / Tuic / Tun / AnyTls / TProxy
  │   │   │       （按 inbound.type 条件渲染其中一个）
  │   │   │       Shadowsocks 子引用: Network, UoT
  │   │   ├── Transport         (props: data)
  │   │   │    └── Http / WebSocket / gRPC / HttpUpgrade  (条件渲染一个)
  │   │   ├── Users             (props: data, clients)  — 仅新建时显示
  │   │   ├── InTLS             (props: inbound, tlsConfigs)  — 简单下拉
  │   │   └── Multiplex         (props: data, direction="in")
  │   │
  │   └─ 客户端面板（Tab "客户端"）— 仅 HasInData 协议显示：
  │       ├── OutJson           (props: inData, type)
  │       │    └── Network / UoT / Headers / AnyTls / Naive  (按 type 条件引用)
  │       ├── Multiplex         (props: data, direction="out")
  │       ├── Dial              (props: dial, mode="client")
  │       └── Addr × N          (props: addr, hasTls)
  │            └── OutTLS       (props: outbound)
  │
  └─ Stats 弹窗 → layouts/modals/Stats.vue  （流量图表）
```

**数据来源**：所有数据从 `store/modules/data.ts` 读取，不直接在子组件中 import store。

---

## 三、核心类型定义

文件：`types/inbounds.ts`

### 3.1 协议类型常量

```typescript
const InTypes = {
  Direct: 'direct',
  Mixed: 'mixed',
  SOCKS: 'socks',
  HTTP: 'http',
  Shadowsocks: 'shadowsocks',
  VMess: 'vmess',
  Trojan: 'trojan',
  Naive: 'naive',
  Hysteria: 'hysteria',
  ShadowTLS: 'shadowtls',
  TUIC: 'tuic',
  Hysteria2: 'hysteria2',
  VLESS: 'vless',
  AnyTls: 'anytls',
  Tun: 'tun',
  Redirect: 'redirect',
  TProxy: 'tproxy',
} // 共 17 种
```

### 3.2 基础数据骨架

`Inbound` 对象是所有入站类型的联合。公共字段如下：

```typescript
// 监听基础字段
interface Listen {
  listen: string              // 监听地址 (如 "::")
  listen_port: number         // 端口 (1-65535)
  tcp_fast_open?: boolean
  tcp_multi_path?: boolean
  udp_fragment?: boolean
  udp_timeout?: string        // 如 "5m"
  detour?: string             // 转发到哪个入站 tag
  disable_tcp_keep_alive?: boolean
  tcp_keep_alive?: string
  tcp_keep_alive_interval?: string
}

// 入站基类（所有协议通用）
interface InboundBasics extends Listen {
  id: number
  type: string                // 协议类型值
  tag: string                 // 唯一标签
  tls_id: number              // 关联 TLS 模板 ID (0=无)
  addrs?: Addr[]              // 多地址（客户端侧）
  out_json?: any              // 客户端侧出站 JSON 配置
}

// 单个多地址条目
interface Addr {
  server: string
  server_port: number
  tls?: boolean
  insecure?: boolean
  server_name?: string
  remark?: string
}
```

### 3.3 工厂函数

```typescript
createInbound(type: InType, json?: Partial<Inbound>): Inbound
```

- 根据 `type` 从预设默认值表中取出该协议的默认字段，再用 `json` 覆盖
- **关键**：切换协议类型时也调用此函数，保留 id / tag / listen / listen_port，其余重置为默认值

### 3.4 各协议默认值（关键片段）

| type | 默认字段 |
|------|---------|
| `direct` | `{ type: "direct" }` |
| `mixed` | `{ type: "mixed" }` |
| `socks` | `{ type: "socks" }` |
| `http` | `{ type: "http", tls_id: 0 }` |
| `shadowsocks` | `{ type: "shadowsocks", method: "none" }` |
| `vmess` | `{ type: "vmess", tls_id: 0, transport: {} }` |
| `trojan` | `{ type: "trojan", tls_id: 0, transport: {} }` |
| `naive` | `{ type: "naive", tls_id: 0 }` |
| `hysteria` | `{ type: "hysteria", up_mbps: 100, down_mbps: 100, tls_id: 0 }` |
| `shadowtls` | `{ type: "shadowtls", version: 3, handshake: {}, handshake_for_server_name: {} }` |
| `tuic` | `{ type: "tuic", congestion_control: "cubic", tls_id: 0 }` |
| `hysteria2` | `{ type: "hysteria2", tls_id: 0 }` |
| `vless` | `{ type: "vless", tls_id: 0, transport: {} }` |
| `anytls` | `{ type: "anytls", tls_id: 0, padding_scheme: [...] }` |
| `tun` | `{ type: "tun", mtu: 9000, stack: "system", udp_timeout: "5m" }` |
| `redirect` | `{ type: "redirect" }` |
| `tproxy` | `{ type: "tproxy" }` |

---

## 四、弹窗中控制组件显隐的常量

这些常量在 Inbound.vue 中定义，决定哪些子组件对当前协议可见：

| 常量名 | 包含的协议 | 用途 |
|--------|-----------|------|
| **HasInData** | socks, http, mixed, shadowsocks, vmess, shadowtls, trojan, hysteria, vless, anytls, tuic, hysteria2, naive | 是否显示"服务端/客户端"双 Tab |
| **HasTls** | http, vmess, trojan, naive, hysteria, tuic, hysteria2, vless, anytls | 是否显示 TLS 模板选择（InTLS） |
| **MuxAvailable** | vless, vmess, trojan, shadowsocks | 是否显示多路复用（Multiplex） |
| **OnlyTLS** | hysteria, hysteria2, tuic, naive, anytls | 保存时校验 tls_id 不能为 0 |
| **inboundWithUsers** | mixed, socks, http, shadowsocks, vmess, trojan, naive, hysteria, shadowtls, tuic, hysteria2, vless, anytls | 新建时是否显示用户分配（Users） |

---

## 五、弹窗核心逻辑

文件：`layouts/modals/Inbound.vue`

### 5.1 Props（从列表页传入）

| prop | 类型 | 说明 |
|------|------|------|
| `visible` | boolean | 弹窗开关 |
| `id` | number | 0 = 添加模式，>0 = 编辑模式 |
| `inTags` | string[] | 所有入站 tag + 有端口的 endpoint tag（供 detour 下拉用） |
| `tlsConfigs` | tls[] | TLS 模板列表（供 InTLS 下拉用） |

### 5.2 内部状态

| 字段 | 初始值 | 说明 |
|------|--------|------|
| `inbound` | `createInbound("direct", { id:0, tag:"" })` | 当前编辑的入站对象 |
| `title` | `"add"` | 弹窗标题关键词（add / edit） |
| `loading` | `false` | 加载骨架屏 |
| `side` | `"s"` | 当前 Tab（"s"=服务端 / "c"=客户端） |
| `initUsers` | `{ model: 'none', values: [] }` | 用户分配模式 |

### 5.3 方法流程

#### 弹窗打开 `updateData(id)` — 弹窗 `onOpen` 时触发

```
if (id > 0) {
  // 编辑模式
  调用 loadData(id) 从后端加载具体入站 → 赋值 this.inbound
  title = "edit"
} else {
  // 添加模式
  随机端口 = randomIntRange(10000, 60000)
  inbound = createInbound("direct", { id:0, tag:"direct-" + 端口, listen:"::", listen_port:端口 })
  if (HasInData 含该类型) → inbound.addrs = [] , inbound.out_json = {}
  else → delete inbound.addrs, delete inbound.out_json
  title = "add"
}
side = "s"
initUsers = { model: 'none', values: [] }
```

#### 切换协议类型 `changeType()`

1. 如果没有 listen_port → 随机生成一个
2. 保留 id, tag（编辑模式保持原 tag，添加模式重新生成 "type-端口"）, listen, listen_port
3. `inbound = createInbound(新类型, 保留的字段)`
4. 根据 HasInData 处理 addrs / out_json
5. `side = "s"` 切回服务端 Tab

#### 保存 `saveChanges()`

1. **校验 tag 唯一**：`Data().checkTag("inbound", id, tag)` — 比对 store 中 inbounds 数组，排除自身 id
2. **校验 tag 非空 + 端口范围**
3. **OnlyTLS 校验**：若当前协议在 OnlyTLS 中且 `tls_id == 0` → 不通过
4. **收集 initUsers**（仅新建时 `hasUser == true`）：
   - `model = 'all'` → 所有 clients 的 id
   - `model = 'group'` → 按 group 过滤 clients 的 id
   - `model = 'client'` → `initUsers.values`（已选中的 id 数组）
5. 调用 `Data().save("inbounds", id==0 ? "new" : "edit", inbound, clientIds)`
6. 成功后关闭弹窗

#### 关闭 `closeModal()`

1. `updateData(0)` 重置内部状态
2. 通知父组件关闭弹窗

---

## 六、列表页逻辑

文件：`views/Inbounds.vue`

### 6.1 数据绑定

| 数据 | 来源 |
|------|------|
| `inbounds` | `Data().inbounds`（store 中的数组） |
| `inTags` | `[...inbounds.map(i=>i.tag), ...endpoints.filter(e=>e.listen_port>0).map(e=>e.tag)]` |
| `tlsConfigs` | `Data().tlsConfigs` |
| `onlines` | `Data().onlines.inbound`（在线 tag 数组） |

### 6.2 操作

| 操作 | 触发方式 | 调用链 |
|------|---------|--------|
| **添加** | 按钮点击 | `showModal(0)` → 打开 Inbound 弹窗（添加模式） |
| **编辑** | 卡片编辑按钮 | `showModal(item.id)` → 打开 Inbound 弹窗（编辑模式） |
| **删除** | 确认覆盖层 | `Data().save("inbounds", "del", tag)` |
| **克隆** | 克隆按钮 | `Data().loadInbounds([id])` → 拿全量数据 → 改随机端口 + 随机后缀 tag → `createInbound(type, 覆盖)` → `Data().save("inbounds", "new", 新对象)` |
| **流量图表** | 图表按钮 | 打开 Stats 弹窗 `{ resource: "inbound", tag }` |

---

## 七、子组件数据契约

所有子组件遵循同一模式：**通过 props 接收数据对象，直接修改该对象**，不自行发起 API 请求，不 import store。

### 7.1 Listen

| prop | 类型 | 说明 |
|------|------|------|
| `data` | Listen | 入站对象本身 |
| `inTags` | string[] | 供 detour 下拉选项 |

**涉及的字段**: `listen`, `listen_port`, `detour`, `tcp_fast_open`, `tcp_multi_path`, `udp_fragment`, `udp_timeout`, `disable_tcp_keep_alive`, `tcp_keep_alive`, `tcp_keep_alive_interval`

**可选字段机制**: 每项高级选项有一个开关（detour / TCP / UDP / TCP KeepAlive）。开关打开时为字段赋默认值（如 `detour = inTags[0]`），关闭时 delete 该字段。字段是否存在决定 UI 是否渲染该行。

### 7.2 OutJson

| prop | 类型 | 说明 |
|------|------|------|
| `inData` | Inbound | 入站对象（修改 `inData.out_json`） |
| `type` | string | 协议类型（决定显示哪些配置项） |

**按 type 显示的字段**:
- `socks`：版本选择 4/4a/5
- `http`：path + Headers
- `vmess`：security（auto/none/zero/...）+ global_padding + authenticated_length
- `vless` / `vmess`：packet_encoding（none/packetaddr/xudp）
- `hysteria`：recv_window
- `tuic`：udp_relay_mode（native/quic）+ udp_over_stream
- `hysteria` / `hysteria2`：server_ports（逗号分隔转数组）+ hop_interval（秒）

**子引用**: Network, UoT, Headers, AnyTls（direction="out_json"）, Naive（direction="out_json"）

### 7.3 Addr

| prop | 类型 | 说明 |
|------|------|------|
| `addr` | Addr | 单个地址对象 |
| `hasTls` | boolean | 是否支持 TLS（取决于入站类型是否在 HasTls 中） |

**涉及的字段**: `server`, `server_port`, `remark`, `tls`（开关控制 OutTLS 子组件显隐）

### 7.4 Users

| prop | 类型 | 说明 |
|------|------|------|
| `data` | `{ model, values }` | 分配模式 + 选中值 |
| `clients` | Client[] | 所有客户端列表 |

**模式**: `none` / `all` / `group` / `client`
- `group` → 从 `clients.map(c => c.group)` 去重生成选项
- `client` → 从 `clients.map(c => ({ title: c.name, value: c.id }))` 生成选项

### 7.5 Multiplex

| prop | 类型 | 说明 |
|------|------|------|
| `data` | Inbound 或 out_json | 对象上挂 `.multiplex` 字段 |
| `direction` | "in" \| "out" | 控制显示哪些字段 |

**涉及的字段（in）**: `enabled`, `padding`, `brutal.{ enabled, up_mbps, down_mbps }`
**涉及的字段（out）**: 同 in + `protocol`（smux/yamux/h2mux）+ `max_connections` + `min_streams` + `max_streams`

类型定义见 `types/multiplex.ts` 和 `types/brutal.ts`。

### 7.6 Transport

| prop | 类型 | 说明 |
|------|------|------|
| `data` | Inbound | 对象上挂 `.transport` 字段 |

**涉及的字段**: `transport.type`（http / ws / grpc / httpupgrade），启用后才赋值 type
**子引用**: 按 type 渲染 Http / WebSocket / gRPC / HttpUpgrade

类型定义见 `types/transport.ts`（TrspTypes 常量 + HTTP/WebSocket/QUIC/gRPC/HTTPUpgrade 接口）。

### 7.7 Dial

| prop | 类型 | 说明 |
|------|------|------|
| `dial` | Dial | 拨号配置对象 |
| `mode` | string | "client" 时隐藏部分选项（detour/bind/ipv4/ipv6/routing_mark/reuse_addr/domain_resolver） |

**涉及的字段**（全部）: `detour`, `bind_interface`, `inet4_bind_address`, `inet6_bind_address`, `bind_address_no_port`, `routing_mark`, `reuse_addr`, `connect_timeout`, `tcp_fast_open`, `tcp_multi_path`, `udp_fragment`, `domain_resolver`, `disable_tcp_keep_alive`, `tcp_keep_alive`, `tcp_keep_alive_interval`

类型定义见 `types/dial.ts`。

### 7.8 InTLS

| prop | 类型 | 说明 |
|------|------|------|
| `inbound` | Inbound | 修改 `inbound.tls_id` |
| `tlsConfigs` | tls[] | TLS 模板列表 |

下拉选项：`[{ title: "无", value: 0 }, ...tlsConfigs.map(t => ({ title: t.name, value: t.id }))]`

### 7.9 OutTLS

| prop | 类型 | 说明 |
|------|------|------|
| `outbound` | 含 `.tls` 的对象 | 修改 `outbound.tls` |

**涉及的字段**（全部可选，通过开关控制显隐）:
- 基础：`enabled`, `server_name`(SNI), `insecure`, `disable_sni`, `alpn`, `min_version`, `max_version`, `cipher_suites`
- 证书：`certificate`(内容) / `certificate_path`(路径)，二选一切换
- UTLS: `utls.{ enabled, fingerprint }`（chrome/firefox/edge/safari/360/qq/ios/android/random/randomized）
- Reality: `reality.{ enabled, public_key, short_id }`
- ECH: `ech.{ enabled, config/config_path, query_server_name }`
- Fragment: `fragment`, `record_fragment`, `fragment_fallback_delay`（ms）

类型定义见 `types/tls.ts`（`oTls` 接口 + `defaultOutTls` 常量）。

### 7.10 叶子组件

| 组件 | 修改的数据字段 | 选项 |
|------|--------------|------|
| Network | `data.network`（undefined = TCP/UDP） | `''` / `'tcp'` / `'udp'` |
| UoT | `data.udp_over_tcp`（undefined = 禁用） | `{ enabled: true, version: 1|2 }` |
| Headers | `data.headers`（kv 对象，值可为字符串或字符串数组） | 支持同 key 多 value |

---

## 八、Store 数据契约

文件：`store/modules/data.ts`

### 8.1 入站相关 State

| state 字段 | 类型 | 说明 |
|------------|------|------|
| `inbounds` | any[] | 所有入站列表 |
| `outbounds` | any[] | 所有出站列表（供 Dial detour 选项） |
| `endpoints` | any[] | 所有端点列表（供 inTags 和 detour） |
| `clients` | any[] | 所有客户端列表（供 Users 组件） |
| `tlsConfigs` | any[] | 所有 TLS 模板（供 InTLS 下拉） |
| `onlines` | `{ inbound:string[], outbound:string[], user:string[] }` | 在线 tag |
| `config` | any | 全局配置（含 dns servers 供 Dial domain_resolver） |

### 8.2 入站相关 Actions

| 方法签名 | 行为 |
|---------|------|
| `loadInbounds(ids: number[]) → Inbound[]` | **GET** `/api/inbounds?id=1,2,3` |
| `save(object:"inbounds", action:"new"\|"edit"\|"del", data:Inbound, clientIds?:number[]) → boolean` | **POST** `/api/save`，成功后调用 `setNewData` 更新本地 |
| `checkTag("inbound", id:number, tag:string) → boolean` | 在 `inbounds` 数组中查找同名 tag，排除自身 id，重复时弹错误提示并返回 true |
| `loadData()` | **GET** `/api/load?lu=` 增量轮询（每 10 秒） |
| `setNewData(response.obj)` | 更新 lastLoad / inbounds / outbounds / clients / tlsConfigs / onlines 等 |

---

## 九、后端 API

| 方法 | URL | 请求/返回要点 |
|------|-----|--------------|
| GET | `/api/load?lu=<timestamp>` | 首次 lu=0 时返回全量（config + inbounds + clients + tls + onlines 等）；后续增量只返回 `onlines` |
| GET | `/api/inbounds?id=1,2` | `{ success:bool, msg:string, obj:{ inbounds:[...] } }` |
| POST | `/api/save` | body: `{ object: "inbounds", action: "new"\|"edit"\|"del", data: JSON.stringify(inbound), initUsers: "1,2,3" }`，返回 `{ success, msg, obj }`，obj 中部分字段用于更新 store |

---

## 十、各协议特有字段速查

| 协议组件 | 文件 | 特有数据字段 |
|---------|------|-------------|
| Direct | `protocols/Direct.vue` | `network`, `override_address`, `override_port` |
| Shadowsocks | `protocols/Shadowsocks.vue` | `method`（9种）, `password`, `network`, `managed`（仅in）, `multiplex` |
| Hysteria | `protocols/Hysteria.vue` | `up_mbps`, `down_mbps`, `obfs`, `recv_window_conn/client/max_conn_client`, `disable_mtu_discovery` |
| Hysteria2 | `protocols/Hysteria2.vue` | `up_mbps`, `down_mbps`, `ignore_client_bandwidth`, `obfs.{ type, password }`, `masquerade`（4种模式） |
| Naive | `protocols/Naive.vue` | `tls`, `quic_congestion_control` |
| ShadowTls | `protocols/ShadowTls.vue` | `version`(1\|2\|3), `password`, `handshake`(server+port+dial), `handshake_for_server_name`, `strict_mode`, `wildcard_sni` |
| TUIC | `protocols/Tuic.vue` | `congestion_control`(cubic/new_reno/bbr), `auth_timeout`, `zero_rtt_handshake`, `heartbeat` |
| VLESS | —（无独立组件，靠 Transport + TLS 覆盖） | `tls`, `transport`, `multiplex` |
| VMess | —（无独立组件，靠 Transport + TLS 覆盖） | `tls`, `transport`, `multiplex` |
| Trojan | —（无独立组件，靠 Transport + TLS 覆盖） | `tls`, `fallback`, `transport`, `multiplex` |
| AnyTls | `protocols/AnyTls.vue` | `padding_scheme`, `tls` |
| Tun | `protocols/Tun.vue` | `interface_name`, `address[]`, `mtu`, `endpoint_independent_nat`, `udp_timeout`, `stack`, `auto_route`, `strict_route`, `auto_redirect`, `exclude_mptcp`, `auto_redirect_iproute2_fallback_rule_index` |
| TProxy | `protocols/TProxy.vue` | `network` |

**重要**：VMess / VLESS / Trojan 没有独立的协议组件，它们的协议特有字段通过 `Transport`（传输层）、`InTLS`（TLS 模板）、`Multiplex`（多路复用）覆盖配置。

---

## 十一、关键校验规则

| 校验项 | 位置 | 规则 |
|--------|------|------|
| tag 非空 | Inbound.vue → `validate` computed | `inbound.tag == ""` 则不可保存 |
| 端口范围 | Inbound.vue → `validate` computed | `1 <= listen_port <= 65535` |
| tag 唯一 | Inbound.vue → `saveChanges()` | `Data().checkTag("inbound", id, tag)` 比对 store 数组 |
| TLS 必填 | Inbound.vue → `validate` computed | OnlyTLS 中的协议且 `tls_id == 0` 则不可保存 |
| ShadowTLS v3 需要密码 | Users 显隐条件 | `type == ShadowTLS && version < 3` → 不显示用户分配 |
| managed 模式 | Users 显隐条件 | `inbound.managed == true` → 不显示用户分配（密码由系统管理） |

---

## 十二、工具函数

文件：`plugins/randomUtil.ts`

| 函数 | 用途 |
|------|------|
| `randomIntRange(min, max)` | 生成 [min, max] 范围内的安全随机整数（用于随机端口） |
| `randomSeq(count)` | 生成 count 位随机字母数字字符串（用于随机 tag 后缀） |
| `randomShadowsocksPassword(n)` | 生成 n 字节随机数据的 base64 字符串 |
| `randomUUID()` | 生成 UUID v4 |

---

> 文档基于 S-UI v1.4.1 源码生成。仅描述文件关联、数据骨架和业务逻辑，不涉及 UI 框架实现细节。
