# S-UI SSL 证书自动化开发规范

> 适用版本：S-UI v1.4.1+
> 文档版本：v1.0
> 文档目的：为实施 AI agent 提供可直接落地、可验收的工程规范。

---

## 0. 文档使用说明

本规范面向具体的实现方（AI agent 或工程师），要求**严格按本文档实施**，不得自由发挥。

- 所有"必须 / 应当 / 不得"为强制要求。
- 所有"建议 / 可选"为优化项，可按实际情况裁剪。
- **不得新增本规范未提及的对外接口、设置项、定时任务**。
- **不得修改本规范明确"不变更"的模块**。
- 实施完成后，必须按 [§17 验收标准](#17-验收标准) 逐条自检。

---

## 1. 设计目标与非目标

### 1.1 目标（必须达成）

| # | 目标 | 强度 |
| --- | --- | --- |
| G1 | S-UI 面板支持 **一键申请 Let's Encrypt IP 证书**（基于 acme.sh 的 shortlived profile）| 必须 |
| G2 | 支持**一键生成自签证书**作为兜底方案，且自签证书在现代浏览器（Chrome / Firefox / Safari 最新版）放行后可正常使用 | 必须 |
| G3 | 申请成功后**自动写入面板设置** `webCertFile` / `webKeyFile`，无需用户手动填写 | 必须 |
| G4 | **自动续签**：6 天有效期的 LE shortlived 证书必须能被自动续签，且任何场景下不掉签（服务器重启、s-ui 进程重启、容器重启均能恢复续签调度）| 必须 |
| G5 | 续签成功后**热加载**新证书，无需重启 s-ui 进程，无需断开现有 HTTPS 连接 | 必须 |
| G6 | **四类入口**全部支持：CLI、装机脚本 `install.sh`、运维菜单 `s-ui.sh`、前端 Settings 页 | 必须 |
| G7 | 失败时**明确报错给用户**，不静默回退，不擅自切换证书类型 | 必须 |
| G8 | 与 S-UI 现有架构兼容：复用 settings 表、changes 审计、PanelService、cronjob 等基础设施 | 必须 |

### 1.2 非目标（明确不做）

- ❌ 不在 Go 中重新实现 ACME 协议（如 lego 库），统一依赖 acme.sh。
- ❌ 不修改 sing-box 内核的 `tls` 表（那是节点用的证书，与面板无关）。
- ❌ 不新增数据库表（仅追加 settings 表的 key）。
- ❌ 不做证书备份模块（用户可走现有 DB 导入导出）。
- ❌ 不实现 DNS-01 验证（保留现有 `s-ui.sh` 的 Cloudflare 域名证书功能，但不属本次范围）。
- ❌ 不支持 IPv6 证书（首版仅 IPv4，后续可扩展）。

---

## 2. 决策摘要

| # | 决策点 | 选择 | 说明 |
| --- | --- | --- | --- |
| D1 | 续签触发器 | **S-UI 内置 cronjob + 启动即检查 + 三重兜底** | 详见 §12 |
| D2 | 申请入口 | **CLI + 装机 + 菜单 + 前端**，所有入口最终都调用同一个 Go service 函数 | |
| D3 | 失败兜底 | **明确报错，不自动切换证书类型** | |
| D4 | 续签后生效 | **热加载（tls.Config.GetCertificate 回调）+ 原子指针替换** | 详见 §13 |
| D5 | acme.sh 位置 | **`/root/.acme.sh/`**（3X-UI 风格，root 运行）| |
| D6 | 证书存放 | `/usr/local/s-ui/cert/ip/`（LE 证书）+ `/usr/local/s-ui/cert/self/`（自签）| |
| D7 | 80 端口冲突 | **检测占用即报错**，让用户自行处理 | |
| D8 | shortlived 失败 | 第一版**不做 fallback**，明确报错 | |
| D9 | 数据库变更 | 仅追加 settings key，不建新表 | 详见 §9 |

---

## 3. 架构总览

```
┌─────────────────────────────────────────────────────────────────┐
│  入口层                                                          │
│  ┌────────────┐  ┌──────────────┐  ┌────────────┐  ┌─────────┐  │
│  │ install.sh │  │   s-ui.sh    │  │ Settings.vue │  │   CLI  │  │
│  │ (装机一键) │  │  (运维菜单)  │  │  (前端按钮)  │  │  sui   │  │
│  └─────┬──────┘  └──────┬───────┘  └──────┬───────┘  └────┬───┘  │
│        │                │                  │ HTTP API     │      │
│        │                │                  │              │      │
│        └─────────CLI 调用─────────┘        ▼              │      │
│                                    api/apiHandler.go      │      │
│                                            │              │      │
│        ┌───────────────────────────────────┴──────────────┘      │
│        ▼                                                          │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │   service/cert.go  （核心 CertService —— 唯一实现）        │  │
│  │   - IssueLeIPCert                                          │  │
│  │   - IssueSelfSignedCert                                    │  │
│  │   - RenewIpCert                                            │  │
│  │   - GetCertStatus                                          │  │
│  │   - RemoveCert                                             │  │
│  │   - ReloadCertOnDisk  → 通知 web.Server 热加载             │  │
│  └────────────┬───────────────────────┬───────────────────────┘  │
│               │                       │                          │
│               ▼                       ▼                          │
│   acme.sh (exec.Command)    SettingService.SetCertFile/SetKeyFile│
│   /root/.acme.sh/                                                │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │   cronjob/cert_renewal_job.go    （续签守护）              │  │
│  │   - 启动时立即检查一次                                      │  │
│  │   - 之后每 6 小时检查一次                                   │  │
│  │   - 剩余有效期 < 3 天 → 触发续签                            │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │   web/web.go    （热加载改造）                              │  │
│  │   - tls.Config 用 GetCertificate 回调                       │  │
│  │   - 内部维护 atomic.Pointer[tls.Certificate]                │  │
│  │   - 暴露 ReloadCert() 方法                                  │  │
│  └────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

**关键不变量**：所有证书操作（申请 / 续签 / 移除）**必须**经过 `service/cert.go` 中的 `CertService`，**禁止**在任何 Shell 脚本中直接调用 acme.sh 后再写入 DB。Shell 脚本只负责"用户交互 + 调用 `sui cert` CLI"。

---

## 4. 模块改动清单（一图速览）

### 4.1 新增文件

| 文件 | 职责 |
| --- | --- |
| `service/cert.go` | 证书核心服务 |
| `service/cert_acme.go` | acme.sh 命令封装（被 cert.go 调用，单独文件便于测试）|
| `service/cert_selfsigned.go` | 自签证书生成（纯 Go，**不依赖 openssl 二进制**）|
| `cmd/cert.go` | CLI 子命令实现 |
| `cronjob/cert_renewal_job.go` | 续签定时任务 |
| `frontend/src/components/CertCard.vue` | 前端证书管理卡片组件 |
| `docs/SSL证书自动化开发规范.md` | 本文档 |

### 4.2 修改文件

| 文件 | 改动 |
| --- | --- |
| `service/setting.go` | 新增 setter；新增证书相关 setting key 默认值 |
| `cmd/cmd.go` | 注册 `cert` 子命令 |
| `cronjob/cronJob.go` | 注入 CertService；注册 CertRenewalJob |
| `web/web.go` | **重要**：tls.Config 改为 GetCertificate 回调；暴露 `ReloadCert()` |
| `app/app.go` | 创建 CertService，并把 web.Server 引用注入给 CertService（用于热加载回调）|
| `api/apiHandler.go` | 注册 `/api/cert/*` 路由 |
| `api/apiService.go` | 新增证书相关 handler 方法 |
| `frontend/src/views/Settings.vue` | 嵌入 CertCard 组件 |
| `frontend/src/locales/*.ts`（6 种语言）| 新增 i18n key |
| `frontend/src/store/modules/data.ts`（可选）| 暴露证书状态拉取方法 |
| `install.sh` | 装机询问 SSL 策略 |
| `s-ui.sh` | 运维菜单 SSL 选项扩充 |
| `entrypoint.sh` | Docker 入口加 acme.sh 预装 |
| `Dockerfile` | 装 acme.sh、curl、openssl、socat、cron |
| `s-ui.service` | 加 `Restart=always`（保证续签调度不掉）|

### 4.3 不变更（明确禁止）

| 模块 | 理由 |
| --- | --- |
| `database/model/*.go` | 不新增表，不改字段 |
| `database/db.go` | 不动迁移 |
| `core/*` | 与 sing-box 内核无关 |
| `sub/*` | 订阅服务暂不在本次范围（仅 webCert，不动 subCert）|
| `service/tls.go` | 节点 TLS 模板，与面板无关 |
| `middleware/*` | 不动域名校验 |

> 说明：本次**仅做面板证书（webCertFile）自动化**。订阅端口 `subCertFile/subKeyFile` 保留手动配置，避免一次性改动过大。后续可扩展同样机制到订阅。

---

## 5. 后端改动详解

### 5.1 `service/cert.go`（新增）

定义 `CertService` 结构体与对外方法。**所有方法必须并发安全**（用 `sync.Mutex` 串行化关键流程，防止前端按钮重复点击导致并发申请）。

#### 5.1.1 数据结构

```go
// 证书模式
const (
    CertModeNone   = "none"   // 未配置（HTTP）
    CertModeSelf   = "self"   // 自签
    CertModeLeIP   = "le-ip"  // Let's Encrypt IP
    CertModeManual = "manual" // 用户手动填写（CLI/前端文本框直填）
)

// 证书状态（对外返回）
type CertStatus struct {
    Mode      string    `json:"mode"`       // none|self|le-ip|manual
    HasCert   bool      `json:"hasCert"`
    CertFile  string    `json:"certFile"`
    KeyFile   string    `json:"keyFile"`
    Subject   string    `json:"subject"`    // CN
    Issuer    string    `json:"issuer"`     // O / CN
    IP        string    `json:"ip"`         // 当模式为 le-ip 时
    NotBefore time.Time `json:"notBefore"`
    NotAfter  time.Time `json:"notAfter"`
    DaysLeft  int       `json:"daysLeft"`
}

// 预检结果
type CertPrecheckResult struct {
    PublicIP   string `json:"publicIp"`
    Port80Free bool   `json:"port80Free"`
    AcmeReady  bool   `json:"acmeReady"`
    OK         bool   `json:"ok"`
    Message    string `json:"message"`
}
```

#### 5.1.2 对外方法签名（必须完全一致）

```go
type CertService struct {
    settingService *SettingService
    panelService   *PanelService
    webReloader    WebReloader  // 接口，由 web.Server 实现
    mu             sync.Mutex
}

type WebReloader interface {
    ReloadCert() error
}

func NewCertService(s *SettingService, p *PanelService, w WebReloader) *CertService

// 预检：检测公网 IP、80 端口、acme.sh 是否就绪
func (s *CertService) Precheck() (*CertPrecheckResult, error)

// 申请 LE IP 证书（同步执行，可能耗时 30~60s）
func (s *CertService) IssueLeIPCert(force bool) (*CertStatus, error)

// 生成自签证书
func (s *CertService) IssueSelfSignedCert() (*CertStatus, error)

// 手动续签
func (s *CertService) RenewIpCert() error

// 移除证书（清空 setting，触发热加载切回 HTTP）
func (s *CertService) RemoveCert() error

// 读取当前证书状态
func (s *CertService) GetCertStatus() (*CertStatus, error)
```

#### 5.1.3 IssueLeIPCert 实现规范

必须严格按顺序：

1. `s.mu.Lock()`，`defer s.mu.Unlock()`。
2. 调用 `Precheck()`；任意失败立即返回错误，**不重试不回退**。
3. 检查 `force=false` 时：若已有 LE 证书且 `DaysLeft > 3`，直接返回当前状态，不重申。
4. `ensureAcme()`：若 `/root/.acme.sh/acme.sh` 不存在，调用 `curl https://get.acme.sh | sh -s email=admin@s-ui.local` 安装。安装失败立即返回错误。
5. `acme.sh --set-default-ca --server letsencrypt --force`。
6. `acme.sh --issue -d <ip> --standalone --server letsencrypt --certificate-profile shortlived --days 6 --httpport 80 --force`。
7. 创建 `/usr/local/s-ui/cert/ip/` 目录（mode 0755）。
8. `acme.sh --installcert -d <ip> --key-file /usr/local/s-ui/cert/ip/privkey.pem --fullchain-file /usr/local/s-ui/cert/ip/fullchain.pem --reloadcmd "/usr/local/s-ui/sui cert -reload"`。
9. 文件权限：`privkey.pem` 设为 0600，`fullchain.pem` 设为 0644。
10. 通过 `SettingService` 写入：
    - `webCertFile` = `/usr/local/s-ui/cert/ip/fullchain.pem`
    - `webKeyFile`  = `/usr/local/s-ui/cert/ip/privkey.pem`
    - `certMode`    = `le-ip`
    - `certDomain`  = `<ip>`
11. `webReloader.ReloadCert()` 触发热加载。
12. 写一条 `changes` 表记录（actor=`cert-system`, key=`cert`, action=`issue-ip`, obj=证书状态 JSON）。
13. 返回最新 `CertStatus`。

**任何步骤失败**：返回带详细原因的 error；**不要清理已生成的部分文件**（便于人工排查）；**不要回退到自签**。

#### 5.1.4 IssueSelfSignedCert 实现规范

**强制要求：必须用 Go 标准库 `crypto/x509` 生成，不调用 openssl 二进制**。理由：

- 跨平台一致（macOS / Linux / Windows 行为相同）。
- 不引入 openssl 版本兼容问题（如 LibreSSL 不支持 ed25519）。
- Docker 镜像可以不装 openssl-cli。

实现要点：

1. 算法选择：默认 **ECDSA P-256**（兼容性最好；如果想更现代可用 ed25519，但 Java 11 之前不支持 → 选 P-256 更稳）。
2. 有效期：**3650 天（10 年）**。
3. CN：`s-ui-panel`。
4. **必须**包含 `SubjectAltName`（这是关键，Chrome 58+ 不再认 CN，只看 SAN）：
   - `DNS: localhost`
   - `IP: 127.0.0.1`
   - `IP: ::1`
   - `IP: 本机所有非 loopback 的 IPv4 地址`（用 `net.InterfaceAddrs()` 枚举）
   - `IP: 本机所有非 link-local 的 IPv6 地址`
   - `IP: 公网 IP`（best-effort，获取失败不阻塞）
5. ExtKeyUsage 必须包含 `x509.ExtKeyUsageServerAuth`。
6. KeyUsage 必须包含 `x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment`。
7. 输出位置：
   - 证书：`/usr/local/s-ui/cert/self/self.crt`（PEM 格式，权限 0644）
   - 私钥：`/usr/local/s-ui/cert/self/self.key`（PEM 格式，权限 0600）
8. 目录创建：`/usr/local/s-ui/cert/self/` 不存在时 `os.MkdirAll(..., 0755)`。
9. 写入 setting：`webCertFile`、`webKeyFile`、`certMode=self`、`certDomain=""`。
10. 触发 `ReloadCert()`。
11. 写 changes 表记录。
12. 返回 `CertStatus`。

**自签证书自检（必须实现的内部 sanity check）**：
生成后，立即用 `tls.LoadX509KeyPair` 加载一次，验证 PEM 编码正确性；失败则删除文件并返回错误，**不写入 setting**。

#### 5.1.5 RenewIpCert 实现规范

1. 读 setting：`certMode`；若 != `le-ip`，直接返回（不报错，不是 LE 证书没续签必要）。
2. 读 `certDomain`（IP）。
3. `acme.sh --renew -d <ip> --force`（必须加 `--force`，因为 shortlived 6 天可能不到 acme 默认续签阈值）。
4. **不重新 installcert**——`--installcert` 时的 `--reloadcmd` 已经注册过，acme.sh 在 renew 后会自动覆盖证书文件并调用 reloadcmd。
5. 等待 reloadcmd 完成；若 reloadcmd 失败，直接调用 `webReloader.ReloadCert()` 兜底。
6. 写 changes 表记录。

#### 5.1.6 GetCertStatus 实现规范

1. 读 setting `webCertFile`、`webKeyFile`、`certMode`、`certDomain`。
2. 若两个文件路径之一为空：返回 `{Mode: none, HasCert: false}`。
3. 用 `os.ReadFile` + `pem.Decode` + `x509.ParseCertificate` 解析 cert 文件。
4. 计算 `DaysLeft = NotAfter.Sub(time.Now()).Hours() / 24`，向下取整。
5. 若解析失败：返回 `{Mode: manual or certMode, HasCert: false, Message: "证书解析失败"}`（不抛错，让前端能展示）。
6. 若 setting 中 `certMode` 为空但磁盘上有证书：标记为 `manual`。

#### 5.1.7 RemoveCert 实现规范

1. 写空 `webCertFile`、`webKeyFile`、`certMode=none`、`certDomain=""`。
2. **不删除磁盘上的证书文件**（用户可能想保留备份）。
3. 触发 `ReloadCert()`，web 切回 HTTP。
4. 写 changes 表。

#### 5.1.8 Precheck 实现规范

1. **公网 IP 检测**：依次尝试 `https://api.ipify.org`、`https://ifconfig.me`、`https://icanhazip.com`、`https://ip.sb`，每个超时 3 秒，取第一个成功的 IPv4 结果。全部失败则 `PublicIP=""`。
2. **80 端口检测**：尝试 `net.Listen("tcp", ":80")`，成功立即 Close 并标记 `Port80Free=true`；失败标记 false。
3. **acme.sh 就绪检测**：`os.Stat("/root/.acme.sh/acme.sh")`，存在即 ready。
4. 汇总 `OK`：`PublicIP != "" && Port80Free`（acme 不就绪不阻塞，可现装）。
5. `Message`：若 `OK=false`，给出**具体原因**，如 "公网 IP 检测失败，请确认服务器可访问公网" 或 "80 端口被占用，请先停止 nginx 等服务"。

---

### 5.2 `service/cert_acme.go`（新增）

封装 acme.sh 命令调用，便于单测。

```go
type AcmeClient struct {
    Home string  // /root/.acme.sh
}

func NewAcmeClient() *AcmeClient

// 是否已安装
func (c *AcmeClient) Installed() bool

// 安装 acme.sh（curl | sh）
func (c *AcmeClient) Install(email string) error

// 设为 letsencrypt CA
func (c *AcmeClient) SetDefaultCALetsEncrypt() error

// 申请 IP 证书（shortlived）
func (c *AcmeClient) IssueIPCert(ip string) error

// installcert 并注册 reloadcmd
func (c *AcmeClient) InstallCert(ip, keyFile, certFile, reloadCmd string) error

// 续签
func (c *AcmeClient) Renew(ip string) error
```

**实现细节**：
- 所有 `exec.Command` 必须设置超时（用 `exec.CommandContext` + `context.WithTimeout`），单条命令 5 分钟超时。
- 标准输出/错误必须捕获并写入 logger（Info 级别），失败时 error 中包含末尾 4KB 输出。
- 不依赖 shell（不用 `bash -c`），直接 `exec.Command("/root/.acme.sh/acme.sh", args...)`，除了安装阶段必须用 `bash -c "curl ... | sh ..."`。

---

### 5.3 `service/cert_selfsigned.go`（新增）

纯 Go 实现，详见 [§5.1.4](#514-issueselfsignedcert-实现规范)。

提供一个内部函数：

```go
// 生成自签证书（PEM 格式）
// 返回 certPEM, keyPEM, error
func generateSelfSignedCert() (certPEM, keyPEM []byte, err error)
```

---

### 5.4 `service/setting.go`（修改）

#### 5.4.1 新增 setter

在合适位置（`SetPort` 附近）追加：

```go
func (s *SettingService) SetCertFile(v string) error { return s.setString("webCertFile", v) }
func (s *SettingService) SetKeyFile(v string)  error { return s.setString("webKeyFile",  v) }
func (s *SettingService) SetCertMode(v string) error { return s.setString("certMode",    v) }
func (s *SettingService) SetCertDomain(v string) error { return s.setString("certDomain", v) }

func (s *SettingService) GetCertMode() (string, error)   { return s.getString("certMode") }
func (s *SettingService) GetCertDomain() (string, error) { return s.getString("certDomain") }
```

#### 5.4.2 默认值表追加

在 `defaultValueMap` 中加：

```go
"certMode":   "none",
"certDomain": "",
```

#### 5.4.3 GetAllSetting 中安全过滤

`certMode` 和 `certDomain` **可以**返回给前端（不是敏感信息）。无需 delete。

---

### 5.5 `web/web.go`（**关键修改**：热加载改造）

这是本次最重要的代码改动。

#### 5.5.1 新增字段

```go
type Server struct {
    httpServer     *http.Server
    listener       net.Listener
    ctx            context.Context
    cancel         context.CancelFunc
    settingService service.SettingService

    // 新增：热加载证书指针
    cert           atomic.Pointer[tls.Certificate]
    certMu         sync.Mutex // 串行化 reload 操作
}
```

#### 5.5.2 ReloadCert 方法（新增）

```go
// ReloadCert 从 settings 中读取 webCertFile/webKeyFile，
// 重新加载证书并原子替换。可在运行期间被定时任务或 HTTP API 调用。
// 若 setting 中证书路径为空，则清除当前证书（HTTPS 握手将失败 → 客户端走 AutoHttpsListener 的 HTTP 路径）。
func (s *Server) ReloadCert() error {
    s.certMu.Lock()
    defer s.certMu.Unlock()

    certFile, _ := s.settingService.GetCertFile()
    keyFile, _  := s.settingService.GetKeyFile()

    if certFile == "" || keyFile == "" {
        s.cert.Store(nil)
        logger.Info("证书已清除，回退到 HTTP")
        return nil
    }

    cert, err := tls.LoadX509KeyPair(certFile, keyFile)
    if err != nil {
        return err
    }
    s.cert.Store(&cert)
    logger.Info("证书已热加载：", certFile)
    return nil
}
```

#### 5.5.3 Start 方法改造

原代码（`web/web.go:172-183`）：

```go
if certFile != "" || keyFile != "" {
    cert, err := tls.LoadX509KeyPair(certFile, keyFile)
    // ...
    c := &tls.Config{ Certificates: []tls.Certificate{cert} }
    listener = network.NewAutoHttpsListener(listener)
    listener = tls.NewListener(listener, c)
}
```

改为：

```go
// 先尝试加载证书（即使失败也继续，启动后允许用户后续配置）
if certFile != "" && keyFile != "" {
    if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
        s.cert.Store(&cert)
    } else {
        logger.Warning("证书加载失败，启动为 HTTP：", err)
    }
}

if s.cert.Load() != nil {
    tlsConfig := &tls.Config{
        // 关键：用 GetCertificate 回调，每次握手取最新指针
        GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
            c := s.cert.Load()
            if c == nil {
                return nil, fmt.Errorf("当前无可用证书")
            }
            return c, nil
        },
        MinVersion: tls.VersionTLS12,
    }
    listener = network.NewAutoHttpsListener(listener)
    listener = tls.NewListener(listener, tlsConfig)
    logger.Info("web server run https on", listener.Addr())
} else {
    logger.Info("web server run http on", listener.Addr())
}
```

**注意**：将 `||` 改为 `&&`，修复"只填一个就崩"的旧 bug（属于本次顺手修复）。

#### 5.5.4 暴露 Reload 接口（提供给 CertService）

`web.Server` 自然实现 `service.WebReloader` 接口（只需有 `ReloadCert() error` 方法）。

---

### 5.6 `app/app.go`（修改）

#### 5.6.1 Init 中创建 CertService

```go
// 在 NewConfigService 后追加
panelService := &service.PanelService{}
certService := service.NewCertService(&a.SettingService, panelService, a.webServer)
a.certService = certService
```

#### 5.6.2 Start 中把 CertService 注入 cronJob

```go
err = a.cronJob.Start(loc, trafficAge, a.certService)
```

> 修改 `cronJob.Start` 签名增加 `certService` 参数。

---

### 5.7 `cronjob/cronJob.go`（修改）

```go
func (c *CronJob) Start(loc *time.Location, trafficAge int, certService *service.CertService) error {
    // ... 原有任务 ...

    // 新增：证书续签
    certJob := NewCertRenewalJob(certService)
    c.cron.AddJob("@every 6h", certJob)

    // 启动即执行一次（防止重启后到期未续）
    go func() {
        time.Sleep(30 * time.Second) // 等 web server 起来
        certJob.Run()
    }()

    return nil
}
```

---

### 5.8 `cronjob/cert_renewal_job.go`（新增）

```go
type CertRenewalJob struct {
    certService *service.CertService
}

func NewCertRenewalJob(s *service.CertService) *CertRenewalJob {
    return &CertRenewalJob{certService: s}
}

func (j *CertRenewalJob) Run() {
    status, err := j.certService.GetCertStatus()
    if err != nil {
        logger.Warning("cert renewal: 读取状态失败：", err)
        return
    }
    if status.Mode != service.CertModeLeIP {
        return // 非 LE 证书不需要续
    }
    if status.DaysLeft > 3 {
        logger.Debug("cert renewal: 剩余", status.DaysLeft, "天，无需续签")
        return
    }
    logger.Info("cert renewal: 剩余", status.DaysLeft, "天，开始续签")
    if err := j.certService.RenewIpCert(); err != nil {
        logger.Error("cert renewal: 续签失败：", err)
        return
    }
    logger.Info("cert renewal: 续签成功")
}
```

---

### 5.9 `cmd/cmd.go` + `cmd/cert.go`（新增 / 修改）

#### 5.9.1 `cmd/cert.go`（新增）

```go
package cmd

func certCmd(args []string) {
    fs := flag.NewFlagSet("cert", flag.ExitOnError)
    var (
        issueIP   bool
        issueSelf bool
        renew     bool
        status    bool
        remove    bool
        reload    bool
        force     bool
    )
    fs.BoolVar(&issueIP,   "issue-ip",   false, "申请 Let's Encrypt IP 证书")
    fs.BoolVar(&issueSelf, "issue-self", false, "生成自签证书")
    fs.BoolVar(&renew,     "renew",      false, "强制续签")
    fs.BoolVar(&status,    "status",     false, "显示证书状态")
    fs.BoolVar(&remove,    "remove",     false, "移除证书（恢复 HTTP）")
    fs.BoolVar(&reload,    "reload",     false, "通知运行中的面板热加载证书（acme.sh reloadcmd 内部使用）")
    fs.BoolVar(&force,     "force",      false, "强制操作（如未到期也重申）")
    fs.Parse(args)

    // 注意：reload 命令需要找到正在运行的 sui 进程并发信号
    // 其余命令直接 InitDB + 调用 service
}
```

**`-reload` 实现要点**：
- 不能直接调用 CertService（CLI 进程与运行中的 panel 是不同进程）。
- 方案：CLI 通过 `pgrep` 或读 `/var/run/s-ui.pid`（需要 systemd 配 PIDFile）找到 panel 进程，发送 **SIGUSR1** 信号。
- panel 在 `main.go` 中捕获 SIGUSR1，调用 `webServer.ReloadCert()`。

#### 5.9.2 `cmd/cmd.go` 注册子命令

在 switch 中加：

```go
case "cert":
    certCmd(os.Args[2:])
```

并在 Usage 中加描述。

#### 5.9.3 `main.go` 修改：监听 SIGUSR1

```go
signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGUSR1)
for {
    sig := <-sigCh
    switch sig {
    case syscall.SIGHUP:
        app.RestartApp()
    case syscall.SIGUSR1:
        if err := app.ReloadWebCert(); err != nil {
            logger.Error("热加载证书失败：", err)
        }
    default:
        app.Stop()
        return
    }
}
```

在 `app/app.go` 增加：

```go
func (a *APP) ReloadWebCert() error {
    if a.webServer == nil { return nil }
    return a.webServer.ReloadCert()
}
```

> 这套机制保证了：**即使 acme.sh 的 reloadcmd 在 panel 进程外触发，也能让运行中的 panel 热加载新证书**，无需 SIGHUP 重启。

---

### 5.10 `api/apiHandler.go` + `api/apiService.go`（修改）

#### 5.10.1 路由注册

`apiHandler.go` 中 `postHandler` 与 `getHandler` 增加：

```go
// POST
case "cert_issueIp":      a.ApiService.CertIssueIP(c)
case "cert_issueSelf":    a.ApiService.CertIssueSelf(c)
case "cert_renew":        a.ApiService.CertRenew(c)
case "cert_remove":       a.ApiService.CertRemove(c)
case "cert_precheck":     a.ApiService.CertPrecheck(c)
// GET
case "cert_status":       a.ApiService.CertStatus(c)
```

> 由于 Gin 路由是 `/:postAction`，单段参数不支持 `cert/issueIp` 这种带斜杠的值。**实施方需选择以下方案之一**：
> - **方案 A（推荐）**：路由 path 改为 `cert_issueIp` 等（用下划线），与现有命名风格更一致。
> - **方案 B**：单独注册 `g.POST("/cert/issueIp", ...)` 等明确路由。

#### 5.10.2 ApiService 方法

```go
func (a *ApiService) CertStatus(c *gin.Context) {
    st, err := a.certService.GetCertStatus()
    if err != nil { jsonMsg(c, "cert", err); return }
    c.JSON(200, gin.H{"success": true, "obj": st})
}

func (a *ApiService) CertPrecheck(c *gin.Context) { /* 类似 */ }

