# 升级

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Upgrading) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

当前 `main` 可能包含尚未进入最新正式 Release 的改动。服务端二进制、配置、服务定义、`orch` 客户端和内嵌 Web 资源应作为一套经过验证的部署集合；源码成功构建不等于已经发布。

## 前置检查

1. 记录准确的源码/Release revision，并阅读下方所有晚于当前部署 revision 的台账条目。
2. 按各自一致性要求备份每个节点的 Raft 目录和独立元数据库。
3. 盘点所有配置层、生成的服务参数、自动化脚本、监控规则、反向代理以及外部 hooks/KV 消费者。
4. 在隔离环境验证启动、多数派、发现、代表性读写、恢复策略、Web/API 认证、指标和回滚。
5. 明确维护负责人、流量切换、终止条件和变更后的独立回读方式。

## 当前主要不兼容项

- 配置文件与 `dump-config` 已统一为 lowerCamel 分层结构；旧平铺字段没有兼容入口，启动前必须完整迁移配置、脚本和生成模板。
- 服务端仅支持 Raft。删除 `RaftEnabled`，配置持久 ID、数据目录、bind 和 advertise 地址。
- `orchestrator server` 取代历史 `http`/`continuous` 入口；`orchestrator admin` 只用于本地维护。
- 独立 `orch` HTTP 客户端取代直连数据库的 CLI、`-c` 命令和 Shell 客户端。
- Prometheus/OpenTelemetry 取代 Graphite 与旧 raw 指标 API；已移除设置会被拒绝。
- ZooKeeper 发布和 `ZkAddress` 已删除，必须先把消费者迁移到 Consul KV 或外部 hook。
- Web 资源已内嵌，应移除依赖外置前端 resources 目录的部署逻辑。
- 源码迁到 `cmd/` 与 `internal/`，不保留历史公开 Go import 路径。
- HTTP transport、后端 DAO 和日志实现已变化，需要验证认证/代理、代表性数据库路径、日志解析和 syslog 可用性。

## 当前变更台账

### 分层配置

配置按 `server`、`raft`、`metadata`、`topology`、`authentication`、`agents`、`observability` 等职责分组，嵌套键使用 lowerCamel。旧字段不会被自动转换，例如 `RaftNodeID`、`BackendDB`、`MySQLTopologyUser` 和 `ConsulAddress` 分别迁移为 `raft.nodeID`、`metadata.type`、`topology.mysql.user` 和 `consul.address`。先从仓库 `conf/` 样例生成每个环境的新配置，用同版本二进制执行 `dump-config` 与启动验证，再进行滚动替换；回退必须同时恢复旧二进制和匹配的旧配置。

### 统一自增主键的元数据库 Schema

全部 50 张表使用 `id` 自增单列主键，原业务主键转为唯一索引，原自增编号列改名并保留数值。新空库执行 [`docs/schema/mysql.sql`](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/mysql.sql)，校验后写入 `canonical-v2`。旧 `canonical-v1` / `legacy-v1` 不在普通启动时自动迁移。

