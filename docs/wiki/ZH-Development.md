# 开发

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Development) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

仓库根 Go module 构建服务端，`tools/orch-cli` 下的嵌套 module 构建客户端。工具链版本以 `go.mod` 和 `web/package.json` 为准，不要使用记忆中的历史版本。

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

前端热更新：

```sh
ORCH_API_TARGET=http://127.0.0.1:3000 make web-dev
```

使用 `make storybook` 查看隔离 UI 状态。Storybook/浏览器 fixture 只能证明界面行为，不能证明真实 MySQL 拓扑、生产 Raft、认证代理或恢复流程。

## 文档工作流

同时修改 `EN-*.md` 与 `ZH-*.md`，保持 `managed-pages.txt` 和导航同步，然后运行 `make test-docs`。源提交合并后，使用 `script/publish-wiki` 发布。Wiki 提交记录源 revision，并保留不在受管清单内的页面。

贡献应同步运行契约与升级文档。交付报告需要区分构建成功、自动化测试、打包、Release 发布、部署和真实环境验收，不能相互替代。
