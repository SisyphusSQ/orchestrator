# 参考资料

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Reference) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

双语 Wiki 是用户、运维和贡献者指南的维护入口。以下仓库链接指向可执行契约、架构决策、生成资产或 Issue 验证证据，而不是另一套相互竞争的指南。

## 文档地图

- [项目概览](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Overview)：架构、入口、能力边界和项目沿革。
- [快速开始](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Getting-Started)：构建、单节点 bootstrap、发现和生产前置检查。
- [配置](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Configuration)：Raft 身份、元数据库、发现、恢复策略、KV、日志和已移除字段。
- [Raft 运维](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Raft-Operations)：建群、成员变更、节点替换、多数派、健康与备份边界。
- [orch 命令行](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-orch-CLI)、[Web 控制台](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Web-Console)和 [HTTP API](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-HTTP-API)：受支持的管理入口及失败语义。
- [故障恢复](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Failure-Recovery)、[计划切换](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Planned-Switchover)与[恢复配置](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Recovery-Configuration)：分析、候选选择、受控主库迁移、23 项页面策略、9 个 Hook 阶段和验收。
- [可观测性](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Observability)与[安全](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Security)：节点本地遥测、认证、TLS、凭据和权限边界。
- [升级](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)、[开发](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Development)与[包职责指南](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Package-Guide)：不兼容变更台账、回滚、源码布局、package 归属、构建、测试和发布流程。

## 可执行与机器可读契约

- 配置样例：[`conf/`](https://github.com/SisyphusSQ/orchestrator/tree/main/conf)；完整字段与校验：[`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go)。
- 元数据库 Schema：[`docs/schema/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/schema)，包括可执行 MySQL DDL、兼容性与迁移说明。
- HTTP 路由注册：[`internal/http/routes.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/routes.go)；CLI catalog：[`tools/orch-cli/internal/cmd/catalog.json`](https://github.com/SisyphusSQ/orchestrator/blob/main/tools/orch-cli/internal/cmd/catalog.json)。
- 架构决策：[`docs/architecture/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/architecture)；生成的文档截图：[`docs/assets/screenshots/`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/assets/screenshots)。
- Web 源码与浏览器测试：[`web/`](https://github.com/SisyphusSQ/orchestrator/tree/main/web)。
- 指标、大盘与告警：[`resources/metrics/`](https://github.com/SisyphusSQ/orchestrator/tree/main/resources/metrics)。
- 构建和验证入口：[`Makefile`](https://github.com/SisyphusSQ/orchestrator/blob/main/Makefile)、[`script/`](https://github.com/SisyphusSQ/orchestrator/tree/main/script)与 [`tests/`](https://github.com/SisyphusSQ/orchestrator/tree/main/tests)。

## 项目与历史

本维护分支源自 [Percona orchestrator](https://github.com/percona/orchestrator) 和 Shlomi Noach 创建的 [openark/orchestrator](https://github.com/openark/orchestrator)。历史页面、演讲、截图和被替代命令保留在 Git 历史中，但不属于当前产品文档。仓库使用 [Apache License 2.0](https://github.com/SisyphusSQ/orchestrator/blob/main/LICENSE)。

叙述文档与可执行行为冲突时，以当前代码和测试作为实现证据，并在同一改动中修正两个语言页面。问题请提交到 [SisyphusSQ/orchestrator](https://github.com/SisyphusSQ/orchestrator/issues)。
