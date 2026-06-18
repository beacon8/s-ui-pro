# 编译和 Git 规范

本文档记录 s-ui-pro 项目在 macOS（Apple Silicon）上交叉编译 Linux amd64 二进制、打包发布 Release、以及 Git 推送的完整规范，包含踩坑记录。

---

## 一、编译规范

### 1.1 目标

在 macOS（arm64）上编译出能在 Linux x86_64（amd64）服务器上运行的二进制文件。

### 1.2 前置依赖

```bash
# 前端构建
node >= 18
npm

# 后端交叉编译工具链（必须用 glibc，不能用 musl）
brew tap messense/macos-cross-toolchains
brew trust messense/macos-cross-toolchains
brew install x86_64-unknown-linux-gnu

# GNU tar（打包用，必须，避免 macOS 自带 tar 污染压缩包）
brew install gnu-tar
```

### 1.3 编译命令

**第一步：构建前端**

```bash
cd frontend
npm i
npm run build
cd ..
mkdir -p web/html
rm -fr web/html/*
cp -R frontend/dist/* web/html/
```

**第二步：交叉编译后端（Linux amd64）**

```bash
CC=x86_64-unknown-linux-gnu-gcc \
CGO_ENABLED=1 \
GOOS=linux \
GOARCH=amd64 \
go build -ldflags="-w -s" \
  -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" \
  -o sui-linux-amd64 main.go
```

**验证产物架构**

```bash
file sui-linux-amd64
# 期望输出：ELF 64-bit LSB executable, x86-64 ... for GNU/Linux 3.2.0
```

### 1.4 踩坑记录

| 错误 | 原因 | 解决 |
|---|---|---|
| `Exec format error` | macOS 本地 `./build.sh` 编译出的是 macOS arm64 二进制，不能在 Linux x86_64 上运行 | 必须交叉编译，指定 `GOOS=linux GOARCH=amd64` |
| `Binary was compiled with CGO_ENABLED=0, go-sqlite3 requires cgo` | 未启用 CGO，go-sqlite3 依赖 CGO | 必须设置 `CGO_ENABLED=1` 并指定交叉编译 CC |
| `cannot find -l:libcronet.a` | 用了 musl 工具链（`musl-cross`），但 libcronet.so 是 glibc 编译的，两者不兼容 | 换用 glibc 工具链 `x86_64-unknown-linux-gnu-gcc` |
| `undefined reference to xxx@GLIBC_2.x.x` | musl 工具链无法解析 glibc 符号 | 同上，必须用 glibc 工具链 |
| `with_musl` 标签导致链接失败 | `build.sh` 里的 `with_musl` 是给 macOS 本地静态构建用的，交叉编译到 Linux 时要换成 `with_purego` | 交叉编译时用 `with_purego` 替代 `with_musl` |

---

## 二、打包规范

### 2.1 打包命令

**必须使用 GNU tar（`gtar`），不能用 macOS 自带 tar。**

macOS 自带 tar 会在压缩包里塞入 `._` 开头的 AppleDouble 文件和 `LIBARCHIVE.xattr.com.apple.provenance` 扩展属性，在 Linux 解压时产生警告，且目录列表不干净。

```bash
# 准备目录
rm -rf /tmp/s-ui-release
mkdir -p /tmp/s-ui-release/s-ui

# 复制产物（注意：sui 用交叉编译的 sui-linux-amd64）
cp sui-linux-amd64     /tmp/s-ui-release/s-ui/sui
cp s-ui.sh             /tmp/s-ui-release/s-ui/
cp s-ui.service        /tmp/s-ui-release/s-ui/
chmod +x /tmp/s-ui-release/s-ui/sui /tmp/s-ui-release/s-ui/s-ui.sh

# 用 GNU tar 打包
cd /tmp/s-ui-release
gtar --format=gnu -czf s-ui-linux-amd64.tar.gz s-ui/
```

**验证包内容（不应有 `._` 开头的文件）**

```bash
gtar tvf s-ui-linux-amd64.tar.gz
# 期望只有：
# s-ui/
# s-ui/sui
# s-ui/s-ui.service
# s-ui/s-ui.sh
```

### 2.2 踩坑记录

