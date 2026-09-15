# TOO-415 全功能、多版本 MySQL、升级与恢复验收计划

- issue_id: TOO-415
- 状态：完成
- 当前基线：`855d45e5a03cf2bd0abc0894b282414301b938a2`
- 分支：`suqing/too-415-full-acceptance`
- 结果入口：`docs/test/too-415-full-acceptance.md`
- 原始证据索引：`.agents/runs/too-415/index.md`（本地忽略，不提交）

## 目标

在隔离环境中验证当前 orchestrator 的部署、配置、元数据库、MySQL 拓扑管理、恢复策略、三节点 Raft、API/CLI/Web、外部集成和可观测性；覆盖可部署的 Oracle MySQL 大版本与当前真实行为分界。发现产品缺陷后在本分支修复并回归，最终形成可复核的脱敏证据。

## 范围与关键决策

- 本机运行三个独立 orchestrator 节点；每个节点使用唯一 ID、HTTP/Raft 端口、Raft 目录和元数据库。
- 第一轮使用三个独立 SQLite 文件；第二轮使用远端三个独立 MySQL metadata schema，禁止共享后端。
- 被管理 MySQL 全部位于授权的远端隔离宿主机，使用任务专属 basedir/datadir/socket/port/config/log，按版本串行轮换。
- 所有可运行数据库版本均使用三数据副本拓扑验收：一个 primary、两个 replica；不得用单实例启动或双副本结果替代该版本结论。
- 本机无法直连远端 SSH/3306，必须经已有跳板通道建立可审计的端口映射；发现返回的 host/port 必须与映射一致。
- 版本以官方可下载二进制为准，每系列固定精确 patch 和 checksum；版本发布存在但无可部署 tarball 时，不写成已部署。
- Oracle MySQL、NDB、MariaDB、Percona、TiDB、OceanBase 与 binlog server 分开记录，产品适用性不能互相替代。
- mutation 只发一次；断连或超时标记结果未知，并先做 MySQL、API、Raft、审计和外部系统回读。
- 原始日志、凭据与数据不提交；提交的 runbook 只保存脱敏摘要和证据索引。

## 阶段

### 1. 契约与环境基线

- [x] 读取仓库、文档、Schema、测试和记忆库约束。
- [x] 核验 Git SHA、工作区、Go/Node/pnpm、本机监听端口、tmux 跳板和远端资源。
- [x] 读取 TOO-415、项目文档、评论、关系和合法状态。
- [x] 从统一契约生成路由、命令、Web action、错误分类和读写属性矩阵。
- [x] 固定下载清单、包格式、checksum、glibc/CPU/磁盘与串行资源边界；Docker-only 安全更新与通用 tarball 分开记录。

### 2. 验收基础设施

- [x] 建立秘密目录、角色分离账号、TLS CA/证书及重跑/清理入口。
- [x] 建立远端 MySQL 下载缓存与串行生命周期脚本；datadir/package 只保留不覆盖，停止与状态入口限定为任务实例。
- [x] 建立本机到远端的确定性地址/端口夹具，证明每个实例的 report_host/report_port 与发现回读一致。
- [x] 建立三节点 orchestrator 配置生成、启动、bootstrap、加入、停止、恢复与清理入口；支持 SQLite 与三个独立 MySQL metadata schema。
- [x] 建立真实浏览器、OTLP receiver、Consul、Hook、Prometheus 抓取和 TLS/认证夹具；真实 Agent seed 因无授权 Agent 主机精确标记 `NOT_RUN`。

### 3. MySQL 8.0 基线

