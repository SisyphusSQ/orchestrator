# 包职责指南

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Package-Guide) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

本页把当前所有包含 Go 代码的目录映射到具体职责，帮助开发者判断新代码应该放在哪里；它不表示所有 internal symbol 都是公开兼容 API。服务端根 module 是 `github.com/openark/orchestrator`，`tools/orch-cli` 是独立 module。

## 依赖方向

```text
cmd/orchestrator
    └── internal/app
          ├── internal/http/* ────────┐
          ├── internal/logic/*        │
          └── internal/agent          │
                                      v
internal/logic/recovery ──> internal/inst/change/regroup
                                      │
                                      v
                           internal/inst/change/relocation
                                      │
                                      v
                           internal/inst/change/replication

http/app/logic/inst/agent/process ──> repository ──> models
                 └───────────────────────────────> models
```

依赖从装配层和用例层指向能力、持久化和稳定模型。`repository` 不得反向导入业务包；所有 `internal/inst/*` 都必须独立于 `app`、`http` 和 `logic`。`internal/inst`、`internal/inst/change` 与 `internal/logic` 根目录只作为命名空间，不放 Go 文件。

## 入口与共享运行时

| package 目录 | 职责与代码落点规则 |
| --- | --- |
| `cmd/orchestrator` | 服务端主程序与本地 `admin` 命令。这里只放参数解析和进程装配，业务行为放到 `internal/`。 |
| `internal/agent` | Agent 轮询与实例拓扑 Agent 运行时。Agent 协议编排放这里，通用拓扑发现不放这里。 |
| `internal/app` | HTTP listener、Raft、可观测性、启动和关闭的进程装配。它负责生命周期连线，不实现领域算法。 |
| `internal/attributes` | 持久化 general attributes 的业务访问层；存储语句保留在 `repository/metadata`。 |
| `internal/config` | 分层配置模型、默认值、校验、CLI 覆盖、Raft 地址规范化、Consul 与可观测性配置。新增用户配置从这里开始，并必须同步文档。 |
| `internal/golib/log` | 项目日志 facade 与格式契约。沿用已有日志能力，不增加领域行为。 |
| `internal/golib/math` | 保留给内部调用方的少量、无依赖数值 helper。 |
| `internal/golib/tests` | Go 测试共享的轻量 spec/helper，不得演变为生产 utility 包。 |
| `internal/golib/util` | 内部 utility 层保留的少量、无依赖文本 helper；有业务语义的行为应进入对应领域包。 |
| `internal/kv` | Consul KV 发布、事务和客户端生命周期。KV 是外部集成，因此独立于 metadata repository。 |
| `internal/observability` | 有边界的 Prometheus 指标与 OpenTelemetry trace 词汇；埋点归这里，HTTP 暴露归 `internal/http/observability`。 |
| `internal/os` | 操作系统进程检查和 Unix 环境检查；可移植的业务逻辑不放这里。 |
| `internal/process` | 运行时组件共享的进程身份、健康状态、主机注册与 access-token store 行为。 |
| `internal/raft` | package 名为 `orcraft`；承载 Hashicorp Raft 运行时、FSM、快照、成员、持久化、状态、peer HTTP client 与 Raft 遥测。应用命令语义位于 `logic/raftstate`。 |
| `internal/recoverypolicy` | 强类型恢复策略值，以及全局/集群覆盖解析。这里只定义策略状态，不执行恢复。 |
| `internal/ssl` | 数据库与服务连接的 TLS 配置 helper。证书策略应显式，构造失败必须向上返回。 |
| `internal/util` | 没有更明确领域归属的跨领域 token 与有限日志缓存。新增代码应优先选择最窄的责任包。 |

## HTTP 边界

