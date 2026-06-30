# 开发任务：路由规则批量导入 API（后端冲突校验）

> **目标读者**：实施此任务的 AI agent。
> **本文档原则**：所有结论已基于源码确认；agent 无需再做仓库探索。按本文逐项实施即可。
> **不允许的动作**：执行 `git commit/push`；修改 sing-box 内核启动链路；改动 `sub/`、`core/`、`cronjob/` 任何文件；改动 `util.GetOutbound` 系列函数。

---

## 1. 背景与目标

S-UI 目前的路由规则批量导入逻辑全部在前端：解析 JSON → push 进 `Data().config.route.rules` → 用户再手动点页面顶部「保存」按钮才落库。

新需求：

1. 新增一个 **后端 API**：`POST /api/importRules`（v1/v2 同步），客户端直接 POST JSON 即可批量追加路由规则。
2. **后端做冲突校验**：当待导入规则中的某个用户名（`user` 或 `auth_user`）已经被现有 `route.rules` 中任一 `action="route"` 的规则占用时，**整条规则跳过**，不写入。
3. 重复策略**固定为「跳过」**（不可选 override；前端 / 第三方调用方都拿不到 override 选项，保护现有规则不被误覆盖）。
4. 接口成功后自动重启 sing-box 内核（沿用现有 config 保存链路）。
5. 接口返回 added / skipped 详情，便于调用方上报。

### 1.1 入参 JSON 形态（与现有「导入规则」弹窗格式保持兼容）

```json
{
  "rules": [
    { "auth_user": ["alice"], "action": "route", "outbound": "proxy-jp" },
    { "auth_user": ["bob"],   "action": "route", "outbound": "proxy-us" },
    { "auth_user": ["carol"], "action": "route", "outbound": "proxy-hk" }
  ],
  "rule_set": [ /* 可选，按 tag 去重追加 */ ],
  "final":   "proxy"   // 可选；非空时覆写 route.final
}
```

字段约定：

- **入站协议不区分**：调用方统一用 `auth_user` 字段写客户端名称即可（这是 S-UI UI 现行行为，与 `frontend/src/components/Rule.vue` 一致）。后端冲突检测同时识别 `user` 和 `auth_user` 两个字段（兼容历史 / 手写 JSON）。
- 单条规则可以是 simple 或 logical（`type:"logical"`）；logical 的冲突检测需要递归遍历子规则。

---

## 2. 当前代码定位

| 文件 | 行号 | 现状 |
| --- | --- | --- |
| `service/config.go` | 188–261 | `ConfigService.Save`：保存 `config` 对象时调用 `SaveConfig` + 异步 `restartCoreWithConfig` |
| `service/config.go` | 136–167 | `restartCoreWithConfig(config json.RawMessage)` 已存在，**直接复用** |
| `service/setting.go` | 349–363 | `GetConfig() / SaveConfig(tx, json.RawMessage)` 已存在 |
| `database/model/model.go` | 57–64 | `Changes` 表结构 |
| `api/apiService.go` | 1–29 | `ApiService` 结构体已嵌入 `ConfigService` |
| `api/apiHandler.go` | 34–64 | v1 POST switch 入口 |
| `api/apiV2Handler.go` | 39–59 | v2 POST switch 入口 |
| `frontend/src/types/rules.ts` | 1–81 | rule / ruleset 字段定义；`actionKeys` 不含 `user` / `auth_user`（冲突检测时要注意） |

> **注意**：`service` 包级变量 `LastUpdate` 与 `corePtr` 必须更新，避免前端 `lu` 检查不到变化、内核未热重载。

---

## 3. 接口契约

### 3.1 路由
```
POST /<basePath>/api/importRules     // 浏览器 session 鉴权
POST /<basePath>/apiv2/importRules   // Token 鉴权
```

### 3.2 请求

- `Content-Type: application/json`
- Body：第 1.1 节的 JSON 形态（**直接 raw JSON body，不用 form**）。

### 3.3 响应

成功（含全部跳过的情况）：
```json
{
  "success": true,
  "obj": {
    "added": 2,
    "skipped": 1,
    "skippedRules": [
      {
        "index": 1,
        "conflictUsers": ["bob"],
        "existingOutbound": ["old-proxy"]
      }
    ],
    "addedRulesets": 0,
    "totalRules": 12,
    "restarted": true
  }
}
```

