# 开发任务：客户端（用户）级上下行限速

> **目标读者**：实施此任务的 AI agent。
> **本文档原则**：所有结论已基于源码确认；agent 无需再做仓库探索。按本文逐项实施即可。
> **不允许的动作**：
> - 执行 `git commit / git push`（仓库就在本地 main 分支，agent 完成后请保持工作树干净待审阅，不要自动提交）；
> - 修改 sing-box 内核源码、修改 `go.mod` 中 sing-box 相关依赖、重新打 sing-box 构建标签；
> - **修改任何协议层代码**——尤其是 Hysteria/Hysteria2/TUIC 等自带 `up_mbps` / `down_mbps` 选项的协议，这些字段属于 sing-box 内置的拥塞控制提示，与本任务的限速实现**完全无关**。本任务的限速一律在 `core/tracker_limit.go` 的 `net.Conn` 包装层完成；
> - 修改 `sub/`、`cronjob/` 任何文件（限速变更不重启 inbound，不需要订阅/定时任务介入）；
> - 修改 `core/register.go`、`core/register_naive*.go`、`core/register_tailscale*.go` 这些协议注册文件；
> - 在限速变更时调用 `RestartInbounds` 或主动断开已有连接。

---

## 1. 背景与目标

S-UI 当前不支持任何形式的用户限速。希望新增「**按客户端（用户）单独限速**」能力：

- 限速粒度：**每一个 `clients` 表里的客户端独立限速**。不是按 inbound 限速，不是全局限速，不是按 group 限速。
- 方向：**上行 / 下行可独立配置**。
  - 上行 = 客户端 → 服务端 = sing-box `Read` 方向；
  - 下行 = 服务端 → 客户端 = sing-box `Write` 方向。
- 单位：DB 存数值 + 单位字段；默认 `mbps`，可选 `kbps` / `bps`。**不支持 gbps**。
- 单位语义：**十进制**，与运营商带宽宣传口径一致。`1 Mbps = 1,000,000 bit/s = 125,000 byte/s`。
- `0 = 不限速`（与现有 `volume` / `expiry` 字段语义一致）。
- 协议层：**TCP 限速即可**；UDP 路径 passthrough，不做限速。
- 同一客户端的多条并发连接**共享同一个令牌桶**（限速值是该用户的全局上限）。
- **修改限速时不重启 inbound、不断开已有连接**，新速率立刻对老连接生效。

### 1.1 关键认知（实施前必读）

**这是纯 S-UI 面板侧改造，不需要改 sing-box。** 

sing-box 已经提供 `router.AppendTracker(adapter.ConnectionTracker)` 作为官方扩展点：tracker 返回的 `net.Conn` 替代原 conn，之后所有 `Read/Write` 走包装层。`core/tracker_stats.go` 与 `core/tracker_conn.go` 是 S-UI 已经在用的两个 tracker，本任务新增的限速器是**完全相同的模式**——只是 `Read/Write` 前面加一道 `rate.Limiter.WaitN`。

**用户标识链路**：

```
client.Name (DB 字段)
   └─ 写入 sing-box inbound users[i].name / username（service/inbounds.go: fetchUsers）
        └─ sing-box 认证后填充 metadata.User（adapter.InboundContext.User）
             └─ tracker 拿到 metadata.User（== client.Name）
                  └─ 用作限速器 map 的 key
```

`StatsTracker.users` 已经在用 `metadata.User` 作为 key 累加流量（`core/tracker_stats.go:60-62, 133-152`），证明这条链路对所有「带用户认证的协议」都成立（mixed/socks/http/shadowsocks/vmess/vless/trojan/naive/shadowtls/anytls/hysteria/hysteria2/tuic）。**限速器复用同一把钥匙**。

---

## 2. 当前代码定位（已确认）

