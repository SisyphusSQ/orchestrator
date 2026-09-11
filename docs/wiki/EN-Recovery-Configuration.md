# Page-managed recovery settings

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Recovery-Configuration) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Recovery policy and pre/post Hooks are managed in Recovery Settings, not server YAML/JSON. Metadata stores sparse overrides; effective values resolve cluster override > global override > code default through Raft. Each recovery stores policy and Hook fingerprints.

## Authorization and scope

- Global scope uses key `*`.
- Cluster scope requires an explicit unique alias from `cluster_alias_override`.
- With authentication, `authentication.configurationAdmins.users/groups` grants writes; empty lists deny them.
- Local unauthenticated deployment still requires leader and quorum.
- Saves use revision optimistic locking. On conflict, reread, compare, and merge.

## All 23 policy settings

Fields without a nonzero/true default use false, zero, or an empty list. Defaults are not production recommendations.

| Field | Code default | Decision and risk |
| --- | --- | --- |
| `autoMasterRecovery` | `false` | automatic primary recovery; enable after fencing/candidate drills |
| `autoIntermediateMasterRecovery` | `false` | automatic intermediate recovery; can rearrange many replicas |
| `recoveryIgnoreHostnameFilters` | `[]` | recovery filter, not discovery exclusion |
| `promotionIgnoreHostnameFilters` | `[]` | forbids promotion while retaining visibility |
| `problemIgnoreHostnameFilters` | `[]` | hides matched problems; broad filters hide failures |
| `failureDetectionPeriodBlockMinutes` | `60` | blocks duplicate detection storms |
| `recoveryPeriodBlockSeconds` | `3600` | blocks repeated recovery in the same scope |
| `reasonableReplicationLagSeconds` | `10` | normal candidate lag threshold |
| `reasonableMaintenanceReplicationLagSeconds` | `20` | maintenance lag threshold |
| `verifyReplicationFilters` | `false` | verifies filters before promotion/relocation |
| `failMasterPromotionOnLagMinutes` | `0` | blocks lagging promotion; verify zero semantics in the same version |
| `sqlThreadPromotionPolicy` | `allow` | `allow`, `wait`, or `reject` |
| `recoverNonWriteableMaster` | `false` | recovery of a nonwritable primary; exclude planned fencing |
| `coMasterRecoveryMustPromoteOtherCoMaster` | `true` | requires peer co-master as successor |
| `detachLostReplicasAfterMasterFailover` | `true` | detaches replicas proven lost |
| `applyMySQLPromotionAfterMasterFailover` | `true` | applies MySQL promotion state |
| `preventCrossDataCenterMasterFailover` | `false` | forbids cross-DC failover; needs classification |
| `preventCrossRegionMasterFailover` | `false` | forbids cross-region failover |
| `masterFailoverDetachReplicaMasterHost` | `false` | detaches replica master host after failover |
| `postponeReplicaRecoveryOnLagMinutes` | `0` | postpones lagging replica recovery |
| `enforceExactSemiSyncReplicas` | `false` | enforces exact semi-sync condition |
| `recoverLockedSemiSyncMaster` | `false` | recovers a semi-sync-locked primary |
| `reasonableLockedSemiSyncMasterSeconds` | `0` | duration threshold for locked primary |

Times must be nonnegative. SQL-thread policy accepts only `allow`, `wait`, and `reject`. Test every hostname filter with positive and negative examples.

## Inheritance example

If global sets lag to 15 and enables filter verification while cluster `payments` overrides only lag to 5, effective values are 5 and true. Removing the cluster field restores inheritance; it does not write zero. Read effective value and source after save.

## Hook profiles

Profiles require nonempty ID/name, at least one nonblank command, 1–3600 seconds per-command timeout, `continue` or `abort`, 1024–1048576 bytes output, enabled state, and revision.

Commands run through `hooks.shellCommand` as the server identity. Do not rely on interactive shell, home, implicit PATH, or working directory. Use external timeouts, recovery UID idempotency, and queryable results. Redaction catches common secrets but secrets must not enter arguments or output.

## Nine Hook phases

| Phase | Point | Typical use | Caution |
| --- | --- | --- | --- |
| `failure_detection` | after detection | notify/freeze competitors | no successful recovery yet |
| `pre_failover` | before writes | fencing/gate | `abort` can stop recovery |
| `post_master_failover` | after primary failover | writer/service discovery | external readback required |
| `post_intermediate_master_failover` | after intermediate recovery | local route/notice | distinct from primary |
| `post_failover` | after any recovery | common audit/notice | does not prove traffic |
| `post_unsuccessful_failover` | after failed recovery | escalation | tolerate partial context |
| `pre_graceful_takeover` | before planned switch | freeze/window check | separate failure flow |
| `post_graceful_takeover` | after planned switch | route/unfreeze | idempotent writer update |
| `post_take_master` | after Take Master | compatibility post action | avoid duplicate post effects |

## Assignment modes

`inherit` uses global and has no profile IDs; `replace` supplies at least one ordered profile and replaces global; `disable` runs nothing and has no IDs. Absence and explicit inherit can execute identically but express different audit intent. Search assignments before deleting a profile.

## Safe change flow

1. Read global, target raw/effective policy, sources, revision, and phase sources.
2. Record reason, expected behavior, rollback, and test cluster.
3. Change an isolated cluster with a sparse patch.
4. Reread revision, values, and source.
5. Test a harmless Hook for identity, environment, timeout, redaction, and audit.
6. Trigger the phase in a drill and correlate recovery UID, fingerprints, and external readback.
7. Roll out by cluster. Accept the global recovery switch separately.

Test Run executes a real command. After timeout, inspect revision, audit, and external idempotency state.

## Upgrade and rollback

Legacy recovery/Hook YAML keys are rejected and not imported. Record values before upgrade, configure and read them afterward, and verify metadata compatibility before rollback. Restoring older metadata rolls policy, assignments, and revisions back; reconfirm before recovery is enabled.