失败（JSON 解析错误、底层 GORM 错误等）：
```json
{ "success": false, "msg": "importRules: <error>" }
```

### 3.4 冲突判定细则

- 仅当一条**已有规则**满足 `action == "route" 或 action 字段缺省` 时，其 `user` / `auth_user` 才计入"占用集合"。
- 一条**待导入规则**只要其全部 user/auth_user 与占用集合的交集非空 → 整条跳过。
- 占用集合是动态的：同一批次内**先成功的规则会把它的用户名加入占用集合**，避免同批内重复。
- logical 规则递归收集所有子规则的 user/auth_user。
- 跳过的规则会在响应里报告冲突的用户名和占用它们的出站列表。

---

## 4. 后端实施步骤

### 4.1 `service/config.go` 末尾追加导入实现

**imports 检查**：确保 `io` `time` `encoding/json` `errors` 都已导入；新加 import：`github.com/admin8800/s-ui/util/common`、`github.com/admin8800/s-ui/database`、`github.com/admin8800/s-ui/database/model`（这几个已存在，无需重复）。

```go
// ImportRuleResult 是 ImportRouteRules 的返回值。
type ImportRuleResult struct {
    Added         int                   `json:"added"`
    Skipped       int                   `json:"skipped"`
    SkippedRules  []ImportSkippedDetail `json:"skippedRules,omitempty"`
    AddedRulesets int                   `json:"addedRulesets"`
    TotalRules    int                   `json:"totalRules"`
    Restarted     bool                  `json:"restarted"`
}

type ImportSkippedDetail struct {
    Index            int      `json:"index"`
    ConflictUsers    []string `json:"conflictUsers"`
    ExistingOutbound []string `json:"existingOutbound,omitempty"`
}

type importRulesPayload struct {
    Rules   []map[string]interface{} `json:"rules"`
    RuleSet []map[string]interface{} `json:"rule_set,omitempty"`
    Final   string                   `json:"final,omitempty"`
}

// ImportRouteRules 批量追加路由规则；按用户名做冲突跳过；最终写库并热重载内核。
func (s *ConfigService) ImportRouteRules(data json.RawMessage, loginUser string) (*ImportRuleResult, error) {
    var payload importRulesPayload
    if err := json.Unmarshal(data, &payload); err != nil {
        return nil, common.NewErrorf("invalid payload: %v", err)
    }
    if len(payload.Rules) == 0 && len(payload.RuleSet) == 0 && payload.Final == "" {
        return nil, common.NewError("nothing to import")
    }

    // 读取当前 sing-box 配置
    cfgStr, err := s.SettingService.GetConfig()
    if err != nil {
        return nil, err
    }
    var cfg map[string]interface{}
    if err := json.Unmarshal([]byte(cfgStr), &cfg); err != nil {
        return nil, common.NewErrorf("invalid stored config: %v", err)
    }

    route, _ := cfg["route"].(map[string]interface{})
    if route == nil {
        route = map[string]interface{}{}
        cfg["route"] = route
    }

    existingRules := castMapSlice(route["rules"])
    existingSets := castMapSlice(route["rule_set"])

    // 收集已占用用户名 → 出站
    occupied := collectOccupiedUsers(existingRules)

    result := &ImportRuleResult{}

    // 逐条处理
    for i, r := range payload.Rules {
        incoming := collectRuleUsers(r)
        if len(incoming) == 0 {
            // 没有 user / auth_user 的规则不参与冲突，直接追加
            existingRules = append(existingRules, r)
            result.Added++
            continue
        }

        var conflicts []string
        outboundSet := map[string]struct{}{}
        for u := range incoming {
            if outbound, ok := occupied[u]; ok {
                conflicts = append(conflicts, u)
                if outbound != "" {
                    outboundSet[outbound] = struct{}{}
                }
            }
        }
        if len(conflicts) > 0 {
            existingOuts := make([]string, 0, len(outboundSet))
            for o := range outboundSet {
                existingOuts = append(existingOuts, o)
            }
            result.Skipped++
            result.SkippedRules = append(result.SkippedRules, ImportSkippedDetail{
                Index:            i,
                ConflictUsers:    conflicts,
                ExistingOutbound: existingOuts,
            })
            continue
        }

        // 通过校验：追加；并把这些用户名加入占用集合
        ob, _ := r["outbound"].(string)
        for u := range incoming {
            occupied[u] = ob
        }
        existingRules = append(existingRules, r)
        result.Added++
    }
    route["rules"] = existingRules

    // rule_set 按 tag 去重追加
    if len(payload.RuleSet) > 0 {
        tagSet := map[string]bool{}
        for _, rs := range existingSets {
            if t, ok := rs["tag"].(string); ok {
                tagSet[t] = true
            }
        }
        for _, rs := range payload.RuleSet {
            tag, _ := rs["tag"].(string)
            if tag == "" || tagSet[tag] {
                continue
            }
            existingSets = append(existingSets, rs)
            tagSet[tag] = true
            result.AddedRulesets++
        }
        route["rule_set"] = existingSets
    }

    // final 覆盖
    if payload.Final != "" {
        route["final"] = payload.Final
    }

    result.TotalRules = len(existingRules)

    // 没有任何实质变更则不写库、不重启
    if result.Added == 0 && result.AddedRulesets == 0 && payload.Final == "" {
        return result, nil
    }

    newCfg, err := json.MarshalIndent(cfg, "", "  ")
    if err != nil {
        return nil, err
    }

    db := database.GetDB()
    tx := db.Begin()
    defer func() {
        if err == nil {
            tx.Commit()
        } else {
            tx.Rollback()
        }
    }()
    if err = s.SettingService.SaveConfig(tx, newCfg); err != nil {
        return nil, err
    }
    if err = tx.Create(&model.Changes{
        DateTime: time.Now().Unix(),
        Actor:    loginUser,
        Key:      "config",
        Action:   "importRules",
        Obj:      data,
    }).Error; err != nil {
        return nil, err
    }
    LastUpdate = time.Now().Unix()

    // 异步重启 sing-box（与 ConfigService.Save 中保存 config 对象的做法一致）
    cfgCopy := make(json.RawMessage, len(newCfg))
    copy(cfgCopy, newCfg)
    go func() { _ = s.restartCoreWithConfig(cfgCopy) }()
    result.Restarted = true

    return result, nil
}

// ---- helpers ----

func castMapSlice(v interface{}) []map[string]interface{} {
    if v == nil {
        return nil
    }
    arr, ok := v.([]interface{})
    if !ok {
        return nil
    }
    out := make([]map[string]interface{}, 0, len(arr))
    for _, x := range arr {
        if m, ok := x.(map[string]interface{}); ok {
            out = append(out, m)
        }
    }
    return out
}

// collectOccupiedUsers 仅收集 action 缺省或 action=="route" 的规则中的 user/auth_user。
// 返回 map[username] = 第一个占用它的 outbound（用于报告冲突）。
func collectOccupiedUsers(rules []map[string]interface{}) map[string]string {
    occupied := map[string]string{}
    for _, r := range rules {
        if act, ok := r["action"].(string); ok && act != "" && act != "route" {
            continue
        }
        ob, _ := r["outbound"].(string)
        for u := range collectRuleUsers(r) {
            if _, exists := occupied[u]; !exists {
                occupied[u] = ob
            }
        }
    }
    return occupied
}

// collectRuleUsers 递归收集一条规则（含 logical 子规则）的所有 user / auth_user。
func collectRuleUsers(r map[string]interface{}) map[string]struct{} {
    out := map[string]struct{}{}
    pickArr := func(key string) {
        arr, ok := r[key].([]interface{})
        if !ok {
            return
        }
        for _, v := range arr {
            if s, ok := v.(string); ok && s != "" {
                out[s] = struct{}{}
            }
        }
    }
    pickArr("user")
    pickArr("auth_user")
    if t, _ := r["type"].(string); t == "logical" {
        if subs, ok := r["rules"].([]interface{}); ok {
            for _, sub := range subs {
                if m, ok := sub.(map[string]interface{}); ok {
                    for u := range collectRuleUsers(m) {
                        out[u] = struct{}{}
                    }
                }
            }
        }
    }
    return out
}
```

