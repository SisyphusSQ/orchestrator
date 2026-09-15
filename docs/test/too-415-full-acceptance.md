# 验证 Runbook：TOO-415 全功能、多版本 MySQL、升级与恢复

## 本次结果

- 时间 / 版本：2026-09-14 开始；`main@855d45e5a03cf2bd0abc0894b282414301b938a2`
- 结论：完成；Oracle MySQL 5.1–5.7、8.0–8.4、9.0–9.7、26.7、MariaDB 11.8.6 与 Percona Server for MySQL 8.4.11-11 的三副本矩阵已完成，Consul transaction/plain KV、TLS/mTLS、ACL 与跨 DC、Hook 边界与固定快照、audit 文件、HTTP/HTTPS/Unix 与完整认证矩阵、CLI 0–4、真实浏览器断连单次提交/结果未知/回读、三 voter Prometheus/Grafana、告警、OTLP、Linux syslog 外部接收、特殊拓扑、主库/中间主库/双主恢复、稳定性、备份恢复与匹配集合回退均通过；最终代码基线回归、运行态清理与 Linear Done 回读完成
- 对应计划 / Issue：`TOO-415`；`.agents/plans/2026-09-14-too-415-full-acceptance.md`
- 脱敏证据入口：`.agents/runs/too-415/index.md`（本地忽略）
- 清理结果：本地 MariaDB 容器、网络、Podman 任务 VM 与 `too415-podman` tmux session 已删除；远端 Oracle 与 Percona 任务实例均已停止，任务 `mysqld` 进程和监听端口回读为 0；受限证据、下载缓存和 datadir 暂留以便复核，共享服务未停止或修改
- 未执行项 / 风险：真实 `orchestrator-agent` seed 因没有已授权 Agent 主机而 `NOT_RUN`；TiDB/OceanBase 已由用户明确移出本轮范围；本机三个 voter 同宿主，不能证明跨宿主容灾

## 目标与成功标准

- 验证当前构建在隔离三节点 Raft 与远端 MySQL 上完成真实发现、管理、恢复、升级/回退、Web/API/CLI 与外部集成闭环。
- 每个版本分别判断“被管理 topology”和“metadata backend”，并以 `PASS`、`FAIL`、`ERROR`、`NOT_RUN`、`N/A` 记录。
- 每个可运行版本都固定使用 1 primary + 2 replica 的三数据副本拓扑；三个 metadata schema 和三个 Raft voter 也必须相互独立。
- 每个 mutation 保留执行前状态、一次请求、MySQL 独立查询、Raft/审计/Hook 外部结果与清理证据。
- 只有所有必做项通过、发现的代码缺陷修复并在最终代码基线回归后，才可将总目标和 TOO-415 置完成。

## 执行范围

- 本机：三个独立 orchestrator 进程、CLI、浏览器、Consul、OTLP receiver、Prometheus/代理和测试驱动。
- 远端：授权隔离宿主机上的任务专属 MySQL basedir/datadir/socket/port/config/log、测试账号与证书；现有服务全部保留。
- 原始输出：本机 `.agents/runs/too-415/` 与受限临时目录；远端任务根为授权目录。两处都不提交凭据、私钥或连接串。
- 清理：先停本任务 orchestrator、隧道和辅助组件，再正常关闭 MySQL；删除任务专属账号/临时数据前完成独立回读。下载缓存是否保留在结果中明确记录。

## 固定事实与证据边界

- 当前统一契约为 336 条 HTTP 路由、139 个 CLI 命令；Web action 数量从生成物实时计算，不沿用历史手工统计。
- 生产 Web 是内嵌 React 资源；旧 `{{yield}}` 与外置 `/css`、`/images`、`/js` 目录不属于当前通过条件。
- Graphite、旧 raw/aggregated metrics API、ZooKeeper 与旧非 Raft server 路径按退役契约验证明确拒绝，不作为待恢复功能。
- 同宿主三 voter 只能验证进程、端口、持久状态、成员和多数派语义，不能证明跨故障域容灾。
- HTTP 200/ACK、进程启动、构建、单元测试、CI、部署、浏览器和业务数据回读分别记录，不能互相替代。