| 文件 | 行号 | 现状 / 用途 |
| --- | --- | --- |
| `database/model/model.go` | 25-46 | `Client` 结构体 |
| `database/db.go` | 93-105 | `AutoMigrate` 调用列表，已包含 `&model.Client{}` |
| `core/box.go` | 38-55 | `Box` 结构体（含 `statsTracker`、`connTracker` 字段） |
| `core/box.go` | 329-332 | `router.AppendTracker(statsTracker)` / `router.AppendTracker(connTracker)` 注册点 |
| `core/box.go` | 586-592 | `StatsTracker()` / `ConnTracker()` getter（仿照即可加 `LimiterTracker()`） |
| `core/tracker_stats.go` | 17-79 | `StatsTracker` 完整实现，**参照该文件结构写 `LimiterTracker`** |
| `core/main.go` | 全文 | `Core` 结构 + `Start([]byte)` / `Stop()`；`Core.GetInstance()` 返回 `*Box` |
| `service/client.go` | 50-171 | `ClientService.Save`：所有 client CRUD 入口 |
| `service/client.go` | 367-431 | `DepleteClients`：到期 / 流量到顶禁用 |
| `service/client.go` | 433-514 | `ResetClients`：延迟启动 / 周期重置 |
| `service/config.go` | 包级 `corePtr` | 全局指向 `*core.Core`，service 层通过它访问 Box |
| `service/config.go` | `StartCore` 函数 | 核心启动入口；**完成后是 BulkLoad 限速值的位置** |
| `frontend/src/types/clients.ts` | 9-48 | `Client` interface 与 `defaultClient` |
| `frontend/src/layouts/modals/Client.vue` | 全文 | 客户端新增/编辑弹窗，需要加 3 个表单字段 |
| `frontend/src/views/Clients.vue` | 154-178, 300-303 | 客户端列表表格列定义，可选加一列「限速」 |
| `frontend/src/locales/{en,fa,vi,zhcn,zhtw,ru}.ts` | - | 6 种语言文案 |

> **注意 1**：`go.mod` 已经间接引入 `golang.org/x/time v0.12.0`（sing-box 依赖）。直接 `import "golang.org/x/time/rate"`，无需 `go get`。
>
> **注意 2**：GORM `AutoMigrate` 会自动为 `clients` 表新增列（带默认值），**无需写 `cmd/migration/1_4.go`**。但实施完成后必须验证：现有数据库升级后，老 client 的新列值是 `0 / 'mbps'` 而不是 NULL。

---

## 3. 数据模型变更

### 3.1 `database/model/model.go` — `Client` 结构体追加 3 个字段

在现有字段末尾追加（注意保持现有 `gorm` 默认值风格）：

| 字段名 | 类型 | gorm tag | JSON tag | 说明 |
| --- | --- | --- | --- | --- |
| `UpLimit` | `int64` | `default:0;not null` | `upLimit` | 上行限速数值，0 = 不限速 |
| `DownLimit` | `int64` | `default:0;not null` | `downLimit` | 下行限速数值，0 = 不限速 |
| `LimitUnit` | `string` | `default:'mbps';not null` | `limitUnit` | 单位枚举：`"mbps"` / `"kbps"` / `"bps"` |

**为什么单位只用一个字段、不分上下行**：上下行用不同单位无业务价值，徒增 UI 复杂度。

**单位语义（十进制）**：

| unit | 1 单位等于 |
| --- | --- |
| `bps` | 1 bit/s = 1/8 byte/s |
| `kbps` | 1,000 bit/s = 125 byte/s |
| `mbps` | 1,000,000 bit/s = 125,000 byte/s |

转换公式（后端用）：

```
bytes_per_sec = value * factor / 8
其中 factor: bps=1, kbps=1000, mbps=1000000
```

### 3.2 验证

```bash
sqlite3 <db_path> ".schema clients"
# 预期：CREATE TABLE 中出现 up_limit / down_limit / limit_unit 三列
sqlite3 <db_path> "SELECT id, name, up_limit, down_limit, limit_unit FROM clients;"
# 预期：所有老数据的新列为 0 / 0 / 'mbps'，无 NULL
```

如果 GORM 没有给老行赋默认值（出现 NULL），需要在 `database/db.go` 的 `InitDB` 中 AutoMigrate 之后追加一段一次性 UPDATE：

