# Web 控制台开发

Web 源码位于 `web/`，使用 React、TypeScript、Vite 和 Ant Design 6。拓扑画布由 React Flow 与 Dagre 提供；实例、权限、故障与操作结果均来自现有 Go API。

## 构建与启动

使用 Node.js 22.22.2 或更新版本、pnpm 10.33.2；Go 版本以根目录 `go.mod` 为准。

```sh
npm install --global pnpm@10.33.2
make web-deps
make build
bin/orchestrator server --config=/absolute/path/orchestrator.conf.json
```

`make binary` 先构建前端，再生成包含 HTML、JS 和 CSS 的 `bin/orchestrator`；`make build` 另外构建独立 HTTP 客户端 `bin/orch`。将 `orchestrator` 复制到任意目录，提供原有配置和后端数据库即可同时运行页面与 API，无需外置 `resources/`、`web/` 或 Node。

前端先写入 `web/dist/`，再同步到 `web/assets/`，由 `web/assets.go` 的 `go:embed` 编入二进制。构建输出不提交到 Git。`web/assets/.keep` 只用于让未构建前端的源码检出可以运行 Go 单元测试；直接 `go build` 不会自动生成前端，正式可用产物应通过 `make binary` 构建。

需要前端热更新时，在另一个终端启动 Vite（部署运行不需要此进程）：

```sh
ORCH_API_TARGET=http://127.0.0.1:3000 make web-dev
```

访问 `http://127.0.0.1:5173/web/clusters`。Vite 只监听回环地址，将 `/api` 和 token 入口代理到 Go 服务，并保留 Host 供同源检查。前端不保存密码，沿用服务端认证。

后端启用 `URLPrefix` 时，在开发命令中同时设置，例如 `ORCH_URL_PREFIX=/orchestrator`，访问 `/orchestrator/web/clusters`。生产构建不绑定固定前缀，Go 注入转义后的 HTML base，从页面路径推导 API 前缀。同源静态资源位于 `/web/assets/`；若绕过构建入口导致内嵌产物缺失，返回明确的 503 并提示重新构建二进制。

Docker 构建入口使用独立 Node 阶段安装锁定依赖并构建前端，再复制到 Go 构建阶段，最终运行镜像无需 Node。`WEB_PREBUILT=1` 要求构建阶段已有 `web/assets/index.html`，适用于 Docker 的独立 Node 构建阶段；它仍然将资源编入二进制。`build.sh -N` 复用已包含页面的服务端二进制与 CLI，不再从磁盘读取前端产物。安装包可携带监控等辅助资源，但 Web 服务不依赖这些文件。

## Storybook

```sh
make storybook               # http://127.0.0.1:6006
make storybook-build         # web/storybook-static/
make test-storybook          # 先构建静态站点；使用已有 Playwright Chromium
```

Storybook 使用与应用一致的中文主题与真实组件，包含 31 个场景：集群总览的正常、异常、空、加载、读取失败与只读；七类实例状态；实例列表；主从、双主、异常、维护和只读拓扑；详情抽屉；维护确认、执行中、成功、HTTP 200 业务失败、结果未知和只读禁止执行。Controls 可调整拓扑显示；操作场景包含可重放的交互断言。

`web/.storybook/vite.config.ts` 与应用开发配置分开，不设置真实 API 代理。MSW 模拟请求仅在 `web/.storybook/public/` 和 `web/src/stories/` 中使用；未声明的 API 返回模拟错误，不会回退到真实后端。示例、文档和 worker 不会进入 `web/dist/` 或服务端二进制。

静态站点可用 `pnpm --dir web preview-storybook` 本地查看，或单独部署 `web/storybook-static/`。浏览器测试遍历所有 story，检查渲染和 play 断言结果、文档入口及 API 隔离。

## 目录与责任

| 目录 | 责任 |
| --- | --- |
| `web/src/api/` | JSON 契约、公共配置、请求与轮询 |
| `web/src/domain/` | 实例状态、操作定义、拓扑规则与布局 |
| `web/src/components/` | 拓扑、实例抽屉、确认与执行结果 |
| `web/src/pages/` | 业务页面 |
| `web/assets.go` | Go 内嵌前端产物 |
| `web/.storybook/`、`web/src/stories/` | 独立组件文档、状态场景与模拟 API |
| `web/storybook-tests/` | Storybook 渲染、交互和请求隔离检查 |
| `web/e2e/` | 浏览器隔离接口用例，无生产 mock 开关 |
| `internal/http/web.go` | 页面路由、token 入口、公共配置、SPA 入口 |
| `internal/http/web_actions.go` | Web 写操作 POST 别名与入口检查 |