| package 目录 | 职责与代码落点规则 |
| --- | --- |
| `internal/http` | 稳定路由装配、兼容别名、Leader 代理位置与 Web action guard。禁止把 capability handler 放回根 package。 |
| `internal/http/agent` | Agent 专用 API 与管理路由。 |
| `internal/http/api/cluster` | 集群查询、别名、搜索、资源池、拓扑投影、标签和集群级 API handler。 |
| `internal/http/api/instance` | 实例详情与直属副本 API handler。 |
| `internal/http/api/maintenance` | maintenance、downtime 请求 handler 及其面向运维的语义。 |
| `internal/http/api/recovery` | 分析、恢复、确认、候选、平滑/强制切换和恢复策略 handler。 |
| `internal/http/api/system` | 系统信息、主机名解析、审计、健康和不属于其他能力的全局控制 handler。 |
| `internal/http/api/topology` | 从 relocation 到 GTID、binlog 的拓扑变更与复制控制 handler；算法仍放在 `inst/change`。 |
| `internal/http/authz` | 根据认证 principal 与配置策略作授权决策；认证 transport 保留在应用 listener 栈。 |
| `internal/http/cli` | 生成式 `orch` HTTP 命令面的注册 adapter；机器可读 catalog 保留在 CLI module。 |
| `internal/http/contract` | 稳定 API response envelope 与共享响应类型，不允许持久化行结构越过这一边界。 |
| `internal/http/observability` | 健康、就绪、指标与 tracing 相关 HTTP 路由；路由契约要求时保持节点本地执行。 |
| `internal/http/presenter` | domain 到 VO 的映射和响应写出；JSON 兼容字段在这里维护，不放进 repository。 |
| `internal/http/raft` | Raft 管理 API 与 Follower 到 Leader 的反向代理；代理写入完成后必须终止本地调用链。 |
| `internal/http/request` | 从路由参数解析并校验实例和集群选择器。 |
| `internal/http/transport` | 项目自有 router、handler、params、responder、principal 契约及 Gin adapter；不得依赖 HTTP capability、logic、inst 或 repository。 |
| `internal/http/web` | 内嵌控制台路由、UI 配置、静态资源与 SPA fallback。 |

