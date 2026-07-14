# 数据库备份、还原与锁安全说明

更新时间：2026-07-15

本文记录 s-ui-pro 数据库安全改动、失败边界、运行限制和验证方法，供后续维护者及 AI 修改 `database/`、`service/`、`cronjob/` 时优先阅读。

> 本文对应 `codex/safe-config-apply` 上的待发布改动。不要据此推断已经发布的 `v1.6.20` 二进制包含这些修复，应以目标 tag/commit 为准。

## 1. 备份行为

- `database.GetDb` 使用 SQLite 原生 `VACUUM INTO`，整个文件来自同一个时间点的一致快照。
- 快照自动包含全部表、索引、触发器及未来新增对象，不再手工逐表复制。
- 当前 11 张持久表全部检查，其中包括旧实现遗漏的 `services` 和 `tokens`。
- 返回备份前必须通过 `PRAGMA integrity_check` 和核心表存在性检查。
- 每个备份使用独立的 `0700` 私有临时目录，数据库文件权限为 `0600`，读取完成后删除。
- 多个备份请求可同时进入，但线上 SQLite 采用单连接池，实际快照操作会安全排队，不会共用临时文件。

### `stats` / `changes` 排除项

Web 界面默认勾选排除 `stats` 和 `changes`。因此：

- 默认下载不是包含历史统计和变更日志的全量数据备份。
- 需要完整备份时，必须取消这两个勾选项，使请求使用 `exclude=""`。
- 排除时只清空对应数据行，表结构仍保留，保证还原后 schema 完整。
- 删除后会再次执行 `VACUUM`，避免已排除内容残留在 SQLite 自由页中。

### 备份文件属于敏感数据

完整备份可能包含：

- API token；
- 面板及订阅配置密钥；
- 客户端账号、密码和节点配置；
- 管理员 bcrypt 密码哈希；
- 流量统计和操作记录。

`0600` 只是 Unix 文件权限，不等于备份加密、Windows ACL 或端到端保护。项目目前没有提供备份加密、远端存储、自动轮换或自动备份，外部存储和生命周期仍由管理员负责。

CLI 写备份时先收紧已有文件权限，再写入同目录 `0600` 临时文件并替换目标，避免覆盖已有 `0644` 文件时泄露或留下半写文件。HTTP 下载响应使用 `Cache-Control: no-store`。

## 2. 还原流程

还原不会在收到上传后立即关闭或覆盖线上数据库。实际顺序如下：

1. 流式读取 multipart 上传，写入数据库目录内唯一的 `0600` 临时文件。
2. `fsync` 并关闭临时文件。
3. 检查 SQLite 文件头和 `PRAGMA integrity_check`。
4. 检查 s-ui 核心表、关键列、至少一个用户、唯一 `config` 设置及其 JSON 有效性。
5. 在临时库上执行数据库迁移；迁移只返回错误，不得 `log.Fatal` 或退出进程。
6. 在临时库上执行初始化/`AutoMigrate`、WAL checkpoint，再检查全部 11 张持久表和完整性。
7. 比较源库与线上库 `PRAGMA page_size`；不同则安全拒绝，不触碰线上数据。
8. 进入备份/还原写锁，使用 SQLite Online Backup API 提交到线上库。
9. 同一线上连接先保存旧库回滚快照，再复制新库并做最终完整性/表检查。
10. 成功后调度“数据库还原专用”应用重载；关闭旧 core 时不会把旧运行态流量写入新库。

默认最大上传数据库为 4 GiB，可用环境变量调整：

```bash
SUI_MAX_RESTORE_BYTES=8589934592  # 示例：8 GiB
```

HTTP 层和数据库复制层都会执行同一上限。上传采用流式 multipart 读取，不会先在系统临时目录保存一份完整副本。还原期间仍需预留上传库、线上库回滚快照和 SQLite 临时/WAL 所需磁盘空间。

## 3. 还原失败边界

| 阶段 | 失败结果 |
| --- | --- |
| 上传、写盘、校验、迁移、AutoMigrate、page size 检查 | 线上库完全不变，原连接继续可用 |
| 保存旧库快照失败 | 线上库完全不变 |
| 新库复制或最终 live 校验失败 | 在同一独占连接内用旧库快照回滚，再返回错误 |
| 数据库已提交，但应用重载调度失败 | **不回滚已验证的新库**；错误明确提示数据库已恢复，需要手动重启 |

重载回调只同步确认“信号已成功调度”。延迟发送 `SIGHUP` 时如果操作系统返回错误，该错误只能写入日志，不能再改变已经返回的 HTTP 结果，也不会回滚已提交数据库。

恢复信号使用进程内一次性标记与普通手工 `SIGHUP` 区分：普通重启会落库最后流量，数据库恢复重启会丢弃旧 tracker 和旧 pending，保证新库与上传快照一致。

CGO 构建使用 `mattn/go-sqlite3` Online Backup API。复制以分页方式进行并带超时；目标写操作占用连接期间，同进程请求在连接池排队，不会与还原互相争抢 SQLite 写锁。

