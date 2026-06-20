# 开发计划：聚合订阅 API（Batch Subscription API）

> 目标读者：实现该功能的 AI / 工程师。本文档自包含，包含所有必要上下文，**无需再回头追问需求**。
>
> 项目：S-UI v1.4.3（基于 sing-box v1.13.4 的 Web 面板）
> 仓库根：`/Users/yuzai/Tools/s-ui`
> 后端：Go 1.25.7 + Gin + GORM(SQLite)
> 前端：Vue 3 + Vuetify 4 + TypeScript + Pinia

---

## 0. 必读项目上下文（5 分钟版）

实现前请阅读：
- `项目代码总结.md` — 全栈架构总览（重点章节 6.2 业务服务、6.4 订阅服务）
- `CLAUDE.md` — 编码行为准则（KISS / 外科手术式改动 / 不动既有逻辑）

**关键文件**：

| 文件 | 作用 | 本次是否改动 |
|---|---|---|
| `sub/subHandler.go` | 现有 `/sub/:subid` 单 client 订阅入口 | **不改** |
| `sub/subService.go` | 原始订阅渲染 | **不改** |
| `sub/jsonService.go` | sing-box JSON 订阅渲染（**复用其内部方法**） | 仅作可见性微调 |
| `sub/clashService.go` | Clash 订阅渲染 | 本期不动 |
| `sub/linkService.go` | link 渲染工具 | **不改** |
| `service/setting.go` | 配置项；含 `defaultValueMap` | 新增 1 个 key |
| `service/client.go` | client CRUD | 新增 1 个查询方法 |
| `util/subInfo.go` | 订阅响应头工具 | 新增 1 个函数 |
| `app/app.go` 或 `sub/sub.go` | 路由组注册 | 新增 1 个 group |
| `frontend/src/views/Settings.vue` | 设置页 | 新增 1 个输入框 |
| `frontend/src/locales/*.ts` | i18n 6 种语言 | 新增 key |

---

## 1. 功能需求

提供一个**聚合订阅 HTTP API**，让外部脚本/平台一次性拉取多个 client 的 sing-box JSON 订阅。

### 1.1 支持的查询场景

| 场景 | 参数 | 匹配方式 |
|---|---|---|
| 模糊用户名（支持多结果） | `name=ali` | `LOWER(name) LIKE LOWER('%ali%')` |
| 精确用户名（单个导出） | `username=alice` | `name = 'alice'` |
| 分组筛选（支持多结果） | `group=vip` | `group = 'vip'` |
| 返回全部 | 无任何过滤参数 | 仅 `enable = true` |
| 组合 | 任意以上 AND | 空参数自动忽略 |

> 同时传 `name` 与 `username` 时，**`username` 优先**（精确匹配胜出）。
> 所有查询前置 `enable = true` 条件；上限 **2000** 条，超限返回 400。

### 1.2 鉴权

- **独立的"订阅 API 密钥"**，与 apiv2 token **完全分开**（用户明确要求）
- 密钥存储在 `settings` 表，key 名为 `subApiKey`
- 通过面板"设置 → 订阅"Tab 由管理员手动设置/修改
- **传递方式**：URL query `?key=xxx`（用户明确指定）
- **空值行为**：
  - 若数据库中 `subApiKey` 为空字符串 → **不鉴权**，任意访问通过（与现有 `/sub/:subid` 行为一致）
  - 若 `subApiKey` 非空 → 必须 `?key=xxx` 完全匹配，否则返回 `401 Unauthorized`

### 1.3 输出格式

**只支持 sing-box JSON**（query `format=json`，目前唯一合法值；缺省也按 json 处理）。

JSON 结构完全沿用现有 `JsonService.GetJson` 的产物（含 tun + mixed 默认入站 + outbounds 集合 + selector/urltest/direct + route 模板），仅区别：
- **outbound `tag` 全局加 client 前缀**：`[<clientName>] <原tag>` —— 防止多 client 引用同 inbound 时 tag 冲突
- selector `proxy` / urltest `auto` 的 `outbounds` 数组 = 所有 client 合并后的全部 tag

---

## 2. API 规格

### 2.1 路径

```
GET /subs/search
```

> 完全独立于现有 `/sub/:subid`；二者**互不影响**。
> `/subs` 路由前缀建议与 `/sub` 共用同一 `subPort`、同一 `subPath` 父级或独立挂载（见 §4.4）。

### 2.2 查询参数

