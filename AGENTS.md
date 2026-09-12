# orchestrator Agent Guide

本文件是仓库内 Agent 的根级入口，记录稳定的项目事实和约束。README.md 负责业务说明。

## 项目事实

初始化后结合代码、配置和已有文档补齐；无法确认的项明确保留为待确认。

- 项目结构与开发入口：Go 服务端入口为 `cmd/orchestrator`，独立 HTTP 客户端为 `tools/orch-cli`，嵌入式 React/TypeScript 控制台在 `web/`；配置样例在 `conf/`，验证与文档入口分别为 `tests/` 和 `docs/README.md`。
- build：要求 Go 1.27.0、Node.js 22.22.2+、pnpm 10.33.2；依赖入口为 `make deps`、`make web-deps`，完整构建使用 `make build`，产物为 `bin/orchestrator` 和 `bin/orch`；仅构建 CLI 可用 `make cli`。
- test：Go 单元测试使用 `make test-unit`；核心集成使用 `make test-integration`；前端使用 `make test-web`；Storybook 使用 `make test-storybook`；文档使用 `make test-docs`；本地组合入口为 `make test`。
- lint / format：当前 Makefile 提供 `make fmt-check` 进行只读 Go 格式检查；未定义统一 lint 目标，漏洞检查入口为 `make cve`，使用固定版本 `govulncheck`。
- 必需的 integration / live 验证：按改动范围选择；`make test-cli-e2e` 需要已有 `mysqld` 的临时回环 MySQL 验收，`make test-system` 需要预置 system 环境，`make test-observability` 需要 `promtool`。构建、自动化测试、发布、部署和真实 MySQL/Raft 验收分别记录，不以单项替代其他项。
- 权限、数据和禁止修改范围：默认仅修改当前仓库与隔离测试资源；未经明确授权不操作生产 MySQL/Raft、外部 Wiki、部署环境或用户数据；运行真实或系统测试前确认环境、资源隔离和清理边界；凭据不得写入仓库。
- 发布入口：版本记录在 `RELEASE_VERSION`；`bump_release_version_and_tag` 会更新版本、创建 tag 并推送，使用前需单独核对版本、提交和目标 remote；GitHub Release/Wiki 发布属于独立外部操作。
- issue provider：linear
- issue prefix：TOO

Issue 系统承载协作记录；代码、配置和可复现验证以当前仓库为准。只有可执行规则才能宣称机械强制。

## 按需工作

- 简单任务直接实施；复杂、跨模块或影响接口、数据、发布的任务参考 .agents/PLANS.md，按需创建计划。
- .agents/plans/ 记录方案与进度；.agents/state/ 只保存中断恢复需要的差量；.agents/runs/ 保存运行记录索引。相同事实优先引用，不在三处重复维护。
- docs/test/ 用于需要复现的集成、live、清理或恢复验证；简单修改不要求另写 runbook。
- 仅使用已安装且匹配任务的技能；缺少技能或模板时按代码、项目约束和用户要求直接工作，不另造全局流程。
- 用户当前请求、已有授权和范围限制优先于技能通用指南。已授权的本地可恢复编辑继续执行，不重复确认；超出授权范围或存在不可逆影响时才询问。
- dry-run / --write 是工具的预览和执行选项，不构成额外人机审批要求。
- 验证与改动影响匹配；已有结果仍适用时复用，出现新改动、失败或未解决风险时才补充。遵守用户对提交/发版收尾不重复测试的要求。

## 本地产物与目录规则

- 默认提交项目文档、按需计划、项目专用技能和脱敏验证摘要。
- .agents/state/、.agents/runs/ 的真实运行文件、日志、数据库、缓存和凭据默认不提交；目录中的 TEMPLATE.md 保持提交。
- 多仓任务由能力所属仓维护 contract 和验收口径，消费仓维护消费规则和验证。
- 大仓可在子目录放更具体的 AGENTS.md，只写稳定的局部实现约束；临时任务内容进入计划。
