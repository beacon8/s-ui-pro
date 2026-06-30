# S-UI 批量导入 API 文档

> 版本：v1.4.1+  
> 鉴权：v1 使用 Cookie Session（浏览器登录后的 `s-ui` Cookie）；v2 使用请求头 `Token: <token>`  
> 所有请求均为 `POST`，`Content-Type: application/x-www-form-urlencoded`（`/api/save` 系列）或 `application/json`（`/api/importRules`）

---

## 一、批量添加出站节点

### 1.1 接口说明

出站节点**没有专用的批量接口**，通过对现有 `POST /api/save` 接口**循环调用**实现批量写入。每次调用写入一条出站，后端即时热插拔到 sing-box（无需重启）。

如果需要从订阅链接或节点文本先做格式转换，可使用辅助接口先解析，再批量写入。

---

### 1.2 辅助接口：节点文本 → 出站对象列表

#### 方式 A：订阅链接转换

```
POST <basePath>/api/subConvert
Content-Type: application/x-www-form-urlencoded

link=<订阅URL>
```

- `link`：可访问的订阅地址（HTTP/HTTPS），后端会 GET 该 URL，支持 sing-box JSON 格式或多行节点 URI（整体 base64 亦可）。
- **不支持** Clash YAML 格式。

**响应：**
```json
{
  "success": true,
  "msg": "",
  "obj": [
    { "type": "vless", "tag": "proxy-01", "server": "1.2.3.4", "server_port": 443, ... },
    { "type": "vmess", "tag": "proxy-02", ... }
  ]
}
```

#### 方式 B：节点文本（多行 URI）转换

```
POST <basePath>/api/subConvertText
Content-Type: application/x-www-form-urlencoded

content=<多行节点URI文本>
```

- `content`：每行一条节点 URI，支持 `vmess://` `vless://` `trojan://` `ss://` `hysteria2://` `hy2://` `tuic://` `anytls://` `naive+https://` 等格式。
- 也支持：`ip:port#tag`（SOCKS5）、`ip:port:user:pass#tag`（带认证 SOCKS5）、`http://ip:port#tag`。

**响应：** 同方式 A，返回出站对象数组。

---

### 1.3 写入接口：单条出站保存

```
POST <basePath>/api/save
Content-Type: application/x-www-form-urlencoded

object=outbounds&action=new&data=<JSON字符串>
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `object` | string | ✅ | 固定值 `outbounds` |
| `action` | string | ✅ | `new`（新增）/ `edit`（编辑）/ `del`（删除） |
| `data` | string | ✅ | 出站对象的 JSON 字符串（见下方各协议示例） |

**响应：**
```json
{
  "success": true,
  "msg": "new",
  "obj": {
    "outbounds": [ ... ]
  }
}
```

---

### 1.4 批量写入脚本示例（curl）

**先从订阅链接解析，再逐条写入：**

```bash
BASE="http://localhost:2095/app"
COOKIE="s-ui=<your-session-cookie>"

# 步骤1：订阅链接解析
NODES=$(curl -s -X POST "$BASE/api/subConvert" \
  -b "$COOKIE" \
  -d "link=https://example.com/sub/your-token" | jq -c '.obj[]')

# 步骤2：逐条写入
echo "$NODES" | while IFS= read -r node; do
  curl -s -X POST "$BASE/api/save" \
    -b "$COOKIE" \
    --data-urlencode "object=outbounds" \
    --data-urlencode "action=new" \
    --data-urlencode "data=$node"
done
```

**或使用 v2 Token：**

```bash
BASE="http://localhost:2095/app"
TOKEN="your-api-token"

curl -s -X POST "$BASE/apiv2/save" \
  -H "Token: $TOKEN" \
  --data-urlencode "object=outbounds" \
  --data-urlencode "action=new" \
  --data-urlencode "data={\"type\":\"vless\",\"tag\":\"proxy-jp\",\"server\":\"1.2.3.4\",\"server_port\":443,\"uuid\":\"xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx\",\"tls\":{\"enabled\":true,\"server_name\":\"example.com\"}}"