| 名称 | 类型 | 必选 | 说明 |
|---|---|---|---|
| `key` | string | 视配置 | 订阅 API 密钥；当 `settings.subApiKey` 非空时必填 |
| `name` | string | 否 | 用户名模糊匹配，**忽略大小写** |
| `username` | string | 否 | 用户名精确匹配；与 `name` 同时存在时优先 |
| `group` | string | 否 | 分组精确匹配 |
| `format` | string | 否 | 仅支持 `json`；缺省视为 `json` |

### 2.3 响应

**成功 200**：`Content-Type: application/json`，body 为聚合后的 sing-box 配置 JSON。

**响应头**：
| Header | 取值规则 |
|---|---|
| `Subscription-Userinfo` | `upload=Σup; download=Σdown; total=Σvolume; expire=<最近到期>` |
| `Profile-Update-Interval` | 复用 `settings.subUpdates`（小时数） |
| `Profile-Title` | 查询摘要，见 §2.4 |

**`expire` 聚合规则**：取所有匹配 client 中 `Expiry > 0` 的最小值；若全部为 0（永久），返回 `0`。

**`total` 聚合规则**：直接 `Σ client.Volume`（0 表示无限制，求和时按 0 处理，不特殊化）。

### 2.4 `Profile-Title` 生成规则

按以下优先级生成（首个匹配的规则胜出）：

| 场景 | Title |
|---|---|
| 仅 `username` | `user:<username>` |
| 仅 `group` | `group:<group>` |
| 仅 `name` | `search:<name>` |
| 多条件组合 | `batch:<k1=v1,k2=v2,...>`（保持参数原始顺序） |
| 无任何参数 | `all` |

### 2.5 错误响应

| HTTP | 触发条件 | Body |
|---|---|---|
| `400 Bad Request` | `format` 非空且 ≠ `json`；或匹配数超 2000 | 纯文本错误信息 |
| `401 Unauthorized` | `subApiKey` 已配置且 `key` 缺失/不匹配 | `unauthorized` |
| `404 Not Found` | 匹配 0 条 client | `no matching clients` |
| `500 Internal Server Error` | 渲染异常 | `Error!` |

---

## 3. 数据模型与配置

### 3.1 Setting 新增

`service/setting.go` 中 `defaultValueMap` 追加：

```go
"subApiKey": "",   // 空字符串表示不鉴权
```

**注意**：
- **不要**将 `subApiKey` 加入 `GetAllSetting` 的 delete 黑名单 —— 前端面板需要读出来展示给管理员
- 因为它是用户自己设置和分发的，不像 `secret`（session 加密）那样严格保密

### 3.2 SettingService 新增 getter

```go
// service/setting.go 追加
func (s *SettingService) GetSubApiKey() (string, error) {
    return s.getString("subApiKey")
}
```

### 3.3 数据库结构

**不需要新建表**，沿用现有 `settings` 表的 K-V 模式。**不写 migration 脚本**，因为 `GetAllSetting` 首次访问时会自动 upsert 默认值。

---

## 4. 后端实现细节

### 4.1 Step 1 — ClientService 新增过滤查询

**文件**：`service/client.go`

```go
// ClientFilter 聚合订阅查询条件；所有字段为空表示不限。
type ClientFilter struct {
    NameLike string // 模糊匹配（忽略大小写）
    Name     string // 精确匹配；与 NameLike 同时存在时优先
    Group    string // 分组精确匹配
}

const batchSearchLimit = 2000

// SearchClients 用于聚合订阅 API。仅返回 enable=true 的 client；超过 batchSearchLimit 返回错误。
func (s *ClientService) SearchClients(f ClientFilter) ([]*model.Client, error) {
    db := database.GetDB()
    tx := db.Model(&model.Client{}).Where("enable = ?", true)

    if f.Name != "" {
        tx = tx.Where("name = ?", f.Name)
    } else if f.NameLike != "" {
        tx = tx.Where("LOWER(name) LIKE LOWER(?)", "%"+f.NameLike+"%")
    }
    if f.Group != "" {
        tx = tx.Where(`"group" = ?`, f.Group)
    }

    var clients []*model.Client
    if err := tx.Limit(batchSearchLimit + 1).Find(&clients).Error; err != nil {
        return nil, err
    }
    if len(clients) > batchSearchLimit {
        return nil, common.NewErrorf("too many matches (>%d), narrow your filter", batchSearchLimit)
    }
    return clients, nil
}
```