### 4.2 `api/apiService.go` 末尾追加 handler

```go
func (a *ApiService) ImportRules(c *gin.Context, loginUser string) {
    data, err := io.ReadAll(c.Request.Body)
    if err != nil {
        jsonMsg(c, "importRules", err)
        return
    }
    result, err := a.ConfigService.ImportRouteRules(json.RawMessage(data), loginUser)
    if err != nil {
        jsonMsg(c, "importRules", err)
        return
    }
    jsonObj(c, result, nil)
}
```

**imports 检查**：补 `"io"`、`"encoding/json"`（应该都已经存在）。

### 4.3 `api/apiHandler.go` 注册 v1 路由

在 `postHandler` switch 内 `case "subConvert":` 之后追加：

```go
case "importRules":
    a.ApiService.ImportRules(c, loginUser)
```

### 4.4 `api/apiV2Handler.go` 注册 v2 路由

在 `postHandler` switch 内 `case "subConvert":` 之后追加（apiv2 鉴权后 `username` 取自 token；handler 签名保持 `loginUser` 参数名以与 v1 对齐）：

```go
case "importRules":
    a.ApiService.ImportRules(c, username)
```

---

## 5. 前端调用（可选改造，本任务**不强制做**）

如果需要让现有「导入规则」对话框走新 API，改 1 处即可：