```

---

### 1.5 各协议出站 data 字段示例

#### VLESS
```json
{
  "type": "vless",
  "tag": "vless-jp-01",
  "server": "1.2.3.4",
  "server_port": 443,
  "uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "flow": "xtls-rprx-vision",
  "tls": {
    "enabled": true,
    "server_name": "example.com",
    "utls": { "enabled": true, "fingerprint": "chrome" }
  }
}
```

#### VMess
```json
{
  "type": "vmess",
  "tag": "vmess-us-01",
  "server": "1.2.3.4",
  "server_port": 443,
  "uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "security": "auto",
  "alter_id": 0,
  "tls": { "enabled": true },
  "transport": { "type": "ws", "path": "/ws" }
}
```

#### Trojan
```json
{
  "type": "trojan",
  "tag": "trojan-hk-01",
  "server": "1.2.3.4",
  "server_port": 443,
  "password": "your-password",
  "tls": { "enabled": true, "server_name": "example.com" }
}
```

#### Shadowsocks
```json
{
  "type": "shadowsocks",
  "tag": "ss-sg-01",
  "server": "1.2.3.4",
  "server_port": 8388,
  "method": "aes-256-gcm",
  "password": "your-password"
}
```

#### Hysteria2
```json
{
  "type": "hysteria2",
  "tag": "hy2-tw-01",
  "server": "1.2.3.4",
  "server_port": 443,
  "password": "your-password",
  "tls": { "enabled": true, "server_name": "example.com" }
}
```

#### TUIC
```json
{
  "type": "tuic",
  "tag": "tuic-kr-01",
  "server": "1.2.3.4",
  "server_port": 443,
  "uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "password": "your-password",
  "congestion_control": "bbr",
  "tls": { "enabled": true, "server_name": "example.com" }
}
```

#### SOCKS5
```json
{
  "type": "socks",
  "tag": "socks5-01",
  "server": "1.2.3.4",
  "server_port": 1080,
  "version": "5",
  "username": "user",
  "password": "pass"
}
```

#### HTTP 代理
```json
{
  "type": "http",
  "tag": "http-proxy-01",
  "server": "1.2.3.4",
  "server_port": 8080,
  "username": "user",
  "password": "pass"
}
```

#### Selector（出站选择器）
```json
{
  "type": "selector",
  "tag": "proxy",
  "outbounds": ["vless-jp-01", "vmess-us-01", "trojan-hk-01"]
}
```

#### URLTest（自动测速）
```json
{
  "type": "urltest",
  "tag": "auto",
  "outbounds": ["vless-jp-01", "vmess-us-01"],
  "interval": "30s",
  "interrupt_exist_connections": false
}
```

---

### 1.6 删除出站

```
POST <basePath>/api/save
Content-Type: application/x-www-form-urlencoded

object=outbounds&action=del&data="proxy-jp-01"
```

- `data` 的值是出站 tag 的 JSON 字符串（即 `"` 包裹的 tag 名称）。

---

---

## 二、批量导入路由规则

### 2.1 接口说明

```
POST <basePath>/api/importRules      // Cookie session 鉴权（v1）
POST <basePath>/apiv2/importRules    // Token 鉴权（v2）

Content-Type: application/json
```

规则直接写入数据库并**异步热重载 sing-box 内核**，无需再点页面「保存」按钮。

**冲突策略（固定为跳过）：**
- 现有 `route.rules` 中 `action=route` 或 `action` 缺省的规则，其 `user` / `auth_user` 计入"占用集合"。
- 待导入规则的 `user` / `auth_user` 与占用集合有交集 → 整条规则跳过，不写入。
- 同批次内先到先得：前一条规则成功后，其用户名立即加入占用集合。
- `type=logical` 的规则递归收集子规则的 `user` / `auth_user` 参与冲突检测。
- `action=reject` / `sniff` / `hijack-dns` 等非 route action 的现有规则不占用用户名。
- 无 `user` / `auth_user` 的规则（纯域名/IP/端口匹配）直接追加，不参与冲突检测。

---

### 2.2 请求体

```json
{
  "rules": [
    {
      "auth_user": ["alice"],
      "action": "route",
      "outbound": "proxy-jp"
    },
    {
      "auth_user": ["bob"],
      "action": "route",
      "outbound": "proxy-us"
    }
  ],
  "rule_set": [
    {
      "tag": "geosite-cn",
      "type": "remote",
      "format": "binary",
      "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
      "download_detour": "direct"
    }
  ],
  "final": "proxy"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `rules` | array | 否 | 待导入的路由规则数组（sing-box route rule 格式） |
| `rule_set` | array | 否 | 待导入的规则集数组，按 `tag` 字段去重，已存在同 tag 跳过 |
| `final` | string | 否 | 非空时覆盖 `route.final` 默认出站 |

三个字段至少提供一个，否则返回错误。

---

### 2.3 响应

**成功（含全部跳过的情况）：**
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
        "existingOutbound": ["proxy-us"]
      }
    ],
    "addedRulesets": 1,
    "totalRules": 5,
    "restarted": true
  }
}
```

