#!/bin/sh
set -eu
if [ ! -e /etc/orchestrator.conf.yaml ] && [ ! -e /etc/orchestrator.conf.yml ] && [ ! -e /etc/orchestrator.conf.json ] ; then
  : "${ORC_RAFT_NODE_ID:?set a stable unique ORC_RAFT_NODE_ID or mount /etc/orchestrator.conf.yaml}"
  : "${ORC_RAFT_ADVERTISE:?set a reachable ORC_RAFT_ADVERTISE or mount /etc/orchestrator.conf.yaml}"
  jq -n \
    --arg id "$ORC_RAFT_NODE_ID" \
    --arg bind "${ORC_RAFT_BIND:-0.0.0.0:10008}" \
    --arg advertise "$ORC_RAFT_ADVERTISE" \
    --arg dir "${ORC_RAFT_DATA_DIR:-/var/lib/orchestrator/raft}" \
    --arg http "${ORC_HTTP_ADVERTISE:-}" \
    --arg topologyUser "${ORC_TOPOLOGY_USER:-orchestrator}" \
    --arg topologyPassword "${ORC_TOPOLOGY_PASSWORD:-orchestrator}" \
    --arg dbHost "${ORC_DB_HOST:-db}" --argjson dbPort "${ORC_DB_PORT:-3306}" \
    --arg dbName "${ORC_DB_NAME:-orchestrator}" --arg dbUser "${ORC_USER:-orc_server_user}" \
    --arg dbPassword "${ORC_PASSWORD:-orc_server_password}" \
    '{ListenAddress:":3000", RaftNodeID:$id, RaftBind:$bind, RaftAdvertise:$advertise,
      RaftDataDir:$dir, HTTPAdvertise:$http, MySQLTopologyUser:$topologyUser,
      MySQLTopologyPassword:$topologyPassword, MySQLOrchestratorHost:$dbHost,
      MySQLOrchestratorPort:$dbPort, MySQLOrchestratorDatabase:$dbName,
      MySQLOrchestratorUser:$dbUser, MySQLOrchestratorPassword:$dbPassword}' > /etc/orchestrator.conf.json
fi

exec /usr/local/orchestrator/orchestrator server
