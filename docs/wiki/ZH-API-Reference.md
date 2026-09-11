# HTTP API 路由参考
[English](https://github.com/SisyphusSQ/orchestrator/wiki/EN-API-Reference) · **中文** · [Wiki 首页](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

本页按能力族说明当前 HTTP 表面。权威清单是 [`internal/http/routes.go`](https://github.com/SisyphusSQ/orchestrator/blob/main/internal/http/routes.go) 与 `internal/http/cli` 注册；调用方不能根据旧版 Wiki 猜测路径或方法。

## 基础契约

- 默认 API 前缀为 `/api`，配置 `server.urlPrefix=/orchestrator` 后完整路径为 `/orchestrator/api/...`。
- 声明的 GET 路由同时支持 HEAD 和带/不带尾斜杠形式；未知路径/方法保持项目 404 语义，不自动重定向或返回 405。
- JSON content type 为 `application/json; charset=UTF-8`。许多接口使用 `Code`, `Message`, `Details` envelope；HTTP 200 不自动代表业务成功。
- 历史兼容路由中存在会修改状态的 GET。只有明确 `registerAPIMethod` 的新接口可直接按 POST/DELETE 理解；重试仍按实际副作用分类。
- 允许代理的请求可由 follower 转发到 Leader。节点本地 health、Raft bootstrap/snapshot/configuration 和部分系统状态不会代理。
- 受保护写入经过 `guardWebAction`，仍需服务端业务校验、Raft readiness 与权限判断。

## 系统、健康与 Raft

| 方法与路径 | 作用 | 代理/副作用 |
| --- | --- | --- |
| `GET /health/live` | 进程存活 | 节点本地，无业务 readiness |
| `GET /health/ready` | backend/Raft 就绪与新鲜度 | 节点本地 |
| `GET /health/leader-ready` | 就绪 Leader | 节点本地 |
| `GET /metrics` | Prometheus 指标 | 节点本地，注意网络暴露 |
| `GET /api/health`, `/lb-check`, `/_ping`, `/leader-check` | 兼容性健康/负载均衡检查 | 节点本地 |
| `GET /api/raft/configuration` | 成员、地址、Leader、index | 节点本地读取 |
| `POST /api/raft/bootstrap` | 初始化一个全新节点 | 节点本地、只执行一次 |
| `POST /api/raft/members` | 加入成员 | 可代理；body 包含 id/address/suffrage/可选 expectedIndex |
| `DELETE /api/raft/members/:id` | 删除成员 | 可代理；删除前确认多数派 |
| `POST /api/raft/leadership/transfer` | 转移 Leader | 可代理；断连先回读 |
| `POST /api/raft/snapshot` | 触发 Raft snapshot | 节点本地，不是元数据库备份 |
| `GET /api/reload-configuration` | 重读可重载配置 | 节点本地且有副作用 |

## 发现、实例与集群读取

| 路由族 | 代表路径 | 说明 |
| --- | --- | --- |
| 实例 | `/instance/:host/:port`, `/instance-replicas/...` | 单实例详情与副本 |
| 发现 | `/discover/...`, `/async-discover/...`, `/refresh/...` | 更新元数据库视图；不是纯读取 |
| 忘记 | `/forget/...`, `/forget-cluster/:clusterHint` | 删除已知视图，可能被再次发现 |
| 集群 | `/cluster/:clusterHint`, `/cluster/alias/:alias`, `/cluster-info/...` | cluster hint 可为实现支持的名字/alias/实例 |
| 总览 | `/clusters`, `/clusters-info`, `/masters`, `/all-instances`, `/problems` | 监控和入口 |
| 拓扑文本 | `/topology/...`, `/topology-tabulated/...`, `/topology-tags/...` | 人类/脚本视图；结构化自动化优先其他 JSON |
| 搜索 | `/search/:searchString`, `/bulk-instances`, `/bulk-promotion-rules` | 搜索与批量读取 |

host 与 port 是独立 path 参数时需要按 URL 规则编码。对 alias、reason、owner、tag value 和搜索字符串同样不能直接拼接未转义输入。

## 拓扑变更

| 能力族 | 路径前缀 | 使用条件 |
| --- | --- | --- |
| 智能重排 | `relocate`, `relocate-below`, `relocate-replicas`, `regroup-replicas` | 服务端选择可用机制；仍需预检查 |
| file:pos | `move-up`, `move-below`, `move-equivalent`, `repoint`, `take-master` | 坐标可关联、过滤兼容 |
| GTID | `move-below-gtid`, `move-replicas-gtid`, `regroup-replicas-gtid` | GTID 兼容且无未处理 errant set |
| Pseudo-GTID | `match`, `match-up`, `match-replicas`, `regroup-replicas-pgtid` | marker 与 binlog 完整 |
| 复制控制 | `start-replica`, `stop-replica`, `restart-replica`, `reset-replica`, `delay-replication` | 会改变 MySQL 状态 |
| 实例状态 | `set-read-only`, `set-writeable`, semi-sync enable/disable | 不能替代应用流量 fencing |
| binary log/GTID | flush、purge、errant locate/reset/inject | 可能不可逆，先核对所有副本 |

部分旧 `slave` 路径仍注册同义 route；新调用方使用 `replica` 语义的规范路径。兼容存在不代表应该继续生成旧名称。

## maintenance、downtime、tags 与 pools

- maintenance：`begin-maintenance/:host/:port/:owner/:reason`、按实例或 key 结束、查询列表。
- downtime：`begin-downtime/:host/:port/:owner/:reason[/:duration]` 可带 duration，end 按实例；duration 和文本均需编码与审计。
- tags：读取 tags/tag-value/tagged，写 tag/untag/untag-all。
- pools：提交 pool、读取集群 pool、heuristic pool 与 lag。
- hostname：resolve cache、register/deregister unresolve；修改会影响规范身份。
- KV：`submit-masters-to-kv-stores` 可按全部或单集群发布，必须外部回读。

## 分析、恢复与审计

| 能力 | 代表路径 | 注意事项 |
| --- | --- | --- |
| 分析 | `/replication-analysis[/...]` | 读取当前判断，不等于将执行恢复 |
| 恢复 | `/recover`, `/recover-lite`, force/graceful takeover/failover | 有真实拓扑副作用；候选可显式指定 |
| 全局开关 | enable/disable/check global recoveries | 写操作后回读 check |
| 恢复审计 | `/audit-recovery`, `/audit-recovery-steps/:uid` | 用 UID 关联步骤、错误和 Hook |
| 故障检测 | `/audit-failure-detection`, analysis changelog | 与恢复结果分开保存 |
| 确认 | `ack-recovery/...`, `ack-all-recoveries` | 确认不是修复或回滚 |
| 阻塞 | `/blocked-recoveries[/cluster/...]` | 读取安全拒绝原因 |

## 页面化恢复配置

这些接口使用明确 method 和 JSON body：

- `GET /api/recovery-policy/:scopeType/:scopeKey`
- `POST /api/recovery-policy`
- `GET|POST /api/recovery-hook-profiles`
- `POST /api/recovery-hook-test`
- `GET /api/recovery-hook-assignments/:scopeType/:scopeKey`
- `POST /api/recovery-hook-assignments`

写入携带 revision 做乐观锁，scope 为 global `*` 或显式 cluster alias。冲突时重新读取并人工合并，不覆盖其他操作人的修改。Hook test 会真实执行命令。

## Agent API

Agent 路由只在 Agent listener/能力启用时可用，覆盖 agent 列表与详情、active/recent seeds、seed details、abort、custom command 等。该 listener 有独立 TLS/OU 边界；不要通过主 HTTP 可达推断 Agent API 已安全暴露。

## 调用与重试模板

```sh
curl --fail-with-body \
  --cacert /path/ca.pem \
  -H 'Accept: application/json' \
  'https://orchestrator.example/orchestrator/api/clusters'
```

写调用记录 request ID、目标节点、完整规范路径（不含秘密）、body checksum、开始/结束时间和响应。连接建立前失败可安全重新选择 endpoint；请求可能已发送后断连则结果未知，必须通过资源、MySQL、Raft configuration 或 audit 回读。

## API 验收

每个集成至少验证：一个读取、一个受控写入、认证失败、权限不足、URL prefix、TLS/mTLS、尾斜杠/HEAD、follower 代理、业务 `Code` 错误、timeout 未知结果和写后审计。路由单测或 curl 200 不能代替真实 MySQL 与外部路由状态。