## Oracle MySQL 版本矩阵

精确二进制版本需同时满足官方发布记录、可下载包和远端 ABI 实际启动。Docker-only 安全更新不能冒充通用 tarball 已部署。

| 系列 | 候选精确版本 | 被管理 topology | metadata backend | 下载/ABI | 备注 |
| --- | --- | --- | --- | --- | --- |
| 5.1 | 5.1.73 | PASS | N/A | PASS | 合同外探索：任务私有 libnsl 下三副本 file:position 与 topology-only 三 voter E2E 通过；未改系统库；metadata 合同最低为 5.7 |
| 5.5 | 5.5.62 | PASS | N/A | PASS | 合同外探索：三副本 file:position 与 topology-only 三 voter E2E 实测通过；metadata 合同最低为 5.7 |
| 5.6 | 5.6.51 | PASS | N/A | PASS | 三副本 GTID 与 topology-only 三 voter E2E 通过；metadata 合同最低为 5.7，探索性实测因 767-byte InnoDB 索引上限报 Error 1071 |
| 5.7 | 5.7.44 | PASS | PASS | PASS | 三副本 GTID、topology-only 三 voter E2E、canonical v2 与 v1→v2 全表迁移通过；a/b/c 独立回读一致 |
| 8.0 | 8.0.46 | PASS | PASS | PASS | 三副本 GTID 拓扑、三个独立 metadata schema、三 voter、恢复策略、Hook 与真实浏览器闭环通过 |
| 8.1 | 8.1.0 | PASS | PASS | PASS | 前向探索：官方 glibc2.17 minimal 包；三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 8.2 | 8.2.0 | PASS | PASS | PASS | 前向探索：官方 glibc2.17 minimal 包；三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 8.3 | 8.3.0 | PASS | PASS | PASS | 前向探索：官方 glibc2.17 minimal 包；三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 8.4 | 8.4.11 | PASS | PASS | PASS | LTS；三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；8.4.12 仅 Docker image CSP 更新，不冒充通用 tarball；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.0 | 9.0.1 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；9.0.0 已撤回，不执行；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.1 | 9.1.0 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.2 | 9.2.0 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.3 | 9.3.0 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.4 | 9.4.0 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.5 | 9.5.0 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.6 | 9.6.0 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；正式 metadata 合同仍以文档声明的 5.7–8.0 为准 |
| 9.7 | 9.7.2 | PASS | PASS | PASS | 前向探索：三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；9.7.3 仅 Docker image CSP 更新 |
| 26.7 | 26.7.0 | PASS | PASS | PASS | 前向探索：首个 YY.M.P 系列；三副本 GTID、topology-only 三 voter、canonical v2、v1→v2 迁移与 a/b/c 独立回读通过；26.7.1 仅 Docker image CSP 更新 |

## 功能域矩阵