```
UPDATE clients SET up_limit = 0 WHERE up_limit IS NULL;
UPDATE clients SET down_limit = 0 WHERE down_limit IS NULL;
UPDATE clients SET limit_unit = 'mbps' WHERE limit_unit IS NULL OR limit_unit = '';
```

---

## 4. 核心限速器（新文件 `core/tracker_limit.go`）

### 4.1 结构

仿照 `core/tracker_stats.go` 写法，新建 `core/tracker_limit.go`，定义：

```text
type LimiterTracker struct {
    mu    sync.RWMutex
    users map[string]*userLimiter   // key 必须是 metadata.User == client.Name
}

type userLimiter struct {
    up   *rate.Limiter   // 客户端 → 服务端方向（Read 端）
    down *rate.Limiter   // 服务端 → 客户端方向（Write 端）
}
```

### 4.2 必须实现的方法

| 方法 | 签名说明 | 行为 |
| --- | --- | --- |
| `NewLimiterTracker()` | 返回 `*LimiterTracker` | 初始化空 map |
| `Reset()` | - | 清空 map（仿 `StatsTracker.Reset`，`Box.Close` 时调用） |
| `SetUserLimit(name string, upBPS, downBPS int64)` | upBPS/downBPS 为 bytes/sec | 若 name 已存在：调 `limiter.SetLimit(rate.Limit(bps))` + `SetBurst(burst)` 动态调速；若不存在：新建 `userLimiter`；若两个方向 BPS 都为 0：调 `DeleteUser` 并返回 |
| `DeleteUser(name string)` | - | 从 map 删除（不打断已有连接，仅停止限速包装） |
| `BulkLoad(limits map[string][2]int64)` | `[2]int64{upBPS, downBPS}` | 启动时一次性加载所有 enable client 的限速值 |
| `GetUserLimit(name string) (upBPS, downBPS int64, ok bool)` | - | 内部查询，供 `RoutedConnection` 用 |
| `RoutedConnection(ctx, conn, metadata, rule, outbound) net.Conn` | 实现 `adapter.ConnectionTracker` | 见 4.3 |
| `RoutedPacketConnection(ctx, conn, metadata, rule, outbound) network.PacketConn` | 实现 `adapter.ConnectionTracker` | **直接返回原 conn，不做限速**（UDP passthrough） |

### 4.3 `RoutedConnection` 行为

伪代码（**禁止照抄到代码**，理解后用 Go 写）：

```
fastpath:
  if metadata.User == "" -> return conn 原样
  upBPS, downBPS, ok := GetUserLimit(metadata.User)
  if !ok || (upBPS == 0 && downBPS == 0) -> return conn 原样
  return &limitedConn{Conn: conn, up: 对应 limiter, down: 对应 limiter}

limitedConn.Read(b):
  n, err := w.Conn.Read(b)
  if n > 0 && w.up != nil:
    w.up.WaitN(ctx, n)   // ctx 派生自 conn.Deadline，无 deadline 则用 context.Background
  return n, err

limitedConn.Write(b):
  if w.down != nil:
    w.down.WaitN(ctx, len(b))
  return w.Conn.Write(b)
```

**重要细节**：

1. **`WaitN` 调用顺序**：`Read` 是「读完再等」（已经消费了带宽），`Write` 是「等完再写」（先申请配额）。两者效果都是平均速率不超过 `rate.Limit`，但 `Read` 的延迟在读后、`Write` 的延迟在写前，对延迟敏感度不同。
2. **令牌桶 burst 大小**：`burst = max(bytes_per_sec, 64 * 1024)`，即「1 秒令牌量」和「64 KiB」取最大值。
   - 64KiB 下限避免小限速下小包频繁等待（影响交互延迟）；
   - 1 秒上限避免 burst 过大导致限速被绕过。
