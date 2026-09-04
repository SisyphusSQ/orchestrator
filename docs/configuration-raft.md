# Configuration: raft

Set up a [orchestrator/raft](raft.md) cluster for high availability.

This setup is for **new clusters only**. Node identity is a required, durable `RaftNodeID`. It is never derived from IP, FQDN, `RaftBind`, `RaftAdvertise`, or DNS.

A new cluster is created by bootstrapping **one** seed node, then adding the remaining nodes through the leader membership API. Nodes do not auto-join from a static peer list.

Assuming you will run `orchestrator/raft` on a `3` node setup, configure each node with its own identity and addresses:

```json
  "RaftEnabled": true,
  "RaftNodeID": "<stable-id-of-this-node>",
  "RaftDataDir": "<path.to.orchestrator.data.directory>",
  "RaftBind": "<local.listen.host:port>",
  "RaftAdvertise": "<address.other.nodes.use:port>",
  "DefaultRaftPort": 10008
```

Some breakdown:

- `RaftEnabled` must be set to `true`, otherwise `orchestrator` runs in shared-backend mode.
- `RaftNodeID` is required, persistent, and unique in the cluster. Do not use bind/advertise addresses as a substitute for identity.
- `RaftDataDir` must be set to a directory writable to `orchestrator`. `orchestrator` will attempt to create the directory if it does not exist. Raft state is stored as `raft.db`, `node-id`, and `snapshots/` under this directory.
- Treat `node-id` as part of the Raft state. If it is missing while `raft.db` or snapshots contain state, startup fails rather than binding that state to a newly supplied ID.
- `RaftBind` is the local listen address (`host:port`).
- `RaftAdvertise` is the address other cluster members use to reach this node. If omitted, it defaults to the normalized `RaftBind`. It is not used as a node ID.
- `DefaultRaftPort` is used only to complete a bind/advertise value that has no port.

As example, the following might be a working setup for node `orc-2`:

```json
  "RaftEnabled": true,
  "RaftNodeID": "orc-2",
  "RaftDataDir": "/var/lib/orchestrator",
  "RaftBind": "10.0.0.2:10008",
  "RaftAdvertise": "10.0.0.2:10008",
  "DefaultRaftPort": 10008
```

as well as this:

```json
  "RaftEnabled": true,
  "RaftNodeID": "orc-2",
  "RaftDataDir": "/var/lib/orchestrator",
  "RaftBind": "0.0.0.0:10008",
  "RaftAdvertise": "node-full-hostname-2.here.com:10008",
  "DefaultRaftPort": 10008
```

### Local three-node bootstrap and join

1. Start all three nodes with distinct `RaftNodeID` values and reachable `RaftAdvertise` addresses. They listen, but they are not a cluster yet.
2. On **one** seed node only, bootstrap a single-voter configuration of that node:

   `POST /api/raft/bootstrap`

3. On the elected leader (the seed), add the other two nodes as voters:

   ```http
   POST /api/raft/members
   {"id":"orc-2","address":"10.0.0.2:10008","suffrage":"voter"}
   ```

   ```http
   POST /api/raft/members
   {"id":"orc-3","address":"10.0.0.3:10008","suffrage":"voter"}
   ```

4. Confirm the same configuration on every node with `GET /api/raft/configuration`.

Do not bootstrap more than one node. A node that already has raft state returns a conflict error.

### Membership and leadership APIs

- `GET /api/raft/configuration` — configuration index, whether that latest configuration is committed, servers (`id` / `address` / `suffrage`), leader id/address, and local identity. Readable on any node.
- `POST /api/raft/bootstrap` — NoProxy. Creates a single-voter configuration of this node.
- `POST /api/raft/members` — JSON `id`, `address`, `suffrage` (`voter` or `nonvoter`), optional `expectedIndex`. Leader or safe proxy.
- `DELETE /api/raft/members/:id` — remove by server ID, optional `expectedIndex`. Leader or safe proxy.
- `POST /api/raft/leadership/transfer` — optional target voter `id` (and `address` for conflict checks). Undirected if both are omitted; an address cannot be supplied alone, and the local server or a nonvoter cannot be the target. No hostname hints.
- `POST /api/raft/snapshot` — wait for the official snapshot future.

Malformed JSON, unknown fields, invalid or conflicting `expectedIndex` values, raft disabled, not bootstrapped, not leader, CAS conflict, confirmed failure, and indeterminate results are returned as distinct `ErrorClass` values. A timed-out mutation is only reported as confirmed success when readback shows the requested configuration and its `committed` field is true.

### NAT, firewalls, routing

If your orchestrator/raft nodes need to communicate via NAT gateways, set `RaftAdvertise` to the address other nodes should contact. `RaftBind` remains the local listen address.

Raft nodes will reverse proxy HTTP requests to the leader. `orchestrator` will attempt to heuristically compute the leader's URL to which redirect requests. If behind NAT, rerouting ports etc., `orchestrator` may not be able to compute that URL. You may configure:

- `"HTTPAdvertise": "scheme://hostname:port"`

to explicitly specify where a node, assuming it were the leader, would be accessed through HTTP API. As example, you would: `"HTTPAdvertise": "http://my.public.hostname:3000"`

### Backend DB

A `raft` setup supports either `MySQL` or `SQLite` backend DB. See [backend](configuration-backend.md) configuration for either. Read [high-availability](high-availability.md) page for scenarios, possibilities and reasons to using either.

### Single raft node setups

In production, you will want using multiple `raft` nodes, such as `3` or `5`.

In a testing environment you may run a `orchestrator/raft` setup composed of a single node. Start the node, then call `POST /api/raft/bootstrap` once. That node becomes the only voter and therefore the leader.
