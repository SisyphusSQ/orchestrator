# HTTP 接口契约

此文件由 `make api-contract` 生成；源定义为 `internal/http/contract/spec.json`。

路径不含部署前缀。`readOnly` 表示操作语义，不能根据 GET 推断。响应列中的 `legacy` 表示历史接口返回格式不统一；空 schema 保留动态 Details，不宣称字段已验证。错误使用 Envelope，ErrorClass 为可选分类。

| 方法 | 路径 | 只读 | 响应 |
| --- | --- | --- | --- |
| GET | `/api/cli/get-candidate-replica/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/cli/rematch/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/cli/master-pos-wait/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/cli/last-executed-relay-entry/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/cli/correlate-relaylog-pos/:host/:port/:belowHost/:belowPort` | 是 | legacy: `unknown` |
| GET | `/api/cli/find-binlog-entry/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/cli/correlate-binlog-pos/:host/:port/:belowHost/:belowPort` | 是 | legacy: `unknown` |
| GET | `/api/cli/find` | 是 | legacy: `unknown` |
| GET | `/api/cli/which-heuristic-domain-instance/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/cli/which-cluster-gh-ost-replicas/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/cli/which-lost-in-recovery` | 是 | legacy: `unknown` |
| GET | `/api/cli/instance-status/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/cli/get-cluster-heuristic-lag/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/cli/cluster-pool-instances` | 是 | legacy: `unknown` |
| GET | `/api/cli/set-heuristic-domain-instance/:clusterHint` | 否 | legacy: `unknown` |
| GET | `/api/cli/active-nodes` | 是 | legacy: `unknown` |
| GET | `/api/cli/show-resolve-hosts` | 是 | legacy: `unknown` |
| GET | `/api/cli/show-unresolve-hosts` | 是 | legacy: `unknown` |
| GET | `/api/relocate/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| POST | `/api/relocate/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/relocate-below/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/relocate-slaves/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/relocate-replicas/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| POST | `/api/relocate-replicas/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/regroup-slaves/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/regroup-replicas/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/regroup-replicas/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/move-up/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/move-up/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/move-up-slaves/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/move-up-replicas/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/move-up-replicas/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/move-below/:host/:port/:siblingHost/:siblingPort` | 否 | legacy: `unknown` |
| POST | `/api/move-below/:host/:port/:siblingHost/:siblingPort` | 否 | envelope: `unknown` |
| GET | `/api/move-equivalent/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| POST | `/api/move-equivalent/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/repoint/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/repoint/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/repoint/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| POST | `/api/repoint/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/repoint-slaves/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/repoint-replicas/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/repoint-replicas/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/make-co-master/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/make-co-master/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/enslave-siblings/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/take-siblings/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/take-siblings/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/enslave-master/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/take-master/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/take-master/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/master-equivalent/:host/:port/:logFile/:logPos` | 是 | raw: `(InstanceKey)[] | null` |
| GET | `/api/regroup-slaves-bls/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/regroup-replicas-bls/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/move-below-gtid/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| POST | `/api/move-below-gtid/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/move-slaves-gtid/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/move-replicas-gtid/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| POST | `/api/move-replicas-gtid/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/regroup-slaves-gtid/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/regroup-replicas-gtid/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/match/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| GET | `/api/match-below/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| POST | `/api/match-below/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/match-up/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/match-slaves/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/match-replicas/:host/:port/:belowHost/:belowPort` | 否 | legacy: `unknown` |
| POST | `/api/match-replicas/:host/:port/:belowHost/:belowPort` | 否 | envelope: `unknown` |
| GET | `/api/match-up-slaves/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/match-up-replicas/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/regroup-slaves-pgtid/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/regroup-replicas-pgtid/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/make-master/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/make-master/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/make-local-master/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/make-local-master/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/enable-gtid/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/enable-gtid/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/disable-gtid/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/disable-gtid/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/locate-gtid-errant/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/gtid-errant-reset-master/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/gtid-errant-reset-master/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/gtid-errant-inject-empty/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/gtid-errant-inject-empty/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/skip-query/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/skip-query/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/start-slave/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/start-replica/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/start-replica/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/restart-slave/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/restart-replica/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/restart-replica/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/stop-slave/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/stop-replica/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/stop-replica/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/stop-slave-nice/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/stop-replica-nice/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/reset-slave/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/reset-replica/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/reset-replica/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/change-master-credentials/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/detach-slave/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/detach-replica/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/detach-replica/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/reattach-slave/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/reattach-replica/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/reattach-replica/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/detach-slave-master-host/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/detach-replica-master-host/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/reattach-slave-master-host/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/reattach-replica-master-host/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/reattach-replica-master-host/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/flush-binary-logs/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/purge-binary-logs/:host/:port/:logFile` | 否 | legacy: `unknown` |
| GET | `/api/restart-slave-statements/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/restart-replica-statements/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/enable-semi-sync-master/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/enable-semi-sync-source/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/disable-semi-sync-master/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/disable-semi-sync-source/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/enable-semi-sync-replica/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/disable-semi-sync-replica/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/delay-replication/:host/:port/:seconds` | 否 | legacy: `unknown` |
| GET | `/api/can-replicate-from/:host/:port/:belowHost/:belowPort` | 是 | legacy: `unknown` |
| GET | `/api/can-replicate-from-gtid/:host/:port/:belowHost/:belowPort` | 是 | legacy: `unknown` |
| GET | `/api/set-read-only/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/set-read-only/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/set-writeable/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/set-writeable/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/kill-query/:host/:port/:process` | 否 | envelope: `unknown` |
| GET | `/api/last-pseudo-gtid/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/submit-pool-instances/:pool` | 否 | legacy: `unknown` |
| POST | `/api/submit-pool-instances/:pool` | 否 | envelope: `unknown` |
| GET | `/api/cluster-pool-instances/:clusterName` | 是 | legacy: `unknown` |
| GET | `/api/cluster-pool-instances/:clusterName/:pool` | 是 | legacy: `unknown` |
| GET | `/api/heuristic-cluster-pool-instances/:clusterName` | 是 | envelope: `(Instance)[] | null` |
| GET | `/api/heuristic-cluster-pool-instances/:clusterName/:pool` | 是 | envelope: `(Instance)[] | null` |
| GET | `/api/heuristic-cluster-pool-lag/:clusterName` | 是 | legacy: `unknown` |
| GET | `/api/heuristic-cluster-pool-lag/:clusterName/:pool` | 是 | legacy: `unknown` |
| GET | `/api/search/:searchString` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/search` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/cluster/:clusterHint` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/cluster/alias/:clusterAlias` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/cluster/instance/:host/:port` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/cluster-info/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/cluster-info/alias/:clusterAlias` | 是 | legacy: `unknown` |
| GET | `/api/cluster-osc-slaves/:clusterHint` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/cluster-osc-replicas/:clusterHint` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/set-cluster-alias/:clusterName` | 否 | envelope: `unknown` |
| POST | `/api/set-cluster-alias/:clusterName` | 否 | envelope: `unknown` |
| GET | `/api/clusters` | 是 | legacy: `unknown` |
| GET | `/api/clusters-info` | 是 | raw: `(Cluster)[] | null` |
| GET | `/api/masters` | 是 | legacy: `unknown` |
| GET | `/api/master/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/instance-replicas/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/all-instances` | 是 | legacy: `unknown` |
| GET | `/api/downtimed` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/downtimed/:clusterHint` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/topology/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/topology/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/topology-tabulated/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/topology-tabulated/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/topology-tags/:clusterHint` | 是 | legacy: `unknown` |
| GET | `/api/topology-tags/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/snapshot-topologies` | 否 | legacy: `unknown` |
| GET | `/api/submit-masters-to-kv-stores` | 否 | legacy: `unknown` |
| GET | `/api/submit-masters-to-kv-stores/:clusterHint` | 否 | envelope: `unknown` |
| GET | `/api/tagged` | 是 | legacy: `unknown` |
| GET | `/api/tags/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/tag-value/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/tag-value/:host/:port/:tagName` | 是 | legacy: `unknown` |
| GET | `/api/tag/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/tag/:host/:port/:tagName/:tagValue` | 否 | envelope: `unknown` |
| GET | `/api/untag/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/untag/:host/:port/:tagName` | 否 | envelope: `unknown` |
| GET | `/api/untag-all` | 否 | legacy: `unknown` |
| GET | `/api/untag-all/:tagName/:tagValue` | 否 | envelope: `unknown` |
| GET | `/api/instance/:host/:port` | 是 | raw: `Instance` |
| GET | `/api/discover/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/discover/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/async-discover/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/refresh/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/refresh/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/forget/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/forget/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/forget-cluster/:clusterHint` | 否 | legacy: `unknown` |
| GET | `/api/begin-maintenance/:host/:port/:owner/:reason` | 否 | legacy: `unknown` |
| POST | `/api/begin-maintenance/:host/:port/:owner/:reason` | 否 | envelope: `unknown` |
| GET | `/api/end-maintenance/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/end-maintenance/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/in-maintenance/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/end-maintenance/:maintenanceKey` | 否 | envelope: `unknown` |
| POST | `/api/end-maintenance/:maintenanceKey` | 否 | envelope: `unknown` |
| GET | `/api/maintenance` | 是 | raw: `(Maintenance)[] | null` |
| GET | `/api/begin-downtime/:host/:port/:owner/:reason` | 否 | legacy: `unknown` |
| POST | `/api/begin-downtime/:host/:port/:owner/:reason` | 否 | envelope: `unknown` |
| GET | `/api/begin-downtime/:host/:port/:owner/:reason/:duration` | 否 | envelope: `unknown` |
| POST | `/api/begin-downtime/:host/:port/:owner/:reason/:duration` | 否 | envelope: `unknown` |
| GET | `/api/end-downtime/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/end-downtime/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/recovery-policy/:scopeType/:scopeKey` | 否 | envelope: `RecoveryPolicyDocument` |
| POST | `/api/recovery-policy` | 否 | envelope: `unknown` |
| GET | `/api/recovery-hook-profiles` | 否 | envelope: `(HookProfile)[] | null` |
| POST | `/api/recovery-hook-profiles` | 否 | envelope: `unknown` |
| POST | `/api/recovery-hook-test` | 否 | envelope: `unknown` |
| GET | `/api/recovery-hook-assignments/:scopeType/:scopeKey` | 否 | envelope: `(HookAssignment)[] | null` |
| POST | `/api/recovery-hook-assignments` | 否 | envelope: `unknown` |
| GET | `/api/replication-analysis` | 是 | raw: `(Analysis)[] | null` |
| GET | `/api/replication-analysis/:clusterName` | 是 | raw: `(Analysis)[] | null` |
| GET | `/api/replication-analysis/instance/:host/:port` | 是 | raw: `(Analysis)[] | null` |
| GET | `/api/recover/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/recover/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/recover/:host/:port/:candidateHost/:candidatePort` | 否 | envelope: `unknown` |
| POST | `/api/recover/:host/:port/:candidateHost/:candidatePort` | 否 | envelope: `unknown` |
| GET | `/api/recover-lite/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/recover-lite/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/recover-lite/:host/:port/:candidateHost/:candidatePort` | 否 | envelope: `unknown` |
| POST | `/api/recover-lite/:host/:port/:candidateHost/:candidatePort` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/graceful-master-takeover/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover/:host/:port/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| POST | `/api/graceful-master-takeover/:host/:port/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover/:clusterHint` | 否 | legacy: `unknown` |
| POST | `/api/graceful-master-takeover/:clusterHint` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover/:clusterHint/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| POST | `/api/graceful-master-takeover/:clusterHint/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover-auto/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover-auto/:host/:port/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| GET | `/api/graceful-master-takeover-auto/:clusterHint` | 否 | legacy: `unknown` |
| GET | `/api/graceful-master-takeover-auto/:clusterHint/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| GET | `/api/force-master-failover/:host/:port` | 否 | envelope: `unknown` |
| POST | `/api/force-master-failover/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/force-master-failover/:clusterHint` | 否 | legacy: `unknown` |
| POST | `/api/force-master-failover/:clusterHint` | 否 | envelope: `unknown` |
| GET | `/api/force-master-takeover/:clusterHint/:designatedHost/:designatedPort` | 否 | legacy: `unknown` |
| GET | `/api/force-master-takeover/:host/:port/:designatedHost/:designatedPort` | 否 | envelope: `unknown` |
| GET | `/api/register-candidate/:host/:port/:promotionRule` | 否 | legacy: `unknown` |
| POST | `/api/register-candidate/:host/:port/:promotionRule` | 否 | envelope: `unknown` |
| GET | `/api/automated-recovery-filters` | 是 | legacy: `unknown` |
| GET | `/api/audit-failure-detection` | 是 | legacy: `unknown` |
| GET | `/api/audit-failure-detection/:page` | 是 | legacy: `unknown` |
| GET | `/api/audit-failure-detection/id/:id` | 是 | legacy: `unknown` |
| GET | `/api/audit-failure-detection/alias/:clusterAlias` | 是 | legacy: `unknown` |
| GET | `/api/audit-failure-detection/alias/:clusterAlias/:page` | 是 | legacy: `unknown` |
| GET | `/api/replication-analysis-changelog` | 是 | legacy: `unknown` |
| GET | `/api/audit-recovery` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/:page` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/id/:id` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/uid/:uid` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/cluster/:clusterName` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/cluster/:clusterName/:page` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/alias/:clusterAlias` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery/alias/:clusterAlias/:page` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/audit-recovery-steps/:uid` | 是 | raw: `(RecoveryStep)[] | null` |
| GET | `/api/active-cluster-recovery/:clusterName` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/recently-active-cluster-recovery/:clusterName` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/recently-active-instance-recovery/:host/:port` | 是 | raw: `(Recovery)[] | null` |
| GET | `/api/ack-recovery/cluster/:clusterHint` | 否 | legacy: `unknown` |
| POST | `/api/ack-recovery/cluster/:clusterHint` | 否 | envelope: `unknown` |
| GET | `/api/ack-recovery/cluster/alias/:clusterAlias` | 否 | envelope: `unknown` |
| POST | `/api/ack-recovery/cluster/alias/:clusterAlias` | 否 | envelope: `unknown` |
| GET | `/api/ack-recovery/instance/:host/:port` | 否 | legacy: `unknown` |
| POST | `/api/ack-recovery/instance/:host/:port` | 否 | envelope: `unknown` |
| GET | `/api/ack-recovery/:recoveryId` | 否 | envelope: `unknown` |
| POST | `/api/ack-recovery/:recoveryId` | 否 | envelope: `unknown` |
| GET | `/api/ack-recovery/uid/:uid` | 否 | envelope: `unknown` |
| POST | `/api/ack-recovery/uid/:uid` | 否 | envelope: `unknown` |
| GET | `/api/ack-all-recoveries` | 否 | legacy: `unknown` |
| GET | `/api/blocked-recoveries` | 是 | raw: `(BlockedRecovery)[] | null` |
| GET | `/api/blocked-recoveries/cluster/:clusterName` | 是 | raw: `(BlockedRecovery)[] | null` |
| GET | `/api/disable-global-recoveries` | 否 | legacy: `unknown` |
| POST | `/api/disable-global-recoveries` | 否 | envelope: `unknown` |
| GET | `/api/enable-global-recoveries` | 否 | legacy: `unknown` |
| POST | `/api/enable-global-recoveries` | 否 | envelope: `unknown` |
| GET | `/api/check-global-recoveries` | 是 | legacy: `unknown` |
| GET | `/api/problems` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/problems/:clusterName` | 是 | raw: `(Instance)[] | null` |
| GET | `/api/audit` | 是 | raw: `(Audit)[] | null` |
| GET | `/api/audit/:page` | 是 | raw: `(Audit)[] | null` |
| GET | `/api/audit/instance/:host/:port` | 是 | raw: `(Audit)[] | null` |
| GET | `/api/audit/instance/:host/:port/:page` | 是 | raw: `(Audit)[] | null` |
| GET | `/api/resolve/:host/:port` | 是 | legacy: `unknown` |
| GET | `/api/headers` | 是 | legacy: `unknown` |
| GET | `/api/health` | 是 | legacy: `unknown` |
| GET | `/api/lb-check` | 是 | legacy: `unknown` |
| GET | `/api/_ping` | 是 | legacy: `unknown` |
| GET | `/api/leader-check` | 是 | legacy: `unknown` |
| GET | `/api/leader-check/:errorStatusCode` | 是 | legacy: `unknown` |
| GET | `/api/raft/configuration` | 是 | legacy: `unknown` |
| POST | `/api/raft/bootstrap` | 否 | legacy: `unknown` |
| POST | `/api/raft/members` | 否 | legacy: `unknown` |
| DELETE | `/api/raft/members/:id` | 否 | legacy: `unknown` |
| POST | `/api/raft/leadership/transfer` | 否 | legacy: `unknown` |
| POST | `/api/raft/snapshot` | 否 | legacy: `unknown` |
| GET | `/api/raft-state` | 是 | legacy: `unknown` |
| GET | `/api/raft-leader` | 是 | legacy: `unknown` |
| GET | `/api/raft-health` | 是 | legacy: `unknown` |
| GET | `/api/raft-status` | 是 | legacy: `unknown` |
| GET | `/api/reload-configuration` | 否 | envelope: `unknown` |
| POST | `/api/reload-configuration` | 否 | envelope: `unknown` |
| GET | `/api/hostname-resolve-cache` | 是 | legacy: `unknown` |
| GET | `/api/reset-hostname-resolve-cache` | 否 | legacy: `unknown` |
| POST | `/api/reset-hostname-resolve-cache` | 否 | envelope: `unknown` |
| GET | `/api/routed-leader-check` | 是 | legacy: `unknown` |
| GET | `/api/reload-cluster-alias` | 否 | envelope: `unknown` |
| GET | `/api/deregister-hostname-unresolve/:host/:port` | 否 | legacy: `unknown` |
| GET | `/api/register-hostname-unresolve/:host/:port/:virtualname` | 否 | legacy: `unknown` |
| GET | `/api/bulk-instances` | 是 | legacy: `unknown` |
| GET | `/api/bulk-promotion-rules` | 是 | legacy: `unknown` |
| GET | `/api/agents` | 是 | raw: `(Agent)[] | null` |
| GET | `/api/agent/:host` | 是 | raw: `Agent` |
| GET | `/api/agent-umount/:host` | 否 | envelope: `unknown` |
| POST | `/api/agent-umount/:host` | 否 | envelope: `unknown` |
| GET | `/api/agent-mount/:host` | 否 | envelope: `unknown` |
| POST | `/api/agent-mount/:host` | 否 | envelope: `unknown` |
| GET | `/api/agent-create-snapshot/:host` | 否 | envelope: `unknown` |
| POST | `/api/agent-create-snapshot/:host` | 否 | envelope: `unknown` |
| GET | `/api/agent-removelv/:host` | 否 | envelope: `unknown` |
| POST | `/api/agent-removelv/:host` | 否 | envelope: `unknown` |
| GET | `/api/agent-mysql-stop/:host` | 否 | envelope: `unknown` |
| POST | `/api/agent-mysql-stop/:host` | 否 | envelope: `unknown` |
| GET | `/api/agent-mysql-start/:host` | 否 | envelope: `unknown` |
| POST | `/api/agent-mysql-start/:host` | 否 | envelope: `unknown` |
| GET | `/api/agent-seed/:targetHost/:sourceHost` | 否 | envelope: `unknown` |
| POST | `/api/agent-seed/:targetHost/:sourceHost` | 否 | envelope: `unknown` |
| GET | `/api/agent-active-seeds/:host` | 是 | raw: `(Seed)[] | null` |
| GET | `/api/agent-recent-seeds/:host` | 是 | raw: `(Seed)[] | null` |
| GET | `/api/agent-seed-details/:seedId` | 是 | raw: `(Seed)[] | null` |
| GET | `/api/agent-seed-states/:seedId` | 是 | raw: `(SeedState)[] | null` |
| GET | `/api/agent-abort-seed/:seedId` | 否 | envelope: `unknown` |
| POST | `/api/agent-abort-seed/:seedId` | 否 | envelope: `unknown` |
| GET | `/api/agent-custom-command/:host/:command` | 否 | legacy: `unknown` |
| GET | `/api/seeds` | 是 | raw: `(Seed)[] | null` |
| GET | `/api/status` | 是 | legacy: `unknown` |
| GET | `/api/web-config` | 是 | raw: `WebConfig` |
