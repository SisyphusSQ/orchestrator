#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 2 ]]; then
  echo "usage: consul_cross_dc_live.sh CONSUL_BINARY OUTPUT_DIR" >&2
  exit 2
fi
consul_binary=$1
output_dir=$2
[[ -x "$consul_binary" ]] || { echo "Consul binary is not executable: $consul_binary" >&2; exit 2; }
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
for tool in curl go jq python3; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done

umask 077
install -d -m 0700 "$output_dir"
runtime_dir=$output_dir/runtime
[[ ! -e "$runtime_dir" ]] || { echo "runtime directory already exists: $runtime_dir" >&2; exit 1; }
install -d -m 0700 "$runtime_dir"

declare -a consul_pids=()
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  for ((index=${#consul_pids[@]}-1; index>=0; index--)); do
    process_id=${consul_pids[$index]}
    if kill -0 "$process_id" 2>/dev/null; then
      kill -TERM "$process_id" 2>/dev/null || true
      wait "$process_id" 2>/dev/null || true
    fi
  done
  for index in 0 1 2 3 4 5; do
    source_log=$runtime_dir/node-$index/consul.log
    [[ -f "$source_log" ]] && cp "$source_log" "$output_dir/node-$index.log"
  done
  rm -rf "$runtime_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

free_ports=$(python3 - <<'PY'
import socket

sockets = []
ports = []
for _ in range(30):
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    sockets.append(sock)
    ports.append(sock.getsockname()[1])
print(" ".join(str(port) for port in ports))
for sock in sockets:
    sock.close()
PY
)
read -r -a ports <<<"$free_ports"
[[ ${#ports[@]} == 30 ]] || { echo "failed to allocate 30 ports" >&2; exit 1; }

declare -a http_ports dns_ports lan_ports wan_ports server_ports
for index in 0 1 2 3 4 5; do
  http_ports[$index]=${ports[$index]}
  dns_ports[$index]=${ports[$((index + 6))]}
  lan_ports[$index]=${ports[$((index + 12))]}
  wan_ports[$index]=${ports[$((index + 18))]}
  server_ports[$index]=${ports[$((index + 24))]}
done

for index in 0 1 2 3 4 5; do
  node_dir=$runtime_dir/node-$index
  install -d -m 0700 "$node_dir/data"
  if (( index < 3 )); then
    datacenter=too415-dc1
    dc_node=$((index + 1))
    first_dc_index=0
    retry_join_wan='[]'
  else
    datacenter=too415-dc2
    dc_node=$((index - 2))
    first_dc_index=3
    retry_join_wan=$(jq -cn --arg target "127.0.0.1:${wan_ports[0]}" '[$target]')
  fi
  retry_join='[]'
  if (( index != first_dc_index )); then
    retry_join=$(jq -cn --arg target "127.0.0.1:${lan_ports[$first_dc_index]}" '[$target]')
  fi
  jq -n \
    --arg dc "$datacenter" \
    --arg node "$datacenter-server-$dc_node" \
    --arg node_id "00000000-0000-0000-0000-00000000061$index" \
    --arg data_dir "$node_dir/data" \
    --argjson retry_join "$retry_join" \
    --argjson retry_join_wan "$retry_join_wan" \
    --argjson http_port "${http_ports[$index]}" \
    --argjson dns_port "${dns_ports[$index]}" \
    --argjson lan_port "${lan_ports[$index]}" \
    --argjson wan_port "${wan_ports[$index]}" \
    --argjson server_port "${server_ports[$index]}" \
    '{server:true,bootstrap_expect:3,datacenter:$dc,node_name:$node,node_id:$node_id,data_dir:$data_dir,bind_addr:"127.0.0.1",advertise_addr:"127.0.0.1",client_addr:"127.0.0.1",retry_join:$retry_join,retry_join_wan:$retry_join_wan,disable_update_check:true,ports:{http:$http_port,https:-1,dns:$dns_port,serf_lan:$lan_port,serf_wan:$wan_port,server:$server_port,grpc:-1,grpc_tls:-1}}' >"$node_dir/config.json"
  "$consul_binary" validate "$node_dir/config.json" >>"$output_dir/config-validate.log"
done

for index in 0 1 2 3 4 5; do
  node_dir=$runtime_dir/node-$index
  "$consul_binary" agent -config-file="$node_dir/config.json" >"$node_dir/consul.log" 2>&1 &
  consul_pids+=("$!")
done

cluster_ready=
for attempt in {1..480}; do
  dc1_peers=$(curl --connect-timeout 0.2 --max-time 1 --fail --silent "http://127.0.0.1:${http_ports[0]}/v1/status/peers" 2>/dev/null || true)
  dc2_peers=$(curl --connect-timeout 0.2 --max-time 1 --fail --silent "http://127.0.0.1:${http_ports[3]}/v1/status/peers" 2>/dev/null || true)
  datacenters=$(curl --connect-timeout 0.2 --max-time 1 --fail --silent "http://127.0.0.1:${http_ports[0]}/v1/catalog/datacenters" 2>/dev/null || true)
  if jq -e 'length == 3' <<<"$dc1_peers" >/dev/null 2>&1 && \
     jq -e 'length == 3' <<<"$dc2_peers" >/dev/null 2>&1 && \
     jq -e 'sort == ["too415-dc1","too415-dc2"]' <<<"$datacenters" >/dev/null 2>&1; then
    printf '%s\n' "$dc1_peers" >"$output_dir/dc1-peers.json"
    printf '%s\n' "$dc2_peers" >"$output_dir/dc2-peers.json"
    printf '%s\n' "$datacenters" >"$output_dir/datacenters.json"
    cluster_ready=1
    break
  fi
  for process_id in "${consul_pids[@]}"; do
    kill -0 "$process_id" 2>/dev/null || { echo "Consul exited before cross-DC cluster became ready" >&2; exit 1; }
  done
  sleep 0.25
done
[[ -n "$cluster_ready" ]] || { echo "two Consul datacenters did not become ready" >&2; exit 1; }

dc1_addresses="127.0.0.1:${http_ports[0]}|127.0.0.1:${http_ports[1]}|127.0.0.1:${http_ports[2]}"
dc2_addresses="127.0.0.1:${http_ports[3]}|127.0.0.1:${http_ports[4]}|127.0.0.1:${http_ports[5]}"
(
  cd "$repo_root"
  ORCH_CONSUL_E2E_CROSS_DC_ADDRESSES="too415-dc1=$dc1_addresses,too415-dc2=$dc2_addresses" \
    go test -mod=readonly ./internal/kv -run '^TestConsulExternalCrossDatacenterDistribution$' -count=1 -v
) >"$output_dir/go-cross-dc-e2e.log" 2>&1

jq -n \
  --arg status PASS \
  --arg consulVersion "$("$consul_binary" version -format=json | jq -r '.Version')" \
  --argjson datacenters 2 \
  --argjson votersPerDatacenter 3 \
  --argjson agentsReadBack 6 \
  --argjson distributedKeys 2 \
  '{status:$status,consulVersion:$consulVersion,datacenters:$datacenters,votersPerDatacenter:$votersPerDatacenter,agentsReadBack:$agentsReadBack,distributedKeys:$distributedKeys}' >"$output_dir/result.json"

echo "PASS datacenters=2 voters_per_dc=3 agents_readback=6 distributed_keys=2"
