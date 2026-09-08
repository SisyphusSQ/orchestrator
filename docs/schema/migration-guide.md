# 元数据库迁移指南

## 空库初始化

当运行时确认目标库不存在任何受管表时，直接执行 [`mysql.sql`](mysql.sql)。第一张表是 `orchestrator_schema_migrations`，随后写入 `canonical-v1-pending`；全部表和索引完成后才写入 `canonical-v1` 并清除 pending。若进程在中途退出，下次启动会识别 pending 标记并继续执行幂等建表语句。续跑只容忍同名二级索引已经存在；其他 canonical DDL 错误会终止初始化，不会写入完成标记。

若已经存在 `canonical-v1` 完成标记但受管表数量不足，运行时会拒绝启动迁移，不会自动重建空表掩盖数据丢失；应先从备份恢复并核对原因。

MySQL 后端数据库由应用创建时会显式使用 `utf8mb4_general_ci`。SQLite 则把同一组语句转换为 SQLite 方言，不维护另一份手写结构。

## 存量数据库升级

只要库内已经存在受管表且没有 canonical pending/完成标记，运行时就继续执行 `generateSQLBase` 与 `generateSQLPatches`，并在完成后写入 `legacy-v1`。当前 base/patch DDL 新建的表和字段同样使用 `utf8mb4`，但这条路线不会在线转换已经存在的字符列；它仍保留原有字段顺序、索引名和补丁容错行为。

本次升级不会对存量大表自动执行以下高风险操作：

- 全表 `CONVERT TO CHARACTER SET utf8mb4`；
- 重命名旧索引；
- 把 `promotion_rule` 从 `ENUM` 改为 `VARCHAR`；
- 删除当前源码未引用的兼容表；
- 修正 `processcing_node_token` 的历史拼写。

原因是这些操作可能重建表、占用额外磁盘、阻塞写入，并且 MySQL、TiDB 与 OceanBase 的在线 DDL 和回退语义不同。若需要让存量库的物理结构与 canonical DDL 完全一致，应另建变更卡，先采集数据量、索引使用、脏数据、平台能力和回滚窗口，再逐表迁移。

canonical 空库与当前 base/patch DDL 都以 `utf8mb4` 为字符集；存量库中此前已存在的 `ascii`、`latin1` 或三字节 `utf8` 列不会被本次启动流程自动重建。canonical 与完整历史补丁链保持相同的表、字段、主键、`NULL` 约束及二级索引列契约；其余有意差异包括中文注释、规范化索引名，`promotion_rule` 从 `ENUM` 改为 `VARCHAR(16)`，拓扑历史的 `cluster_name` 从不可完整索引的 `TINYTEXT` 改为 `VARCHAR(128)`，以及三个原本没有显式默认值的非空时间字段使用安全的 `1971-01-01 00:00:00`。这些差异只作用于新建 canonical 库，不会在线改写存量库。

## 上线前检查

1. 确认每个 Raft 节点仍使用自己的独立元数据库，不让不同节点或新旧部署共享一个写后端。
2. 备份元数据库，并记录当前应用版本、表数、字符集、排序规则与 `orchestrator_db_deployments`。
3. 在相同产品和精确版本的隔离空库运行外部 Schema 测试。
4. 对存量库先升级一个非生产副本，确认写入 `legacy-v1`，再验证发现、维护、标签、审计和故障恢复记录。
5. 观察启动日志中选择的是 `canonical` 还是 `legacy`，并核对应用版本部署记录。

## 回读

```sql
SELECT migration_id, applied_at
FROM orchestrator_schema_migrations
ORDER BY applied_at, migration_id;

SELECT deployed_version, deployed_timestamp
FROM orchestrator_db_deployments
ORDER BY deployed_timestamp DESC;
```

禁止仅根据进程启动成功判断迁移完成；还应回读受管表数、关键字段、关键索引，并执行代表性的业务读写。

## 回退

- 存量 `legacy-v1` 数据库没有被批量改写，可在停止新进程后恢复旧二进制和匹配配置。
- 新建的 `canonical-v1` 数据库包含旧代码需要的表和字段，但旧二进制不理解 canonical 标记，启动时可能再次执行历史补丁并补出旧索引名。回退前应恢复数据库备份，或在确认结构完整后使用 `SkipOrchestratorDatabaseUpdate` 阻止旧二进制改写结构。
- MySQL DDL 不能依赖事务回滚。任何真实结构回退都必须基于备份、反向 DDL 和独立回读，不得假设 `ROLLBACK` 能撤销建表或改表。