**`frontend/src/views/Rules.vue` 的 `saveImportRule`** 改为：

```ts
async function saveImportRule(block: any, mode: 'merge' | 'replace', applyFinal: boolean) {
  if (mode === 'replace') {
    // replace 模式仍走老路径（整体替换）
    route.value.rules = block.rules ?? []
    route.value.rule_set = block.rule_set ?? []
    if (applyFinal && block.final) route.value.final = block.final
    importRulesModal.value.visible = false
    await saveConfig()
    return
  }

  // merge 模式走新后端 API（后端做冲突校验 + 自动提交 + 热重载）
  const payload = {
    rules: block.rules ?? [],
    rule_set: block.rule_set ?? [],
    final: applyFinal ? block.final : undefined,
  }
  const msg = await HttpUtils.post('api/importRules', payload, { headers: { 'Content-Type': 'application/json' } })
  if (msg.success) {
    push.success({
      message: `added ${msg.obj.added}, skipped ${msg.obj.skipped}`
    })
    importRulesModal.value.visible = false
    // 触发一次全量拉取，刷新内存 config
    await Data().loadData()
  }
}
```

> 如果 `HttpUtils.post` 当前只支持 form-urlencoded，需要先在 `frontend/src/plugins/httputil.ts` 加一个支持 JSON body 的重载或新方法（**本任务不强求**，curl 调用 API 测试即可）。

---

## 6. 测试用例（必须全部通过）

### 6.1 基础：3 条全部新增（无冲突）

前置：现有 `route.rules` 为空，clients 表里有 alice/bob/carol。

```bash
curl -X POST http://localhost:2095/app/api/importRules \
  -b cookie.txt -H 'Content-Type: application/json' \
  -d '{
    "rules": [
      { "auth_user": ["alice"], "action": "route", "outbound": "proxy-jp" },
      { "auth_user": ["bob"],   "action": "route", "outbound": "proxy-us" },
      { "auth_user": ["carol"], "action": "route", "outbound": "proxy-hk" }
    ]
  }'
```

期望：
```json
{ "success": true, "obj": {
  "added": 3, "skipped": 0, "addedRulesets": 0,
  "totalRules": 3, "restarted": true
}}
```
后续 sing-box 重启日志可见；面板 Rules 页面刷新后能看到 3 张卡片。

### 6.2 冲突跳过：再来一次同样的请求

期望：
```json
{ "success": true, "obj": {
  "added": 0, "skipped": 3,
  "skippedRules": [
    { "index": 0, "conflictUsers": ["alice"], "existingOutbound": ["proxy-jp"] },
    { "index": 1, "conflictUsers": ["bob"],   "existingOutbound": ["proxy-us"] },
    { "index": 2, "conflictUsers": ["carol"], "existingOutbound": ["proxy-hk"] }
  ],
  "addedRulesets": 0, "totalRules": 3, "restarted": false
}}
```
关键校验：`restarted=false`（没有实质变更不重启）；DB 未写新 changes 记录。