> ⚠️ SQL 注意：`group` 是 SQLite 保留字，**必须用双引号包裹**：`"group" = ?`。

### 4.2 Step 2 — 复用 JsonService 内部方法

**文件**：`sub/jsonService.go`

**仅做最小可见性调整**，不改任何行为：
- 现有方法 `getData`、`getOutbounds`、`addDefaultOutbounds`、`addOthers`、`pushMixed` **已经是小写包内可见**，无需改动；新文件 `batchJsonService.go` 同包即可直接调用。
- **不要**改方法签名，不要重构。

### 4.3 Step 3 — 新增 BatchJsonService

**新文件**：`sub/batchJsonService.go`

```go
package sub

import (
    "encoding/json"
    "fmt"

    "github.com/admin8800/s-ui/database"
    "github.com/admin8800/s-ui/database/model"
    "github.com/admin8800/s-ui/service"
    "github.com/admin8800/s-ui/util"
)

type BatchJsonService struct {
    service.SettingService
    service.ClientService
    JsonService  // 复用 getOutbounds / addDefaultOutbounds / addOthers / pushMixed
    LinkService
}

// GetBatchJson 渲染聚合 sing-box JSON 订阅。
// clients 由调用方先用 ClientService.SearchClients 过滤好。
func (b *BatchJsonService) GetBatchJson(clients []*model.Client, title string) (*string, []string, error) {
    if len(clients) == 0 {
        return nil, nil, nil // 调用方负责翻译为 404
    }

    // 一次性预加载所有 client 引用到的 inbound（避免 N+1）
    inboundMap, err := b.preloadInbounds(clients)
    if err != nil {
        return nil, nil, err
    }

    var mergedOutbounds []map[string]interface{}
    var mergedTags []string

    for _, c := range clients {
        // 1. 取该 client 的 inbound 列表
        var clientInbounds []uint
        if err := json.Unmarshal(c.Inbounds, &clientInbounds); err != nil {
            return nil, nil, err
        }
        var inbounds []*model.Inbound
        for _, id := range clientInbounds {
            if in, ok := inboundMap[id]; ok {
                inbounds = append(inbounds, in)
            }
        }

        // 2. 复用 JsonService.getOutbounds 拿到 outbounds + tags
        outs, tags, err := b.JsonService.getOutbounds(c.Config, inbounds)
        if err != nil {
            return nil, nil, err
        }

        // 3. 给 tag 加 client 前缀，避免冲突
        prefix := fmt.Sprintf("[%s] ", c.Name)
        for i := range *outs {
            origTag, _ := (*outs)[i]["tag"].(string)
            (*outs)[i]["tag"] = prefix + origTag
        }
        prefixedTags := make([]string, len(*tags))
        for i, t := range *tags {
            prefixedTags[i] = prefix + t
        }

        // 4. 处理 client.Links 的 external link（与 JsonService.GetJson 同逻辑）
        links := b.LinkService.GetLinks(&c.Links, "external", "")
        tagNumEnable := 0
        if len(links) > 1 {
            tagNumEnable = 1
        }
        for index, link := range links {
            jOut, tag, err := util.GetOutbound(link, (index+1)*tagNumEnable)
            if err == nil && len(tag) > 0 {
                prefTag := prefix + tag
                (*jOut)["tag"] = prefTag
                *outs = append(*outs, *jOut)
                prefixedTags = append(prefixedTags, prefTag)
            }
        }

        mergedOutbounds = append(mergedOutbounds, *outs...)
        mergedTags = append(mergedTags, prefixedTags...)
    }

    // 5. 复用默认 selector/urltest/direct + 模板
    b.JsonService.addDefaultOutbounds(&mergedOutbounds, &mergedTags)

    var jsonConfig map[string]interface{}
    if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
        return nil, nil, err
    }
    jsonConfig["outbounds"] = mergedOutbounds
    if err := b.JsonService.addOthers(&jsonConfig); err != nil {
        return nil, nil, err
    }

    result, err := json.MarshalIndent(jsonConfig, "", "  ")
    if err != nil {
        return nil, nil, err
    }
    resultStr := string(result)

    updateInterval, _ := b.SettingService.GetSubUpdates()
    headers := util.GetBatchHeaders(clients, updateInterval, title)

    return &resultStr, headers, nil
}

func (b *BatchJsonService) preloadInbounds(clients []*model.Client) (map[uint]*model.Inbound, error) {
    idSet := make(map[uint]struct{})
    for _, c := range clients {
        var ids []uint
        if err := json.Unmarshal(c.Inbounds, &ids); err != nil {
            return nil, err
        }
        for _, id := range ids {
            idSet[id] = struct{}{}
        }
    }
    if len(idSet) == 0 {
        return map[uint]*model.Inbound{}, nil
    }
    allIds := make([]uint, 0, len(idSet))
    for id := range idSet {
        allIds = append(allIds, id)
    }

    var inbounds []*model.Inbound
    db := database.GetDB()
    if err := db.Model(model.Inbound{}).Preload("Tls").Where("id IN ?", allIds).Find(&inbounds).Error; err != nil {
        return nil, err
    }
    out := make(map[uint]*model.Inbound, len(inbounds))
    for _, in := range inbounds {
        out[in.Id] = in
    }
    return out, nil
}
```

