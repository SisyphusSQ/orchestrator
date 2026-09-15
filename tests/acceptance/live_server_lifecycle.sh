#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 1 ]]; then
  echo "usage: live_server_lifecycle.sh OUTPUT_DIR" >&2
  exit 2
fi
output_dir=$1
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
server_binary=$repo_root/bin/orchestrator
cli_binary=$repo_root/bin/orch
[[ -x "$server_binary" && -x "$cli_binary" ]] || { echo "run make build first" >&2; exit 1; }
for tool in curl jq openssl python3 rg; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done

umask 077
install -d -m 0700 "$output_dir"
free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
http_port=$(free_port)
raft_port=$(free_port)
otlp_port=$(free_port)
agent_port=$(free_port)
while [[ "$raft_port" == "$http_port" ]]; do raft_port=$(free_port); done
while [[ "$otlp_port" == "$http_port" || "$otlp_port" == "$raft_port" ]]; do otlp_port=$(free_port); done
while [[ "$agent_port" == "$http_port" || "$agent_port" == "$raft_port" || "$agent_port" == "$otlp_port" ]]; do agent_port=$(free_port); done

password=$(openssl rand -hex 24)
cert_file=$output_dir/server.crt
key_file=$output_dir/server.key
config_file=$output_dir/config.json
reload_file=$output_dir/reload.json
receiver_ready=$output_dir/otlp.ready
receiver_log=$output_dir/otlp.log
receiver_binary=$output_dir/otlp-receiver
server_log=$output_dir/server.log
audit_log=$output_dir/audit.log

go build -o "$receiver_binary" "$repo_root/tests/acceptance/otlp_receiver"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=localhost \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' \
  -keyout "$key_file" -out "$cert_file" >/dev/null 2>&1
chmod 0600 "$key_file" "$cert_file"

jq -n \
  --arg listen "127.0.0.1:$http_port" \
  --arg advertise "https://localhost:$http_port" \
  --arg cert "$cert_file" \
  --arg key "$key_file" \
  --arg data "$output_dir/metadata.sqlite" \
  --arg raft "127.0.0.1:$raft_port" \
  --arg raft_dir "$output_dir/raft" \
  --arg agent_listen "127.0.0.1:$agent_port" \
  --arg password "$password" \
  --arg audit "$audit_log" \
  --arg otlp "http://127.0.0.1:$otlp_port/v1/traces" \
  '{
    observability:{tracing:{endpoint:$otlp,sampleRatio:1}},
    logging:{debug:false,syslog:{enabled:false}},
    server:{listen:{address:$listen},httpAdvertise:$advertise,urlPrefix:"/orchestrator",tls:{enabled:true,mutualTLS:false,skipVerify:false,privateKeyFile:$key,certFile:$cert,caFile:""},web:{message:"TOO-415 initial"}},
    raft:{nodeID:"too415-live",bind:$raft,advertise:$raft,dataDir:$raft_dir},
    metadata:{type:"sqlite",sqlite:{dataFile:$data}},
    topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},
    authentication:{method:"basic",basic:{user:"too415",password:$password},power:{users:["too415"]},configurationAdmins:{users:["too415"]}},
    agents:{serveHTTP:true,serverPort:$agent_listen,pollMinutes:60,unseenForgetHours:6,staleSeedFailMinutes:60,tls:{enabled:true,mutualTLS:false,skipVerify:false,privateKeyFile:$key,certFile:$cert,caFile:""}},
    audit:{logFile:$audit,toSyslog:false,toBackend:true}
  }' >"$config_file"
jq -n '{server:{web:{message:"TOO-415 reloaded"}}}' >"$reload_file"
chmod 0600 "$config_file" "$reload_file"

"$receiver_binary" --listen "127.0.0.1:$otlp_port" --ready-file "$receiver_ready" >"$receiver_log" 2>&1 &
receiver_pid=$!
server_pid=
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if kill -0 "$receiver_pid" 2>/dev/null; then
    kill -TERM "$receiver_pid" 2>/dev/null || true
    wait "$receiver_pid" 2>/dev/null || true
  fi
  exit "$rc"
}
trap cleanup EXIT INT TERM

for attempt in {1..60}; do
  [[ -s "$receiver_ready" ]] && break
  kill -0 "$receiver_pid" 2>/dev/null || { echo "OTLP receiver exited" >&2; exit 1; }
  sleep 0.25
done
[[ -s "$receiver_ready" ]] || { echo "OTLP receiver was not ready" >&2; exit 1; }

"$server_binary" server --config="$config_file" --discovery=false >"$server_log" 2>&1 &
server_pid=$!
base=https://localhost:$http_port/orchestrator
for attempt in {1..120}; do
  if curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/health/live" >/dev/null 2>&1; then
    break
  fi
  kill -0 "$server_pid" 2>/dev/null || { tail -n 120 "$server_log" >&2; exit 1; }
  sleep 0.25
done

unauthorized_code=$(curl --silent --cacert "$cert_file" --output /dev/null --write-out '%{http_code}' "$base/health/live")
[[ "$unauthorized_code" == 401 ]] || { echo "unauthenticated health status=$unauthorized_code" >&2; exit 1; }
if curl --silent --show-error --fail https://localhost:"$http_port"/orchestrator/health/live >/dev/null 2>&1; then
  echo "untrusted self-signed certificate unexpectedly succeeded" >&2
  exit 1
