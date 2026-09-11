# 配置参考
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Configuration-Reference) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

本页是 [`internal/config/model.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/model.go) 的运维索引，不替代同版本二进制的严格解析与校验。默认值来自 [`internal/config/config.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/config/config.go)；样例文件只展示常见组合，没有出现的字段仍可能有效。

## 加载与生效规则

- 显式 `--config` 接受一份 JSON 或 YAML，格式按内容识别。
- 未显式指定时按 `/etc/orchestrator.conf` → `conf/orchestrator.conf` → `orchestrator.conf` 查找，每个位置可追加 `.yaml`、`.yml` 或 `.json`；后加载位置覆盖先加载位置。
- 同一位置同时存在多种格式、未知字段、重复键、旧平铺字段、多个 YAML document 或尾随内容都会失败。
- 配置是 lowerCamelCase 分层结构。环境变量展开后的秘密仍进入进程内存与 `dump-config` 输出。
- reload 只更新明确支持的运行时值。listener、Raft 身份/地址/目录、数据库连接池和 tracing exporter 必须重启。

## 顶层域一览

| 域 | 子域/字段 | 主要风险 | 生效 |
| --- | --- | --- | --- |
| `observability` | `tracing.endpoint`, `sampleRatio` | endpoint 可携带环境敏感信息；采样影响成本 | 重启 |
| `logging` | `debug`, `syslog.enabled` | syslog 初始化失败会阻止启动 | 重启验证 |
| `server` | `listen`, `httpAdvertise`, `urlPrefix`, `readOnly`, `tls`, `status`, `web`, `responseIdentity` | 外部暴露、身份、写权限 | listener/TLS/prefix 重启 |
| `raft` | `nodeID`, `bind`, `advertise`, `dataDir`, `defaultPort` | 持久身份与多数派 | 全部重启，禁止随意改 ID/目录 |
| `mysql` | `connectTimeoutSeconds`, `connectionLifetimeSeconds` | 所有 MySQL 连接基础参数 | 连接池重启 |
| `metadata` | `type`, `sqlite`, `schema`, `mysql` | 元数据库一致性与 Schema | 重启；迁移另行执行 |
| `topology` | `mysql`, `replication`, `discovery`, `writeBuffer`, `compatibility`, `snapshot`, `hostname`, `candidate`, `classification`, `pools`, `analysis`, `operations` | 发现、候选与真实 MySQL 写入 | 连接/身份字段重启，其余需验证 reload |
| `authentication` | `method`, `basic`, `proxy`, `power`, `configurationAdmins`, `accessToken` | 身份冒用和越权 | 变更后重启并重新验收 |
| `agents` | listener、poll、seed、`tls` | 额外管理端口与远程副作用 | listener/TLS 重启 |
| `pseudoGTID` | marker、query、chunk、skip | 重排能力与 binlog 扫描成本 | 所有相关主库共同验证 |
| `hooks` | `shellCommand` | 服务身份执行外部命令 | 重启并安全测试 |
| `osc` | `ignoreHostnames` | OSC 候选过滤 | reload 后回读 |
| `audit` | `logFile`, `toSyslog`, `toBackend`, `purgeDays` | 证据缺失或敏感输出 | sink 变更后重启验证 |
| `consul` | endpoint、ACL、TLS、KV | 外部发布部分成功 | 客户端/TLS 变更后重启 |

## 代码默认值

未列出的 bool/int/string 使用 Go 零值，但“零值”不一定是可生产使用的有效配置。

| 路径 | 默认值 | 说明 |
| --- | --- | --- |
| `observability.tracing.sampleRatio` | `0.1` | endpoint 为空时不应假定 exporter 已启用 |
| `server.listen.address` | `:3000` | 生产建议显式绑定受控地址 |
| `server.status.endpoint` | `/api/status` | 状态入口 |
| `raft.bind` / `defaultPort` | `127.0.0.1:10008` / `10008` | 多节点必须改为成员可达地址 |
| `mysql.connectTimeoutSeconds` | `2` | 样例可覆盖，不要混淆样例与代码默认 |
| `metadata.type` | `mysql` | MySQL pool 默认 128、port 3306、read timeout 30、packet `-1` |
| `topology.mysql.defaultPort` | `3306` | discovery timeout 10、read timeout 600、mixed TLS true |
| `topology.discovery.pollSeconds` | `5` | dead factor 1、dead max 300、forget 240h、并发 300、队列 100000 |
| `topology.writeBuffer` | size 100、flush 100ms | `enabled` 默认 false |
| `topology.hostname` | `default` / `@@hostname` / expiry 60m | 默认跳过 binlog server unresolve check |
| `topology.candidate.expireMinutes` | `60` | 候选注册过期 |
| `topology.pools.expiryMinutes` | `60` | 默认 fuzzy hostname true |
| `topology.operations` | bulk wait 10s、并发 5 | 批量操作边界 |
| `authentication.proxy.userHeader` | `X-Forwarded-User` | 只能信任受控代理 |
| `authentication.power.users` | `[*]` | 开启认证后必须按环境收紧 |
| `authentication.accessToken` | use 60s、expiry 1440m | token 生命周期 |
| `agents` | port `:3001`、poll 60m、unseen 6h、stale seed 60m | `serveHTTP` 默认 false |
| `pseudoGTID.binlogEventsChunkSize` | `10000` | pattern 默认空 |
| `hooks.shellCommand` | `bash` | Hook profile 另在页面管理 |
| `audit.purgeDays` | `7` | 仅影响对应审计保留逻辑 |
| `consul` | scheme `http`、timeout 60s、prefix `mysql/master` | provider `consul`，每集群默认 5 KV |