### 6.3 部分冲突 + 部分新增

前置：6.1 已执行。

```bash
curl -X POST http://localhost:2095/app/api/importRules \
  -b cookie.txt -H 'Content-Type: application/json' \
  -d '{
    "rules": [
      { "auth_user": ["bob"],  "action": "route", "outbound": "different-jp" },
      { "auth_user": ["dave"], "action": "route", "outbound": "proxy-eu" }
    ]
  }'
```

期望 `added=1, skipped=1`；`route.rules` 总数变为 4；bob 仍然走 proxy-us（旧规则未被改）。

### 6.4 同批内自冲突

```bash
curl -X POST .../api/importRules ... -d '{
  "rules": [
    { "auth_user": ["eve"], "action": "route", "outbound": "x" },
    { "auth_user": ["eve"], "action": "route", "outbound": "y" }
  ]
}'
```

期望：第 1 条 added，第 2 条 skipped（eve 已在同批次中被占用）；`skippedRules[0].existingOutbound = ["x"]`。

### 6.5 logical 规则冲突

前置：现有规则含 `{ "auth_user": ["alice"], "action":"route", "outbound":"proxy-jp" }`。

```bash
curl -X POST .../api/importRules ... -d '{
  "rules": [
    {
      "type": "logical", "mode": "or",
      "rules": [
        { "user": ["alice"] },
        { "domain_keyword": ["youtube"] }
      ],
      "action": "route", "outbound": "proxy-us"
    }
  ]
}'
```

期望 `added=0, skipped=1, skippedRules[0].conflictUsers=["alice"]`。

### 6.6 非 route action 不参与冲突

前置：现有规则含 `{ "auth_user": ["alice"], "action":"reject" }`（不是 route）。

```bash
curl -X POST .../api/importRules ... -d '{
  "rules": [
    { "auth_user": ["alice"], "action":"route", "outbound":"proxy-jp" }
  ]
}'
```

期望 `added=1, skipped=0`（reject 不占用用户名）。

### 6.7 空 user / 通用规则

```bash
curl -X POST .../api/importRules ... -d '{
  "rules": [
    { "domain_suffix":["youtube.com"], "action":"route", "outbound":"proxy" }
  ]
}'
```

期望 `added=1, skipped=0`（无用户名，不参与冲突）。

### 6.8 ruleset 去重

```bash
curl -X POST .../api/importRules ... -d '{
  "rule_set": [
    { "tag":"geosite-cn", "type":"remote", "format":"binary",
      "url":"https://example.com/geosite-cn.srs" }
  ]
}'
```

第一次 `addedRulesets=1`，第二次 `addedRulesets=0`（同 tag 跳过）。

### 6.9 final 覆写

```bash
curl ... -d '{ "final": "proxy-hk" }'
```

期望 `added=0, addedRulesets=0`，但 `restarted=true`，DB 中 config.route.final 变为 `proxy-hk`。

### 6.10 鉴权

- 不带 cookie 调 `/api/importRules` → 302 重定向到登录页 / `{"success":false,"msg":"Invalid login"}`。
- 用 v2 token 调 `/apiv2/importRules` → 正常返回。
- 错误的 token → `{"success":false,"msg":"invalid token"}`。

---

## 7. 验收 checklist

- [ ] `service/config.go` 新增 `ImportRuleResult` / `ImportSkippedDetail` / `importRulesPayload` 三个类型 + `ImportRouteRules` 方法 + 三个 helper（`castMapSlice` / `collectOccupiedUsers` / `collectRuleUsers`）。
- [ ] `api/apiService.go` 新增 `ImportRules(c, loginUser)`。
- [ ] `api/apiHandler.go` 与 `api/apiV2Handler.go` 同步新增 `case "importRules"`。
- [ ] `LastUpdate` 在成功导入后被更新（前端 `lu` 增量能正确触发拉取）。
- [ ] 实质变更（`added > 0` 或 `addedRulesets > 0` 或 `final` 非空）时才写库 + 异步重启核心；全跳过时 `restarted=false` 且不写 changes。
- [ ] 每次实质变更都向 `changes` 表写一条 `key="config", action="importRules"`，`actor` 是登录用户 / token 对应用户名。
- [ ] 同批内自冲突：先到先得，后到者跳过。
- [ ] logical 规则的 user / auth_user 被递归收集，参与冲突检测。
- [ ] 非 `route` 的 action（reject / hijack-dns / sniff 等）**不占用**用户名。
- [ ] `go vet ./...` 与 `go build -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" -o /tmp/sui main.go` 均成功。
- [ ] 「6. 测试用例」全部 pass。
- [ ] 没有改动 `sub/` / `core/` / `cronjob/` / `database/model/` / `frontend/` 任何文件（前端调用为可选项，本任务不要求做）。

