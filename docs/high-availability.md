# Orchestrator 高可用

Orchestrator 仅支持 Raft。生产集群通常使用 3 或 5 个投票节点；开发与测试可使用单节点 Raft，后者没有节点容灾能力。

每个节点持有稳定、唯一的 RaftNodeID、独立的 RaftDataDir，以及独立的 MySQL 或 SQLite 元数据后端。元数据库不共享、不互相复制；MySQL/SQLite 是存储选择，不是运行模式。

新节点启动后仅监听，不会自动建群。只对一个种子节点执行 `orch raft-bootstrap`，再经 Leader 将其余节点加入。已有节点使用原数据目录重启，不重复 bootstrap。具体步骤见 [Raft 配置](configuration-raft.md) 与 [Raft 部署](deployment-raft.md)。

所有就绪节点独立发现 MySQL 拓扑；只有经多数派确认的 Leader 执行恢复和业务写入。Follower 可以转发请求到 Leader。失去多数派时停止 Leader 业务写入，不回退到共享数据库选主。

客户端使用 [orch](orch.md) 的单个服务地址或多个节点地址。负载均衡可使用 `/api/leader-check`；节点自身的存活与就绪应分别使用 [监控](observability.md) 中的本地健康接口。

![Raft 部署](images/orchestrator-ha--raft.png)

共享后端选主、非 Raft 单机和半高可用部署已移除。旧配置和部署切换边界见 [升级说明](upgrading.md)。