3. **方向独立**：若 `upBPS == 0` 但 `downBPS > 0`，只包 Write，不包 Read（节省 CPU）。`limitedConn` 内部用 `nil` 判断。
4. **ctx 处理**：`WaitN` 需要 ctx，可以直接传 `context.Background()`。如果 conn 设置了 ReadDeadline / WriteDeadline，sing-box 会自己处理超时；`WaitN` 在限速很低 + 大 N 的极端情况下可能返回 `rate: Wait(n=...) exceeds limiter's burst` 错误，**此时 fallback 为 `Allow()` 直接放行**（防止 `n > burst` 时无限阻塞），并在 logger.Warn 一行（debug 等级）。
5. **不实现 `Upstream() any`**：`StatsTracker` 用的 `bufio.NewInt64CounterConn` 内部已经处理 Upstream；我们自己写 `limitedConn` 时**必须实现** `func (w *limitedConn) Upstream() any { return w.Conn }`，否则 sing-box 的某些 splice 路径会失效。

### 4.4 在 `core/box.go` 集成

**修改点 1：`core/box.go:38-55` `Box` 结构体追加字段：**

```text
limiterTracker  *LimiterTracker
```

**修改点 2：`core/box.go:329-332` 在两个现有 tracker 注册之后追加：**

```text
limiterTracker := NewLimiterTracker()
router.AppendTracker(limiterTracker)
```

**修改点 3：`core/box.go:378-395` `return &Box{...}` 字面量补字段：**

```text
limiterTracker: limiterTracker,
```

**修改点 4：`core/box.go:553-559` `Close()` 末尾追加：**

```text
if s.limiterTracker != nil {
    s.limiterTracker.Reset()
}
```

**修改点 5：`core/box.go:586-592` 仿 `StatsTracker()` 加 getter：**

```text
func (s *Box) LimiterTracker() *LimiterTracker {
    return s.limiterTracker
}
```

### 4.5 暴露给 service 层

`core/main.go` 中 `Core` 结构通常提供 `GetInstance() *Box`。service 层访问方式：

```text
limiter := corePtr.GetInstance().LimiterTracker()
if limiter != nil { ... }
```

**注意：核心未启动时 `GetInstance()` 可能返回 nil。所有 service 层调用都必须做 nil 判断。** 未启动情况下不调用 limiter，等下次 `StartCore` 走 `BulkLoad` 重建即可。

---

## 5. Service 层接线

### 5.1 单位换算工具

在 `service/client.go` 顶部加一个**包级辅助函数**（不导出）：

```text
func toBytesPerSec(value int64, unit string) int64
```

规则：
- `value <= 0` → 返回 0；
- `unit == "bps"` → `value / 8`；
- `unit == "kbps"` → `value * 1000 / 8`；
- `unit == "mbps"` 或其他/空 → `value * 1_000_000 / 8`（mbps 作为默认）；
- 输入 unit 做 `strings.ToLower` 兼容。

### 5.2 `ClientService.Save` 触发点

`service/client.go:50-171` 的 `Save` 方法。**在 `tx.Save(...)` 成功之后**（在 return 之前），针对每个 act 加调用：

| act 分支 | 操作 |
| --- | --- |
| `"new"` | 对新增 client：`SetUserLimit(client.Name, toBytesPerSec(UpLimit, LimitUnit), toBytesPerSec(DownLimit, LimitUnit))` |
| `"edit"` | 1) 在 `findInboundsChanges` 已加载的 `oldClient` 中拿 `oldClient.Name`；2) 若 `oldClient.Name != client.Name`：先 `DeleteUser(oldClient.Name)`；3) `SetUserLimit(client.Name, ...)` 用新值 |
| `"addbulk"` | 遍历 `clients` 每个调 `SetUserLimit` |
| `"editbulk"` | 同 edit 逻辑，但要在循环中：`tx.Where("id = ?", c.Id).First(&oldClient)` 取旧名 |
| `"del"` | 已加载的 `client` 对象拿 `client.Name`，调 `DeleteUser` |
| `"delbulk"` | 循环中已读取每个 `client`，对每个 `Name` 调 `DeleteUser` |

**关键约束**：
- 上述所有调用前必须做 `if corePtr.GetInstance() != nil && corePtr.GetInstance().LimiterTracker() != nil`。
- **绝对禁止**在限速变更后调用 `RestartInbounds` 或断开连接。这是本任务最关键的设计要求。
- `findInboundsChanges` (`service/client.go:516-548`) **保持原样**：现有逻辑里改 `client.Name` 会触发 inbound 重启（因为 sing-box 内的 `users[].name` 必须更新），这是无关本任务的旧行为，**不要改**。我们的限速变更**额外**叠加在这之上：rename 时既有 inbound 重启，也有 limiter 的 oldName→newName 迁移。

