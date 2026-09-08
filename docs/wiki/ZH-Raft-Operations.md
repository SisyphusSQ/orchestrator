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

## 健康与流量

- `/health/live`：进程正在提供 HTTP。
- `/health/ready`：本地后端与 Raft 状态就绪且新鲜。
- `/health/leader-ready`：本节点是就绪 Leader。
- `/api/leader-check`：兼容性/负载均衡 Leader 检查。
- `/api/raft/configuration`：读取本地成员与领导状态。

Follower 参与拓扑发现，并可转发受支持的请求；只有 Leader 执行恢复和受协调写入。失去多数派时，不得绕过 Raft 直接写元数据库。
