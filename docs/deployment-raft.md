# Orchestrator deployment: raft

This text describes deployments for [orchestrator on raft](raft.md).

This complements general [deployment](deployment.md) documentation.

### Backend DB

You may choose between using `MySQL` and `SQLite`. See [backend configuration](configuration-backend.md).

- For MySQL:
  - The backend servers will be standalone. No replication setup. Each `orchestrator` node will interact with its own dedicated backend server.
  - You _must_ have a `1:1` mapping `orchestrator:MySQL`.
  - Suggestion: run `orchestrator` and its dedicated MySQL server on same box.
  - Make sure to `GRANT` privileges for the `orchestrator` user on each backend node.

- For `SQLite`:
  - `SQLite` is bundled with `orchestrator`.
  - Make sure the `SQLite3DataFile` is writable by the `orchestrator` user.

### High availability

`orchestrator` high availability is gained by using `raft`. You do not need to take care of backend DB high availability.

### What to deploy: service

- Deploy the `orchestrator` service onto service boxes.
  As suggested, you may want to put `orchestrator` service and `MySQL` service on same box. If using `SQLite` there's nothing else to do.

- Consider adding a proxy on top of the service boxes; the proxy would redirect all traffic to the leader node. There is one and only one leader node, and the status check endpoint is `/api/leader-check`.
  - Clients may only interact with healthy raft nodes.
    - Simplest is to just interact with the leader. Setting up a proxy is one way to ensure that. See [proxy: leader section](raft.md#proxy-leader).
    -  Otherwise all healthy raft nodes will reverse proxy your requests to the leader. See [proxy: healthy raft nodes section](raft.md#proxy-healthy-raft-nodes).
- Nothing should directly interact with a backend DB. Only the leader is capable of coordinating changes to the data with the other `raft` nodes.

- `orchestrator` nodes communicate through the ports configured in `RaftBind` / `RaftAdvertise`; `DefaultRaftPort` only fills in an omitted port. These ports should be open to all `orchestrator` nodes, and no one else needs access to them.

### What to deploy: client

To interact with orchestrator from shell/automation/scripts, you may choose to:

- Directly interact with the HTTP API
  - You may only interact with the _leader_. A good way to achieve this is using a proxy.
- Use the [orchestrator-client](orchestrator-client.md) script.
  - Deploy `orchestrator-client` on any box from which you wish to interact with `orchestrator`.
  - Create and edit `/etc/profile.d/orchestrator-client.sh` on those boxes to read:
    ```
    ORCHESTRATOR_API="http://your.orchestrator.service.proxy:80/api"
    ```
    or
    ```
    ORCHESTRATOR_API="http://your.orchestrator.service.host1:3000/api http://your.orchestrator.service.host2:3000/api http://your.orchestrator.service.host3:3000/api"
    ```
    In the latter case you will provide the list of all `orchestrator` nodes, and the `orchestrator-client` script will automatically figure out which is the leader. With this setup your automation will not need a proxy (though you may still wish to use a proxy for web interface users).

    Make sure to chef/puppet/whatever the `ORCHESTRATOR_API` value such that it adapts to changes in your environment.

- The `orchestrator` command line client will refuse to run given a raft setup, since it interacts directly with the underlying database and doesn't participate in the raft consensus, and thus cannot ensure all raft members will get visibility into it changes.
  - Fortunately `orchestrator-client` provides an almost identical interface as the command line client.
  - You may force the command line client to run via `--ignore-raft-setup`. This is a "I know what I'm doing" risk you take. If you do choose to use it, then it makes more sense to connect to the leader's backend DB.


### Orchestrator service

After one seed node is bootstrapped and a leader is elected, only the leader will:

- Run recoveries

However all nodes will:

- Discover (probe) your MySQL topologies
- Run failure detection
- Register their own health check

Non-leader nodes must _NOT_:

- Run arbitrary commands (e.g. `relocate`, `begin-downtime`)
- Run recoveries per human request.
- Serve client HTTP requests (but some endpoints, such as load-balancer and health checks, are valid).

### A visual example

![orchestrator deployment, raft](images/orchestrator-deployment-raft.png)

In the above there are three `orchestrator` nodes in a `raft` cluster, each using its own dedicated database (either `MySQL` or `SQLite`).

`orchestrator` nodes communicate with each other.

Only one `orchestrator` node is the leader.

All `orchestrator` nodes probe the entire `MySQL` fleet. Each `MySQL` server is probed by each of the `raft` members.

### orchestrator/raft operational scenarios

##### A node crashes:

Start the node, start the `MySQL` service if applicable, start the `orchestrator` service. The `orchestrator` service should join the `raft` group, get a recent snapshot, catch up with `raft` replication log and continue as normal.

##### A new node is provisioned / a node is re-provisioned

Such that the backend database and Raft data directory are completely empty/missing.

- Give the process a new, stable `RaftNodeID`, configure its reachable `RaftAdvertise`, and start it. It listens but does not auto-join.
- If `MySQL`, first grant the privileges described in [backend configuration](configuration-backend.md#mysql-backend-db-setup). If `SQLite`, ensure its database path is writable.
- On the current leader, call `POST /api/raft/members` with the new ID/address and desired suffrage. Confirm a committed configuration containing that server before considering it joined; the leader will then replicate logs or a snapshot as needed.

##### Cloning is valid

If you choose to, you may also provision new boxes by cloning your existing backend databases using your favorite backup/restore  or dump/load method.

This is valid for preparing the backend database, but it does not copy or establish Raft membership.

- If `MySQL`, run backup/restore, either logical or physical.
- If `SQLite`, run `.dump` + restore, see [10. Converting An Entire Database To An ASCII Text File](https://sqlite.org/cli.html).

- Start the `orchestrator` service with its own stable `RaftNodeID`, then add it through the current leader's membership API. Do not clone another node's `RaftDataDir` or `node-id` file.

##### Replacing a node

Assuming voters `node1`, `node2`, `node3`, and you wish to replace `node3` with `nodeX`.

- You may take down `node3`, and the `raft` cluster will continue to work as long as `node1` and `node2` are good.
- Create `nodeX` with a new stable `RaftNodeID` (for example `nodeX`) and its own `RaftBind` / `RaftAdvertise`. Generate backend db data (see above) and start `orchestrator`. It listens but does not auto-join.
- On the current leader, add the new voter:

  `POST /api/raft/members` with `{"id":"nodeX","address":"<nodeX-advertise:port>","suffrage":"voter"}`
- After `nodeX` is in the configuration and healthy, remove the old member by ID:

  `DELETE /api/raft/members/node3`
- Confirm `GET /api/raft/configuration` on remaining nodes. The raft configuration log is the only membership source of truth.
