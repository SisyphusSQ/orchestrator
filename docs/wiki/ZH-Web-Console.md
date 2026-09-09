# Web 控制台

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Web-Console) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

生产 Web 控制台是 React、TypeScript 与 Ant Design 应用，通过 `go:embed` 编入 `bin/orchestrator`。它与服务端共用监听、URL 前缀、认证和 TLS 策略；运行时不需要 `resources/`、`web/`、Node.js 或独立静态服务器。

`make binary` 先把前端构建到 `web/dist/`，同步到 `web/assets/` 下的 embed 源，再构建服务端；`make build` 还会构建 `bin/orch`。直接执行 `go build` 不会准备生产 Web 资源。Docker 使用独立 Node 阶段并把产物复制到 Go 构建阶段，最终运行镜像不包含 Node。内嵌资源缺失时返回明确 503，不会从工作目录读取旧资源。

从服务端同源地址打开 `/web/clusters`。如果 `URLPrefix` 是 `/orchestrator`，则访问 `/orchestrator/web/clusters`。

## 运维流程

控制台提供集群与问题总览、拓扑/列表视图、实例详情、发现与搜索、维护和停机控制、拓扑调整、恢复视图、审计历史，以及可选的 Agent/seed 页面。可用操作会依据服务端能力与只读策略显示。

拓扑支持缩放平移、小地图、折叠、紧凑与机房视图、实例别名和列表回退，并可提出智能/经典/GTID/Pseudo-GTID 调整。实例详情展示复制状态、位点、延迟、半同步、错误、GTID、标签、等价位点、维护和近期恢复。在对应路由仍受支持时，恢复、审计、检测和 Agent/seed 页面保留集群、别名、实例、ID 与 UID 深链。

写操作需要确认，执行中禁止重复提交。界面同时检查 HTTP 状态和 API 响应的 `Code`。断连或超时时显示“结果未知”，不会自动重放；继续操作前应回读拓扑、恢复、维护和审计状态。

Follower 可将受支持的请求代理到 Leader，但最终权限、就绪与拓扑校验仍由 Leader 完成。拖放等浏览器侧提示不能代替服务端校验。

## 认证与安全

控制台沿用服务端的匿名、Basic、multi、proxy、token、只读、TLS 与 mTLS 配置。`/api/web-config` 只返回 UI 能力，不含密钥。不得把凭据写入前端源码或 URL。反向代理必须正确保留 URL 前缀、可信身份头和浏览器来源检查。

## 开发预览

`make web-dev` 提供 Vite 热更新，`make storybook` 提供隔离的组件与业务状态场景。二者默认只监听回环地址，不属于部署产物。前置条件与验证边界见[开发指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Development)。

Vite 代理通过 `ORCH_API_TARGET` 指定，测试 URL prefix 时同时设置 `ORCH_URL_PREFIX`。Storybook 只使用 MSW fixture，不会回退到真实 API。浏览器 fixture 只能证明 UI 行为，不能证明真实 MySQL 拓扑、生产 Raft、代理认证或恢复结果。生产验收应通过真实代理和认证路径完成“发现 → 拓扑 → 详情 → 一次受控操作 → API/MySQL/审计回读”。
