# Web 控制台

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Web-Console) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

生产 Web 控制台是 React、TypeScript 与 Ant Design 应用，通过 `go:embed` 编入 `bin/orchestrator`。它与服务端共用监听、URL 前缀、认证和 TLS 策略；运行时不需要 `resources/`、`web/`、Node.js 或独立静态服务器。

`make binary` 先把前端构建到 `web/dist/`，同步到 `web/assets/` 下的 embed 源，再构建服务端；`make build` 还会构建 `bin/orch`。直接执行 `go build` 不会准备生产 Web 资源。Docker 使用独立 Node 阶段并把产物复制到 Go 构建阶段，最终运行镜像不包含 Node。内嵌资源缺失时返回明确 503，不会从工作目录读取旧资源。

从服务端同源地址打开 `/web/clusters`。如果 `server.urlPrefix` 是 `/orchestrator`，则访问 `/orchestrator/web/clusters`。

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

## 页面与路由地图

下表路径均位于 `<server.urlPrefix>/web` 的单页应用内。深链刷新时反向代理必须把未知 Web 子路径回退到嵌入入口页，同时不能把 `/api`、`/health` 或 `/metrics` 重写成前端页面。

| 路由 | 页面 | 主要读取与操作 |
| --- | --- | --- |
| `/` | 默认入口 | 重定向到 `/clusters` |
| `/clusters` | 集群总览 | 健康摘要、问题优先排序、搜索、进入拓扑 |
| `/cluster/*` | 集群拓扑 | 图/列表、缩放折叠、实例选择、拓扑变更提案 |
| `/clusters-analysis` | 故障分析 | replication analysis、恢复候选与阻塞原因 |
| `/search/*` | 搜索实例 | hostname、版本、端口查询与跳转 |
| `/discover` | 发现实例 | 显式 discover；会更新 orchestrator 状态 |
| `/cluster-pools/*` | 集群资源池 | pool 映射与启发式候选 |
| `/audit/*` | 操作审计 | 分页、实例过滤、参数与结果 |
| `/audit-failure-detection/*` | 故障检测 | detection 记录，与恢复结果分开 |
| `/audit-recovery/*` | 恢复记录 | analysis、successor、错误、确认状态 |
| `/audit-recovery-steps/*` | 恢复步骤 | 按 recovery UID 展开执行步骤 |
| `/agents`, `/agent/*` | Agent | 状态、磁盘/MySQL 与远程动作；需启用 Agent |
| `/seeds`, `/seed-details/*` | 数据恢复任务 | 活动/历史 seed、详情和终止；需启用 Agent |
| `/recovery-settings` | 恢复配置 | 23 项策略、Hook profile 与 assignment |
| `/status` | 系统状态 | 当前节点、用户、Raft、readiness、Leader |
| `/about`, `/home`, `/faq`, `/keep-calm` | 使用帮助 | 兼容帮助入口 |
| `*` | 404 | 未声明的 Web 路由返回页面内 404 |

## 界面示例

以下图片由仓库当前 Storybook fixture 生成并纳入文档校验。它们用于说明信息布局，不代表生产数据或真实 E2E。

![集群总览](https://raw.githubusercontent.com/SisyphusSQ/orchestrator/main/docs/assets/screenshots/cluster-overview.png)

![拓扑详情](https://raw.githubusercontent.com/SisyphusSQ/orchestrator/main/docs/assets/screenshots/topology-detail.png)

![恢复配置](https://raw.githubusercontent.com/SisyphusSQ/orchestrator/main/docs/assets/screenshots/recovery-configuration.png)

## 拓扑页面操作流程

1. 先看更新时间与连接状态，手工刷新并解决 query error。
2. 在总览核对异常数、主库和实例数，再进入目标 cluster。
3. 在图与列表间交叉确认 source/destination，打开详情核对复制线程、lag、GTID/坐标、只读、semi-sync、tags、maintenance 与恢复。
4. 选择操作后阅读提案与禁用原因。浏览器校验不替代服务端能力、候选与权限检查。
5. 在对话框记录 owner/reason/destination 等完整参数，只确认一次。
6. 完成后刷新拓扑、实例、审计和恢复；超时或断连显示未知，不能自动重放。

## 恢复、只读与并发

故障分析页说明“当前为什么认为有问题”，恢复记录说明“已经尝试了什么”，故障检测说明“何时确认异常”，三者不能互相替代。以 recovery UID 关联步骤、successor、all errors、策略/Hook 指纹和外部结果。acknowledge 只改变确认状态。

页头“可操作/只读”来自 `/api/web-config`。按钮执行中禁用并不能阻止另一个浏览器或 CLI 并发修改；页面配置依赖 revision 防覆盖，拓扑操作依赖前后回读。全局恢复开关 enabled 也不证明某集群会恢复，还需 Leader/多数派、过滤、阻塞窗口、候选、位置规则和有效策略。

启动连接失败、局部 query error、确认失败和结果未知是不同状态。保存 request ID、节点、时间和审计入口，不要刷新清掉证据后直接重试。

## 生产浏览器验收

- 通过真实域名、代理、TLS/mTLS 和认证进入，而不是直连后端。
- 验证根入口与 cluster/audit/recovery 深链刷新、URL prefix 和资源路径。
- 用只读身份确认写按钮不可用；用有权身份执行一个隔离、可回滚操作。
- 模拟断网，确认 UI 不重放写入，并能从 API/审计恢复结果。
- 分别回读 Web、API、MySQL、Raft 和审计；Storybook 与截图不覆盖这些层。