### 5.3 `ClientService.DepleteClients` 触发点

`service/client.go:367-431`。**在 `tx.Model(...).Update("enable", false)` 成功之后**，对每个被禁用的 `client.Name` 调 `DeleteUser`。

### 5.4 `ClientService.ResetClients` 触发点

`service/client.go:433-514`。两种情况要 `SetUserLimit`：

1. **「Set periodic reset」分支**（`service/client.go:477-494`）里，对每个 `if !client.Enable { client.Enable = true; ... }` 的客户端，恢复启用后**也要恢复限速**（之前禁用时 DeleteUser 了）。
2. 其他 reset 分支不影响 enable 状态、也不改限速字段，**不动**。

### 5.5 `ConfigService.StartCore` 触发点

文件：`service/config.go`，找 `StartCore` 函数。在 sing-box 启动成功（不再返回错误）之后、return 之前，加一段**全量加载**：

伪代码：

```text
db := database.GetDB()
var clients []model.Client
db.Model(model.Client{}).Where("enable = ?", true).
    Select("name, up_limit, down_limit, limit_unit").Find(&clients)

limits := map[string][2]int64{}
for _, c := range clients {
    up := toBytesPerSec(c.UpLimit, c.LimitUnit)
    down := toBytesPerSec(c.DownLimit, c.LimitUnit)
    if up == 0 && down == 0 { continue }
    limits[c.Name] = [2]int64{up, down}
}
if box := corePtr.GetInstance(); box != nil {
    box.LimiterTracker().BulkLoad(limits)
}
```

**为什么放 StartCore 末尾**：`Box` 每次重建都会新建空的 `LimiterTracker`（`Close()` 时 Reset），所以每次启动核心都要重新灌入。这是单一数据源（DB）+ 内存重建的简单模型。

---

## 6. 前端改造

### 6.1 类型 (`frontend/src/types/clients.ts`)

**改 `Client` interface（9-28 行附近）**，追加 3 个可选字段：

```text
upLimit?: number
downLimit?: number
limitUnit?: 'mbps' | 'kbps' | 'bps'
```

**改 `defaultClient` 字面量（30-48 行）**，追加：

```text
upLimit: 0,
downLimit: 0,
limitUnit: 'mbps',
```

### 6.2 编辑弹窗 (`frontend/src/layouts/modals/Client.vue`)

参考现有 Volume 字段（44 行附近的 `v-text-field`）的写法，在适当位置（建议放在 Volume / Expiry 那一行下面、Reset Days 上面）**新增一行三列布局**：

| 列 | 控件 | 字段 | 校验 |
| --- | --- | --- | --- |
| 上行限速 | `v-text-field type="number" min="0"` | `client.upLimit` | 整数 ≥ 0 |
| 下行限速 | `v-text-field type="number" min="0"` | `client.downLimit` | 整数 ≥ 0 |
| 单位 | `v-select` 选项 `['mbps', 'kbps', 'bps']` | `client.limitUnit` | 必选 |

**suffix 显示**：上下行输入框的 `suffix` 应该根据 `client.limitUnit` 动态变化（`Mbps` / `Kbps` / `bps`），与 Volume 字段写 `suffix="GiB"` 同款。

**0 = 不限速**：可以加 `:hint="$t('client.zeroIsUnlimited')"`（i18n 见 6.4）。

**不需要 computed getter/setter 换算**：DB 直接存用户输入的数值 + 单位，前后端透传，不做转换。

### 6.3 列表展示 (`frontend/src/views/Clients.vue`)

**必须实现**：在 `volume` 列后面（300-303 行的 `headers` 数组）插入一列：

```text
{ title: i18n.global.t('client.limit'), key: 'limit', sortable: false }
```

并在模板里加 `<template v-slot:item.limit="{ item }">`，按以下规则显示：