`CGO_ENABLED=0` 构建只保留可编译的安全拒绝实现，**不支持数据库还原**。当前 Linux 发布构建必须启用 CGO。Windows ARM64 non-CGO 目标能通过编译，但不能据此宣称实际支持还原。

## 4. SQLite 锁与事务策略

- 线上数据库继续使用 WAL 和 `_busy_timeout=10000`。
- 应用连接池限制为一个连接；面板、cron、备份提交和还原写入在 `database/sql` 层排队，避免同一进程内部出现 `SQLITE_BUSY`。
- 定时 checkpoint 使用 `PASSIVE`，不再用会等待活跃读写者的 `FULL`。
- cron 任务先同步注册再启动，所有任务使用 `SkipIfStillRunning`；停止时等待正在运行的任务退出，避免快速重载后产生两套重复写库任务。
- 配置核心生命周期、流量记账和数据库操作使用固定锁顺序：核心生命周期 → 流量记账 → SQLite。
- Stats 事务显式检查 Begin/Commit/Rollback；写库失败会把已取出的流量放回当前 tracker。
- 用户流量重置与审计记录在同一事务中提交，并与 stats 快照互斥；任一步失败都会整体回滚。
- 重置成功后同步清理对应内存计数，避免旧周期流量重新写回。
- 客户端禁用/重置只在事务提交后更新限速运行态和 `LastUpdate`。
- inbound 热重载会先构造全部替换配置，再删除任何运行中 inbound，避免配置行损坏造成半运行态。
- 客户端引起的 inbound 热重载在数据库提交后执行；其他旧热更新若事务回滚，会从已回滚数据库完整恢复运行态。
- core 重启会在关闭前后各取一次流量快照；局部热重载失败会退回完整重启，启动失败时计数保留到下次成功启动后继续落库。
- 待恢复计数只会在稳定启动、数据库提交或旧配置恢复成功后消费，候选配置启动本身不会提前清空计数。
- 正常 `SIGTERM` / `SIGHUP` 关闭会先刷新当前计数，再关闭 core 并落库连接收尾期间的最后一批流量。

## 5. 必须保留的回归测试

主要测试位于：

- `database/backup_test.go`
- `database/restore_cgo_test.go`
- `cmd/migration/main_test.go`
- `cmd/backup_test.go`
- `core/tracker_stats_test.go`
- `service/client_test.go`
- `cronjob/cronJob_test.go`

至少验证：

- 备份包含 `services`、`tokens`，并通过完整性检查；
- 排除项保留空表且不保留数据；
- 多路并发备份均为独立有效文件；
- 损坏 SQLite、无关 SQLite、超限文件不能影响线上连接；
- 成功还原后数据库可继续读写；
- live 最终校验失败会恢复旧库；
- 数据库已提交后的重启失败会报告正确提交边界；
- 迁移失败返回 error，不退出宿主进程；CLI 迁移失败返回非零状态；
- CLI 覆盖已有宽权限文件后仍为 `0600`；
- Windows 盘符能生成正确 SQLite URI；Windows ARM64 non-CGO 仍可编译；
- stats 写入失败能够回填计数，用户计数重置不破坏已有连接的计数指针。
- 流量重置的审计写入失败时，客户端累计值和启用状态整体回滚。
- core 关闭尾段流量可被重启接管，配置事务失败不会消费待恢复计数。
- StopCore 会在关闭前后落库计数，避免正常退出丢失最后一个统计周期。
- 数据库恢复专用关闭不会将旧 runtime 计数写入恢复后的新库。

推荐验证命令：

```bash
go test -count=1 ./...
go test -race -count=1 ./database ./cmd/migration ./core ./service ./cronjob
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build ./...
```

## 6. 运维还原检查清单

1. 还原前保留一份独立、离线的当前全量备份。
2. 确认磁盘空间足够，且备份来源可信。
3. 对全量备份取消 Web 界面的 `stats` / `changes` 排除项。
4. 还原成功后确认面板重新可访问、管理员可以登录、sing-box core 已启动。
5. 抽查 settings、clients、inbounds、outbounds、services、tokens 和统计数据。
6. 若提示“数据库已恢复但重启失败”，不要重复导入；应手动重启服务并检查日志。

`integrity_check=ok` 只证明 SQLite 结构一致，不证明 sing-box 配置语义、证书路径或外部资源一定有效。上线前仍需做应用级检查。

## 7. 关键代码位置

| 文件 | 作用 |
| --- | --- |
| `database/backup.go` | 备份、上传预检、schema/完整性校验 |
| `database/restore_cgo.go` | CGO 在线提交、live 校验和回滚 |
| `database/restore_nocgo.go` | non-CGO 安全拒绝 |
| `database/db.go` | WAL、busy timeout、单连接池、候选 DB 初始化 |
| `cmd/migration/main.go` | 可返回错误、可指定路径的迁移 |
| `service/stats.go`、`core/tracker_stats.go` | 流量事务失败回填和计数重置 |
| `service/client.go`、`service/config.go` | 客户端重置、事务提交和 core 状态一致性 |
| `cronjob/cronJob.go`、`cronjob/WALCheckpointJob.go` | cron 生命周期与非阻塞 checkpoint |
