# 开发任务：出站批量导入支持「本地粘贴节点」模式

> **目标读者**：实施此任务的 AI agent。
> **本文档原则**：所有结论已基于源码确认；agent 无需再做仓库探索。按本文逐项实施即可。
> **不允许的动作**：执行 `git commit/push`、修改 sing-box 配置生成主链路、改动 `sub/` 订阅服务、改动 `util.GetOutbound` 原签名。

---

## 1. 背景

当前出站批量导入仅支持「输入订阅 URL」，由后端 HTTP GET 后解析。
现需新增「本地粘贴节点」模式：用户在前端 textarea 直接粘贴多行节点，后端不发外部请求即可解析为 sing-box outbound 数组。

### 1.1 需要新增的输入格式

| 格式 | 例子 | 说明 |
| --- | --- | --- |
| 现有 URI 协议 | `vmess://...`、`vless://...`、`trojan://...`、`hy/hy2://...`、`tuic://...`、`anytls://...`、`ss://...`、`naive+https://...`、`naive+quic://...`、`http2://...` | 已实现于 `util.GetOutbound`，**继续复用，不动签名** |
| **socks 简写**（新） | `1.2.3.4:1080`<br>`1.2.3.4:1080#tag01`<br>`1.2.3.4:1080:user:pass#tag02`<br>`socks://1.2.3.4:1080:user:pass#tag03` | 默认 `version="5"`，可选用户密码，可选 tag |
| **http 简写**（新） | `http://1.2.3.4:8080#tag04`<br>`http://1.2.3.4:8080:user:pass#tag05` | 必须显式 `http://` 前缀，**不带 version 字段** |

### 1.2 整体行为

- 多行混排：上述任意格式同一份 textarea 内混用须能逐行解析；空行跳过；解析失败的行静默跳过，不阻断整体。
- 整体 base64：textarea 内容若是合法 base64，须自动解码后再按行解析（沿用 `util.StrOrBase64Encoded`）。
- 整体 sing-box JSON：内容若整体是 `{ ... }` 且含 `outbounds` 数组，按既有 JSON 分支处理（与外部订阅 URL 完全一致的逻辑）。

---

## 2. 当前代码定位（已经替你定位好，按行号直接改）

| 文件 | 行号 | 现状 |
| --- | --- | --- |
| `util/linkToJson.go` | 14–39 | `GetOutbound(uri, i)` 入口；switch 各协议；**保留不动** |
| `util/subToJson.go` | 14–36 | `GetExternalLink(url)` 抓取 + 自动 base64 解码 |
| `util/subToJson.go` | 38–97 | `GetExternalSub(url)` 抓取后判断 JSON / 多行 URI 分支 |
| `util/base64.go` | 6–12 | `StrOrBase64Encoded(str)` 自动 base64 |
| `api/apiService.go` | 333–337 | `SubConvert(c)` 接收表单 `link` 参数 |
| `api/apiHandler.go` | 51–52 | `case "subConvert"` 路由 |
| `api/apiV2Handler.go` | 52–53 | `case "subConvert"` 路由（apiv2 镜像） |
| `frontend/src/layouts/modals/OutboundBulk.vue` | 1–155 | 批量导入对话框，唯一前端调用点 |
| `frontend/src/locales/{en,zhcn,zhtw,fa,vi,ru}.ts` | — | 国际化文案 |

---

## 3. 接口契约

### 3.1 新增 HTTP 接口

```
POST /<basePath>/api/subConvertText
POST /<basePath>/apiv2/subConvertText

Content-Type: application/x-www-form-urlencoded
Body:
  content=<原始多行文本，整体可被 base64 编码>

Response（与 /api/subConvert 完全一致）:
{
  "success": true,
  "obj": [
    { "type": "vmess",  "tag": "...", "server": "...", "server_port": 443, ... },
    { "type": "socks",  "tag": "proxy01", "server": "1.2.3.4", "server_port": 1080,
      "version": "5", "username": "u", "password": "p" },
    ...
  ]
}
```

### 3.2 解析规则（严格按此实现）

