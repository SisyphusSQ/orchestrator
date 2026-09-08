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

## Health and traffic

- `/health/live`: process is serving HTTP.
- `/health/ready`: local backend and Raft state are ready and fresh.
- `/health/leader-ready`: this node is the ready leader.
- `/api/leader-check`: compatibility/load-balancer leader check.
- `/api/raft/configuration`: local membership and leadership readback.

Followers discover topology and can proxy supported requests. Only the leader performs recovery and coordinated writes. When quorum is lost, do not bypass Raft by writing directly to a metadata database.