| 错误 | 原因 | 解决 |
|---|---|---|
| 解压时出现 `._s-ui`、`._sui` 等文件 | macOS 自带 tar 打包时写入了 AppleDouble 文件（macOS 扩展属性） | 改用 `gtar`（GNU tar） |
| `tar: Ignoring unknown extended header keyword 'LIBARCHIVE.xattr.com.apple.provenance'` | macOS tar 写入了 libarchive 扩展头 | 改用 `gtar --format=gnu` |
| `COPYFILE_DISABLE=1` 不完全有效 | macOS tar 部分元数据不受 `COPYFILE_DISABLE` 控制 | 必须直接用 `gtar` |

---

## 三、发布 Release 规范

使用 `gh` CLI 发布，需提前登录（`gh auth login`）。

```bash
# 删除旧版本（如果存在）
gh release delete v1.4.1 --repo beacon8/s-ui-pro --yes

# 创建新 Release 并上传压缩包
gh release create v1.4.1 \
  /tmp/s-ui-release/s-ui-linux-amd64.tar.gz \
  --repo beacon8/s-ui-pro \
  --title "v1.4.1" \
  --notes "变更说明"
```

---

## 四、Git 推送规范

### 4.1 remote 配置

```bash
# 查看当前 remote
git remote -v

# 修改为正确仓库（首次或 remote 指向错误时）
git remote set-url origin https://github.com/beacon8/s-ui-pro.git
```

### 4.2 推送流程

```bash
# 只暂存需要的文件，不用 git add .
git add <文件1> <文件2> ...

# 提交
git commit -m "feat/fix/chore: 描述"

# 推送
git push origin main
```

### 4.3 踩坑记录

| 错误 | 原因 | 解决 |
|---|---|---|
| `error: remote origin already exists` | 执行 `git remote add origin` 时 origin 已存在 | 用 `git remote set-url origin <url>` 修改而不是 add |
| `Permission denied (publickey)` | SSH key 未添加到 GitHub | 换用 HTTPS 方式，或把 `~/.ssh/id_ed25519.pub` 添加到 GitHub SSH keys |
| `Authentication failed` | GitHub 不支持密码登录，需要 Personal Access Token | 用 `gh auth login` 或在 remote URL 中嵌入 token |
| push 到了旧仓库 `admin8800/s-ui` | remote origin 未更新 | `git remote set-url origin https://github.com/beacon8/s-ui-pro.git` |

### 4.4 认证推荐方式

优先使用 `gh` CLI 管理认证，配置一次永久有效：

```bash
gh auth login
# 选择 GitHub.com → HTTPS → Login with a web browser
```

---

## 五、完整一键发布流程

每次发布新版本执行以下步骤：

```bash
cd /Users/yuzai/Tools/s-ui

# 1. 构建前端
cd frontend && npm i && npm run build && cd ..
mkdir -p web/html && rm -fr web/html/* && cp -R frontend/dist/* web/html/

# 2. 交叉编译后端
CC=x86_64-unknown-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
go build -ldflags="-w -s" \
  -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" \
  -o sui-linux-amd64 main.go

# 3. 打包
rm -rf /tmp/s-ui-release && mkdir -p /tmp/s-ui-release/s-ui
cp sui-linux-amd64 /tmp/s-ui-release/s-ui/sui
cp s-ui.sh s-ui.service /tmp/s-ui-release/s-ui/
chmod +x /tmp/s-ui-release/s-ui/sui /tmp/s-ui-release/s-ui/s-ui.sh
cd /tmp/s-ui-release && gtar --format=gnu -czf s-ui-linux-amd64.tar.gz s-ui/

# 4. 验证包（确认无 ._ 文件）
gtar tvf s-ui-linux-amd64.tar.gz

# 5. 发布 Release
cd /Users/yuzai/Tools/s-ui
gh release delete vX.X.X --repo beacon8/s-ui-pro --yes 2>/dev/null || true
gh release create vX.X.X \
  /tmp/s-ui-release/s-ui-linux-amd64.tar.gz \
  --repo beacon8/s-ui-pro \
  --title "vX.X.X" \
  --notes "变更说明"

# 6. 推送源码
git add <修改的文件>
git commit -m "描述"
git push origin main
```

---

## 六、服务器安装命令

```bash
bash <(curl -Ls https://raw.githubusercontent.com/beacon8/s-ui-pro/main/install.sh)
```