这是停写升级：先停止所有节点及外部写入者，备份每个节点的独立元数据库和 Raft 数据目录，使用新二进制执行 `orchestrator admin migrate-metadata-id --config=/absolute/path/orchestrator.yaml`。逐表成功后才写完成标记，失败保留 pending 并按真实结构续跑。回读主键、业务唯一性、历史编号和关联读写后再启动集群。详细路线、SQLite/引擎边界及回退见 [Schema 迁移指南](https://github.com/SisyphusSQ/orchestrator/blob/main/docs/schema/migration-guide.md)。

不要向非空库导入 `mysql.sql`，不要混跑依赖旧列名的新旧二进制。旧 Raft 快照通过编号列名映射恢复；新增节点本地代理 id 不进入快照。回退须恢复旧二进制及匹配的元数据库/Raft 备份，不能只删除迁移标记。

### 仅支持 Raft 的服务端

非 Raft、共享元数据库选主和 semi-HA 路径已删除。已有 Raft 成员保留身份、日志、快照和独立元数据库；修改配置和启动命令后重启，不能再次 bootstrap。原非 Raft 部署必须先停止旧系统发现、恢复和写入，准备独立元数据库与身份，建立并验收新 Raft 集群后再切流。不得让新旧恢复系统并行工作。

### 独立 HTTP 客户端

Shell 客户端、直连数据库业务 CLI、`-c`/`cli`、别名及对应环境变量已删除。安装 `orch`、配置 `ORCH_ENDPOINT`，并把启动脚本改为 `orchestrator server`；本地维护统一放在 `orchestrator admin`。回滚时成套恢复匹配的服务端/客户端二进制、配置和自动化。新增命令或写入结果契约不能默认支持混合版本。

### 源码、Web 与 HTTP transport

Go 源码从 `go/` 迁到 `cmd/` 和 `internal/`，旧公开 import 路径不属于兼容 API。`make binary` 内嵌 React Web 资源，运行时不再读取外置前端资源。Gin v1.12.0 被隔离在项目 transport adapter 后；路由同义词、尾斜杠/HEAD、认证、prefix、TLS/mTLS、Raft 代理终止以及 HTTP/HTTPS/Unix listener 都是需要通过真实代理验证的契约。

### 后端 DAO 与连接生命周期

GORM 在 MySQL 和 SQLite 上承担稳定的后端 DAO 读写，并复用唯一进程级连接池；不会运行 `AutoMigrate`，有序 SQL 仍是 Schema 权威。`LastInsertId`、拓扑、快照、Raft 和动态结果路径保留显式 adapter。backend、discovery 和 topology-operation 连接池在关闭时释放，且不会被 `SIGHUP` 重建；endpoint、凭据、TLS、超时、packet、连接寿命或池大小变化后必须重启。

### 可观测性与日志

Prometheus/OpenTelemetry 取代 Graphite 与 raw/aggregated Collection API。删除[可观测性](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Observability)列出的六个退役字段，逐节点抓取并更新大盘和告警。本变更不包含 Schema 或 Raft 格式迁移。

Zap 文本格式为 `time<TAB>[LEVEL]<TAB>[caller]<TAB>message`，日志解析器需要同步更新。`logging.syslog.enabled` 或 `audit.toSyslog` 初始化失败现在会阻止启动，审计 sink 写入失败保持可见。写入从每条一个 goroutine 改为同步执行，需要在代表性负载下验证 sink 延迟。

### Consul 与 ZooKeeper

内建 ZooKeeper 发布与 `ZkAddress` 已删除，升级前必须把消费者迁到 Consul 或外部恢复 hook。Consul 改用官方 SDK，只通过 `X-Consul-Token` 发送 ACL token，默认校验 HTTPS，并在 client/TLS 构造失败时阻止启动。若旧部署依赖跳过校验，替换前必须配置可信 CA、server name 和成对 mTLS 文件。跨机房写入可能部分成功且不会回滚；超时写入不会自动重放。Consul 设置变化后需要重启。

## 部署切换检查单

完成上方各变更台账的专项准备后，按以下顺序切换：

1. 把配置、二进制、服务定义、`orch`、大盘/规则和 hook 版本冻结为一套带 revision 的部署集合，记录 checksum 与准确回退集合。
2. 停止或 fence 所有可能作用于同一拓扑的旧 discovery/recovery writer。确认只有一个预期 Raft 集群、成员身份稳定，并逐节点核对其独立元数据库。
3. 需要停写的元数据库迁移对每个独立 backend 只执行一次；启动应用节点前回读 Schema marker 与表结构不变量。
4. 每次只启动或替换一个预期成员。继续下一节点前，回读当前节点的进程健康、readiness、Raft 身份、advertise 地址、成员关系、元数据库连接和日志。
5. 确认唯一 Leader 与多数派后，验证 discovery 和代表性只读 API/Web 路径。在回读恢复策略、认证、代理、指标与 hook 配置前，继续 fence 业务写入与自动恢复。
6. 只开放一条受控写路径，并回读对应元数据和 Raft 状态。责任归属唯一后再启用 discovery/recovery，禁止新旧恢复系统重叠运行。
7. 切换客户端/代理流量，验证 Leader-aware 路由以及真实认证/TLS，再逐节点检查指标、traces、日志、大盘和告警。
8. 代表性拓扑操作、应用读写以及重启或故障转移演练满足已声明验收标准后，才能结束回退窗口。

身份、多数派、Schema 状态、凭据、路由归属或写入结果存在歧义时应终止推进。客户端超时不能证明服务端已经回滚，重复写操作前必须回读。如果部署变更还包含 MySQL 主库迁移，应拆成独立步骤并遵循[计划切换](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Planned-Switchover)流程。

## 发布与回滚

不得从同一逻辑部署建立多个 Raft 集群，也不得让新旧系统同时对同一拓扑执行恢复。现有 Raft 成员复用原状态，不能再次 bootstrap。不要假设任意混合版本成员都安全，应按跨越版本的具体兼容边界执行。

回滚时同时恢复相互匹配的旧服务端、客户端、配置和服务定义。持久状态如何处理取决于跨越的具体变更，必须遵循详细升级条目，不要临时删除或改写 Raft/数据库状态。
