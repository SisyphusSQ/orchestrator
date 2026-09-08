# HTTP API

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-HTTP-API) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

HTTP API 是 `orch` 与 Web 控制台共同使用的契约。默认基础路径是 `/api`；配置 `URLPrefix` 后需要在前面加上该前缀。

## 响应契约

许多接口返回包含 `Code`、`Message` 和 `Details` 的 envelope。调用方必须同时判断 HTTP 状态和业务 `Code`；部分兼容接口可能在 HTTP 200 中返回业务错误。

常见读取：

```http
GET /api/clusters
GET /api/instance/db.example.com/3306
GET /api/raft/configuration
GET /health/ready
```

Raft 成员管理使用明确的 HTTP 方法和 JSON：

```http
POST /api/raft/members
Content-Type: application/json

{"id":"node-2","address":"node-2:10008","suffrage":"voter"}
```

权威路由注册位于 [`internal/http/api.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/api.go)；[`tools/orch-cli/internal/cmd/catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json) 记录了生成 `orch` 命令的当前映射。

## 路由与重试

Raft Follower 可以把受支持的业务请求代理到 Leader。节点本地的健康、指标、bootstrap、snapshot 和配置回读仍在被访问节点执行。日常自动化建议使用 Leader-aware 代理或 `orch`。

为兼容历史 API，部分 GET 路由也可能修改状态，因此不能只按 HTTP 方法决定是否重试。写请求发送后发生超时或断连时，应先回读资源或审计状态。Raft `expectedIndex` 等 compare-and-set 字段可用时应优先使用。

## 安全

使用 TLS、认证和最小化网络暴露，不要把凭据放进 URL。使用 proxy 认证时，边缘代理必须先删除用户提供的身份头，并且只在认证成功后写入可信头。除非集成必须直接使用 API，否则优先使用 `orch`，因为它已经实现业务响应检查和“结果未知”的显式退出码。
