# Orchestrator deployment: shared backend

This text describes deployments for shared backend DB. See [High availability](high-availability.md) for the various backend DB setups.

This complements general [deployment](deployment.md) documentation.

### Shared backend

You will need to set up a shared backend database. This could be synchronous replication (Galera/XtraDB Cluster/InnoDB Cluster) for high availability, or it could be a master-replicas setup etc.

The backend database has the _state_ of your topologies. `orchestrator` itself is almost stateless, and trusts the data in the backend database.

In a shared backend setup multiple `orchestrator` services will all speak to the same backend.

- For **synchronous replication**, the advice is:
  - Configure multi-writer mode (each node in the MySQL cluster is writable)
  - Have `1:1` mapping between `orchestrator` services and `MySQL` nodes: each `orchestrator` service to speak with its own node.
- For **master-replicas** (asynchronous & semi-synchronous), do:
  - Configure all `orchestrator` nodes to access the _same_ backend DB (the master)
  - Optionally you will have your own load balancer to direct traffic to said master, in which case configure all `orchestrator` nodes to access the proxy.

### MySQL backend setup and high availability

Setting up the backend DB is on you. Also, `orchestartor` doesn't eat its own dog food, and cannot recover a failure on its own backend DB.
You will need to handle, for example, the issue of adding a Galera node, or of managing your proxy health checks etc.

### What to deploy: service

- Deploy the `orchestrator` service onto service boxes. The decision of how many service boxes  to deploy
  will depend on your [availability needs](high-availability.md).
  - In a synchronous replication shared backend setup, these may well be the very MySQL boxes, in a `1:1` mapping.
- Consider adding a proxy on top of the service boxes; the proxy would ideally redirect all traffic to the leader node. There is one and only one leader node, and the status check endpoint is `/api/leader-check`. It is OK to direct traffic to any healthy service. Since all `orchestrator` nodes speak to the same shared backend DB, it is OK to operate some actions from one service node, and other actions from another service nodes. Internal locks are placed to avoid running contradicting or interfering commands.


### What to deploy: client

Install the independent Go HTTP client [orch](orch.md) on operator and automation hosts.
Set `ORCH_ENDPOINT` in the process environment, or pass `--endpoint`. Use one service/proxy
URL or multiple comma-separated API endpoints for leader discovery. The client does not
load a shell profile or server configuration and never accesses the backend database.

```bash
export ORCH_ENDPOINT="http://node1:3000/api,http://node2:3000/api,http://node3:3000/api"
orch clusters
orch topology --cluster my-cluster
```

Direct database business commands and the old Shell client have been removed. Both Raft
and shared-backend deployments use HTTP. Local server maintenance remains under
`orchestrator admin`; it must not be used as an alternative remote management interface.

### Orchestrator service

In a shared-backend deployment, you may deploy the number of `orchestrator` nodes as suits your requirements. 

However, as noted, one `orchestrator` node will be [elected leader](http://code.openark.org/blog/mysql/leader-election-using-mysql). Only the leader will:

- Discover (probe) your MySQL topologies
- Run failure detection
- Run recoveries

All nodes will:

- Serve HTTP requests
- Register their own health check

All nodes may:

- Run arbitrary command (e.g. `relocate`, `begin-downtime`)
- Run recoveries per human request.

For more details about deploying multiple nodes, please read about [high availability](high-availability.md).

### Go HTTP client

The client sends commands to the service. The service performs topology queries and
operations and owns backend database access. See [orch](orch.md).