| 功能域 | 入口与关键预期 | 当前结果 | 证据 |
| --- | --- | --- | --- |
| 部署/配置/生命周期 | 内嵌 Web + CLI；源码/包/容器分层；bootstrap、JSON/YAML、分层配置、严格校验、reload、shutdown | PASS | build、配置校验、TLS/basic auth、URL prefix、独立 Agent TLS listener、reload、graceful shutdown 与 fail-closed seed 均通过；`LIVE-SERVER-001` |
| 元数据库 | MySQL/SQLite、50 表、DAO 边界、唯一 pool、独立 schema、migration/pending/backup/restore/rollback | PASS | 三个独立 schema 各 50 表；canonical v1/v2、全表 ID migration、26.7 dump/restore 与旧二进制/配置/SQLite/Raft 匹配集合回退通过；`REMOTE-METADATA-8046`、`BACKUP-RESTORE-2670`、`ROLLBACK-MATCHED-001` |
| 发现/日常管理 | sync/async、取消/超时/并发/退出、解析/过滤、poll、maintenance/downtime/tags/pools/alias/candidates/audit | PASS | 8.0.46 三副本/三 voter 已通过同步发现、异步接受与完成审计、三份 metadata 回读、downtime create/read/delete 与非法 duration 拒绝、alias create/update/立即 read 与空 alias 拒绝、forget 三份删除与重复删除拒绝；隔离测试覆盖 HTTP 取消后后台任务仅提交一次、timeout、并发上限与 shutdown 取消，原有 maintenance、tags/pools、候选、解析/过滤与 poll 证据继续通过。修复显式 alias 只写 override、未同步当前 `cluster_alias` 投影的问题；`REMOTE-FULL-8046`、`DISCOVERY-LIFECYCLE-8046`、`ALIAS-PROJECTION-001` |
| 拓扑操作 | GTID/file:pos/Pseudo-GTID、move/regroup/repoint、threads、RO/RW、semi-sync、delay、binlog、errant GTID、特殊模式 | PASS | 已通过 GTID relocation、复制 stop/start、RO/RW、业务数据读回与拓扑恢复；8.0.46 三副本/三 voter 下通过 delay `3→0`、source 与两 replica semi-sync `enable→disable`、errant GTID 定位与向主库注入空事务修复、两次 binlog flush、三副本 marker 回读后安全 purge，以及三节点 Pseudo-GTID 严格定位、显式 `match` 下移和经典 file:position `move-up` 恢复；`REMOTE-FULL-8046`、`TOPOLOGY-SPECIAL-8046`、`ERRANT-GTID-8046`、`PSEUDO-FILEPOS-8046` |
| 恢复/策略 | planned auto/non-auto、主库/中间/双主、候选、23 策略、9 Hook phase、revision/权限/快照/指纹 | PASS | 已通过全局/集群策略、revision 冲突、23 策略、9 Hook phase、Hook 1 秒 timeout/1024-byte 限额/continue/abort/秘密脱敏，以及三副本 non-auto/auto takeover、真实主库故障、中间主库故障和双主故障恢复。三轮故障分别回读业务 marker 418/419/420、跨 voter UID/步骤/指纹与重复确认幂等，最终恢复 a→b/c；执行中固定 Hook 内容、revision 与 fingerprint 由仓库集成测试覆盖。修复 SQLite recovery 注册静默丢行与空错误列表；`GRACEFUL-TAKEOVER-8046`、`FAULT-RECOVERY-8046`、`RECOVERY-SNAPSHOT-001` |
| Raft | bootstrap、member、CAS、transfer、proxy、snapshot、restart、replacement、catch-up、no quorum、unknown | PASS | 三 voter 完成 follower proxy、leadership transfer、snapshot/restart、leader replacement 与 no-quorum 拒写；`REMOTE-RAFT-8046`、`REMOTE-FULL-8046` |
| Web/CLI/API | 336 routes、139 commands、Web actions、deep-link、single-submit、status+Code、listeners/prefix/debug/metrics | PASS | 契约 336 routes/139 commands/59 actions、本地 CLI E2E、8.0.46 真实浏览器、HTTP/HTTPS/Unix、basic/multi/proxy/token/mTLS、readOnly/config admin、custom status/debug/metrics 与 CLI 0–4 已通过；CLI 与 Chromium 均在写入实际提交但响应断开后展示结果未知、只提交一次，并经 SQLite/API/实例抽屉独立回读确认状态；`CONTRACT-DOCS-001`、`LOCAL-CLI-E2E-001`、`BROWSER-8046`、`BROWSER-DISCONNECT-001`、`HTTP-AUTH-TRANSPORT-001`、`CLI-EXIT-CODES-001` |
| 外部集成/观测 | Consul transaction/TLS/ACL/cross-DC、Hook、Agent/seed、Prometheus/alerts/dashboard、OTLP、health/log/syslog/secrets | PASS（真实 seed NOT_RUN） | 已通过官方 Consul 1.22.2 三 server/voter transaction/plain KV；HTTPS 校验、mTLS、ACL 默认拒绝、前缀受限 token 正反例；两个 DC 各三 voter 的 `DistributePairs` 与六 agent 回读；Hook 边界和 audit 文件；三 voter Prometheus、8 条告警、Grafana 44 panels/43 PromQL、OTLP 和健康。Linux 上直接运行项目 `internal/golib/log` 与 `internal/inst/audit` 的静态测试二进制，journald 分别回读 6/6 级别日志和 1/1 audit marker；真实 Agent seed 因无已授权主机精确保留 `NOT_RUN`；`CONSUL-THREE-001`、`CONSUL-SECURE-THREE-001`、`CONSUL-CROSS-DC-001`、`HOOK-AUDIT-BOUNDARIES-001`、`SYSLOG-LINUX-EXTERNAL-001`、`OBSERVABILITY-THREE-LIVE-001` |