#### A. 简写正则与拆分策略
**不要直接用纯正则吃 password**（密码可能含 `:`、`#`）。改用以下分步：

1. `line = strings.TrimSpace(line)`；空 → 跳过。
2. 若以 `vmess://`、`vless://`、`trojan://`、`hy://`、`hy2://`、`hysteria://`、`hysteria2://`、`tuic://`、`anytls://`、`ss://`、`shadowsocks://`、`naive+https://`、`naive+quic://`、`http2://` 之一开头 → 走 `GetOutbound`，本段不处理。
3. 提前剥协议前缀：
   - 若以 `socks://` / `socks5://` 开头 → 协议 `socks`，剥前缀。
   - 若以 `http://` 开头 → 协议 `http`，剥前缀。
   - 否则 → 协议默认 `socks`。
4. 用 `strings.LastIndex(rest, "#")` 切出 tag（tag 部分原样保留，做 URL Unescape 容错）；剩余记为 `body`。
5. `parts := strings.SplitN(body, ":", 4)`：
   - `len(parts) == 2` → host, port。
   - `len(parts) == 4` → host, port, user, pass。
   - 其它长度 → 视为非法行，返回错误让外层跳过。
6. host 形如 `[xxxx::1]` → 剥方括号写入 `server`；其余原样。
7. `port, err := strconv.Atoi(parts[1])`；err != nil → 非法行跳过。
8. tag 缺省：`fmt.Sprintf("%s-%d", protocol, i)`（`i` 由上层传入，对应行号或自增）。

#### B. 返回的 outbound 结构

```jsonc
// socks
{
  "type": "socks",
  "tag":  "<tag>",
  "server": "<host>",
  "server_port": <port>,
  "version": "5",            // 固定 "5"
  "username": "<可选>",       // 没有就不写这个 key
  "password": "<可选>"        // 没有就不写这个 key
}

// http
{
  "type": "http",
  "tag":  "<tag>",
  "server": "<host>",
  "server_port": <port>,
  "username": "<可选>",
  "password": "<可选>"
  // 注意：不要写 version
}
```

---

## 4. 后端实施步骤

### 4.1 `util/linkToJson.go` — 新增简写解析与统一行入口

**追加到文件末尾**：

```go
// parseShortLine parses non-URI shorthand lines into a sing-box outbound.
// Supported shapes:
//   ip:port
//   ip:port#tag
//   ip:port:user:pass
//   ip:port:user:pass#tag
//   socks://ip:port[:user:pass][#tag]
//   http://ip:port[:user:pass][#tag]
//   [v6]:port[:user:pass][#tag]
func parseShortLine(line string, i int) (*map[string]interface{}, string, error) {
    line = strings.TrimSpace(line)
    if line == "" {
        return nil, "", common.NewError("empty line")
    }

    proto := "socks"
    switch {
    case strings.HasPrefix(line, "socks5://"):
        proto, line = "socks", strings.TrimPrefix(line, "socks5://")
    case strings.HasPrefix(line, "socks://"):
        proto, line = "socks", strings.TrimPrefix(line, "socks://")
    case strings.HasPrefix(line, "http://"):
        proto, line = "http", strings.TrimPrefix(line, "http://")
    }

    // split tag
    var tag string
    if idx := strings.LastIndex(line, "#"); idx >= 0 {
        tag = line[idx+1:]
        if t, err := url.QueryUnescape(tag); err == nil {
            tag = t
        }
        line = line[:idx]
    }

    // strip IPv6 brackets if present at start
    var host string
    var rest string
    if strings.HasPrefix(line, "[") {
        end := strings.Index(line, "]")
        if end < 0 {
            return nil, "", common.NewError("malformed ipv6")
        }
        host = line[1:end]
        rest = strings.TrimPrefix(line[end+1:], ":") // expect :port[:user:pass]
    } else {
        first := strings.Index(line, ":")
        if first < 0 {
            return nil, "", common.NewError("missing port")
        }
        host = line[:first]
        rest = line[first+1:]
    }

    parts := strings.SplitN(rest, ":", 3) // port, user, pass(rest)
    if len(parts) < 1 {
        return nil, "", common.NewError("invalid line")
    }
    port, err := strconv.Atoi(parts[0])
    if err != nil {
        return nil, "", common.NewError("invalid port")
    }

    out := map[string]interface{}{
        "type":        proto,
        "server":      host,
        "server_port": port,
    }
    if proto == "socks" {
        out["version"] = "5"
    }
    if len(parts) == 3 {
        out["username"] = parts[1]
        out["password"] = parts[2]
    } else if len(parts) == 2 {
        // only user without pass — treat whole thing as invalid to be safe
        return nil, "", common.NewError("missing password")
    }

    if tag == "" {
        tag = fmt.Sprintf("%s-%d", proto, i)
    }
    out["tag"] = tag

    return &out, tag, nil
}

// GetOutboundLine tries standard URI first, then shorthand. Returns (nil,"",nil) for empty.
func GetOutboundLine(line string, i int) (*map[string]interface{}, string, error) {
    line = strings.TrimSpace(line)
    if line == "" {
        return nil, "", nil
    }
    if out, tag, err := GetOutbound(line, i); err == nil {
        return out, tag, nil
    }
    return parseShortLine(line, i)
}
```