> 实现注意：`defaultJson` 常量在 `jsonService.go` 中已定义为包级常量，新文件同包可直接引用。

### 4.4 Step 4 — HTTP 路由 & Handler

**文件**：`sub/subHandler.go`

在 `SubHandler` 结构体追加 `BatchJsonService`，在 `initRouter` 中**新增**一个独立 group：

```go
type SubHandler struct {
    service.SettingService
    SubService
    JsonService
    ClashService
    BatchJsonService // 新增
}

func NewSubHandler(g *gin.RouterGroup) {
    a := &SubHandler{}
    a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
    g.GET("/:subid", s.subs)
    g.HEAD("/:subid", s.subHeaders)
}

// 在 sub.Server 中（见 §4.5）额外挂载一个独立 RouterGroup："/subs"
func (s *SubHandler) InitBatchRouter(g *gin.RouterGroup) {
    g.GET("/search", s.batchSearch)
}

func (s *SubHandler) batchSearch(c *gin.Context) {
    // 1. 鉴权
    expectedKey, _ := s.SettingService.GetSubApiKey()
    if expectedKey != "" {
        if c.Query("key") != expectedKey {
            c.String(401, "unauthorized")
            return
        }
    }

    // 2. 校验 format
    format := c.Query("format")
    if format != "" && format != "json" {
        c.String(400, "unsupported format")
        return
    }

    // 3. 构造 filter
    filter := service.ClientFilter{
        Name:     c.Query("username"),
        NameLike: c.Query("name"),
        Group:    c.Query("group"),
    }

    clients, err := s.BatchJsonService.ClientService.SearchClients(filter)
    if err != nil {
        logger.Error(err)
        c.String(400, err.Error())
        return
    }
    if len(clients) == 0 {
        c.String(404, "no matching clients")
        return
    }

    title := buildBatchTitle(filter)
    result, headers, err := s.BatchJsonService.GetBatchJson(clients, title)
    if err != nil || result == nil {
        logger.Error(err)
        c.String(500, "Error!")
        return
    }

    s.addHeaders(c, headers)
    c.String(200, *result)
}

func buildBatchTitle(f service.ClientFilter) string {
    switch {
    case f.Name != "" && f.NameLike == "" && f.Group == "":
        return "user:" + f.Name
    case f.NameLike != "" && f.Name == "" && f.Group == "":
        return "search:" + f.NameLike
    case f.Group != "" && f.Name == "" && f.NameLike == "":
        return "group:" + f.Group
    case f.Name == "" && f.NameLike == "" && f.Group == "":
        return "all"
    }
    // 组合
    parts := []string{}
    if f.Name != "" {
        parts = append(parts, "username="+f.Name)
    }
    if f.NameLike != "" {
        parts = append(parts, "name="+f.NameLike)
    }
    if f.Group != "" {
        parts = append(parts, "group="+f.Group)
    }
    return "batch:" + strings.Join(parts, ",")
}
```

> 需 import `strings`、`github.com/admin8800/s-ui/service`、`github.com/admin8800/s-ui/logger`。

### 4.5 Step 5 — 在 sub.Server 中挂载 `/subs` 路由组

**文件**：`sub/sub.go`（查阅现有 `NewSubHandler(g)` 的调用位置；通常类似下面这种模式）

找到 sub 服务器初始化 Gin 引擎并注册路由的代码段（一般在 `Start()` 或 `initRouter()` 中调用 `NewSubHandler(rootGroup)`），在同一个 Gin engine 上额外注册：

