# 元数据库兼容矩阵

## 支持定义

这里的“支持”指 orchestrator 能通过 MySQL 协议连接目标后端，能在空库执行 [`mysql.sql`](mysql.sql)，并能按既有 DAO 契约读写元数据。它不表示不同产品具有相同的在线 DDL、事务、锁、复制、优化器或故障恢复语义。

| 后端 | 结构契约 | 自动化/实际证据 | 当前边界 |
| --- | --- | --- | --- |
| MySQL 5.7 | 支持 5.7 与 8.0 的共同语法 | CI 固定在 5.7.44 执行空库初始化与 `information_schema` 回读 | 没有逐个认证所有 5.7 小版本；低于 5.7.44 仍受共同语法约束 |
| MySQL 8.0 | 与 5.7 使用同一份 DDL | CI 固定在 8.0.46；开发阶段已在隔离的 8.0.46 空库实际执行 | 不使用 `utf8mb4_0900_*` 等 8.0 专属能力 |
| TiDB | 使用 MySQL 模式共同 DDL | 静态规则检查；提供同一个外部数据库测试入口 | 未指定精确 TiDB 版本，也没有在本次本地环境连接真实 TiDB 集群 |
| OceanBase | 使用 MySQL 模式共同 DDL | 静态规则检查；提供同一个外部数据库测试入口 | 仅面向 MySQL 模式；未指定精确 OceanBase 版本，也没有在本次本地环境连接真实集群 |
| SQLite | 由同一权威 DDL 转换 | Go 测试执行 47 表初始化、重复执行和布局识别 | SQLite 不保存 MySQL 表/字段注释和字符集属性 |

TiDB 的 `CREATE TABLE` 文档明确接受 `ENGINE=InnoDB`、`DEFAULT CHARSET=utf8mb4`、`COLLATE` 和普通索引语法；TiDB 与 OceanBase MySQL 模式的字符集文档均列出 `utf8mb4_general_ci` 和 `utf8mb4_bin`。因此所有字符列统一使用 `utf8mb4`，业务文本使用 `utf8mb4_general_ci`，协议标识使用 `utf8mb4_bin`，不依赖各引擎不同的默认排序规则。实际部署仍应以目标版本运行外部数据库测试，而不是用文档兼容性替代验收。

官方参考：

- [TiDB CREATE TABLE](https://docs.pingcap.com/tidb/stable/sql-statement-create-table/)
- [TiDB character set and collation](https://docs.pingcap.com/tidb/stable/character-set-and-collation/)
- [TiDB MySQL compatibility](https://docs.pingcap.com/tidb/stable/mysql-compatibility/)
- [OceanBase MySQL compatibility](https://en.oceanbase.com/docs/common-oceanbase-database-10000000003450034)
- [OceanBase collations](https://en.oceanbase.com/docs/common-oceanbase-database-10000000003455275)
- [OceanBase CREATE TABLE](https://en.oceanbase.com/docs/common-oceanbase-database-10000000001106216)

## 共同语法子集

- 类型限于整数、`VARCHAR`、`TEXT`、`TIMESTAMP`、`VARBINARY` 和 `BLOB`。
- 表默认排序规则为 `utf8mb4_general_ci`；协议标识列显式使用 `CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`，任何字符列都不使用 `ascii`、`latin1` 或三字节 `utf8`。
- 状态枚举使用短字符串或整数；`candidate_database_instance.promotion_rule` 使用 `VARCHAR(16)`，不使用数据库 `ENUM`。
- 时间默认值只使用字面量或 `CURRENT_TIMESTAMP`，不使用表达式默认值和毫秒精度。
- 二级索引使用独立的 `CREATE INDEX` / `CREATE UNIQUE INDEX`，不声明索引算法、可见性或在线 DDL 选项。
- 不使用外键、分区、生成列、函数索引、降序索引和 `CHECK` 约束。
- 表名和字段名保持小写 `snake_case`；保留历史标识是为了滚动升级和快照兼容，不代表允许新结构继续复制旧拼写。

## 环境前提

- MySQL 协议账号需要对独立元数据库具有建库、建表、建索引和日常 DML 权限。
- 自动创建的新数据库使用 `utf8mb4_general_ci`。对已存在的数据库，`CREATE DATABASE IF NOT EXISTS` 不会改变原字符集或排序规则。
- 目标环境的 SQL mode、时区和大小写规则仍需在上线前核对；DDL 明确默认值，避免依赖 MySQL 5.7 与 8.0 不同的 `explicit_defaults_for_timestamp` 默认行为。
- `mysql.sql` 只用于空库；TiDB/OceanBase 上的存量结构变化必须走对应平台的变更与回读流程。