## 产品适用性矩阵

| 产品 | 当前结果 | 判定要求 |
| --- | --- | --- |
| Oracle MySQL Community | PASS | 上方所有可部署精确版本均完成三副本；5.7–26.7 适用/探索 metadata 路径通过，5.1/5.5/5.6 的 metadata 边界单列为 N/A |
| MySQL NDB Cluster | N/A（合同边界） | 与 Oracle InnoDB 拓扑分开；当前项目仅保留识别/限制契约，不把 Oracle InnoDB 三副本结果冒充 NDB 集群验收 |
| MariaDB | PASS | 11.8.6 一主两从、GTID 复制、三 metadata schema、三 voter、canonical v2 与 v1→v2 迁移及 a/b/c 50 表回读通过；本地产物已清理 |
| Percona Server for MySQL | PASS | 8.4.11-11 官方 minimal tarball（177,496,446 bytes，本次实测 SHA-256 `91ef42d055cf707fa0bb2e1d4a2240dec02037b6473378bdd1c09a3612f854f1`）完成 ABI/版本回读；一主两从、三 voter topology、canonical v2、v1→v2 migration 与 a/b/c 各 50 表独立回读通过；发布方 checksum sidecar 未取得可用文本，不将本次哈希冒充官方校验 |
| TiDB | NOT_RUN（移出范围） | 用户确认本轮不使用该路径，不以 MySQL/MariaDB 结果替代 |
| OceanBase MySQL 模式 | NOT_RUN（移出范围） | 用户确认本轮不使用该路径，不以 MySQL/MariaDB 结果替代 |
| binlog server | N/A（合同边界） | 当前项目仅保留识别与可适用重排契约，不存在可独立部署的本轮产品闭环 |

## 步骤