| `upLimit` | `downLimit` | 显示文本（unit 同样按 `limitUnit` 渲染单位后缀） |
| --- | --- | --- |
| 0 | 0 | `-` |
| 5 | 0 | `↑5 Mbps` |
| 0 | 20 | `↓20 Mbps` |
| 5 | 20 | `↑5 / ↓20 Mbps` |

unit 后缀按 `client.limitUnit` 渲染（`Mbps` / `Kbps` / `bps`）。建议封装一个小工具函数 `formatLimit(client)` 放在 Clients.vue 的 `<script>` 区域。

### 6.4 i18n (`frontend/src/locales/*.ts`)

6 种语言（en, fa, vi, zhcn, zhtw, ru）都要加。**已知现有结构是嵌套对象**（参考 `client.delayStart` 等键），追加到 `client` 子对象下：

| key | 简体中文 | 英文（其他 5 语种按风格自行翻译） |
| --- | --- | --- |
| `client.upLimit` | 上行限速 | Upload Limit |
| `client.downLimit` | 下行限速 | Download Limit |
| `client.limitUnit` | 限速单位 | Unit |
| `client.limit` | 限速 | Limit |
| `client.zeroIsUnlimited` | 0 表示不限速 | 0 means unlimited |

---

## 7. 行为规范（必须保证的语义）

| 场景 | 期望行为 |
| --- | --- |
| 新建 client，UpLimit=5, LimitUnit=mbps | 该 user 的 Read（上行）方向限速 625,000 byte/s |
| 编辑老 client，UpLimit 从 5 改为 10 | **不重启 inbound，不断连**，老连接的下一次 `Read` 立刻按 10Mbps 计算令牌 |
| 编辑老 client，rename alice → bob | inbound 重启（旧行为，sing-box users 列表换名）；同时 limiter map 内 `alice` 删除、`bob` 新建 |
| 删除 client | limiter map 该 user 移除；已有连接因 inbound user 列表也被移除会被现有 `RestartInbounds` 路径断开（旧行为） |
| client 流量到顶被 DepleteJob 禁用 | limiter map 该 user 移除 |
| 周期 reset 后 client 重新启用 | limiter map 重新写入 |
| 同一 client 开 5 个并发 TCP 连接 | 5 个连接共享同一个 `*rate.Limiter`，总和不超过限速值 |
| 客户端 UpLimit=0, DownLimit=0 | **完全不包装 conn**，零 CPU 开销路径 |
| 没有 `metadata.User`（tun/redirect/tproxy/direct/无认证 inbound） | passthrough，不限速 |
| sing-box 重启 | `StartCore` 末尾的 `BulkLoad` 重建所有限速；老 TCP 连接在重启时本来就会断 |

---

## 8. 验证清单

### 8.1 数据库

```bash
sqlite3 <db_path> ".schema clients"
sqlite3 <db_path> "SELECT id, name, up_limit, down_limit, limit_unit FROM clients;"
```

### 8.2 编译 & 启动

```bash
./build.sh && ./sui
```

预期日志：核心启动正常，无 `LimiterTracker` 相关报错。

### 8.3 功能测试

| 测试项 | 操作 | 预期 |
| --- | --- | --- |
| 默认无限速 | 新建 client，不填限速，连接代理 `iperf3` / `speedtest` | 跑满带宽 |
| 5 Mbps 上行 | UpLimit=5, mbps；客户端 `iperf3 -c <server>` 上传 | 实测稳定 ≈ 5 Mbps（±5%） |
| 20 Mbps 下行 | DownLimit=20, mbps；客户端 `iperf3 -c <server> -R` 下载 | 实测稳定 ≈ 20 Mbps（±5%） |
| 双向独立 | UpLimit=5, DownLimit=20 | 上下行各自符合各自限速，互不影响 |
| 动态调速不断连 | 测速过程中改限速 5→10 Mbps，保存 | 同一 TCP 连接速率立刻变化，连接不断 |
| 多连接共享 | 同 client 开 5 个并发 `iperf3` 流，UpLimit=10 | 5 条流总和 ≈ 10 Mbps |
| 单位切换 | UpLimit=1000, kbps | 实测 ≈ 1 Mbps |
| 0=不限速 | UpLimit=0 | 跑满带宽（且 pprof 验证 `limitedConn.Read` 不在调用栈） |
| 禁用回收 | 流量到顶被 DepleteJob 禁用 | 该 user 从 limiter map 移除（通过添加临时日志或 debugger 验证） |
| 核心重启 | 改完限速保存 → `sui` 重启 | 老限速值在新 sing-box 实例上立刻生效 |
| UDP 不限速 | 用 hysteria2 / tuic（UDP 协议）配 5 Mbps 下行 | **当前期望**：UDP 不限速、跑满带宽（TCP-only 的明确取舍） |