```go
// 旧：现有单 client 订阅，挂在 settings.subPath 下（默认 /sub/）
subGroup := engine.Group(subPath)
sub.NewSubHandler(subGroup)

// 新增：聚合订阅，挂在 /subs 下（不受 subPath 影响，独立固定路径）
batchGroup := engine.Group("/subs")
handler := &sub.SubHandler{}
handler.InitBatchRouter(batchGroup)
```

> 若 `SubHandler` 不便复用，可在 `sub.go` 中直接 `NewBatchSubHandler(batchGroup)`：与 `NewSubHandler` 同款封装一个独立构造器。**任选其一**，保持 KISS。

### 4.6 Step 6 — 响应头聚合函数

**文件**：`util/subInfo.go`（追加，不改动 `GetHeaders`）

```go
func GetBatchHeaders(clients []*model.Client, updateInterval int, title string) []string {
    var sumUp, sumDown, sumTotal int64
    var minExpire int64 = 0
    for _, c := range clients {
        sumUp += c.Up
        sumDown += c.Down
        sumTotal += c.Volume
        if c.Expiry > 0 && (minExpire == 0 || c.Expiry < minExpire) {
            minExpire = c.Expiry
        }
    }
    return []string{
        fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", sumUp, sumDown, sumTotal, minExpire),
        fmt.Sprintf("%d", updateInterval),
        title,
    }
}
```

---

## 5. 前端实现细节

### 5.1 Settings.vue 新增输入框

**文件**：`frontend/src/views/Settings.vue`

在「订阅」Tab（与 `subEncode` / `subShowInfo` 同区域）追加一个可视/可隐藏的密码型输入框：

```vue
<v-text-field
  v-model="subApiKey"
  :label="$t('setting.subApiKey')"
  :hint="$t('setting.subApiKeyHint')"
  persistent-hint
  :type="showApiKey ? 'text' : 'password'"
  :append-inner-icon="showApiKey ? 'mdi-eye-off' : 'mdi-eye'"
  @click:append-inner="showApiKey = !showApiKey"
  clearable
  density="comfortable"
/>
```

`<script setup>` 中：

```ts
const showApiKey = ref(false)
const subApiKey = computed({
  get: () => settings.value.subApiKey ?? "",
  set: (v: string) => { settings.value.subApiKey = v ?? "" }
})
```

并将 `subApiKey: ""` 加入本地默认值对象（`Settings.vue` 中那个初始化 settings 的对象，第 ~170 行附近）。

### 5.2 i18n 新增 key

**文件**：`frontend/src/locales/en.ts`、`fa.ts`、`vi.ts`、`zhcn.ts`、`zhtw.ts`、`ru.ts`

在 `setting` 节点下追加：

| 语言 | subApiKey | subApiKeyHint |
|---|---|---|
| en | `"Batch Subscription API Key"` | `"Leave empty to disable auth on /subs/search"` |
| zhcn | `"聚合订阅 API 密钥"` | `"留空则 /subs/search 接口不鉴权"` |
| zhtw | `"聚合訂閱 API 金鑰"` | `"留空則 /subs/search 介面不鑑權"` |
| fa | （波斯文翻译，由实现者决定） | 同上 |
| vi | `"Khóa API Đăng ký gộp"` | `"Để trống để tắt xác thực /subs/search"` |
| ru | `"API-ключ агрегированной подписки"` | `"Оставьте пустым, чтобы отключить аутентификацию /subs/search"` |

### 5.3 严禁的改动

- **不要**修改前端任何与 client 列表、单 client 订阅相关的视图与逻辑
- **不要**在前端添加调用 `/subs/search` 的代码 —— 该 API 是给外部调用的，前端只负责"展示密钥配置"

---

## 6. 验证清单

启动面板后（`./runSUI.sh` 或直接运行 `sui`），用 curl 完成以下场景：

