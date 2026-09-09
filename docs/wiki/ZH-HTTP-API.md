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

## Transport 契约

Gin v1.12.0 被隔离在项目自有的 `Params`、`Responder`、`Principal`、`Handler` 与 `Router` 契约后，业务 handler 不依赖 `gin.Context`。标准 listener 与可选 Agent listener 共用 adapter，但保留不同的路由和暴露边界。

自动尾斜杠重定向、fixed-path 重定向、路径清理和自动 405 都被关闭。声明路由同时注册两种尾斜杠形式，GET 路由显式支持 HEAD；未知路径或方法继续保持项目的 404 行为。参数名会映射回业务路由声明的名称。

中间件顺序覆盖请求日志、panic recovery、认证、gzip 和可选 mTLS OU 校验。认证或证书失败会在业务 handler 前终止。Follower 代理一旦提交响应也会结束调用链，防止同一写操作继续在本地执行。

JSON 使用 `application/json; charset=UTF-8` 且不追加换行；HTML 使用配置的 UTF-8 content type，重定向保留显式状态，Web/UI 配置响应使用 `Cache-Control: no-store`。listener 创建、TLS key 加载、HTTP/HTTPS/Unix socket 选择、Agent 启动和进程关闭仍由 `internal/app` 负责，而不是 router。

## 路由与重试

Raft Follower 可以把受支持的业务请求代理到 Leader。节点本地的健康、指标、bootstrap、snapshot 和配置回读仍在被访问节点执行。日常自动化建议使用 Leader-aware 代理或 `orch`。

为兼容历史 API，部分 GET 路由也可能修改状态，因此不能只按 HTTP 方法决定是否重试。写请求发送后发生超时或断连时，应先回读资源或审计状态。Raft `expectedIndex` 等 compare-and-set 字段可用时应优先使用。

## 安全

使用 TLS、认证和最小化网络暴露，不要把凭据放进 URL。使用 proxy 认证时，边缘代理必须先删除用户提供的身份头，并且只在认证成功后写入可信头。除非集成必须直接使用 API，否则优先使用 `orch`，因为它已经实现业务响应检查和“结果未知”的显式退出码。

API 验收应覆盖代表性读写、两种尾斜杠、HEAD、认证、gzip、mTLS 拒绝、URL prefix、Follower 代理终止，以及部署实际选择的 HTTP/HTTPS/Unix listener。路由单测不能证明真实证书、代理、Raft、MySQL、浏览器或恢复行为。