| 字段 | 说明 |
|---|---|
| `added` | 成功写入的规则条数 |
| `skipped` | 因用户名冲突跳过的规则条数 |
| `skippedRules` | 跳过详情：`index`（在请求数组中的下标）、`conflictUsers`（冲突用户名）、`existingOutbound`（占用该用户名的现有出站） |
| `addedRulesets` | 成功写入的规则集条数 |
| `totalRules` | 写入后 `route.rules` 的总条数 |
| `restarted` | 是否触发了 sing-box 异步重启（全跳过时为 `false`） |

**失败：**
```json
{ "success": false, "msg": "importRules: invalid payload: ..." }
```

---

### 2.4 curl 示例

#### 基础：3 条规则全部新增

```bash
curl -X POST http://localhost:2095/app/api/importRules \
  -b "s-ui=<session-cookie>" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      { "auth_user": ["alice"], "action": "route", "outbound": "proxy-jp" },
      { "auth_user": ["bob"],   "action": "route", "outbound": "proxy-us" },
      { "auth_user": ["carol"], "action": "route", "outbound": "proxy-hk" }
    ]
  }'
```

#### 使用 v2 Token

```bash
curl -X POST http://localhost:2095/app/apiv2/importRules \
  -H "Token: your-api-token" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      { "auth_user": ["dave"], "action": "route", "outbound": "proxy-eu" }
    ],
    "final": "proxy"
  }'
```

#### 带 rule_set 和 final

```bash
curl -X POST http://localhost:2095/app/api/importRules \
  -b "s-ui=<session-cookie>" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      { "rule_set": ["geosite-cn"], "action": "route", "outbound": "direct" },
      { "rule_set": ["geoip-cn"],   "action": "route", "outbound": "direct" }
    ],
    "rule_set": [
      {
        "tag": "geosite-cn",
        "type": "remote",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs"
      },
      {
        "tag": "geoip-cn",
        "type": "remote",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs"
      }
    ],
    "final": "proxy"
  }'
```

#### logical 规则

```bash
curl -X POST http://localhost:2095/app/apiv2/importRules \
  -H "Token: your-api-token" \
  -H "Content-Type: application/json" \
  -d '{
    "rules": [
      {
        "type": "logical",
        "mode": "and",
        "rules": [
          { "auth_user": ["eve"] },
          { "domain_suffix": ["youtube.com"] }
        ],
        "action": "route",
        "outbound": "proxy-us"
      }
    ]
  }'
```

---

### 2.5 冲突行为速查表

| 场景 | added | skipped | restarted |
|---|---|---|---|
| 现有规则为空，3 条新规则 | 3 | 0 | true |
| alice 已被占用，bob/carol 新增 | 2 | 1 | true |
| alice/bob/carol 全部已被占用 | 0 | 3 | false |
| 同批两条含 eve，无历史占用 | 1 | 1 | true |
| 现有 `action:reject, auth_user:[alice]` | alice 视为未占用，可正常加入 | — | — |
| 待导入 logical 规则含已占用用户名 | 整条 logical 规则跳过 | — | — |
| 无 user/auth_user 的纯域名规则 | 直接追加 | — | true |
| 仅更新 final，无新规则 | 0 | 0 | true |
| rule_set 同 tag 重复 | — | — | addedRulesets=0 |

---

## 三、获取 Session Cookie（v1 鉴权）

```bash
curl -c cookie.txt -X POST http://localhost:2095/app/api/login \
  -d "user=admin&pass=admin"
# 之后所有请求加 -b cookie.txt
```

## 四、获取 API Token（v2 鉴权）

在面板「管理员」→「API Token」页面创建，或：

```bash
curl -b cookie.txt -X POST http://localhost:2095/app/api/addToken \
  -d "expiry=0&desc=my-script"
```

响应中 `obj.token` 即为 token 值，之后所有请求加 `-H "Token: <token>"`。

---

## 五、basePath 说明

默认 `basePath` 为 `app`，即 URL 前缀为 `/app/`。  
若面板设置了自定义路径，将 `app` 替换为对应路径即可。

```
http[s]://<host>:<webPort>/<webPath>/api/...
```

默认：`http://localhost:2095/app/api/...`