## `server`

- `listen.address` 与 `listen.socket` 选择 TCP 或 Unix socket；部署只配置实际使用的一种入口。
- `httpAdvertise` 是其他节点/客户端可达的 Web/API origin，不等于本地 bind。
- `urlPrefix` 必须与代理路径、Web 路由、metrics/health 抓取和 CLI endpoint 一致。
- `readOnly` 禁止受保护操作，但不能替代 MySQL fencing。
- `tls.enabled`, `mutualTLS`, `privateKeyFile`, `certFile`, `caFile`, `validOUs` 组成服务端信任域；`skipVerify` 不用于生产常态。
- `status.endpoint` 与 `verifyOU` 控制状态入口及证书 OU 检查。
- `web.message`, `web.removeTextFromHostname` 只影响显示。
- `responseIdentity.mode/custom` 控制响应身份显示，不是认证源。

## `metadata`

`type` 只能选择当前实现支持的 MySQL 或 SQLite 路径。SQLite 需要绝对且可写的 `sqlite.dataFile`。MySQL 字段包括 `host`, `port`, `database`, `user`, `password`, `credentialsConfigFile`, 三个 TLS 文件、`sslSkipVerify`, `useMutualTLS`, `readTimeoutSeconds`, `rejectReadOnly`, `maxAllowedPacket`, `maxPoolConnections`。

直接密码与 credentials file 二选一并限制权限。`schema.skipUpdate` 和 `panicOnDifferentDeployment` 改变启动时 Schema 检查边界，但不会执行 `migrate-metadata-id`；旧库迁移必须在停写、备份后显式运行。

## `topology.mysql` 与发现

拓扑连接字段包含账号/凭据文件、TLS key/cert/CA、skip/mutual/mixed TLS、packet、TLS cache factor、默认端口及 discovery/read timeout。连接池创建后这些字段需要重启。

发现字段完整集合：`useShowReplicaHosts`, `pollSeconds`, `deadPollSecondsFactor`, `deadPollMaxSeconds`, `deadMaxConcurrency`, `deadLogsEnabled`, `reasonableCheckSeconds`, `unseenForgetHours`, `maxConcurrency`, `queueCapacity`, `seeds`, `ignoreReplicaHostnames`, `ignoreMasterHostnames`, `ignoreHostnames`, `ignoreReplicationUsernames`, `filterLogsEnabled`。

修改 poll、并发和队列时同时观察 MySQL 连接数、发现延迟、队列深度和元数据库写入。过滤器先用正反例验证；空列表与过宽正则含义完全不同。

## `topology` 其余策略

- `replication.lagQuery` 与 `credentialsQuery` 执行在被管理实例，先验证权限、返回类型和延迟。
- `writeBuffer.size/enabled/flushIntervalMilliseconds` 影响元数据写入批量与延迟。
- `compatibility.skipMaxScaleCheck/lowerReplicaVersionAllowed` 改变兼容判断，不能用于掩盖未经验证的数据库差异。
- `snapshot.intervalHours` 控制定期拓扑 snapshot；`0` 的含义应按同版本实现验证。
- `hostname` 包含两种 resolve method、binlog server unresolve、expiry 与 reject pattern。
- `classification` 包含 cluster alias map、cluster/domain/instance/promotion/data-center/region/environment/semi-sync 查询及三种位置 pattern。
- `pools`, `analysis.reduceCount`, `operations.useSuperReadOnly/bulkWaitTimeoutSeconds/maxConcurrentReplicaOperations` 影响视图和写操作边界。

## 认证、Agent 与秘密字段

`authentication.method` 只接受空、`basic`、`multi`、`proxy`、`token`。Basic 密码、access token、metadata/topology 密码、Consul ACL token、TLS private key 路径均按秘密处理。`power.users/groups` 控制一般操作，`configurationAdmins` 专门控制恢复配置写入；空管理员列表会拒绝该类写入。

Agent listener 和 TLS 是独立暴露面。启用 `agents.serveHTTP` 前确认 `serverPort`、证书/OU、poll/unseen/stale seed 与远端命令权限。

## Pseudo-GTID、审计与 Consul

Pseudo-GTID 完整字段为 `auto`, `pattern`, `patternIsFixedSubstring`, `monotonicHint`, `detectQuery`, `binlogEventsChunkSize`, `skipBinlogContaining`。必须与 binlog 注入和保留共同验收。

审计支持 file、syslog、backend 和 purge days。sink 初始化或写入失败必须可见。Consul 的 address/scheme/token/datacenter/timeout、TLS 六字段与 KV provider/max/prefix/cross-DC 共同构成外部发布契约。

## 变更检查单

1. 用相同版本 `dump-config` 比较变更前后脱敏输出。
2. 标记秘密、节点专属字段和是否需要重启；不要把一份节点配置复制到全部 voter。
3. 严格解析配置，再滚动重启一个 follower。
4. 回读 health、Raft、连接池、发现、认证、metrics/log/audit 与外部集成。
5. 保持自动恢复关闭，直到分类、候选、恢复策略与 Hook 再次确认。