权威路由清单位于 [`internal/http/routes.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/routes.go)。应用 handler 依赖项目 transport 契约，不直接依赖 `gin.Context`。

## 实例与拓扑能力

| package 目录 | 职责与代码落点规则 |
| --- | --- |
| `internal/inst/analysis` | 复制故障分析模型、计算与分析历史。检测证据放这里，执行恢复放 `logic/recovery`。 |
| `internal/inst/audit` | 记录与读取运维和拓扑审计事件。 |
| `internal/inst/binlog` | 匹配算法所需的 binlog 事件、cursor、坐标查找与相关性查询。 |
| `internal/inst/candidate` | 故障转移候选建议、过期和候选元数据。 |
| `internal/inst/change/regroup` | GTID、Pseudo-GTID、file-position 和 binlog server 策略下的候选排序与拓扑重组。它可以依赖 relocation，反向依赖禁止。 |
| `internal/inst/change/relocation` | move、repoint、Pseudo-GTID 对齐与高层 relocation 算法。它可以依赖底层 replication，不能依赖 regroup。 |
| `internal/inst/change/replication` | 最底层复制与服务状态变更：启停复制、变更 source、read-only、GTID 控制、凭据、binlog 等命令。 |
| `internal/inst/cluster` | 集群身份、别名、集群级投影、启发式计算和持久化集群元数据。 |
| `internal/inst/discovery` | 单实例 MySQL 拓扑探测、dead-instance filter 与 group replication 发现；连续调度归 `logic/discovery`。 |
| `internal/inst/downtime` | 运维/恢复 downtime 窗口及其面向持久化的业务规则。 |
| `internal/inst/equivalence` | 已知坐标关系形成的等价主库 binlog 位置，用于辅助 relocation。 |
| `internal/inst/gtid` | 纯 MySQL Oracle GTID 解析与集合运算，不依赖 repository 或其他项目 package。 |
| `internal/inst/instance` | 规范 `Instance`、`InstanceKey`、binlog 坐标、复制状态与核心模型行为；transport 和持久化层不能复制这些模型。 |
| `internal/inst/inventory` | 持久化实例清单、查询、缓存、写缓冲和 Pseudo-GTID 状态；不得导入 discovery 或 change package。 |
| `internal/inst/maintenance` | 实例 maintenance 窗口及相关运维归属规则。 |
| `internal/inst/mysql` | 感知版本的 MySQL/MariaDB 查询及返回字段词汇，包括旧复制术语；不依赖其他项目 package。 |
| `internal/inst/pool` | 集群资源池提交、成员关系与基于 pool 的选择数据。 |
| `internal/inst/resolve` | 主机名、地址、默认端口解析与持久化解析状态。 |
| `internal/inst/tag` | 实例标签与按标签选择；标签是运维元数据，写标签属于业务变更。 |
| `internal/inst/topology` | 组合 discovery、inventory 与 tag 的拓扑展示、关系判断和拓扑读取。 |

拓扑变更强制遵循 `regroup -> relocation -> replication`。应选择能够表达用例的最高层，不要从 HTTP handler 直接调用底层 SQL 或复制命令。

## 长时间运行逻辑

| package 目录 | 职责与代码落点规则 |
| --- | --- |
| `internal/logic/discovery` | 发现队列、连续调度与运行时协调；发布观测结果后可以调用 recovery。 |
| `internal/logic/raftstate` | 应用复制命令以及构建/恢复应用快照数据，负责连接业务状态与 Raft FSM。 |
| `internal/logic/recovery` | 故障检测协调、候选计划、恢复执行、hooks、postponed 工作与恢复历史；不得依赖 discovery、HTTP 或 app。 |

## 模型与仓储

| package 目录 | 职责与代码落点规则 |
| --- | --- |
| `internal/models/do` | 带显式列映射的稳定数据库行与持久化投影；DO 不得越过 repository API。 |
| `internal/models/domain` | 独立于持久化、transport、配置和 GORM 的业务对象与值类型。 |
| `internal/models/dto` | HTTP 或命令边界接受的输入契约及其校验字段。 |
| `internal/models/vo` | HTTP 与可观测性使用的具体输出契约；JSON 字段兼容性在这里维护。 |
| `internal/repository` | repository 生命周期与架构测试；负责初始化/关闭存储能力，不暴露原始连接池。 |
| `internal/repository/database` | 进程级连接池、驱动、GORM adapter、TLS、动态结果扫描和数据库运行时原语；仅 repository package 可以导入。 |
| `internal/repository/metadata` | 按业务领域拆分的节点本地 orchestrator 元数据库强类型读写；保证一次元数据变更原子性的事务放这里。 |
| `internal/repository/schema` | 元数据库 Schema 探测、初始化、旧版本 patch、方言规则与显式 metadata-ID 迁移；不使用 `AutoMigrate`。 |
| `internal/repository/topology` | 面向被管理 MySQL 实例的连接、查询、命令与事务；调用方只拿 client 接口，不拿原始数据库 handle。 |

DO/domain/DTO/VO 的详细约束见 [Repository 与模型分层](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/architecture/repository-models.md)。

## 生成资产、客户端与可执行检查

| package 目录 | 职责与代码落点规则 |
| --- | --- |
| `docs/schema` | Go 内嵌的可执行元数据库 DDL 与 Schema 契约检查；同目录 SQL 文件保持权威。 |
| `tests/cli` | CLI、Raft 与恢复配置契约的 Go E2E；这里测试进程/API 边界，不放可复用运行时代码。 |
| `tools/orch-cli` | 独立构建的 `orch` 主 package 与单独 Go module；不得初始化服务端运行时或读取服务端配置。 |
| `tools/orch-cli/internal/client` | `orch` 的 HTTP endpoint 选择、认证、TLS、请求执行、响应校验和“结果未知”处理。 |
| `tools/orch-cli/internal/cmd` | 从 `catalog.json` 生成的 Cobra 命令、共享参数、校验、投影、批处理行为和退出码映射。 |
| `web` | package 名为 `webassets`，把 React 构建产物嵌入服务端；TypeScript 应用、Storybook 与浏览器测试也位于此目录下。 |

## 新代码应该放在哪里？

1. 从用户可见能力出发，不以“哪个现有文件方便”为出发点。
2. 外部输入/输出分别放 `models/dto` 和 `models/vo`；跨能力业务值放 `models/domain`；数据库行只放 `models/do`。
3. 元数据库或被管理 MySQL 的访问增加到对应 repository；业务包不得导入驱动、GORM 或原始连接池。
4. 单实例读取进入对应 `inst/*` 能力；拓扑变更遵循 `regroup -> relocation -> replication`。
5. 长时间 discovery/recovery/Raft 命令编排放 `logic/*`；HTTP package 只负责解析、授权、调用和呈现。
6. 能力接口稳定后，再在 `app` 和 `cmd` 完成装配。
7. 行为变化时同步两个语言页面，执行聚焦 package 测试和 `make test-docs`。

`internal/repository` 下的架构测试会强制关键依赖方向。当前拆包依据和仍需关注的大 package 记录在 [`docs/architecture/package-boundaries.md`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/architecture/package-boundaries.md)。