- [x] 受管 topology：sync/async 发现生命周期、分类与日常管理、GTID 重排、复制控制、8.0.46 三副本 delay `3→0`、semi-sync source/replicas 正反切换、errant GTID 定位与空事务修复、binlog flush 与三副本回读后安全 purge、Pseudo-GTID `match`、经典 file:position `move-up` 及代表性故障恢复全部闭环。
- [x] metadata backend：空库初始化、50 表、DAO 语义、唯一连接池、独立节点状态、备份恢复、v1/v2 迁移与匹配集合回退。
- [x] 三节点 Raft：成员、CAS、transfer、proxy、snapshot、restart、replacement、catch-up、无多数派拒写和结果未知；8.0.46 远端 metadata 基线已完成。
- [x] API/CLI/Web：336 路由、139 命令、59 个 Web action、真实浏览器、HTTP/HTTPS/Unix、basic/multi/proxy/token/mTLS、readOnly/config admin、custom status/debug/metrics 与 CLI 0–4 已通过；CLI 与 Chromium 断连均在实际提交后呈现结果未知、只写一次，并完成独立回读。
- [x] 外部集成与观测：Consul 三 voter transaction/plain KV、TLS/mTLS、ACL 默认拒绝与前缀受限 token、两个 DC 各三 voter 的分发及六 agent 回读；Hook 脱敏、timeout、输出上限、continue/abort 与 audit 文件；三 voter Prometheus/Grafana、告警、OTLP 与健康通过；Linux journald 对项目应用日志和 audit marker 的外部接收分别回读 6/6 与 1/1。真实 Agent seed 因缺少授权主机精确保留 `NOT_RUN`。

### 4. 多版本与产品适用性

- [x] Oracle MySQL 5.6、5.7、8.0、8.1、8.2、8.3、8.4、9.0–9.7、26.7：逐系列以三副本拓扑执行可用能力与 metadata backend。
- [x] 盘点 MySQL 5.1/5.5 及更早归档；5.1.73 与 5.5.62 在任务私有依赖边界内完成三副本 file:position 和 topology-only 三 voter 实测。
- [x] MariaDB 11.8.6 三副本及 metadata/Raft 验收；本地容器、网络和任务 VM 已清理。
- [x] Percona 8.4.11-11 官方 minimal 包、三副本及 metadata/Raft 验收；一主两从、三 voter、canonical、migration 和三份独立回读通过，远端任务实例已停止。
- [x] NDB、TiDB、OceanBase、binlog server 单列未覆盖原因；TiDB/OceanBase 由用户明确移出本轮范围。

### 5. 升级、恢复与稳定性

- [x] 用仓库历史 schema/config/Raft 路径构造隔离旧状态，执行升级、pending 中断续跑、备份实际恢复和匹配集合回退。
- [x] 三副本 non-auto/auto graceful takeover、主库故障、中间主库故障、双主恢复、marker 回读与原拓扑恢复全部通过；修复 SQLite recovery 注册静默丢行和空错误列表，并用仓库集成测试固定执行中策略/Hook 内容、revision 与 fingerprint。
- [x] 低负载稳定运行，观察 topology 角色、Raft 健康、内存与磁盘；错误的 PID 采样不计证据，修正后完整重跑。
- [x] 对修复影响域重跑矩阵，并在最终代码基线上核验关键整体链路。

### 6. 交付记录

- [x] 将 TOO-415 改为总验收卡并建立 8 个执行子卡；发现的代码缺陷另建关联卡。
- [x] 每次 Linear 写入后回读；八个执行子卡和 TOO-415 总卡均已写入脱敏 PASS 摘要并回读为 `Done/completed`。
- [x] 对抗式审查 readiness、管道退出码、GTID 初始化、统一地址、RSS/PID 采样和 Leader 竞态并完成必要修正。
- [x] 清理本任务创建的运行进程、隧道、临时 receiver、本地容器/网络/VM；远端 Oracle/Percona 任务进程和端口回读为 0。受限证据、下载缓存、datadir、账号与证书作为可复核审计资产明确保留，不计作运行资源泄漏。

## 停止条件与恢复

- 发现共享/生产数据库、远端现有服务受影响、资源逼近 OOM/磁盘阈值、两个可写主库、Raft 多数派或身份不明、Schema 状态不明、写结果未知且无独立回读时，停止后续 mutation。
- 仅终止本任务进程和映射；不重启宿主机、不改共享防火墙、不处理非本任务服务。
- MySQL DDL 失败保留对应 datadir 与 pending 状态，先备份和取证后按真实结构续跑；不删标记伪造回退。

## 完成标准

必做矩阵在最终代码基线上有真实 PASS，失败和阻塞均已解决或被用户明确移出范围；runbook、Linear、原始证据索引一致；测试资源完成清理；未把本机同宿主 Raft、HTTP 200、单元测试、构建或模拟浏览器结果夸大为其他证据层。