| 步骤或脚本入口 | 预期结果 | 实际结果 |
| --- | --- | --- |
| 基线检查 | Git、工具链、端口、跳板、远端资源和禁用边界明确 | PASS：仓库干净；工具链可用；跳板可达；远端 4 CPU/约 3.7 GiB RAM/4 GiB swap/约 92 GiB 数据盘；本机不可直连目标 SSH/3306 |
| 统一契约盘点 | route/command/Web action/error/read-write 无遗漏 | PASS：生成契约校验通过；336 routes、139 commands、59 Web actions、3 errors；115 read-only、221 mutating |
| 本地构建与 race 回归 | Web、server、CLI 构建；全仓与 CLI race 测试 | PASS：`make build`、`make test-unit RACE=1`；文档校验 53 页通过 |
| 下载与 ABI 盘点 | 每系列包、checksum、启动或明确失败证据 | PASS：候选清单均有官方 URL 回应；5.1.73、5.5.62、5.6.51、5.7.44、8.0.46、8.1.0、8.2.0、8.3.0、8.4.11、9.0.1–9.7.2、26.7.0 全部完成 checksum、`ldd` 与三实例启动回读；5.1 仅使用任务私有 libnsl，未安装系统包 |
| MySQL 8.0 基线 | 八个功能域完成真实闭环 | PASS：三副本 GTID、三个独立 metadata schema、三 voter、发现/特殊拓扑、三类故障恢复、恢复策略/Hook、API/CLI/Web 与真实浏览器断连回读闭环通过；外部集成和升级专项也由独立步骤覆盖 |
| 多版本串行矩阵 | 每系列 topology/backend 有独立结论 | PASS：所有可部署精确版本均使用一主两从；每个适用/探索版本各自完成三 voter topology、canonical、migration 和 a/b/c 独立回读 |
| 升级/恢复/回退 | 旧状态、pending、备份、恢复和匹配集合回退可复现 | PASS：pending/迁移覆盖；26.7 dump/restore 三份回读；`8691bb31` 旧二进制、配置、SQLite 与 Raft 匹配集合恢复后 `canonical-v1`/业务状态/Leader 均回读通过 |
| 稳定性 | 低负载持续运行、角色/健康/内存/磁盘可读 | PASS：远端 MySQL 36×5s 与本地三 voter 36×5s 修正监控均无角色/健康异常；RSS 与磁盘范围已记录 |
| 最终代码基线回归 | 修复影响域与必要整体链路通过 | PASS：最终工作区完成验收脚本语法、diff、fmt、336 routes、53 docs、`make test-web`（16 tests）、`make test-unit`（全仓 Go + CLI）、`make build`、固定快照专项与新鲜 Vite 下 Playwright（7 PASS、live 专项按环境变量跳过）；live 浏览器断连已在独立真实夹具 PASS；此前全仓/CLI race 结果继续有效 |
| 清理及回读 | 任务运行资源无泄漏，共享服务未变化 | PASS（审计资产保留）：用户指定的本地容器/网络/VM 已删除，远端 Oracle/Percona 任务实例、三 voter、转发和临时 receiver 均停止，任务进程/监听端口为 0；受限证据、下载缓存、datadir、账号与证书作为可复核审计资产明确保留，不计作运行资源清理完成 |

## 失败与恢复

