# 开发

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Development) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

仓库根 Go module 构建服务端，`tools/orch-cli` 下的嵌套 module 构建客户端。当前基线为 Go 1.26.8、Node.js 22.22.2 或更新版本、pnpm 10.33.2；准备构建时仍需重新读取 `go.mod` 和 `web/package.json`。

## 源码布局

| 路径 | 责任 |
| --- | --- |
| `cmd/orchestrator/` | 服务端与本地管理入口 |
| `internal/` | 应用、HTTP、Raft、发现、恢复、配置和存储包 |
| `tools/orch-cli/` | 独立 HTTP 客户端 module |
| `web/` | React/TypeScript/Ant Design 控制台、Storybook 和浏览器测试 |
| `conf/` | 仓库内配置示例 |
| `resources/metrics/` | Grafana 大盘与 Prometheus 规则示例 |
| `docs/wiki/` | 双语 GitHub Wiki 源文件 |
| `docs/schema/` | 可执行元数据库 DDL、兼容性、迁移和生成检查 |
| `docs/verification/` | 不发布到 Wiki 的 Issue 验证证据 |
| `script/`、`tests/` | 兼容构建脚本与测试集合 |

## 常用命令

```sh
make help
make deps
make web-deps
make build
make test-unit
make test-integration
make test-docs
make test-web
make storybook
```

`make binary` 先构建 Web、同步到 embed 源，再生成 `bin/orchestrator`；`make build` 还会生成 `bin/orch`。直接执行 `go build` 不会准备可用于生产的 Web 资源。

## 验证层级

| 入口 | 能证明什么 |
| --- | --- |
| `make fmt-check` | 不改写文件的 Go 格式检查 |
| `make test-unit` | 根模块包和独立 CLI 测试 |
| `make test-integration` | 已配置的核心集成测试 |
| `make test-docs` | Wiki 配对、导航、链接、Schema 索引和文档边界 |
| `make test-web` | 前端类型检查与单元测试 |
| `make test-storybook` | 隔离组件渲染与交互 |
| `pnpm --dir web test:e2e` | 基于测试 fixture 的浏览器行为 |
| `make build` | 内嵌 Web 的服务端和独立客户端构建 |

容器、打包、CVE、system 与 Raft target 可通过 `make help` 查看。它们可能下载依赖、构建镜像或启动容器，执行前应检查 target 和环境。CI 复用相同 Make 入口；在凭据和服务可用时，还会针对隔离的受支持数据库引擎校验生成的元数据库 Schema。

构建成功、单元/集成测试、浏览器 fixture、产物打包、Release 发布、部署就绪和真实 MySQL/Raft 验收是不同证据面。未运行的层级必须明确报告，不能从其他层推断。

前端热更新：

```sh
ORCH_API_TARGET=http://127.0.0.1:3000 make web-dev
```

使用 `make storybook` 查看隔离 UI 状态。Storybook/浏览器 fixture 只能证明界面行为，不能证明真实 MySQL 拓扑、生产 Raft、认证代理或恢复流程。

## 文档工作流

同时修改 `EN-*.md` 与 `ZH-*.md`，保持 `managed-pages.txt` 和导航同步，然后运行 `make test-docs`。源提交合并后，使用 `script/publish-wiki` 发布。Wiki 提交记录源 revision，并保留不在受管清单内的页面。

贡献应同步运行契约与升级文档。交付报告需要区分构建成功、自动化测试、打包、Release 发布、部署和真实环境验收，不能相互替代。

仓库不再在 `docs/wiki/` 之外维护第二套叙述性文档树。可长期维护的机器可读契约与责任方放在一起（`conf/`、`internal/`、`tools/orch-cli`、`resources/` 和 `docs/schema/`）；历史叙述由 Git 历史保留。
