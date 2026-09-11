# Raft operations

**English** · [中文](https://github.com/SisyphusSQ/orchestrator/wiki/ZH-Raft-Operations) · [Wiki home](https://github.com/SisyphusSQ/orchestrator/wiki/Home)

Production normally uses three or five voting nodes. Give every node a unique stable ID, an independent metadata backend, a durable Raft directory, and an advertise address reachable by every other member.

## Form a new cluster

1. Start all nodes. An uninitialized node listens but is not yet a cluster member.
2. Configure `orch` with only the seed endpoint and bootstrap exactly that node:

   ```sh
   orch --endpoint http://node-1:3000 raft-bootstrap
   ```

3. Through the leader, add the remaining voters:

   ```sh
   orch --endpoint http://node-1:3000 raft-add-member \
     --body '{"id":"node-2","address":"node-2:10008","suffrage":"voter"}'
   orch --endpoint http://node-1:3000 raft-add-member \
     --body '{"id":"node-3","address":"node-3:10008","suffrage":"voter"}'
   ```

4. Read each node and confirm the same committed configuration:

   ```sh
   orch --endpoint http://node-1:3000 raft-configuration --output json
   ```

Never bootstrap multiple seeds. Never treat an HTTP success alone as membership proof; verify the committed configuration and IDs/addresses.

## Routine changes

Use `raft-add-member`, `raft-remove-member --id <stable-id>`, and `raft-transfer-leadership`. Membership writes can include `expectedIndex` in the JSON body for compare-and-set protection. A timed-out or disconnected mutation can have an unknown result: read configuration before deciding whether to retry.

Remove a failed voter only while the remaining voters still have quorum. Prefer adding and catching up a replacement before removing a healthy member. A re-provisioned node gets a new durable identity unless its complete Raft state is restored consistently.

## Identity, storage, and replacement

`raft.nodeID` is independent of DNS and network addresses. The Raft directory contains `raft.db`, `node-id`, and `snapshots/`; `node-id` is part of the persistent state. Startup fails when log or snapshot state exists without its matching identity rather than silently binding that state to a new ID. Identity, data directory, bind, and advertise changes require restart and cannot be applied by reload.

Every Raft member has an independent MySQL or SQLite metadata backend. A backend copy can seed a replacement, but it does not create Raft membership and another member's Raft directory must not be cloned casually. To replace `node-3`, start a clean node with a new stable ID, add it through the leader, confirm committed membership and catch-up, then remove `node-3` by ID.

Back up the metadata backend and Raft directory as separate consistency domains. Restoring only one side may produce stale application state or an invalid consensus member. A node with intact state can normally restart and catch up; an empty re-provisioned node must be added explicitly and must not be bootstrapped as a second cluster.

## Network and deployment

`raft.bind` is the local listener; `raft.advertise` is the address peers use. Set advertise explicitly behind NAT and allow the Raft port only between members. `server.httpAdvertise` can provide the externally reachable Web/API origin when automatic leader URL derivation is wrong.

Clients may use a leader-aware proxy or healthy nodes that proxy supported business calls. Load balancers should use `/health/leader-ready` when routing only to the leader, or `/health/ready` when follower proxying is intended. Never infer quorum or committed membership from process liveness.

## Health and traffic

- `/health/live`: process is serving HTTP.
- `/health/ready`: local backend and Raft state are ready and fresh.
- `/health/leader-ready`: this node is the ready leader.
- `/api/leader-check`: compatibility/load-balancer leader check.
- `/api/raft/configuration`: local membership and leadership readback.

Followers discover topology and can proxy supported requests. Only the leader performs recovery and coordinated writes. When quorum is lost, do not bypass Raft by writing directly to a metadata database.

For a three-voter cluster quorum is two; for five voters it is three. Place voters so a single expected failure domain cannot retain an isolated minority as the service entry point. Raft protects orchestrator coordination, but application routing and MySQL fencing remain separate controls.

For snapshot, metadata, single-member replacement, and lost-quorum procedures, continue with [Backup, restore, and disaster handling](https://github.com/SisyphusSQ/orchestrator/wiki/EN-Backup-and-Restore). A successful `raft-snapshot` alone is never a complete restore plan.
