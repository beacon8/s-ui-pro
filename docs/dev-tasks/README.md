# S-UI 开发任务文档

本目录存放**开发任务提示词**（dev-task / frontend-task），用于指导 AI agent 实施特定功能。这些文档不是 API 接口文档，而是功能开发的设计规范与实施步骤。

> API 接口文档请见 [../api/](../api/)。

## 文档索引

| 文档 | 状态 | 说明 |
|------|------|------|
| [dev-task-user-rate-limit.md](./dev-task-user-rate-limit.md) | 已实施 | 客户端级上下行限速（LimiterTracker + DB 字段 + 前端表单） |
| [dev-task-top-users-ranking.md](./dev-task-top-users-ranking.md) | 已实施 | 用户流量排行 API + Clients 页排行视图 |
| [dev-task-rule-import-api.md](./dev-task-rule-import-api.md) | 已实施 | 路由规则批量导入 API（后端冲突校验 + 热重载） |
| [dev-task-local-node-parse.md](./dev-task-local-node-parse.md) | 已实施 | 出站批量导入支持「本地粘贴节点」模式 |
| [frontend-task-inbound-modal.md](./frontend-task-inbound-modal.md) | 参考文档 | 入站添加/编辑弹窗的前端组件关联、数据骨架与业务逻辑参考 |