- 停止条件：宿主资源逼近安全阈值、共享服务受影响、拓扑出现两个可写主库、Raft 身份/多数派不明确、Schema 不一致、审计缺失或 mutation 结果未知且无法回读。
- 恢复 / 重跑：保留版本目录、配置、checksum、pid、状态与脱敏日志；修复原因后只重跑受影响步骤，再在最终代码基线上回归关键闭环。
- MySQL DDL 不是跨表事务；失败时保留 pending 和 datadir，不用删除 marker 或导入空表伪装成功。
- 清理失败时记录残留进程、端口、目录与 owner；不触碰本任务以外的进程或数据。
- 9.3.0 独立回读命令首次两次因本地向 tmux 发送时发生变量转义错误，均在远端 shell 执行前失去有效路径并立即失败，未连接数据库、未改变数据、未产出有效结果文件；改用单引号保护的无插值发送后，a/b/c 三份回读与两条复制通道均 PASS。
- MariaDB 初始夹具暴露 ping readiness、管道退出码、GTID 初始化与统一地址问题；均保留失败证据并在修正夹具后用全新任务实例完成三副本重跑。
- Percona 分段下载首轮因远端旧版 `curl` 不支持 `--retry-all-errors` 而立即失败，未下载有效数据；移除不兼容参数后按 4 MiB 分段重跑，字节数、组装和本次 SHA-256 回读通过。
- Percona 首轮三 voter 流程在 Raft 成员形成后因夹具未创建 `too415_app` 报 Error 1049；补齐 bootstrap 建库并确认复制到 a/b/c 后，从全新本地夹具目录完整重跑并 PASS。
- 三 voter Prometheus/Grafana 首轮因 Grafana 未采用预期的 CLI 配置覆盖而回落到 Homebrew 全局配置，尝试监听已占用的 3000 端口并失败；脚本自动清理全部临时进程和数据，改用任务私有 `grafana.ini` 并禁用插件自动预装后从新目录完整重跑并 PASS。
- HTTP 认证/传输首轮在 Unix 配置校验阶段因 `httpAdvertise` 缺少端口被拒绝，第二轮因 macOS Unix socket 路径超过内核上限返回 `EINVAL`；两轮均由 trap 清理，改用带端口的占位 advertise 与 `/tmp` 下随机短 socket 后全流程重跑并 PASS。
- 回滚脚本首轮暴露 BSD `paste` stdin 参数差异，后两轮分别暴露当前与恢复后实例的 Leader readiness 竞态；三项均修正后从新目录全流程重跑并 PASS。
- Consul 安全夹具首轮误用了 `operator raft list-peers` 不支持的 `-format=json`，第二轮把 ACL 过滤后的空读误判为拒绝必须返回错误，第三轮又因全局 `CONSUL_*` 环境变量让负例继承了管理身份，第四轮的秘密扫描包含了本就应存在于临时运行目录的 token 文件；每轮均停止全部 agent、删除证书/私钥/token/data，从全新目录重跑。最终轮用写入拒绝校验匿名 ACL，把 CLI 凭据环境限制到单条 readiness 命令，并只扫描保留证据，三 voter TLS/mTLS/ACL 全量 PASS。
- 本机 syslog 应用与 audit writer 调用均返回成功，但 unified-log 和 `/var/log/system.log` 未回读 marker，因此该轮不计外部接收 PASS。改在远端 Linux 直接运行项目交叉编译测试：应用日志经 journald 回读 6/6，audit marker 回读 1/1。首次 base64 经 PTY 传输因单行过长截断，改为 76 字符换行后 SHA 匹配；audit 动态 musl 二进制因宿主无 loader 未执行测试，随后改为静态链接并完整重跑 PASS，临时二进制已删除。
- 8.0.46 特殊拓扑首轮在两次 binlog flush 和三副本 marker 回读后尝试 purge 时，orchestrator 仍持有副本轮转前的发现坐标并按安全策略拒绝删除；该轮不计 PASS。加入 b/c 显式重新发现后从全新三 voter 流程重跑，副本应用坐标新鲜时安全 purge 通过，随后原 relocation/Raft 流程也完整通过。
- 8.0.46 故障恢复全链路首轮因 Go 默认 10 分钟测试超时发生在人工 ready 哨兵等待期间而结束，不是产品断言失败；将夹具测试超时显式提高到 15 分钟后，从全新三副本/三 voter 基线完成主库、中间主库、双主故障恢复及后续完整拓扑/Raft 回归。
- 真实 Chromium 断连专项首轮在 mutation 前因 live fixture 的负责人为空触发表单校验，没有产生写入；补齐负责人后完整重跑，实际 POST 已提交再断开、双击仍只产生一次写入，页面展示结果未知并由 GET 和实例抽屉回读确认。
- 默认 Playwright 收尾首轮复用了从 9 月 8 日持续运行的共享 5173 Vite dev server，数值端口用例提交期间发生同路径整页重载而丢失 Modal；该用例单 worker 连续 5 次通过，改用任务临时新鲜 5174 Vite 后并行完整套件 7 PASS、1 个 live 专项按预期跳过，临时 5174 已停止。没有证据指向产品端口处理缺陷，因此未修改产品代码。

## 记录规则

- 原始输出和秘密不进入 Git、Linear、截图或本文件；报告只引用脱敏 evidence ID。
- `N/A` 仅用于原本不适用且有契约或产品依据的能力；失败不能事后改成不适用。
- 每次状态变更都更新“本次结果”和对应矩阵行，避免计划、runbook 与 Linear 漂移。
