# 元数据库主键迁移指南

## 目标与前置条件

本次把全部 50 张表统一为单列自增 `id`。旧自增编号原值保留；业务字段原主键转为唯一约束。存量迁移不转换字符集、业务列类型或重命名无关索引，不删除兼容表。

这是物理结构的不兼容升级，旧二进制仍会查询旧编号列。必须停止使用目标元数据库的 Orchestrator 和外部写入者，备份数据库及对应 Raft 数据目录，安排停机窗口。每个 Raft 节点继续使用各自独立元数据库，不允许新旧进程共享写后端。整个集群协调升级后再恢复服务；不承诺新旧二进制混跑。

MySQL DDL 可能重建表、占用额外磁盘并阻塞写入，不能依赖事务回滚。TiDB/OceanBase 的存量 ALTER 必须在精确目标版本上预演，通过对应平台变更流程执行；语法可接受不等于在线行为相同。

## 空库初始化

空库执行 [mysql.sql](mysql.sql)，首先建迁移记录表及其业务唯一索引，再写 `canonical-v2-pending`。所有 DDL 成功且回读主键/业务唯一性通过后，才写 `canonical-v2` 并删除 pending。重复初始化只容忍同名索引已经存在；其他 DDL 错误直接返回。完成标记存在但表缺失或主键契约不符合时拒绝初始化，不自动用空表掩盖数据丢失。

## 存量升级命令

确认目标配置中的数据库主机、数据库名或 SQLite 文件路径及备份后，在停写窗口执行：

```bash
orchestrator admin migrate-metadata-id --config=/absolute/path/orchestrator.yaml
```

该命令会关闭 `skipUpdate`，使用配置中的元数据库，不启动 HTTP、发现或 Raft 服务。它不是只读检查命令，不能在服务仍写入时执行。普通 `server`、`admin redeploy-internal-db` 和 `--enable-database-update` 都不会自动执行旧库主键迁移。

升级路线：

1. 完整 `canonical-v1` 使用已有结构；`canonical-v1-pending` 或只有旧迁移表的中断初始化先续跑冻结的 [mysql-v1.sql](migrations/mysql-v1.sql)。
2. 无 canonical 标记的 legacy 库先完成已有 base/patch 链，再进入主键迁移。历史补丁仅在这个显式入口执行。
3. 预检全部表的原主键；已有非主键 `id`、未知主键或缺表时停止。已迁移表必须满足新 `id` 和业务唯一约束，不能仅凭列存在判断完成。
4. 写入 `auto-id-v2-pending`；逐表迁移并回读。旧自增列改名为 `id`，MySQL 同时统一为 `BIGINT UNSIGNED`；业务主键表增加 `id` 并保留业务唯一键。
5. MySQL 每张表用一条 ALTER；SQLite 每张表在事务内建立新结构、按列复制数据、替换旧表并重建原索引/触发器。SQLite 使用原表定义，保留原业务列与额外列；不手写第二份目标 schema。
6. 全部表校验成功后事务写入 `canonical-v2` 并清除 pending。旧 `canonical-v1` / `legacy-v1` 标记保留作为历史记录。

迁移按照真实结构识别进度，不依赖每表重复记录。MySQL 多表 DDL 无整体事务：失败可能留下已迁移的前半部分表。保留停写状态，排除明确错误后重新执行同一命令；`auto-id-v2-pending` 会阻止重放旧 DDL，已完成表只校验。不要在部分迁移状态下启动旧二进制或手工写完成标记。

SQLite 表复制需要额外磁盘。若报未知结构、同名临时表或自定义依赖错误，先检查实际对象和备份；不要盲目删表。自定义视图、外键或外部消费者不属于受管 schema 契约，迁移前必须识别并安排同步变更。

## Raft 快照与业务编号

- 原业务主键表新增的 `id` 是节点本地代理键，不写入快照；恢复时也过滤这类本地 `id`，继续以业务唯一键匹配。
- 已有自增编号的快照使用历史列名，例如 `recovery_id`、`detection_id`、`recovery_step_id`；恢复到新结构时映射为 `id`，保留数值。旧快照可由新版本读取。
- `last_detection_id` 等关联列继续保存业务编号，不随其引用的主键列一起改名。
- 快照读取、逐表写入失败会返回错误。恢复不是跨全部表的原子替换；发生错误时不能把部分恢复当作成功。
- API 的恢复/维护/Agent 等业务 ID 字段保持原契约。数据库列名统一不等于外部 API 字段统一。

## 回读与业务验证

```sql
SELECT migration_id, applied_at
FROM orchestrator_schema_migrations
ORDER BY applied_at, migration_id;

SELECT table_name, column_name, column_type, extra
FROM information_schema.columns
WHERE table_schema = DATABASE() AND column_key = 'PRI'
ORDER BY table_name;

SELECT deployed_version, deployed_timestamp
FROM orchestrator_db_deployments
ORDER BY deployed_timestamp DESC;
```

应有 50 张受管表，每张主键仅为 `id`；MySQL 中均为无符号 BIGINT 和 auto_increment。检查原业务唯一键、行数、历史自增值及关联关系，再执行实例发现、维护、审计、恢复记录、策略/Hook 保存及快照恢复验证。不能只根据命令退出成功判断业务验收完成。

## 回退

保留原二进制、配置及匹配的元数据库/Raft 备份。停止新进程后恢复完整备份及旧二进制，再独立回读。不能只换回旧二进制：其旧列名已不存在。不能以 `ROLLBACK` 撤销 MySQL DDL，也不能通过删除完成标记伪装回退。`metadata.schema.skipUpdate` 不会恢复旧字段。