## 原 Web 能力映射

| 能力 | 新入口 |
| --- | --- |
| 集群列表、全局问题和恢复开关 | 集群总览、页头自动恢复开关 |
| 集群名、别名、实例定位 | 保留 `cluster/:clusterName`、`cluster/alias/:clusterAlias`、`cluster/instance/:host/:port` |
| 拓扑、折叠、机房与匿名显示 | 拓扑/列表切换、缩放平移、小地图、折叠、紧凑、机房颜色、匿名与别名选项 |
| 拖放和双主 | 智能/经典/GTID/Pseudo-GTID 模式，提出操作并确认提交 |
| 批量下游调整 | 实例详情 → 拓扑调整；目标与可选正则筛选 |
| 探测、复制、只读、维护、停机、GTID | 实例详情的常用按钮与分组菜单 |
| 实例诊断 | 位点、延迟、半同步、错误、GTID、标签、等价位点、维护与近期恢复 |
| 故障恢复、指定接任、平滑切换、强制故障转移 | 实例详情 → 恢复与切换；故障分析页 |
| 当前/近期/阻塞恢复、结构问题 | 集群拓扑与故障分析页 |
| 搜索、发现、OSC 候选与资源池 | 搜索/发现页面；集群操作菜单 |
| 审计、检测、恢复及确认 | 保留分页、实例、集群、别名、ID、UID 深链，详情包含步骤、参与实例、错误和检测历史 |
| Agent、卷、快照、Seed 恢复与中止 | Agent 与数据恢复页面，依据 `ServeAgentsHttp` 显示 |
| 状态、帮助、token | 系统状态、使用帮助；保留 `/web/access-token` |

旧 `resources/public/js/` 和模板作为历史源码保留，不再由服务端挂载，新 Web 页面不加载 jQuery、Bootstrap、D3，也不使用旧整页刷新。

## 请求与状态

- 读取每 15 秒轮询，完成后再安排下一次；切换资源取消旧读取，旧响应不能覆盖新资源。后台刷新保留筛选、抽屉与拓扑布局。
- HTTP 200 不代表成功，必须读取 API `Code`。保留失败详情和部分结果。连接中断、120 秒未完成或确认不明确时显示“操作结果未知”，仅提供回读，不自动重试或跳转重放。普通读取超时 30 秒。
- 写操作先确认，执行中防止重复提交。路径参数逐段编码，原因、别名、正则和资源池列表单独编码。
- `/api/web-config` 只返回 UI 能力，不暴露凭据。Readonly、Basic/multi、proxy、token 沿用现有服务端判定；配置读取失败停用写入口。
- 操作审计的写库由 `AuditToBackendDB` 控制，未启用时页面显示说明。`WebMessage` 按纯文本显示，不执行 HTML。
- POST 在入口检查用户权限和浏览器来源。Raft Follower 继续代理到 Leader，业务 handler 在 Leader 检查就绪状态；旧 GET API 保持兼容。
- 读取错误保留上次数据并提示异常；未知健康状态不能显示为正常。拖放检查辅助用户选择，服务端负责最终校验。

## 验证

```sh
make test-web
pnpm --dir web exec playwright install chromium --only-shell
pnpm --dir web test:e2e
go test ./internal/http ./internal/app
make build
make test-docs
```

浏览器用例覆盖总览到拓扑、抽屉刷新、只读、HTTP 200 业务失败、不确定结果不重放、URLPrefix 深链刷新和窄屏。这些用例的接口 fixture 只存在于测试浏览器。CI 会把二进制复制到临时空目录，再从那里启动页面与 API，验证运行时不依赖源码目录。

真实验收使用隔离 MySQL 拓扑，检查发现 → 拓扑 → 详情 → 维护或复制调整 → API/MySQL 回读 → 审计。生产 Raft、真实 Agent/Seed 与用户视觉验收属于独立验证项，构建或模拟测试通过不代表这些验证已经完成。
