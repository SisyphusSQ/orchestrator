# Raft 运维

[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Raft-Operations) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

生产环境通常使用 3 或 5 个投票节点。每个节点都要有唯一且稳定的 ID、独立元数据库、持久化 Raft 目录，以及其他成员可达的 advertise 地址。

## 建立新集群

1. 启动所有节点。未初始化节点会监听端口，但还不是集群成员。
2. 让 `orch` 只连接一个种子节点，并且只 bootstrap 这一个节点：

   ```sh
   orch --endpoint http://node-1:3000 raft-bootstrap
   ```

3. 通过 Leader 加入其余投票节点：

   ```sh
   orch --endpoint http://node-1:3000 raft-add-member \
     --body '{"id":"node-2","address":"node-2:10008","suffrage":"voter"}'
   orch --endpoint http://node-1:3000 raft-add-member \
     --body '{"id":"node-3","address":"node-3:10008","suffrage":"voter"}'
   ```

4. 读取每个节点，确认它们看到相同且已提交的配置：

   ```sh
   orch --endpoint http://node-1:3000 raft-configuration --output json
   ```

绝对不要 bootstrap 多个种子节点。HTTP 成功也不能单独证明成员变更完成；必须回读已提交配置中的 ID 和地址。

## 日常变更

使用 `raft-add-member`、`raft-remove-member --id <stable-id>` 和 `raft-transfer-leadership`。成员写操作可在 JSON body 中携带 `expectedIndex` 做 compare-and-set 保护。超时或断连可能产生“结果未知”，此时先回读配置，再决定是否重试。

只有剩余投票节点仍能形成多数派时，才移除故障 voter。替换健康成员时，优先加入并追平新节点，再删除旧节点。重装节点默认使用新的持久身份，除非完整且一致地恢复了其 Raft 状态。

## 身份、存储与节点替换

`RaftNodeID` 与 DNS 和网络地址相互独立。Raft 目录包含 `raft.db`、`node-id` 和 `snapshots/`，其中 `node-id` 是持久状态的一部分。存在日志或快照但缺少匹配身份时，启动会失败，而不是把旧状态静默绑定到新 ID。身份、数据目录、bind 和 advertise 变化都需要重启，不能通过 reload 生效。

每个 Raft 成员使用独立的 MySQL 或 SQLite 元数据库。元数据库副本可以用于准备替代节点，但不会自动建立 Raft 成员关系，也不能随意克隆其他成员的 Raft 目录。替换 `node-3` 时，应使用新的稳定 ID 启动干净节点，通过 Leader 加入，确认配置已提交且状态追平后，再按 ID 移除 `node-3`。

元数据库和 Raft 目录是两个一致性域，需要分别备份。只恢复一侧可能得到陈旧业务状态或无效共识成员。保留完整状态的节点通常可重启并追平；空白重建节点必须显式加入，不能 bootstrap 出第二个集群。

## 网络与部署

`RaftBind` 是本地监听地址，`RaftAdvertise` 是其他成员访问该节点的地址。位于 NAT 后时显式设置 advertise，Raft 端口只对成员开放。自动推导 Leader URL 不正确时，可用 `HTTPAdvertise` 指定外部可达的 Web/API origin。

客户端可以使用 Leader-aware 代理，也可以访问能代理业务请求的健康节点。只路由 Leader 时负载均衡使用 `/health/leader-ready`；允许 Follower 代理时使用 `/health/ready`。不能根据进程存活推断多数派或成员配置已经提交。

## 健康与流量

- `/health/live`：进程正在提供 HTTP。
- `/health/ready`：本地后端与 Raft 状态就绪且新鲜。
- `/health/leader-ready`：本节点是就绪 Leader。
- `/api/leader-check`：兼容性/负载均衡 Leader 检查。
- `/api/raft/configuration`：读取本地成员与领导状态。

Follower 参与拓扑发现，并可转发受支持的请求；只有 Leader 执行恢复和受协调写入。失去多数派时，不得绕过 Raft 直接写元数据库。

三 voter 集群的多数派为二，五 voter 集群的多数派为三。投票节点布局应确保单个预期故障域不能把隔离少数派继续暴露为服务入口。Raft 保护 orchestrator 协调，但应用流量路由和 MySQL fencing 仍是独立控制面。