**imports 检查**：`strings`、`strconv`、`fmt`、`net/url`、`github.com/admin8800/s-ui/util/common`。前 4 个文件应已存在，确认即可。

### 4.2 `util/subToJson.go` — 抽函数 + 新本地入口

把当前 `GetExternalSub` 的 51–96 行（base64 已解码后的内容判定逻辑）抽出为私有 `parseSubContent`，并把文本分支里的 `GetOutbound` 换成 `GetOutboundLine`：

```go
// parseSubContent 接收已经过 base64 兜底解码的数据。
func parseSubContent(data string) ([]map[string]interface{}, error) {
    var result []map[string]interface{}

    if strings.HasPrefix(data, "{") && strings.HasSuffix(data, "}") {
        var jsonData map[string]interface{}
        if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
            logger.Warning("sub: Error unmarshalling JSON:", err)
            return nil, err
        }
        outbounds, ok := jsonData["outbounds"].([]any)
        if !ok {
            return nil, common.NewError("no outbounds in json")
        }
        for _, outbound := range outbounds {
            outboundMap, ok := outbound.(map[string]interface{})
            if ok && len(outboundMap) > 0 {
                oType, _ := outboundMap["type"].(string)
                switch oType {
                case "urltest", "direct", "selector", "block":
                    continue
                default:
                    result = append(result, outboundMap)
                }
            }
        }
        if len(result) == 0 {
            return nil, common.NewError("no result")
        }
        return result, nil
    }

    // 多行：每行先 URI 再简写
    links := strings.Split(data, "\n")
    for idx, link := range links {
        out, _, err := GetOutboundLine(link, idx+1)
        if err == nil && out != nil {
            result = append(result, *out)
        }
    }
    if len(result) == 0 {
        return nil, common.NewError("no result")
    }
    return result, nil
}

// GetExternalSub 远端 URL 模式（保留原签名）。
func GetExternalSub(url string) ([]map[string]interface{}, error) {
    if len(url) == 0 {
        return nil, common.NewError("no url")
    }
    data := GetExternalLink(url)
    if len(data) == 0 {
        return nil, common.NewError("no result")
    }
    return parseSubContent(data)
}

// ParseLocalSub 本地文本模式。
func ParseLocalSub(content string) ([]map[string]interface{}, error) {
    if len(content) == 0 {
        return nil, common.NewError("no content")
    }
    data := StrOrBase64Encoded(content)
    return parseSubContent(data)
}
```

> **不要删除** `GetExternalLink`、`GetExternalSub`、`StrOrBase64Encoded` 的导出名，外部仍可能调用。

### 4.3 `api/apiService.go` — 新增 handler

在 `SubConvert` 函数下方追加：

```go
func (a *ApiService) SubConvertText(c *gin.Context) {
    content := c.Request.FormValue("content")
    result, err := util.ParseLocalSub(content)
    jsonObj(c, result, err)
}
```

### 4.4 `api/apiHandler.go` — 注册 v1 路由