```bash
BASE='http://127.0.0.1:2096'

# 1. 未配置 subApiKey → 无需 key，应 200
curl -i "$BASE/subs/search?format=json"

# 2. 配置 subApiKey=abc 后
curl -i "$BASE/subs/search?format=json"             # 期望 401 unauthorized
curl -i "$BASE/subs/search?format=json&key=wrong"   # 期望 401
curl -i "$BASE/subs/search?format=json&key=abc"     # 期望 200

# 3. 分组筛选（假设存在 group=vip 的 client）
curl -s "$BASE/subs/search?group=vip&key=abc" | jq '.outbounds | length'

# 4. 模糊用户名
curl -s "$BASE/subs/search?name=ali&key=abc" | jq '.outbounds[].tag'
# 期望：所有 tag 形如 "[alice] xxx" 或 "[ali-test] xxx"

# 5. 精确用户名（单个导出）
curl -s "$BASE/subs/search?username=alice&key=abc" | jq

# 6. 组合（AND）
curl -s "$BASE/subs/search?group=vip&name=ali&key=abc" | jq

# 7. 命中 0 条
curl -i "$BASE/subs/search?username=__nope__&key=abc"  # 期望 404

# 8. 非法 format
curl -i "$BASE/subs/search?format=yaml&key=abc"        # 期望 400

# 9. 响应头检查（任一成功请求）
curl -sI "$BASE/subs/search?key=abc" | grep -E 'Subscription-Userinfo|Profile-'
# 期望：upload/download/total 为累加值；expire 为最近到期；Profile-Title=all
```

### 验证通过标准

- ✅ JSON 合法可被 sing-box 解析（`outbounds[].tag` 全局唯一）
- ✅ tag 全部带 `[clientName] ` 前缀
- ✅ selector `proxy` 和 urltest `auto` 的 `outbounds` 字段包含所有合并 tag
- ✅ 响应头流量字段为所有命中 client 之和；expire 为最近一个正值
- ✅ 现有 `/sub/:subid` 接口行为完全不变
- ✅ 面板「设置 → 订阅」中可以正常显示、编辑、保存 `subApiKey`

---

## 7. 编码约束（必读）

来自 `CLAUDE.md`，**违反任一条都需要返工**：

1. **KISS**：用最少代码完成功能；不写未要求的"灵活性"
2. **YAGNI**：不为"未来可能要支持 Clash" 提前抽象；当前只做 JSON
3. **外科手术式改动**：每一行新增/修改都要可追溯到本文档的某项需求
4. **不改无关代码**：哪怕你看到风格不一致、有注释错别字，**不动**
5. **匹配现有风格**：变量命名、错误返回、import 顺序参考同包既有文件
6. **注释语言一致**：现有代码使用中文/英文混合，新增注释**沿用同文件风格**
7. **绝对不要**：
   - 修改 `/sub/:subid` 现有逻辑
   - 修改 `ClashService`
   - 改数据库 schema 或新建表
   - 引入新的第三方依赖
   - 修改 apiv2 token 系统
   - 主动执行 `git commit` / `git push`（用户会自己提交）

---

## 8. 实施顺序（推荐）

```
1. 后端：service/setting.go  +  defaultValueMap 加 subApiKey + GetSubApiKey
   ↓ 验证：编译通过；启动后 DB 出现 key 行
2. 后端：service/client.go   +  ClientFilter + SearchClients
   ↓ 验证：编译通过；写个临时 main 跑一下 SQL（可选）
3. 后端：util/subInfo.go     +  GetBatchHeaders
   ↓ 验证：编译通过
4. 后端：sub/batchJsonService.go  新文件
   ↓ 验证：编译通过
5. 后端：sub/subHandler.go    追加 InitBatchRouter / batchSearch
   后端：sub/sub.go           注册 /subs RouterGroup
   ↓ 验证：启动面板，curl 走通 §6 全部场景
6. 前端：Settings.vue + 6 个 locale 文件
   ↓ 验证：面板 UI 正常显示和保存
7. 自检：再跑一遍 §6 所有 curl，确认无回归
```

---

## 9. 提交规范（如需 commit）

参考 `编译和git规范.md`。本功能预计 1 个 feat commit：

```
feat: 聚合订阅 API（/subs/search）

- 新增 GET /subs/search，支持按 name 模糊 / username 精确 / group 筛选
- 新增 settings.subApiKey 用于该接口鉴权，空值时不鉴权
- 输出 sing-box JSON 订阅，多 client outbound tag 加前缀防冲突
- 响应头流量累加、expire 取最近到期
```

> **不要**自行执行 commit，等待用户指示。

---

## 10. 已知限制与不做事项

- **本期不支持** Clash YAML 聚合输出（仅 JSON）
- **本期不支持** 原始 base64 订阅聚合输出
- **本期不支持** 前端 UI 调用 / 展示聚合订阅 URL（仅展示密钥设置）
- **本期不做** 聚合订阅访问日志独立审计（沿用 gin 默认 log）
- **本期不做** 速率限制、IP 白名单

如需以上特性，需另立计划。

---

**文档版本**：1.0
**生成日期**：2026-06-20
**对应代码版本**：S-UI v1.4.3（HEAD `e1606e6`）