fi

export ORCH_PASSWORD=$password
cli_args=(--endpoint "$base" --user too415 --ca "$cert_file" --timeout 15s --output json)
"$cli_binary" "${cli_args[@]}" raft-bootstrap >"$output_dir/raft-bootstrap.json"
for attempt in {1..80}; do
  if "$cli_binary" "${cli_args[@]}" api leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done
"$cli_binary" "${cli_args[@]}" api leader-check >"$output_dir/leader-check.json"

agent_base=https://localhost:$agent_port/orchestrator
agent_ready=0
for attempt in {1..80}; do
  if curl --silent --show-error --fail --cacert "$cert_file" "$agent_base/api/agent-ping" >"$output_dir/agent-ping.json" 2>/dev/null; then
    agent_ready=1
    break
  fi
  sleep 0.25
done
(( agent_ready == 1 )) || { echo "agent listener did not become ready" >&2; exit 1; }
jq -e '. == "OK"' "$output_dir/agent-ping.json" >/dev/null
agent_token=$(openssl rand -hex 24)
curl --silent --show-error --fail --cacert "$cert_file" "$agent_base/api/submit-agent/127.0.0.1/$agent_port/$agent_token" >"$output_dir/agent-submit.json"
jq -e '. == "127.0.0.1"' "$output_dir/agent-submit.json" >/dev/null
curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/api/agents" >"$output_dir/agents.json"
jq -e 'any(.[]; .Hostname == "127.0.0.1")' "$output_dir/agents.json" >/dev/null
if rg -q "$agent_token" "$output_dir/agents.json"; then
  echo "agent token leaked through management API" >&2
  exit 1
fi

curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" --request POST \
  "$base/api/agent-seed/missing-target.invalid/missing-source.invalid" >"$output_dir/seed-submit.json"
seed_id=$(jq -r 'if type == "number" then . else (.Details // empty) end' "$output_dir/seed-submit.json")
[[ "$seed_id" =~ ^[1-9][0-9]*$ ]] || { echo "invalid seed id" >&2; exit 1; }
seed_complete=0
for attempt in {1..80}; do
  curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/api/agent-seed-details/$seed_id" >"$output_dir/seed-details.json"
  if jq -e 'length == 1 and .[0].IsComplete == true and .[0].IsSuccessful == false' "$output_dir/seed-details.json" >/dev/null; then
    seed_complete=1
    break
  fi
  sleep 0.25
done
(( seed_complete == 1 )) || { echo "missing-agent seed did not fail closed" >&2; exit 1; }
curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/api/agent-seed-states/$seed_id" >"$output_dir/seed-states.json"
jq -e 'length >= 1 and any(.[]; (.ErrorMessage | length) > 0)' "$output_dir/seed-states.json" >/dev/null

curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/metrics" >"$output_dir/metrics.txt"
rg -q '^# HELP (go_|process_|orchestrator_)' "$output_dir/metrics.txt"
ready=0
for attempt in {1..80}; do
  if curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/health/ready" >"$output_dir/health-ready.json" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.25
done
(( ready == 1 )) || { echo "health readiness did not converge" >&2; exit 1; }
wrong_prefix_code=$(curl --silent --cacert "$cert_file" --user "too415:$password" --output /dev/null --write-out '%{http_code}' "https://localhost:$http_port/metrics")
[[ "$wrong_prefix_code" == 404 ]] || { echo "unprefixed metrics status=$wrong_prefix_code" >&2; exit 1; }
curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/web/audit/2" >"$output_dir/web-deep-link.html"
rg -q '<div id="root"></div>' "$output_dir/web-deep-link.html"

curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" --get \
  --data-urlencode "config=$reload_file" "$base/api/reload-configuration" >"$output_dir/reload.json.response"
jq -e '.Code == "OK"' "$output_dir/reload.json.response" >/dev/null
curl --silent --show-error --fail --cacert "$cert_file" --user "too415:$password" "$base/api/web-config" >"$output_dir/web-config.json"
jq -e '.webMessage == "TOO-415 reloaded" and .urlPrefix == "/orchestrator" and .authorizedForAction == true and .authorizedForConfiguration == true' "$output_dir/web-config.json" >/dev/null

kill -TERM "$server_pid"
wait "$server_pid"
server_pid=
for attempt in {1..40}; do
  stats=$(curl --silent --show-error --fail "http://127.0.0.1:$otlp_port/stats")
  if [[ $(jq -r '.batches' <<<"$stats") -gt 0 && $(jq -r '.bytes' <<<"$stats") -gt 0 ]]; then break; fi
  sleep 0.25
done
printf '%s\n' "$stats" >"$output_dir/otlp-stats.json"
jq -e '.batches > 0 and .bytes > 0' "$output_dir/otlp-stats.json" >/dev/null

kill -TERM "$receiver_pid"
wait "$receiver_pid"
trap - EXIT INT TERM
printf 'PASS tls=verified auth=basic prefix=/orchestrator agent_listener=tls agent_token=redacted seed=fail_closed metrics=prometheus reload=readback shutdown=graceful otlp_batches=%s otlp_bytes=%s\n' \
  "$(jq -r '.batches' "$output_dir/otlp-stats.json")" "$(jq -r '.bytes' "$output_dir/otlp-stats.json")"