---

## 8. 反模式（不要做）

1. ❌ 把整个 sing-box 配置反序列化成强类型结构体。原始 `config` 在 settings 表里是开放 JSON，存在大量未在 S-UI 类型中的字段；用 `map[string]interface{}` 保留原貌，**只动 `route.rules` / `route.rule_set` / `route.final`**。
2. ❌ 用 GORM 行级 sql 写规则。规则**不是独立表**，全部存于 `settings.config` 的 JSON 串里；走 `SettingService.SaveConfig` 即可。
3. ❌ 暴露 `override` 参数。本接口契约里冲突策略**固定为 skip**，便于 API 调用方放心调用，不会破坏现有规则。
4. ❌ 同步重启核心。复用 `restartCoreWithConfig` 必须 `go func()` 异步调用，避免阻塞 HTTP 响应（参考 `ConfigService.Save` 中 `case "config"` 分支）。
5. ❌ 直接 `db.Save(&model.Setting{...})` 写 config。**必须**走 `tx.Begin` → `SaveConfig(tx, ...)` → `Commit`，保证写 config 和 changes 在一个事务里。
6. ❌ 忘记 `LastUpdate = time.Now().Unix()`。否则前端 `Data.loadData` 的 `lu` 增量检查会"看不到"这次变更，UI 不刷新。
7. ❌ 拷贝 newCfg 时直接传 `newCfg` 给 goroutine。`json.RawMessage` 底层是 `[]byte`，外层若后续修改会污染 goroutine 内的数据；按文档示例**先 make+copy 再传**。
8. ❌ 给规则强制加 `action` 默认值。sing-box 接受 action 缺省（视为 route），保留原样即可，但**冲突检测**时缺省 action 必须视为 route。
9. ❌ 用 form-urlencoded 接收。嵌套数组用 JSON body 才不会丢字段顺序与类型；handler 用 `io.ReadAll(c.Request.Body)` 读 raw body。

---

## 9. 设计取舍备忘

- **为什么冲突策略是 skip 不可配置**：本接口主要给第三方脚本与未来 GUI 批量场景使用；不可配置策略避免误操作覆盖现有 vmess/vless/socks 规则。如确实需要"替换某用户的旧规则"，调用方应先调 `GET /api/load` 自行计算 diff，再走 `POST /api/save?object=config`（已有接口）。
- **为什么不区分 user / auth_user 字段**：sing-box 这两个字段语义不同（前者匹配 vmess/vless/trojan 等，后者匹配 socks/http/mixed），但 S-UI UI 现行默认统一用 `auth_user`。本 API 冲突检测**双字段都识别**，调用方写哪个都不会绕过校验。
- **为什么把实现放 `ConfigService` 而非新建 `RulesService`**：rules 不是独立表，逻辑就是"读 config → 改 route 子节点 → 写 config + 重启"，与 ConfigService 现有职责完全对齐；新建 service 反而要重复嵌入 SettingService 与处理 corePtr。
- **为什么 final 覆盖也算"实质变更"**：final 影响默认出站，必须重启 sing-box 才能生效，所以 `restarted=true`。
- **为什么 ruleset 不做冲突检测**：ruleset 用 tag 作为标识，同 tag 视为重复，沿用现有 RuleImport 的语义即可。

---

## 10. 完成后的输出

完成后请向用户报告：

1. 修改的文件清单（路径）。
2. 新增的行数总和与删除的行数总和。
3. `go build` 退出码。
4. 测试用例 6.1–6.10 的逐项 pass/fail（curl 实测，附返回 JSON 摘要）。

**不要**自动 `git commit/push`，**不要**修改其它无关文件。