func (a *ApiService) CertIssueIP(c *gin.Context) {
    force := c.PostForm("force") == "true"
    st, err := a.certService.IssueLeIPCert(force)
    if err != nil { jsonMsg(c, "cert", err); return }
    c.JSON(200, gin.H{"success": true, "obj": st})
}

// 其余类似
```

**`ApiService` 需要持有 `*CertService` 引用**——修改 `api/apiService.go` 中 `ApiService` 结构体增加字段，并在 `web.initRouter` 创建 `APIHandler` 时注入。

---

## 6. 前端改动详解

### 6.1 `frontend/src/components/CertCard.vue`（新增）

完整的证书管理卡片。位置：嵌入 `Settings.vue` 的"接口"Tab 顶部。

#### 6.1.1 UI 设计

```
┌──────────────────────────────────────────────────────────────────┐
│ 🔒 SSL 证书                                                       │
├──────────────────────────────────────────────────────────────────┤
│ 状态：[已启用 HTTPS]  类型：Let's Encrypt (IP)                    │
│ 证书 IP：1.2.3.4   到期：2026-06-26   剩余：5 天                  │
│ 下次续签检查：6 小时内                                            │
│                                                                  │
│ [申请 IP 证书] [生成自签证书] [手动续签] [恢复 HTTP]              │
└──────────────────────────────────────────────────────────────────┘
```

未启用 HTTPS 时：

```
┌──────────────────────────────────────────────────────────────────┐
│ 🔒 SSL 证书                                                       │
├──────────────────────────────────────────────────────────────────┤
│ 状态：[未启用，当前为 HTTP]                                       │
│                                                                  │
│ ⚠️ 建议启用 HTTPS 提升安全性                                       │
│                                                                  │
│ [申请 IP 证书 (推荐)] [生成自签证书]                              │
└──────────────────────────────────────────────────────────────────┘
```

#### 6.1.2 交互流程

**申请 IP 证书**按钮：
1. 先调 `GET /api/cert_status` 拉最新状态。
2. 调 `POST /api/cert_precheck`。
3. 弹出确认对话框，显示：
   - 检测到公网 IP：`1.2.3.4`
   - 80 端口：`可用 / 被占用`
   - 预计耗时：30~60 秒
   - 重要提示：**申请期间面板会临时中断 HTTP 服务以让 acme.sh 使用 80 端口**
4. 用户确认 → 调 `POST /api/cert_issueIp`，loading 状态最多 90 秒。
5. 成功：notivue 提示成功 + 自动刷新 status + 提示"请用 HTTPS 重新访问"。
6. 失败：notivue 弹错误（含 message 字段，可能很长，用 `duration: 0` 让用户主动关闭）。

**生成自签证书**按钮：
1. 弹确认："自签证书会被浏览器警告，确认继续？"
2. 调 `POST /api/cert_issueSelf`。
3. 成功后提示"请用 HTTPS 访问，浏览器警告页选'继续访问'即可"。

**手动续签**按钮：
1. 调 `POST /api/cert_renew`，loading。
2. 成功 → 刷新 status。

**恢复 HTTP**按钮：
1. 二次确认。
2. 调 `POST /api/cert_remove`。
3. 成功后提示"请用 HTTP 重新访问"，3 秒后自动跳转。

#### 6.1.3 自动刷新

证书卡片挂载时 `mounted` 调 `cert_status`；每次操作后再调一次。无需轮询。

### 6.2 `frontend/src/views/Settings.vue`（修改）

在"接口"Tab（`t1`）最顶部，`webListen` 字段之前，插入 `<CertCard />`。

保留原 `webCertFile`/`webKeyFile` 两个手动文本框（作为"手动模式"入口），但加 hint：
"使用上方一键申请，或在此手动指定证书文件路径"。

### 6.3 `frontend/src/locales/*.ts`（修改 6 个文件）

至少新增以下 key（每种语言都要加）：

```
cert.title              SSL 证书 / SSL Certificate / ...
cert.status.https       已启用 HTTPS
cert.status.http        未启用，当前为 HTTP
cert.type.leIp          Let's Encrypt (IP)
cert.type.self          自签证书
cert.type.manual        手动配置
cert.type.none          未配置
cert.field.ip           证书 IP
cert.field.expiry       到期
cert.field.daysLeft     剩余天数
cert.field.nextRenew    下次续签检查
cert.action.issueIp     申请 IP 证书
cert.action.issueSelf   生成自签证书
cert.action.renew       手动续签
cert.action.remove      恢复 HTTP
cert.precheck.title     预检结果
cert.precheck.publicIp  公网 IP
cert.precheck.port80    80 端口
cert.precheck.warning   申请期间面板会临时中断 HTTP 服务
cert.confirm.issueIp    确认申请 IP 证书？
cert.confirm.issueSelf  自签证书会被浏览器警告，确认继续？
cert.confirm.remove     恢复 HTTP 后将无法用 HTTPS 访问，确认继续？
cert.hint.manualFile    使用上方一键申请，或在此手动指定证书文件路径
cert.notify.issueSuccess 证书申请成功，请用 HTTPS 重新访问
cert.notify.renewSuccess 续签成功
cert.notify.removeSuccess 已恢复 HTTP，3 秒后跳转
```

6 个文件：`en.ts`, `fa.ts`, `vi.ts`, `zhcn.ts`, `zhtw.ts`, `ru.ts`。

### 6.4 `frontend/src/types/`（按需新增）

可选：`frontend/src/types/cert.ts` 定义 `CertStatus` 与 `CertPrecheckResult` 类型，与后端结构体保持一致。

---

## 7. 装机 / 运维脚本改动

### 7.1 `install.sh`（修改）

在 `install_s-ui()` 函数中，`config_after_install` 后、`prepare_services` 前，新增：

```bash
ask_ssl_strategy() {
    echo -e "${yellow}是否为面板配置 SSL 证书？${plain}"
    echo -e "  ${green}1.${plain} Let's Encrypt IP 证书（推荐，需公网 IP 和 80 端口可用）"
    echo -e "  ${green}2.${plain} 自签证书（浏览器会警告，但可用）"
    echo -e "  ${green}3.${plain} 跳过（之后可在面板内配置）"
    read -p "请选择 [1/2/3，默认 3]: " ssl_choice
    ssl_choice=${ssl_choice:-3}

    case "$ssl_choice" in
        1)
            echo -e "${yellow}正在申请 Let's Encrypt IP 证书...${plain}"
            /usr/local/s-ui/sui cert -issue-ip
            if [[ $? -ne 0 ]]; then
                echo -e "${red}IP 证书申请失败，请稍后在面板内手动配置${plain}"
            fi
            ;;
        2)
            echo -e "${yellow}正在生成自签证书...${plain}"
            /usr/local/s-ui/sui cert -issue-self
            ;;
        3)
            echo -e "${yellow}已跳过 SSL 配置${plain}"
            ;;
    esac
}
```

调用顺序：

```bash
config_after_install
ask_ssl_strategy        # ← 新增
prepare_services
systemctl enable s-ui --now
```

`install_base` 中预装 `socat`、`cron`、`openssl`（Debian/Ubuntu）或 `cronie`（RHEL 系），以备 acme.sh 用。

### 7.2 `s-ui.sh`（修改）

在 `ssl_cert_issue_main` 菜单中增加 "Let's Encrypt IP 证书" 选项：

```bash
ssl_cert_issue_main() {
    echo -e "${green}\t1.${plain} Let's Encrypt 域名证书（HTTP-01）"
    echo -e "${green}\t2.${plain} Let's Encrypt 域名证书（Cloudflare DNS-01）"
    echo -e "${green}\t3.${plain} Let's Encrypt IP 证书（NEW）"
    echo -e "${green}\t4.${plain} 自签证书"
    echo -e "${green}\t5.${plain} 显示当前证书状态"
    echo -e "${green}\t6.${plain} 手动续签"
    echo -e "${green}\t0.${plain} 返回上一级"
    read -p "请选择: " choice
    case "$choice" in
        1) ssl_cert_issue ;;          # 保留旧
        2) ssl_cert_issue_CF ;;       # 保留旧
        3) /usr/local/s-ui/sui cert -issue-ip ;;
        4) /usr/local/s-ui/sui cert -issue-self ;;
        5) /usr/local/s-ui/sui cert -status ;;
        6) /usr/local/s-ui/sui cert -renew ;;
        0) return ;;
    esac
}
```

原有 `generate_self_signed_cert`（Shell openssl 版）**保留但不再从菜单调用**（避免双实现），可删除菜单入口或标注 deprecated。

### 7.3 `s-ui.service`（修改）

```ini
[Unit]
Description=s-ui Service
After=network.target
Wants=network.target

[Service]
Type=simple
WorkingDirectory=/usr/local/s-ui/
ExecStart=/usr/local/s-ui/sui
Restart=always            # ← 改：原 on-failure 改为 always
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

**改 `Restart=always` 的理由**：保证 SIGHUP 重启、手动 kill、acme.sh 续签触发的进程异常都能自动恢复，确保续签调度不掉。

### 7.4 `entrypoint.sh`（Docker 入口，修改）

在 `migrate` 之后、启动 `sui` 之前，预装 acme.sh（如果用户没装）：

```bash
if [ ! -f /root/.acme.sh/acme.sh ]; then
    curl -s https://get.acme.sh | sh -s email=admin@s-ui.local || true
fi
```

> 不让安装失败阻塞启动；用户可以后续手动通过面板按钮重试。

---

## 8. Dockerfile 改动

runtime 阶段追加：

```dockerfile
RUN apk add --no-cache curl openssl socat bash
RUN curl -s https://get.acme.sh | sh -s -- --install --home /root/.acme.sh \
    --accountemail admin@s-ui.local || true
ENV LE_WORKING_DIR=/root/.acme.sh
```

**说明**：
- 不装 `dcron` / `cronie`：续签由 s-ui 内部 cronjob 完成，不依赖系统 cron（这是相对 3X-UI 的优势）。
- `socat` 是 acme.sh standalone 模式可能用到的工具，预装备用。

---

## 9. 数据库 / 设置项变更

**不新增表，不改字段**。

### 9.1 新增 setting key（追加到 `defaultValueMap`）

| Key | 默认值 | 含义 | 前端是否可见 |
| --- | --- | --- | --- |
| `certMode` | `"none"` | `none` / `self` / `le-ip` / `manual` | 是（只读）|
| `certDomain` | `""` | 证书绑定的 IP 或域名 | 是（只读）|

### 9.2 现有 key 复用

| Key | 用途 |
| --- | --- |
| `webCertFile` | 证书文件路径 |
| `webKeyFile` | 私钥文件路径 |

### 9.3 changes 审计

所有证书操作（issue / renew / remove）必须写 `changes` 表，actor 取登录用户名；自动续签由 cronjob 触发时 actor 写 `"cert-cron"`。

---

## 10. 自签证书完整规范（**关键，禁止删减**）

> 用户明确要求"自签证书必须没有问题"，本节为强制规范。

### 10.1 算法

- **默认 ECDSA P-256**。
- 不使用 ed25519（兼容性问题：JDK < 11 不支持、Windows Server < 2022 不支持）。
- 不使用 RSA 2048（速度慢、密钥大）。

### 10.2 证书字段

| 字段 | 值 |
| --- | --- |
| Serial | 随机 128 bit（`crypto/rand` 生成）|
| Subject CN | `s-ui-panel` |
| Subject O | `s-ui` |
| Issuer | 同 Subject（自签）|
| NotBefore | `time.Now().Add(-1 * time.Hour)`（防时区/时钟漂移）|
| NotAfter | `time.Now().Add(10 * 365 * 24 * time.Hour)` |
| KeyUsage | `DigitalSignature \| KeyEncipherment \| CertSign` |
| ExtKeyUsage | `ServerAuth, ClientAuth` |
| BasicConstraints | `CA: true, MaxPathLen: 0`（自签需 CA=true 才能签自己）|
| **SubjectAltName** | **必须**包含下列全部 |

#### SubjectAltName 详细

| 类型 | 值 | 来源 |
| --- | --- | --- |
| DNS | `localhost` | 固定 |
| DNS | `*.localhost` | 固定 |
| IP | `127.0.0.1` | 固定 |
| IP | `::1` | 固定 |
| IP | 本机所有非 loopback IPv4 | `net.InterfaceAddrs()` 枚举 |
| IP | 本机所有非 link-local IPv6 | 同上 |
| IP | 公网 IPv4 | best-effort，2 秒超时，失败跳过 |
| DNS | `setting.webDomain`（若已配置）| 读 setting |

### 10.3 密钥

- 算法：ECDSA P-256
- PEM 编码格式：`EC PRIVATE KEY` 或 `PRIVATE KEY`（PKCS#8），二选一，但必须能被 `tls.LoadX509KeyPair` 正确解析。
- **生成后强制 sanity check**：用 `tls.LoadX509KeyPair` 加载一次，失败则直接报错并删除已生成的文件，**不写入 setting**。

### 10.4 文件

| 文件 | 路径 | 权限 |
| --- | --- | --- |
| 证书 | `/usr/local/s-ui/cert/self/self.crt` | 0644 |
| 私钥 | `/usr/local/s-ui/cert/self/self.key` | 0600 |
| 目录 | `/usr/local/s-ui/cert/self/` | 0755 |

### 10.5 多次生成行为

每次调用 `IssueSelfSignedCert` **覆盖**旧文件（带备份），具体：
1. 若旧文件存在，先 `mv` 到 `self.crt.bak.<timestamp>`。
2. 生成新文件。
3. sanity check 通过 → 不删 bak。
4. sanity check 失败 → 恢复 bak。

### 10.6 浏览器信任流程（不在代码范围，文档用）

文档/前端提示中明确说明：
- Chrome / Edge：访问时点"高级 → 继续前往"。
- Firefox：访问时点"高级 → 接受风险并继续"。
- Safari：访问时点"显示详细信息 → 访问此网站"。

可选：在前端 `cert_status` 接口返回 `caInstallTip` 字段，给出"如何把自签 CA 加入系统信任"的简短链接（不强制）。

### 10.7 测试用例（实施方必须自测）

| 用例 | 期望 |
| --- | --- |
| 全新机器生成自签 | 文件正确，sanity check 通过 |
| 用 `openssl x509 -in self.crt -noout -text` 查看 | SAN 包含本机 IP 与 127.0.0.1 |
| Chrome 最新版访问 `https://127.0.0.1:2095/app/` | 警告页放行后能登录 |
| Chrome 最新版访问 `https://<本机内网 IP>:2095/app/` | 警告页放行后能登录 |
| Firefox 访问 | 同上 |
| Safari 访问 | 同上 |
| 二次生成 | 旧文件备份 `.bak.<ts>` 存在 |
| 故意把 self.crt 写坏 → sanity check | 报错，setting 不变 |
| `tls.LoadX509KeyPair` 加载 | 成功 |
| 写入 setting + 热加载后访问 | 浏览器警告页放行后能登录 |

---

## 11. IP 证书完整规范

### 11.1 acme.sh 调用序列（必须完全一致）

```
acme.sh --set-default-ca --server letsencrypt --force
acme.sh --issue -d <IP> --standalone \
        --server letsencrypt \
        --certificate-profile shortlived \
        --days 6 \
        --httpport 80 \
        --force
acme.sh --installcert -d <IP> \
        --key-file /usr/local/s-ui/cert/ip/privkey.pem \
        --fullchain-file /usr/local/s-ui/cert/ip/fullchain.pem \
        --reloadcmd "/usr/local/s-ui/sui cert -reload"
```

### 11.2 续签

```
acme.sh --renew -d <IP> --force
```

> `--force` 必须加，因为 shortlived 6 天比 acme.sh 默认续签阈值短。

### 11.3 失败处理

| 失败场景 | 处理 |
| --- | --- |
| 公网 IP 检测失败 | Precheck 返回错误，不调 acme.sh |
| 80 端口被占用 | Precheck 返回错误，不调 acme.sh |
| acme.sh 安装失败 | 报错，提示用户检查网络/curl |
| `--issue` 失败 | 把 acme.sh 末尾 4KB 输出包进 error，返回给前端 |
| `--installcert` 失败 | 同上 |
| `--reloadcmd` 失败 | 在 RenewIpCert 中兜底再调一次 `webReloader.ReloadCert()` |

### 11.4 重复申请

- `force=false` 时，若当前 `certMode=le-ip` 且 `DaysLeft > 3`，跳过申请，返回当前状态（不报错）。
- `force=true` 时强制重申。

---

## 12. 续签可靠性保证（**关键**）

> 用户明确要求"服务器重启后续签也要存在，保证不掉签"。本节为强制规范。

### 12.1 三重保障

| 层 | 机制 | 作用 |
| --- | --- | --- |
| **L1** | systemd `Restart=always` | s-ui 进程异常崩溃自动拉起 |
| **L2** | s-ui 启动时 cronjob 立即执行一次 RenewalJob.Run() | 重启后第一时间检查是否已到期 |
| **L3** | cronjob `@every 6h` 周期检查 | 持续守护 |

### 12.2 启动即检查的实现

`cronJob.Start` 末尾：

```go
go func() {
    time.Sleep(30 * time.Second) // 等网络、web server、时钟同步
    certJob.Run()
}()
```

**30 秒延迟**目的：
- 等待网络（systemd 的 After=network.target 不保证 DNS / 公网可达）。
- 等待 web server 监听完成（避免续签后热加载时找不到 server）。
- 等待时钟同步（NTP）。

### 12.3 重启后场景验证

| 场景 | 期望行为 |
| --- | --- |
| s-ui 进程被 `kill -9` | systemd 10 秒内拉起，30 秒后 cronjob 立即检查续签 |
| 服务器重启 | systemd 启动 s-ui，同上 |
| s-ui 停止 30 天后重启（证书早过期）| cronjob 立即触发 RenewIpCert，acme.sh 因证书过期但记录尚存可直接 renew；若 renew 失败，调 IssueLeIPCert 重申 |
| Docker 容器重启 | 同上（acme.sh 在 /root/.acme.sh，需 volume 持久化）|

### 12.4 Docker 持久化要求

`docker-compose.yml` 必须 volume 映射：

```yaml
volumes:
  - ./db:/usr/local/s-ui/db
  - ./cert:/usr/local/s-ui/cert
  - ./acme:/root/.acme.sh    # 新增
```

否则 acme.sh 的账户信息和证书记录会随容器销毁丢失，重启后等于全新申请。

### 12.5 续签失败不掉签的兜底

`RenewIpCert` 失败时**不动**当前证书文件——旧证书继续提供服务直到过期。日志写 Error 级别，前端 `/api/cert_status` 返回 `DaysLeft` 让前端可视化告警（如剩余 < 2 天时高亮红色）。

### 12.6 极端场景：证书已过期

若 `DaysLeft <= 0`：
1. `RenewIpCert` 仍正常调用 `acme.sh --renew --force`，多数情况能成功。
2. 若失败，cronjob 6 小时后重试。
3. 期间面板 HTTPS 会因证书过期而失败 —— 浏览器报警告但不致命；用户可通过 CLI `sui cert -issue-ip` 强制重申。

---

## 13. 热加载方案（**关键**）

> 用户明确要求"支持热生效、热加载"。本节为强制规范。

### 13.1 核心机制：tls.Config.GetCertificate 回调

详见 [§5.5](#55-webwebgo关键修改热加载改造)。

- 启动时：把磁盘证书加载到 `atomic.Pointer[tls.Certificate]`。
- 每次 TLS 握手：从 atomic 指针读取，不经过任何锁，无性能损耗。
- 续签后：`ReloadCert()` 用新 cert 替换 atomic 指针，**不影响已建立的连接**（已建立的连接继续用旧证书数据，新连接握手用新证书）。

### 13.2 触发热加载的 4 个时机

| 时机 | 调用路径 |
| --- | --- |
| 内部续签成功 | `RenewIpCert` → `webReloader.ReloadCert()` |
| 内部首次申请成功 | `IssueLeIPCert` → `webReloader.ReloadCert()` |
| acme.sh reloadcmd | `sui cert -reload` → SIGUSR1 → `main.go` → `app.ReloadWebCert` → `webServer.ReloadCert` |
| 前端手动 | `POST /api/cert_issue*` → ApiService → CertService → ReloadCert |

### 13.3 验证热加载生效的测试方法

1. 启动 s-ui。
2. `openssl s_client -connect 127.0.0.1:2095 < /dev/null 2>/dev/null | openssl x509 -noout -dates` 记录当前 NotAfter。
3. 调用 `sui cert -issue-self` 重新生成。
4. **不重启 s-ui**，再次 `openssl s_client` 看 NotAfter，应该变化。
5. 期望：变化后无任何 502 / connection refused。

### 13.4 边界情况

- `ReloadCert` 时新证书加载失败 → 保留旧指针，不替换，返回 error。
- `ReloadCert` 时 setting 中证书路径为空 → atomic 存 nil，新 HTTPS 握手会失败。**这种情况下应该重启 web Server 才能完全切回 HTTP**——实施方处理方式：在 `RemoveCert` 中除了 ReloadCert 外，**额外调用 `PanelService.RestartPanel(3 * time.Second)`** 触发 SIGHUP 重启，这是从 HTTPS 切 HTTP 唯一安全路径。

---

## 14. 错误处理 / 日志 / 国际化

### 14.1 错误码

所有 CertService 方法返回的 error 必须用 `common.NewError` 或 `fmt.Errorf` 包装，且**中文**描述（与 logger 输出语言一致）。前端展示时直接显示后端 error.Error()。

### 14.2 日志级别

| 事件 | 级别 |
| --- | --- |
| 申请开始 / 成功 | Info |
| 续签开始 / 成功 | Info |
| 续签不需要 | Debug |
| Precheck 失败 | Warning |
| 申请失败 / 续签失败 | Error |
| 热加载失败 | Error |
| acme.sh 命令输出 | Info |

### 14.3 国际化

CertService 返回的 error 主要给运维看，**用中文**即可（与项目其他 logger 输出一致）。前端展示时直接透传。前端按钮、状态文案等用 i18n。

---

## 15. CLI 接口契约（外部依赖此接口，禁止改动）

```
sui cert -issue-ip [-force]
    申请 Let's Encrypt IP 证书。
    -force: 即使当前证书尚未到期也强制重申。
    exit code: 0 成功，1 失败（stderr 打印详细错误）。

sui cert -issue-self
    生成自签证书。
    exit code: 0 成功，1 失败。

sui cert -renew
    强制续签当前 LE IP 证书。
    若当前不是 LE 证书，输出提示并 exit 0。

sui cert -status
    打印当前证书状态（人类可读格式）。
    exit code: 0。

sui cert -remove
    清空证书配置，恢复 HTTP。
    exit code: 0。

sui cert -reload
    内部命令：通知运行中的 sui 进程热加载证书。
    （acme.sh reloadcmd 使用，用户一般不需要手动调用）
    exit code: 0 即使 sui 进程不在也返回 0（避免 acme.sh 报错）。
```

---

## 16. HTTP API 接口契约

所有接口需要 session 鉴权（已登录管理员）。

### 16.1 GET `/api/cert_status`

响应：
```json
{
  "success": true,
  "obj": {
    "mode": "le-ip",
    "hasCert": true,
    "certFile": "/usr/local/s-ui/cert/ip/fullchain.pem",
    "keyFile":  "/usr/local/s-ui/cert/ip/privkey.pem",
    "subject":  "1.2.3.4",
    "issuer":   "Let's Encrypt",
    "ip":       "1.2.3.4",
    "notBefore":"2026-06-20T00:00:00Z",
    "notAfter": "2026-06-26T00:00:00Z",
    "daysLeft": 5
  }
}
```

### 16.2 POST `/api/cert_precheck`

请求：无 body 或 `{"type":"ip"}`。
响应：
```json
{
  "success": true,
  "obj": {
    "publicIp":   "1.2.3.4",
    "port80Free": true,
    "acmeReady":  true,
    "ok":         true,
    "message":    ""
  }
}
```

### 16.3 POST `/api/cert_issueIp`

请求（form）：`force=true|false`。
响应：成功返回 CertStatus（同 cert_status），失败返回 `{success:false, msg:"..."}`。
超时：客户端 90 秒。

### 16.4 POST `/api/cert_issueSelf`

无请求体，返回 CertStatus。

### 16.5 POST `/api/cert_renew`

无请求体，成功返回 CertStatus。

### 16.6 POST `/api/cert_remove`

无请求体，返回 `{success: true}`。

---

## 17. 验收标准

实施方必须**逐条**完成自检，并附带测试截图或命令输出。

### 17.1 后端基础功能

| # | 用例 | 验收方法 | 期望 |
| --- | --- | --- | --- |
| A1 | `sui cert -status`（全新机器）| 命令行 | 输出 `mode=none, hasCert=false` |
| A2 | `sui cert -issue-self`（全新机器，离线环境）| 命令行 + `ls /usr/local/s-ui/cert/self/` | 文件存在，权限正确，无 openssl 依赖 |
| A3 | `sui cert -status`（A2 之后）| 命令行 | 输出 `mode=self, hasCert=true, daysLeft≈3650` |
| A4 | 浏览器访问 `https://127.0.0.1:2095/app/`（A2 之后）| Chrome/Firefox/Safari | 警告页放行后能登录 |
| A5 | `openssl x509 -in self.crt -noout -text` | 命令行 | SAN 包含本机 IP、127.0.0.1、::1 |
| A6 | `sui cert -issue-ip`（具备公网 IP + 80 可用）| 命令行 | 5 分钟内成功，文件存在 |
| A7 | A6 之后浏览器访问 `https://<公网IP>:2095/app/` | Chrome | **无警告，绿锁** |
| A8 | `sui cert -issue-ip`（80 端口被 nginx 占用）| 命令行 | 立即报错，提示 80 端口被占用，不调 acme.sh |
| A9 | `sui cert -renew` | 命令行 | 成功，证书 NotAfter 推后 |
| A10 | `sui cert -remove` | 命令行 + 访问 `http://...` | 浏览器跳转到 HTTP，无 502 |

### 17.2 热加载

| # | 用例 | 验收方法 | 期望 |
| --- | --- | --- | --- |
| B1 | 启动 s-ui，记录 cert SHA256 | `openssl s_client -connect 127.0.0.1:2095 < /dev/null \| openssl x509 -fingerprint -noout` | 得到指纹 1 |
| B2 | 不重启 s-ui，执行 `sui cert -issue-self`，再次获取指纹 | 同上 | 得到指纹 2，**与指纹 1 不同** |
| B3 | B2 期间，已建立的 HTTPS 长连接 | curl --keepalive 或 浏览器保持页面 | 不断开 |
| B4 | 同 B2 但用 `sui cert -reload`（模拟 acme.sh 触发）| `pgrep sui` 后 `kill -SIGUSR1 <pid>` 也行 | 指纹变化 |
| B5 | 热加载后新连接 | curl https://... | 200 OK |

### 17.3 续签可靠性

| # | 用例 | 验收方法 | 期望 |
| --- | --- | --- | --- |
| C1 | 成功申请 LE 证书后，`systemctl stop s-ui && systemctl start s-ui` | journalctl -u s-ui | 30 秒后日志显示 "cert renewal: ..." |
| C2 | 服务器重启 | `reboot` 后 5 分钟看日志 | s-ui 自启，cronjob 启动，30 秒后检查证书 |
| C3 | `kill -9 <sui-pid>` | systemctl status s-ui | 10 秒内自动拉起（Restart=always） |
| C4 | 模拟证书过期：把 `--days 6` 改成 `--days 1` 重申，等 24h | 24h 后看日志 | cronjob 自动续签成功 |
| C5 | Docker 容器重启 | `docker compose restart` | acme.sh 数据保留（验证 ./acme 目录有内容），续签成功 |
| C6 | acme.sh 不在 → 调 `IssueLeIPCert` | 删除 /root/.acme.sh 后调用 | 自动重装 acme.sh 后申请成功 |

### 17.4 前端 UI

| # | 用例 | 验收方法 | 期望 |
| --- | --- | --- | --- |
| D1 | Settings 页"接口"Tab 顶部 | 浏览器 | 显示 CertCard 卡片 |
| D2 | 卡片显示当前状态 | 浏览器 | 模式 / IP / 到期 / 剩余天数齐全 |
| D3 | 点"申请 IP 证书" | 浏览器 | 预检对话框 → 确认 → loading → 成功提示 |
| D4 | 申请失败 | 浏览器 | 错误消息完整可读，含 acme.sh 失败原因 |
| D5 | 点"生成自签证书" | 浏览器 | 二次确认 → 成功 |
| D6 | 点"恢复 HTTP" | 浏览器 | 二次确认 → 成功 → 3 秒后跳转到 HTTP |
| D7 | 六种语言 | 切换语言 | CertCard 内文案随语言切换 |

### 17.5 装机/运维

| # | 用例 | 验收方法 | 期望 |
| --- | --- | --- | --- |
| E1 | 全新机器跑 `install.sh` | 跟随提示选 IP 证书 | 装完直接 HTTPS 可访问 |
| E2 | 同 E1 选自签 | 装完 HTTPS 可访问（带警告）|
| E3 | 同 E1 选跳过 | 装完仅 HTTP |
| E4 | `s-ui` 菜单 SSL 子菜单 | 终端 | 显示新增的 IP 证书选项 |
| E5 | Docker 镜像 `docker run` | 容器日志 | acme.sh 已预装 |

### 17.6 代码质量

| # | 检查项 | 验收方法 | 期望 |
| --- | --- | --- | --- |
| F1 | 自签证书生成不调用 openssl 二进制 | grep `openssl` 在 service/cert*.go | 仅 acme.sh 调用相关；无 exec openssl |
| F2 | 所有 CertService 方法并发安全 | review | 关键方法有 mutex |
| F3 | tls.Config 用 GetCertificate 回调 | review web/web.go | 不再传 Certificates 切片 |
| F4 | s-ui.service 使用 Restart=always | cat | 是 |
| F5 | settings 表新增 key 默认值落库 | DB 查询 | certMode/certDomain 存在 |
| F6 | 中文注释，与项目其他代码一致 | review | 是 |
| F7 | 不引入新数据库表 | grep AutoMigrate | 数量不变 |
| F8 | go build 无 warning | `go build -tags "$BUILD_TAGS" ./...` | 通过 |
| F9 | frontend `npm run build` 通过 | build | 通过 |

### 17.7 文档

| # | 检查项 | 期望 |
| --- | --- | --- |
| G1 | README 增加 SSL 章节，说明三种证书模式 | 是 |
| G2 | 本规范文档（`docs/SSL证书自动化开发规范.md`）已存在 | 是 |
| G3 | CHANGELOG 记录新增功能 | 是 |

---

## 18. 附录

### 附录 A：acme.sh 命令完整参考

```bash
# 安装
curl https://get.acme.sh | sh -s email=admin@s-ui.local

# 设默认 CA
~/.acme.sh/acme.sh --set-default-ca --server letsencrypt --force

# 申请 IP 证书（shortlived 6 天）
~/.acme.sh/acme.sh --issue -d 1.2.3.4 --standalone \
    --server letsencrypt \
    --certificate-profile shortlived \
    --days 6 \
    --httpport 80 \
    --force

# 安装证书 + 注册 reloadcmd
~/.acme.sh/acme.sh --installcert -d 1.2.3.4 \
    --key-file /usr/local/s-ui/cert/ip/privkey.pem \
    --fullchain-file /usr/local/s-ui/cert/ip/fullchain.pem \
    --reloadcmd "/usr/local/s-ui/sui cert -reload"

# 强制续签
~/.acme.sh/acme.sh --renew -d 1.2.3.4 --force

# 升级 acme.sh 并启用自动升级
~/.acme.sh/acme.sh --upgrade --auto-upgrade

# 查看已有证书
~/.acme.sh/acme.sh --list
```

### 附录 B：目录约定

```
/usr/local/s-ui/
├── sui                          # 主程序
├── s-ui.sh                      # 运维脚本
├── db/
│   └── s-ui.db
├── cert/
│   ├── ip/                      # Let's Encrypt IP 证书
│   │   ├── fullchain.pem        # 0644
│   │   └── privkey.pem          # 0600
│   └── self/                    # 自签证书
│       ├── self.crt             # 0644
│       └── self.key             # 0600
└── ...

/root/.acme.sh/                  # acme.sh 安装目录（3X-UI 风格）
├── acme.sh
├── account.conf
└── 1.2.3.4_ecc/                 # IP 证书工作目录
    └── ...
```

### 附录 C：错误码 / 错误消息建议

| 场景 | 错误消息 |
| --- | --- |
| 公网 IP 检测失败 | `公网 IP 检测失败，请确认服务器可访问公网，或手动指定 IP` |
| 80 端口被占用 | `80 端口被占用，请先停止占用 80 端口的服务（如 nginx）再申请` |
| acme.sh 安装失败 | `acme.sh 安装失败：%s，请检查 curl 和网络` |
| acme.sh --issue 失败 | `证书申请失败，acme.sh 输出：\n%s` |
| 证书文件不存在 | `证书文件不存在：%s` |
| 证书解析失败 | `证书解析失败：%s` |
| 自签 sanity check 失败 | `自签证书自检失败，已回滚：%s` |

### 附录 D：测试用例清单（实施方自查）

实施方完成后，提交时附带：

- [ ] §17.1 后端基础（10 项）
- [ ] §17.2 热加载（5 项）
- [ ] §17.3 续签可靠性（6 项）
- [ ] §17.4 前端 UI（7 项）
- [ ] §17.5 装机/运维（5 项）
- [ ] §17.6 代码质量（9 项）
- [ ] §17.7 文档（3 项）

合计 **45 项**全部通过为最终验收标准。

---

## 19. 实施顺序（建议）

可按以下顺序逐步提 PR，每个 PR 独立可验证：

### Phase 1：后端核心（必须通过 §17.1 A1-A5 + §17.6 F1-F2 + F7-F8）

1. `service/setting.go` 加 setter + 默认值
2. `service/cert_selfsigned.go` 实现纯 Go 自签
3. `service/cert.go` 实现 CertService（先只实现 IssueSelfSigned + GetCertStatus + RemoveCert）
4. `cmd/cert.go` + `cmd/cmd.go` 注册子命令（先支持 -issue-self / -status / -remove）

### Phase 2：热加载（必须通过 §17.2）

5. `web/web.go` 改造 tls.Config + ReloadCert
6. `app/app.go` 注入 webReloader 到 CertService
7. `main.go` 监听 SIGUSR1
8. `cmd/cert.go` 增加 -reload 命令

### Phase 3：LE IP 证书（必须通过 §17.1 A6-A10）

9. `service/cert_acme.go` 封装 acme.sh
10. `service/cert.go` 实现 IssueLeIPCert + RenewIpCert + Precheck
11. `cmd/cert.go` 增加 -issue-ip / -renew 命令

### Phase 4：续签守护（必须通过 §17.3）

12. `cronjob/cert_renewal_job.go` 新增
13. `cronjob/cronJob.go` 注入 + 注册
14. `s-ui.service` 改 `Restart=always`

### Phase 5：HTTP API + 前端（必须通过 §17.4）

15. `api/apiHandler.go` + `api/apiService.go` 加路由
16. `frontend/src/components/CertCard.vue` 新增
17. `frontend/src/views/Settings.vue` 嵌入
18. `frontend/src/locales/*.ts` 加 i18n

### Phase 6：装机 + Docker（必须通过 §17.5）

19. `install.sh` 加 ask_ssl_strategy
20. `s-ui.sh` 菜单扩充
21. `Dockerfile` 加依赖
22. `entrypoint.sh` 预装 acme.sh
23. `docker-compose.yml` 加 volume

### Phase 7：文档

24. README 新增 SSL 章节
25. CHANGELOG 更新

---

## 20. 最终交付物清单

实施方完成后必须提交：

1. ✅ 所有 §4 列出的代码改动（新增 + 修改）
2. ✅ §17 所有验收用例的执行记录（截图或命令输出）
3. ✅ 一份 `IMPLEMENTATION_REPORT.md`，说明：
   - 实际新增 / 修改的文件清单（精确到行数）
   - 与本规范的偏差点（如有），并说明原因
   - 已知问题与限制
4. ✅ Git commit 按 §19 阶段拆分，commit message 遵循项目 `编译和git规范.md`
5. ✅ 测试视频或 GIF（可选但推荐）：展示装机 → 选 LE → 自动 HTTPS 访问

---

## 结语

本规范的核心理念：

1. **唯一真理源**：所有证书逻辑收敛到 `service/cert.go`，Shell / 前端 / CLI 只是壳。
2. **可观测**：每个操作有日志、有 changes 审计、有前端状态。
3. **可恢复**：续签三重保障，重启不掉签。
4. **真热加载**：原子指针 + GetCertificate 回调，0 重启切证书。
5. **自签可靠**：纯 Go 实现，强制 sanity check，绝不交付坏证书。

如有规范不清楚或冲突之处，**先停下问，不要自行决断**。