### 8.4 回归

| 检查 | 通过标准 |
| --- | --- |
| 老 client 升级 | 升级后老 client 默认 0/0/mbps，行为与升级前一致（无限速） |
| StatsTracker 流量计数 | 限速生效情况下，流量统计页面数字仍然准确（计数与限速互不干扰） |
| 订阅服务 | 客户端订阅 URL 拉取节点不受影响 |
| Rule import / OutJson 等已有功能 | 全部不受影响 |

---

## 9. 边界与已知取舍

1. **TCP-only**：UDP 协议（hysteria/hysteria2/tuic）的 UDP 路径不限速。如果用户使用纯 UDP 协议跑大流量，限速失效。**这是产品确认的取舍**。
2. **不影响 sing-box 协议握手**：限速发生在数据面，握手阶段（认证、TLS、QUIC handshake）不受令牌桶影响。
3. **令牌桶 burst 1 秒**：极短时间内（< 1 秒）可能允许 burst 流量，长期均值符合限速。
4. **rename 边界（必须实现）**：rename 时旧名 user 的存量连接在 inbound 重启时会被现有逻辑断开（这是旧行为）；新名 user 重新建立连接时走新限速值。limiter map 必须做 `DeleteUser(oldName) → SetUserLimit(newName)` 迁移，**不允许残留 oldName**——否则长期累积会成内存泄漏。
5. **deplete 时清理 limiter（必须实现）**：被禁用的 client 在 sing-box inbound 的 users 列表里也被移除（现有逻辑），新连接进不来；但 limiter map 里的 entry 不会自动消失。**必须显式 DeleteUser** 清理孤儿条目，防止 map 长期累积废数据导致内存膨胀。
6. **限速变更不入 `changes` 表的额外影响**：现有 `ConfigService.Save` 已经对 `clients` 写 `changes` 表（actor / obj 包含整个 client JSON），新增字段会自动包含在内，无需额外处理。

---

## 10. 提交规范

完成后请按以下 commit message 格式提交（**不要执行 `git commit`，仅准备好工作树**）：

```
feat: per-user upload/download bandwidth limit

- DB: add up_limit/down_limit/limit_unit to clients (mbps default, 0 = unlimited)
- core: new LimiterTracker (TCP-only, token-bucket via x/time/rate)
- service: bind limiter ops to client CRUD + deplete/reset + StartCore bulk-load
- frontend: 3 fields in client modal + i18n for 6 languages
```

---

## 11. FAQ

**Q：为什么不在 inbound 配置里下发 `up_mbps` / `down_mbps`（hysteria 那种）？**
A：那是 QUIC 拥塞控制的速率提示，不是硬性限速；且只有 hysteria 系协议支持。本任务要求所有协议统一限速。

**Q：为什么不写在 sing-box 路由 rule 里？**
A：sing-box 路由规则没有 per-user 限速 action。最接近的是 v2ray-api 的限速，但 SUI 没启用 v2ray-api，且那也要改 sing-box 实验特性。

**Q：为什么不开新 goroutine 单独管理限速？**
A：`rate.Limiter` 本身就是并发安全的、无背景 goroutine 的纯内存结构。所有限速逻辑在 `Read/Write` 调用栈内同步完成，无额外协程开销。

**Q：限速会不会让 sing-box 的健康检查 / urltest 出问题？**
A：不会。urltest 走 outbound，不经过 inbound 的 `metadata.User`。