在 `postHandler` switch 内 `case "subConvert"` 之后追加：

```go
case "subConvertText":
    a.ApiService.SubConvertText(c)
```

### 4.5 `api/apiV2Handler.go` — 注册 v2 路由

在 `postHandler` switch 内 `case "subConvert"` 之后追加同一行（apiv2 必须镜像 v1，保持脚本调用方一致性）：

```go
case "subConvertText":
    a.ApiService.SubConvertText(c)
```

---

## 5. 前端实施步骤

### 5.1 `frontend/src/layouts/modals/OutboundBulk.vue`

#### 改动 A：template 第 9–23 行替换为 Tab 结构

```vue
<v-row v-if="outbounds.length==0">
  <v-col cols="12">
    <v-tabs v-model="mode" density="compact" color="primary" align-tabs="center">
      <v-tab value="url">{{ $t('client.sub') }}</v-tab>
      <v-tab value="text">{{ $t('out.pasteNodes') }}</v-tab>
    </v-tabs>
  </v-col>
  <v-col cols="12">
    <v-window v-model="mode">
      <v-window-item value="url">
        <v-text-field v-model="link"
          dir="ltr"
          :label="$t('client.sub')"
          placeholder="http[s]://<domain>[:]<port>/<path>"
          hide-details />
      </v-window-item>
      <v-window-item value="text">
        <v-textarea v-model="content"
          dir="ltr"
          rows="10"
          :label="$t('out.pasteNodes')"
          :placeholder="textPlaceholder"
          hide-details />
      </v-window-item>
    </v-window>
  </v-col>
  <v-col cols="12">
    <v-checkbox v-model="addUrlTest" :label="$t('out.addUrlTest')" />
  </v-col>
  <v-col cols="12" align="center">
    <v-btn hide-details variant="tonal" :loading="loading" @click="submitConvert">{{ $t('submit') }}</v-btn>
  </v-col>
</v-row>
```

#### 改动 B：`data()` 新增字段

```ts
data() {
  return {
    loading: false,
    mode: 'url',
    link: "",
    content: "",
    outbounds: <Outbound[]>[],
    outChecks: <number[]>[],
    addUrlTest: false,
  }
},
```

#### 改动 C：把 `linkCheck` 拆成 `submitConvert` + 公共填充

```ts
async submitConvert() {
  this.loading = true
  this.outbounds = []
  const msg = this.mode === 'url'
    ? await HttpUtils.post('api/subConvert',     { link: this.link })
    : await HttpUtils.post('api/subConvertText', { content: this.content })
  if (msg.success && msg.obj?.length > 0) {
    this.fillOutbounds(msg.obj)
  }
  this.loading = false
},

fillOutbounds(list: any[]) {
  list.forEach((o: any, index: number) => {
    if (this.newOutboundTags.includes(o.tag)) o.tag = o.tag + "-" + (index + 1)
    this.outbounds.push(createOutbound(o.type, o))
    this.outChecks.push(0)
  })
  if (this.addUrlTest) {
    const urlTestTag = "urltest-" + RandomUtil.randomSeq(3)
    this.outbounds.push(createOutbound("urltest", {
      tag: urlTestTag,
      outbounds: this.outbounds.map((o: Outbound) => o.tag),
      interrupt_exist_connections: false,
      interval: "30s"
    }))
  }
},
```

> 旧的 `linkCheck` 方法删除；模板里所有 `@click="linkCheck"` 改为 `@click="submitConvert"`。

#### 改动 D：`resetData` 补两行

```ts
resetData() {
  this.outbounds = []
  this.outChecks = []
  this.link = ""
  this.content = ""
  this.mode = 'url'
  this.addUrlTest = false
  this.loading = false
},
```

#### 改动 E：computed 补 placeholder（避免在模板里写多行字符串）

```ts
computed: {
  newOutboundTags(): string[] {
    return this.outbounds.map((o: Outbound) => o.tag)
  },
  textPlaceholder(): string {
    return 'vmess://...\nvless://...\n1.2.3.4:1080#proxy01\n1.2.3.4:1080:user:pass#proxy02\nhttp://1.2.3.4:8080#proxy03'
  }
}
```

