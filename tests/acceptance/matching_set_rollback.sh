#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 2 ]]; then
  echo "usage: matching_set_rollback.sh OLD_GIT_REF OUTPUT_DIR" >&2
  exit 2
fi

old_ref=$1
output_dir=$2
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
current_server=$repo_root/bin/orchestrator
current_cli=$repo_root/bin/orch
[[ -x "$current_server" && -x "$current_cli" ]] || { echo "run make build first" >&2; exit 1; }
for tool in curl git go jq python3 shasum sqlite3 tar; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done
git -C "$repo_root" rev-parse --verify "$old_ref^{commit}" >/dev/null

umask 077
[[ ! -e "$output_dir" ]] || { echo "OUTPUT_DIR already exists" >&2; exit 1; }
install -d -m 0700 "$output_dir"
source_dir=$(mktemp -d "${TMPDIR:-/tmp}/too415-rollback-source.XXXXXX")
server_pid=
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf -- "$source_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

git -C "$repo_root" archive "$old_ref" | tar -x -C "$source_dir"
old_server=$output_dir/orchestrator-old
old_cli=$output_dir/orch-old
(
  cd "$source_dir"
  go build -mod=readonly -o "$old_server" ./cmd/orchestrator
)
(
  cd "$source_dir/tools/orch-cli"
  go build -mod=readonly -o "$old_cli" .
)

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
http_port=$(free_port)
raft_port=$(free_port)
while [[ "$raft_port" == "$http_port" ]]; do raft_port=$(free_port); done
endpoint=http://127.0.0.1:$http_port
live_dir=$output_dir/live
rollback_dir=$output_dir/rollback-set
upgraded_dir=$output_dir/upgraded-live
install -d -m 0700 "$live_dir"

config_file=$live_dir/config.json
jq -n \
  --arg listen "127.0.0.1:$http_port" \
  --arg endpoint "$endpoint" \
  --arg raft "127.0.0.1:$raft_port" \
  --arg data "$live_dir/metadata.sqlite" \
  --arg raft_dir "$live_dir/raft" \
  --arg audit "$live_dir/audit.log" \
  '{
    logging:{debug:false,syslog:{enabled:false}},
    server:{listen:{address:$listen},httpAdvertise:$endpoint},
    raft:{nodeID:"too415-rollback",bind:$raft,advertise:$raft,dataDir:$raft_dir},
    metadata:{type:"sqlite",sqlite:{dataFile:$data}},
    topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},
    audit:{logFile:$audit,toSyslog:false}
  }' >"$config_file"
chmod 0600 "$config_file"

wait_cli() {
  local cli=$1
  for _ in $(seq 1 160); do
    if "$cli" --endpoint "$endpoint" --timeout 3s --output json raft-configuration >/dev/null 2>&1; then
      return 0
    fi
    kill -0 "$server_pid" 2>/dev/null || return 1
    sleep 0.25
  done
  return 1
}
wait_leader() {
  local cli=$1
  for _ in $(seq 1 80); do
    if "$cli" --endpoint "$endpoint" --timeout 3s --output json api leader-check >/dev/null 2>&1; then
      return 0
    fi
    kill -0 "$server_pid" 2>/dev/null || return 1
    sleep 0.25
  done
  return 1
}
stop_server() {
  kill -TERM "$server_pid"
  wait "$server_pid"
  server_pid=
}
read_rule() {
  sqlite3 "$live_dir/metadata.sqlite" "SELECT promotion_rule FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19999;"
}
read_migrations() {
  sqlite3 "$live_dir/metadata.sqlite" "SELECT migration_id FROM orchestrator_schema_migrations ORDER BY migration_id;" | paste -sd, -
}

"$old_server" server --config="$config_file" --discovery=false >"$live_dir/old-server.log" 2>&1 &
server_pid=$!
wait_cli "$old_cli" || { tail -n 120 "$live_dir/old-server.log" >&2; exit 1; }
"$old_cli" --endpoint "$endpoint" --timeout 10s --output json raft-bootstrap >"$live_dir/old-bootstrap.json"
wait_leader "$old_cli"
"$old_cli" --endpoint "$endpoint" --timeout 10s --output json register-candidate \
  -i 127.0.0.1:19999 --promotion-rule prefer >"$live_dir/old-write.json"
[[ $(read_rule) == prefer ]]
old_migrations=$(read_migrations)
[[ "$old_migrations" == *canonical-v1* && "$old_migrations" != *canonical-v2* ]]
stop_server

cp -a "$live_dir" "$rollback_dir"
cp "$old_server" "$rollback_dir/orchestrator"
cp "$old_cli" "$rollback_dir/orch"
(
  cd "$rollback_dir"
  shasum -a 256 orchestrator orch config.json metadata.sqlite >SHA256SUMS
)

"$current_server" admin migrate-metadata-id --config="$config_file" >"$live_dir/migrate.log" 2>&1
upgraded_migrations=$(read_migrations)
[[ "$upgraded_migrations" == *canonical-v2* ]]
"$current_server" server --config="$config_file" --discovery=false >"$live_dir/current-server.log" 2>&1 &
server_pid=$!
wait_cli "$current_cli" || { tail -n 120 "$live_dir/current-server.log" >&2; exit 1; }
wait_leader "$current_cli" || { tail -n 120 "$live_dir/current-server.log" >&2; exit 1; }
"$current_cli" --endpoint "$endpoint" --timeout 10s --output json register-candidate \
  -i 127.0.0.1:19999 --promotion-rule must_not >"$live_dir/current-write.json"
[[ $(read_rule) == must_not ]]
stop_server

mv "$live_dir" "$upgraded_dir"
cp -a "$rollback_dir" "$live_dir"
config_file=$live_dir/config.json
"$live_dir/orchestrator" server --config="$config_file" --discovery=false >"$live_dir/restored-old-server.log" 2>&1 &
server_pid=$!
wait_cli "$live_dir/orch" || { tail -n 120 "$live_dir/restored-old-server.log" >&2; exit 1; }
wait_leader "$live_dir/orch" || { tail -n 120 "$live_dir/restored-old-server.log" >&2; exit 1; }
restored_rule=$(read_rule)
restored_migrations=$(read_migrations)
raft_state=$("$live_dir/orch" --endpoint "$endpoint" --timeout 10s --output json api raft-state | jq -r '.')
[[ "$restored_rule" == prefer ]]
[[ "$restored_migrations" == "$old_migrations" ]]
[[ "$raft_state" == Leader ]]
stop_server

old_commit=$(git -C "$repo_root" rev-parse "$old_ref^{commit}")
printf 'PASS old_commit=%s old_migrations=%s upgraded_migrations=%s restored_migrations=%s restored_rule=%s raft_state=%s\n' \
  "$old_commit" "$old_migrations" "$upgraded_migrations" "$restored_migrations" "$restored_rule" "$raft_state"
