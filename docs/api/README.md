# S-UI API 文档

本目录包含 S-UI 面板的接口文档，按前端 / 后端视角分开维护。

## 文档索引

| 文档 | 说明 | 适用读者 |
|------|------|----------|
| [backend.md](./backend.md) | **后端 REST API 完整参考**：所有接口的路径、参数、响应结构、鉴权方式、错误处理 | 后端开发、第三方脚本/集成开发者 |
| [backend-bulk-import.md](./backend-bulk-import.md) | **后端批量导入指南**：出站节点批量写入、路由规则批量导入的 curl 示例与各协议 data 字段模板 | 运维脚本编写者 |
| [frontend.md](./frontend.md) | **前端 API 调用文档**：HTTP 层封装、Pinia Store、鉴权流程、各视图的 API 调用场景与数据契约 | 前端开发、需要理解前端如何消费后端接口的开发者 |

## 接口概览

### 面板接口（`/api`）— Cookie Session 鉴权

面向 Web 面板内部调用。登录后通过 `s-ui` Cookie 维持会话。

### 开放接口（`/apiv2`）— Token 请求头鉴权

面向第三方/脚本。请求头 `Token: <令牌>`，令牌通过面板接口 `POST /api/addToken` 创建。

> 两套接口共用同一套业务逻辑，`/apiv2` 是 `/api` 的子集（不含会话/账户管理类动作）。差异详见 [backend.md §十四](./backend.md#十四接口组差异api-vs-apiv2)。

### 订阅服务（独立端口，无鉴权）

运行在单独端口（设置项 `subPort`，默认 `2096`），无 Cookie/Token 鉴权。详见 [backend.md §十三](./backend.md#十三订阅服务独立端口)。

## 版本

当前文档对应版本：**v1.6.7**