### 5.2 i18n（6 个文件）

在以下 6 个文件的 `out` 对象内追加 `pasteNodes` 字段：

| 文件 | 值 |
| --- | --- |
| `frontend/src/locales/en.ts`   | `pasteNodes: 'Paste nodes'` |
| `frontend/src/locales/zhcn.ts` | `pasteNodes: '粘贴节点'` |
| `frontend/src/locales/zhtw.ts` | `pasteNodes: '貼上節點'` |
| `frontend/src/locales/fa.ts`   | `pasteNodes: 'چسباندن نودها'` |
| `frontend/src/locales/vi.ts`   | `pasteNodes: 'Dán nút'` |
| `frontend/src/locales/ru.ts`   | `pasteNodes: 'Вставить узлы'` |

> 如果 `out` 对象在某语言文件里不存在，沿用与 `addUrlTest` 同层级即可。

---

## 6. 测试用例（必须全部通过）

### 6.1 后端单测（建议新建 `util/linkToJson_test.go`）

| 输入 | 期望 type | 期望 tag | 期望 server / port | 备注 |
| --- | --- | --- | --- | --- |
| `1.2.3.4:1080` | `socks` | `socks-7` | `1.2.3.4` / `1080` | i=7，tag 缺省自动生成；无 user/pass |
| `1.2.3.4:1080#abc` | `socks` | `abc` | 同上 | tag 来自 `#` |
| `1.2.3.4:1080:u:p#abc` | `socks` | `abc` | 同上 | 带账号 |
| `socks5://1.2.3.4:1080:u:p#abc` | `socks` | `abc` | 同上 | socks5 前缀 |
| `http://1.2.3.4:8080:u:p#abc` | `http` | `abc` | `1.2.3.4` / `8080` | 不应含 `version` 字段 |
| `[2001:db8::1]:1080#v6` | `socks` | `v6` | `2001:db8::1` / `1080` | IPv6 字面量 |
| `1.2.3.4:abc#bad` | （错误） | — | — | 端口非数字 → 跳过 |
| `vmess://eyJ2IjoiMiIs...` | `vmess` | （来自原协议解析） | — | 标准 URI 走 `GetOutbound` |
| 空行 | （nil, nil）| — | — | 空行不报错也不产出 |

### 6.2 接口端到端

```bash
# 1. 文本模式 — 多行混排
curl -X POST http://localhost:2095/app/api/subConvertText \
  -b cookie.txt \
  --data-urlencode 'content=1.2.3.4:1080#a
5.6.7.8:1080:u:p#b
http://9.9.9.9:8080#c'
# 预期 obj 长度 = 3，依次为 socks(无pass)/socks(带pass)/http

# 2. 文本模式 — 整体 base64
echo -n '1.2.3.4:1080#a
5.6.7.8:1080#b' | base64
# 把输出贴到 content 字段，期望 obj 长度 = 2

# 3. 文本模式 — sing-box JSON
curl -X POST http://localhost:2095/app/api/subConvertText \
  -b cookie.txt \
  --data-urlencode 'content={"outbounds":[{"type":"vmess","tag":"x","server":"1.1.1.1","server_port":443,"uuid":"..."},{"type":"direct","tag":"d"}]}'
# 预期 obj 长度 = 1（direct 被过滤）

# 4. 回归 — URL 模式必须仍然工作
curl -X POST http://localhost:2095/app/api/subConvert \
  -b cookie.txt --data-urlencode 'link=https://example.com/sub'
# 行为与改动前一致
```

### 6.3 前端冒烟

1. 打开「出站」→「批量导入」对话框。
2. 默认 Tab 为「订阅 URL」，输入框、按钮、行为与改动前一致。
3. 切换到「粘贴节点」Tab，textarea 出现，placeholder 展示示例。
4. 粘贴 3 行混排（vmess + socks 简写 + http 简写），点击按钮 → 表格出现 3 条记录，type 列分别为 vmess/socks/http。
5. 勾选 `addUrlTest` 再点按钮 → 末尾追加一条 `urltest`。
6. 点击保存 → 各条 outbound 入库；存在重复 tag 时按现有逻辑显示红叉。
7. 关闭对话框再打开 → `resetData` 确保 textarea、link、mode 均归零。

