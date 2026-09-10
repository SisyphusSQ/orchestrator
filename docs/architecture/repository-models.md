# Repository 与模型分层

## 目标

数据库连接、查询执行、事务和方言适配统一由 `internal/repository` 持有；业务包只表达编排与业务规则。持久化模型、领域模型以及输入输出模型集中在 `internal/models`，避免在 `inst`、`logic`、`agent`、`process` 等功能包中继续新增 DAO 或数据库行结构。

本边界覆盖 orchestrator metadata 数据库以及被管理 MySQL 实例的拓扑访问。Raft 自身的存储实现和 Consul KV 客户端保持独立，它们不是 metadata repository 的组成部分。

## 目录职责

```text
internal/
  models/
    do/       # 稳定数据库行与持久化投影
    domain/   # 跨模块业务对象、值类型与动态结果形状
    dto/      # API/命令输入
    vo/       # API/可观测性输出
  repository/
    database/ # 连接池、驱动、GORM、动态结果扫描
    metadata/ # orchestrator metadata 的查询与写入
    schema/   # metadata schema 探测、初始化、兼容升级
    topology/ # 被管理 MySQL 实例的连接、查询和事务
```

依赖方向为：

```text
http/app -> inst/logic/agent/process -> repository
                     |                    |
                     +-----> models <-----+
```

`repository` 不得反向依赖 `http`、`app`、`inst`、`logic`、`agent`、`process` 或 `recoverypolicy`。

## 模型约定

- `do` 只描述 repository 内部的持久化投影，不承载业务判断。数据库列名必须显式写在 `gorm:"column:..."` 标签中，repository 的导出接口不得暴露 DO。
- `domain` 承载跨模块业务语义和值类型，不依赖 repository、HTTP、config 或 GORM。
- `dto` 只描述外部输入和校验所需字段。
- `vo` 固化外部 JSON 契约。迁移既有接口时必须保持字段名和兼容字段不变。
- repository 完成 `DO <-> domain` 映射；HTTP/app 完成 `DTO <-> domain <-> VO` 映射。Instance、ReplicationAnalysis、Recovery、Agent、Raft 和 Health 的 HTTP 输出统一映射为 concrete VO。
- 调用方必须直接依赖 canonical model，仓库内不使用类型别名保留旧包 API。

## 数据访问约定

- 只有 `internal/repository` 可以导入数据库驱动、GORM 或 `internal/repository/database`。
- metadata SQL 按领域拆入 `repository/metadata`，由 typed repository 方法接收过滤条件、领域模型或标量值；DO 只用于 repository 内部扫描，不得向业务包暴露通用 Query/Exec 逃生口或原始 `*sql.DB`。
- topology repository 的 `Client` 不暴露原始连接。发现与运维算法可以组合 MySQL 命令，但打开连接、执行、扫描和事务必须通过该 Client。
- 事务由完成原子写入的 repository 方法拥有。业务层只在事务成功后更新缓存或发布事件。
- schema 由 `repository/schema` 维护；初始化、探测与关闭通过 repository 根包的生命周期入口完成，命令与 app 不直接操作连接池。

## 新代码检查清单

1. 新增表行或查询投影时，先在 `internal/models/do` 定义 DO。
2. 新增业务对象或 API 契约时，分别放入 `domain`、`dto`、`vo`，不要复用 DO 作为响应。
3. 新增 metadata 或 topology 访问时，在对应 repository 中提供方法。
4. 功能包不得新增 `*_dao.go`，不得导入驱动、GORM 或底层 database 包。
5. 不得增加 `type Old = models.New` 一类兼容别名；迁移时同步修改调用方。
6. 执行 `go test ./internal/repository`，架构测试会检查上述依赖边界、DO 越界、原始查询参数和类型别名。
