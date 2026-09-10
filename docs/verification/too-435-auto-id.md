# TOO-435 自增主键开发验证

日期：2026-09-10（Asia/Shanghai）。适用环境：本次 macOS 本地工作区。分支：`suqing/too-435-schema-auto-id`。未执行线上数据库迁移，未提交、推送或合并。

## 实现

- 50 张受管表全部使用名为 `id` 的单列自增主键；MySQL 目标为 BIGINT UNSIGNED。
- 33 个业务主键转为唯一索引，新空库共 108 个二级索引；17 个历史自增主键改名，保留编号值及外部业务关联语义。
- 空库初始化写 canonical-v2；旧库普通启动明确拒绝自动主键迁移。显式入口为 `orchestrator admin migrate-metadata-id`。
- 迁移预检全表，逐表回读，失败保留 auto-id-v2-pending；旧 canonical 未完成初始化使用冻结 DDL 续跑，legacy 在显式入口完成原补丁链。
- 快照排除新增的节点本地代理 id，保留旧自增编号的线格式列名并在写入时映射。相关读取/恢复错误向上传播；拒绝缺失或冲突的身份列。
- 更新 Schema 文档、索引矩阵、双语 Wiki 源文件、未发布变更记录与 MySQL CI 矩阵。

## 验证结果

| 层级 | 入口 / 结果 |
| --- | --- |
| 全仓单元测试 | `make test-unit` 通过，包含主模块和独立 orch-cli 模块 |
| 审查后回归 | `go test -mod=readonly ./internal/db ./internal/logic ./cmd/orchestrator` 通过，覆盖畸形快照拒绝及已有应用版本不能跳过 pending 初始化 |
| 格式与文档 | `make fmt-check test-docs` 通过，校验 31 个受管 Wiki 页面 |
| 构建 | `make build` 完成包含 Web 的服务端及 CLI 构建；审查后以 `make build WEB_PREBUILT=1` 重建最终 Go 产物 |
| SQLite 迁移 | canonical-v1 与 legacy-v1 的全部表带数据迁移、重复执行、历史编号延续、额外索引保留、中断续跑及未知 id 拒绝通过 |
| 快照适配 | 节点本地 id 同值但业务键不同不会误覆盖；旧 recovery/detection/step 编号映射、检测关联保留及畸形输入拒绝通过 |
| MySQL 8.0.46 | 临时目录、仅 Unix socket 的独立实例，空库初始化及全部表带数据迁移测试通过。独立 information_schema 回读两库各 50 个 id 自增主键；迁移库保留 canonical-v1 并新增 canonical-v2，无 pending |
| 管理命令 | 用构建二进制将独立 SQLite 的 canonical-v1-pending 迁移完成；独立 sqlite3 回读 50 表，所有主键为 id，只有 canonical-v1 / canonical-v2 完成标记 |
| MySQL 拓扑 E2E | `ORCH_E2E=1 go test -mod=readonly -count=1 -v -timeout=5m ./tests/cli` 通过发现、维护、标签、池成员、GTID 调整、数据回读、强制故障转移及 Raft 快照创建 |
| 三节点 Raft E2E | 同一入口通过 follower 路由、SQL 回读、领导权转移、快照/重启、领导者替换及无多数派拒写 |
| 恢复策略 E2E | `ORCH_RECOVERY_SETTINGS_E2E=1 go test -mod=readonly -count=1 -v -timeout=2m ./tests/cli -run '^TestRecoverySettingsLifecycle$'` 通过 defaults、全局/集群稀疏覆盖、revision 冲突、Hook 脱敏、继承存储及 Web 路由 |

开发日志保存在本机 `/tmp/orchestrator-too435-*.log`，临时 MySQL 目录为 `/tmp/orchestrator-too435-mysql.QrpReG`，命令验证目录为 `/tmp/orchestrator-too435-cli`。这些路径是本次临时证据，不是部署配置。MySQL 已通过 mysqladmin 正常关闭，socket 和 pid 文件均消失；E2E 自行清理其进程和临时目录。

## 对抗式审查

1. **唯一性丢失**：每个原业务主键都有同序唯一约束；初始化与迁移均回读主键及业务非空/唯一契约，不能只检测 id 列存在。
2. **编号关联断裂**：旧自增值复制/改名保留；DAO 只改主键引用，`last_detection_id`、`blocking_recovery_id`、`agent_seed_state.agent_seed_id` 保留关联含义。
3. **跨节点错覆盖**：业务主键表的本地 id 双向过滤；旧编号映射仍保留，碰撞场景有数据回读测试。
4. **部分完成被误报成功**：普通启动拒绝旧库；显式迁移 pending 独立于空库 pending。逐表失败可续跑，全部验证后才写完成标记；已有应用版本不能跳过未完成的结构初始化。
5. **升级/回退边界遗漏**：旧二进制依赖旧列名，采用停写协调升级和匹配备份回退。SQLite 逐表事务，MySQL 无多表 DDL 原子保证，不能声明无锁或事务回滚。

## 未覆盖层级

- MySQL 5.7.44、TiDB、OceanBase 的 v2 真实执行未运行；CI 已增加 MySQL 5.7.44/8.0.46 的空库与迁移场景，但本次没有远端 CI 运行结果。
- MySQL 实际迁移测试来源为冻结的 canonical-v1；legacy 来源的完整数据保留在 SQLite 验证。
- 未验证外部自定义视图/外键/旧 SQL Raft 消费者的迁移兼容性；这些不是仓库受管结构的一部分。
- Web E2E 本次验证 HTTP 路由与真实恢复配置链路，不包含新的浏览器人工验收。
- 线上迁移、PR/合并及 GitHub Wiki 发布尚未执行；卡片留在 Human Review。
