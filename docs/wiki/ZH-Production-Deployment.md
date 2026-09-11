# 生产部署
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Production-Deployment) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

本页给出从容量规划到上线验收的生产部署基线。orchestrator 服务端只以 Raft 模式运行；生产环境应使用 3 或 5 个 voter。Raft 协调状态、每个节点的元数据库、被管理 MySQL 的真实状态是三个独立一致性域，不能互相替代。

## 1. 先确定部署模型

| 决策 | 推荐 | 为什么 |
| --- | --- | --- |
| voter 数量 | 3 个起步，跨更多故障域时使用 5 个 | 3 节点容忍 1 个 voter 故障，5 节点容忍 2 个；偶数 voter 不增加容错 |
| 节点身份 | 每个节点固定 `raft.nodeID` | ID 属于持久状态，不能随 IP、Pod 名或重启漂移 |
| Raft 存储 | 本地持久盘、每节点独立目录 | `raft.db`、`node-id` 与 `snapshots/` 必须一起保持身份一致 |
| 元数据库 | 每节点独立 MySQL 后端或独立 SQLite 文件 | 多个 Raft 节点不能共享同一个元数据库 |
| HTTP 入口 | 受控负载均衡或反向代理 | 可统一 TLS、认证、URL prefix 与健康探测 |
| Raft 网络 | 仅成员互通 | 不应暴露到公共网络，也不能经无状态七层代理 |

跨可用区部署时，把 voter 分散到可以同时保留多数派的故障域。三节点横跨三个故障域最直观；不要把两个节点放在同一台宿主机后宣称可以容忍两个节点故障。

## 2. 选择交付物

### 从源码构建

```sh
make deps
make web-deps
make build
```

产物为 `bin/orchestrator` 和 `bin/orch`。`make build` 会先构建并嵌入 Web 资源；直接 `go build` 不等价。适合内部可复现构建，但必须固定源码 revision、Go/Node/pnpm 版本并保存构建日志。

### 软件包与 systemd

仓库的 `make package`、`docker/Dockerfile.packaging` 和 `etc/systemd/orchestrator.service` 是软件包路径的可执行入口。安装后逐项核对二进制、配置、状态目录、运行用户和 unit 中的路径；不要假设示例路径与本机包管理器布局完全一致。

### 容器镜像

`docker/Dockerfile` 构建常规运行镜像，`docker/Dockerfile.raft` 与 `docker/Dockerfile.system` 用于对应的验证环境。生产编排必须挂载持久的 `raft.dataDir` 和 SQLite 文件（如使用 SQLite），并为每个副本提供稳定、唯一的节点 ID 与可达 advertise 地址。仅重建容器不应丢失成员身份。

## 3. 节点级目录与权限

建议由专用低权限用户运行，并至少准备：

- 只读且权限受限的配置文件；
- 仅该用户可写的 `raft.dataDir`；
- 独立 SQLite 文件及其父目录，或独立 MySQL 元数据库凭据；
- 可写审计文件目录（启用 `audit.logFile` 时）；
- 服务端 TLS、拓扑 MySQL TLS、元数据库 TLS 和 Consul TLS 所需证书；
- Hook 可执行文件及其最小化环境。

启动前检查路径为绝对路径、挂载确实持久、磁盘剩余空间与 inode 充足，证书和凭据不能被其他本机用户读取。Hook 以 orchestrator 进程身份运行，因此不要通过赋予进程 root 权限来解决 Hook 权限问题。

## 4. 网络与反向代理

允许以下方向：客户端到 HTTP/Web 端口、voter 间双向 Raft 端口、每个节点到其元数据库、每个节点到被管理 MySQL、按需到 Consul/Agent/遥测后端。默认拒绝公网访问 Raft、metrics 和管理 API。

使用 URL 前缀时，代理和服务端都配置同一个 `server.urlPrefix`，例如 `/orchestrator`。代理必须保留认证所需头部和原始 scheme/host；使用 proxy 认证时，只能由可信代理写入 `authentication.proxy.userHeader`，并在边界删除客户端伪造的同名头。

健康检查分层使用：

- `/health/live`：只判断进程能响应；用于进程存活，不用于接收管理流量。
- `/health/ready`：节点后端与 Raft 状态可用；允许 follower 代理时使用。
- `/health/leader-ready`：节点是就绪 Leader；仅把写流量送到 Leader 时使用。

## 5. 初始化顺序

1. 为所有节点生成各自配置，确认 `nodeID`、bind、advertise、HTTP 地址、Raft/元数据路径互不冲突。
2. 启动全部进程，先验证 `/health/live`；未初始化节点尚未是集群成员。
3. 只选择一个节点执行一次 `raft-bootstrap`。
4. 通过当前 Leader 逐个 `raft-add-member`；每加入一个就回读 `raft-configuration`。
5. 确认所有节点看到相同成员 ID、地址和配置 index，并确认 Leader 稳定。
6. 配置负载均衡健康探测，再开放 Web/API 入口。
7. 执行一个受控 `discover`，回读集群、拓扑和实例详情。
8. 自动恢复保持关闭，直到候选规则、机房/区域分类、恢复策略、Hook、fencing、审计和演练都完成。

详细命令见 [Raft 运维](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Raft-Operations)。绝对不要在多个空节点分别 bootstrap 后再尝试拼接集群。

## 6. systemd 运行基线

unit 应具备固定 `User`/`Group`、明确的 `ExecStart` 与配置绝对路径、合理的重启策略、启动超时和文件描述符上限。修改 unit 后执行 daemon reload，再滚动重启单个 voter。每次只动一个节点，并在继续前确认它重新加入、追平且集群仍有多数派。

不要把“systemd 显示 active”当成上线成功。进程存活、Raft readiness、Leader、成员配置、元数据库访问、MySQL 发现和 Web/API 都需要分别回读。

## 7. 上线验收清单

- 所有节点版本、配置来源和构建 revision 可追溯。
- 每个节点的 `nodeID`、Raft 地址、HTTP advertise 与存储路径唯一且持久。
- 每个节点使用独立元数据库；Schema 已按 [`docs/schema`](https://github.com/SisyphusSQ/orchestrator/tree/main/docs/schema) 初始化或迁移。
- 每个 voter 的 `raft-configuration` 一致，三节点有 2 个可用 voter、五节点有 3 个可用 voter。
- 代理分别验证普通请求、Leader 写请求、TLS/mTLS、认证、URL prefix 和超时语义。
- 真实拓扑完成 discover → cluster → topology → instance readback；没有把过滤器误判为连通性问题。
- `/metrics` 被抓取，日志、审计和 tracing 到达预期 sink，告警能够路由到责任人。
- 已完成元数据库与 Raft 状态的独立备份，并在隔离环境演练恢复。
- 自动恢复启用前完成一次计划切换与一次代表性故障演练，分别回读 MySQL、Raft、审计、Hook 和外部 KV/路由状态。

## 8. 变更与回滚

采用逐节点滚动方式升级二进制、配置和证书。一次变更只包含可解释的一类差异，先在一个 follower 验证，再继续其他 follower，最后处理 Leader。配置 reload 不会重建监听器、Raft 身份/地址、数据库连接池或 tracing exporter；这些变更必须重启。版本回滚前先阅读[升级](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Upgrading)并确认元数据库 Schema 与页面化恢复配置的兼容边界。