---

## 7. 验收 checklist

- [ ] `util/linkToJson.go` 新增 `parseShortLine` + `GetOutboundLine`，未改 `GetOutbound` 签名与原行为。
- [ ] `util/subToJson.go` 抽出 `parseSubContent`，新增 `ParseLocalSub`，多行分支改用 `GetOutboundLine`。
- [ ] `api/apiService.go` 新增 `SubConvertText`。
- [ ] `api/apiHandler.go` 与 `api/apiV2Handler.go` 同步新增 `case "subConvertText"`。
- [ ] `frontend/src/layouts/modals/OutboundBulk.vue` 增加 Tab 切换、`content` 字段、`submitConvert`、`fillOutbounds`、`resetData` 补全、`textPlaceholder`；删除旧 `linkCheck`。
- [ ] 6 个语言文件均新增 `out.pasteNodes`。
- [ ] 全部「6. 测试用例」通过。
- [ ] `go vet ./...` 与 `go build -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" -o /tmp/sui main.go` 均成功。
- [ ] `cd frontend && npm run build` 成功（vue-tsc 不报错）。
- [ ] **没有**修改 `sub/`、`core/`、`service/` 任何文件。
- [ ] **没有**修改 `util.GetOutbound`、`util.GetExternalLink`、`util.GetExternalSub`、`util.StrOrBase64Encoded` 的导出签名。
- [ ] **没有**执行任何 `git commit`、`git push`、`git tag`。

---

## 8. 反模式（不要做）

1. ❌ 把简写解析塞进 `util.GetOutbound`。理由：`sub/jsonService.go`、`sub/clashService.go` 在解析 client.Links 的「external link」时调用了 `GetOutbound`，那里只允许标准 URI；引入简写会让用户填的普通文本被误吃。
2. ❌ 直接复用 `SubConvert` 这一个 handler 同时接收 `link` 和 `content`。理由：增加 if 分支语义混乱，且和现有 `link` 参数名冲突；新增独立 handler 更清晰。
3. ❌ 在前端用 `axios` 之外的 http 库。沿用 `HttpUtils`。
4. ❌ 修改 `OutboundBulk.vue` 保存逻辑（`saveChanges`）。这次只动「获取候选 outbound」前半段。
5. ❌ 向 i18n 文件批量加无关 key。本次只允许新增 `out.pasteNodes` 一个 key。
6. ❌ 写「自动嗅探协议」之类的探活逻辑。只做格式解析，绝不发外网请求。
7. ❌ 用 base64 解码失败就报错。`StrOrBase64Encoded` 已经保证「解不出就原样返回」，沿用即可。

---

## 9. 设计取舍备忘

- **为什么 `socks` 默认 v5**：sing-box 的 socks outbound 必须显式 `version` 字段；老服务商常给 `ip:port:user:pass` 默认就是 socks5 协议。
- **为什么 http 简写需要前缀**：避免 `1.2.3.4:8080` 之类纯地址被歧义识别；socks 是更常见的"无前缀"形态，保留它作为默认。
- **为什么 tag 缺省用 `proto-i`**：与现有 `urltest-xxx`、`direct` 等 tag 风格一致；i 由调用方传入，可避免重复。
- **为什么把解析下沉到 `util` 而非 `api`**：方便将来扩展（如 CLI 子命令 `sui parse < file`）复用同一份解析逻辑。
- **为什么 v1/v2 都注册**：apiv2 是脚本/外部集成用，许多用户的运维脚本会调用该接口批量导入节点。

---

## 10. 完成后的输出

完成后请向用户报告：

1. 修改的文件清单（路径）。
2. 新增的行数总和与删除的行数总和。
3. `go build`、`npm run build` 两条命令的退出码。
4. 测试用例 6.1、6.2、6.3 的逐项 pass/fail。

**不要**自动 `git commit`，**不要**自动 `git push`，**不要**修改其它无关文件。
