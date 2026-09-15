#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 2 ]]; then
  echo "usage: consul_secure_three_live.sh CONSUL_BINARY OUTPUT_DIR" >&2
  exit 2
fi
consul_binary=$1
output_dir=$2
[[ -x "$consul_binary" ]] || { echo "Consul binary is not executable: $consul_binary" >&2; exit 2; }
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
for tool in curl go jq openssl python3 rg; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done

umask 077
install -d -m 0700 "$output_dir"
runtime_dir=$output_dir/runtime
[[ ! -e "$runtime_dir" ]] || { echo "runtime directory already exists: $runtime_dir" >&2; exit 1; }
install -d -m 0700 "$runtime_dir/certs"

declare -a consul_pids=()
management_token=
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
  for index in 1 2 3; do
    source_log=$runtime_dir/node-$index/consul.log
    target_log=$output_dir/node-$index.log
    if [[ -f "$source_log" ]]; then
      if [[ -n "$management_token" ]]; then
        sed "s/$management_token/[REDACTED]/g" "$source_log" >"$target_log"
      else
        cp "$source_log" "$target_log"
      fi
    fi
  done
  rm -rf "$runtime_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

free_ports=$(python3 - <<'PY'
import socket

sockets = []
ports = []
for _ in range(15):
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
[[ ${#ports[@]} == 15 ]] || { echo "failed to allocate 15 ports" >&2; exit 1; }

cert_dir=$runtime_dir/certs
(
  cd "$cert_dir"
  "$consul_binary" tls ca create -domain=consul >"$output_dir/tls-ca-create.log"
  for index in 1 2 3; do
    "$consul_binary" tls cert create -server -dc=too415-dc1 -node="too415-consul-$index" >>"$output_dir/tls-server-cert-create.log"
  done
  "$consul_binary" tls cert create -client >>"$output_dir/tls-client-cert-create.log"
)
ca_file=$cert_dir/consul-agent-ca.pem
declare -a server_certificates=() server_keys=()
while IFS= read -r certificate_file; do
  server_certificates+=("$certificate_file")
done < <(find "$cert_dir" -maxdepth 1 -type f -name 'too415-dc1-server-consul-*.pem' ! -name '*-key.pem' | sort)
while IFS= read -r private_key_file; do
  server_keys+=("$private_key_file")
done < <(find "$cert_dir" -maxdepth 1 -type f -name 'too415-dc1-server-consul-*-key.pem' | sort)
client_certificate=$(find "$cert_dir" -maxdepth 1 -type f -name '*-client-consul-*.pem' ! -name '*-key.pem' | head -1)
client_key=$(find "$cert_dir" -maxdepth 1 -type f -name '*-client-consul-*-key.pem' | head -1)
[[ -f "$ca_file" && ${#server_certificates[@]} == 3 && ${#server_keys[@]} == 3 && -f "$client_certificate" && -f "$client_key" ]] || {
  echo "generated Consul TLS files are incomplete" >&2
  exit 1
}

management_token=$(python3 -c 'import uuid; print(uuid.uuid4())')
token_file=$runtime_dir/management-token
printf '%s\n' "$management_token" >"$token_file"
chmod 0600 "$token_file"

declare -a https_ports dns_ports lan_ports wan_ports server_ports
for index in 0 1 2; do
  https_ports[$index]=${ports[$index]}
  dns_ports[$index]=${ports[$((index + 3))]}
  lan_ports[$index]=${ports[$((index + 6))]}
  wan_ports[$index]=${ports[$((index + 9))]}
  server_ports[$index]=${ports[$((index + 12))]}
done

for index in 1 2 3; do
  offset=$((index - 1))
  node_dir=$runtime_dir/node-$index
  install -d -m 0700 "$node_dir/data"
  config_file=$node_dir/config.json
  retry_join='[]'
  if (( index > 1 )); then
    retry_join=$(jq -cn --arg target "127.0.0.1:${lan_ports[0]}" '[$target]')
  fi
  jq -n \
    --arg node "too415-consul-$index" \
    --arg node_id "00000000-0000-0000-0000-00000000051$index" \
    --arg data_dir "$node_dir/data" \
    --arg ca_file "$ca_file" \
    --arg cert_file "${server_certificates[$offset]}" \
    --arg key_file "${server_keys[$offset]}" \
    --arg token "$management_token" \
    --argjson retry_join "$retry_join" \
    --argjson https_port "${https_ports[$offset]}" \
    --argjson dns_port "${dns_ports[$offset]}" \
    --argjson lan_port "${lan_ports[$offset]}" \
    --argjson wan_port "${wan_ports[$offset]}" \
    --argjson server_port "${server_ports[$offset]}" \
    '{server:true,bootstrap_expect:3,datacenter:"too415-dc1",node_name:$node,node_id:$node_id,data_dir:$data_dir,bind_addr:"127.0.0.1",advertise_addr:"127.0.0.1",client_addr:"127.0.0.1",retry_join:$retry_join,ports:{http:-1,https:$https_port,dns:$dns_port,serf_lan:$lan_port,serf_wan:$wan_port,server:$server_port,grpc:-1,grpc_tls:-1},tls:{defaults:{ca_file:$ca_file,cert_file:$cert_file,key_file:$key_file,verify_incoming:true,verify_outgoing:true,verify_server_hostname:true},internal_rpc:{verify_server_hostname:true}},acl:{enabled:true,default_policy:"deny",enable_token_persistence:true,tokens:{initial_management:$token}}}' >"$config_file"
  "$consul_binary" validate "$config_file" >>"$output_dir/config-validate.log"
done

for index in 1 2 3; do
  node_dir=$runtime_dir/node-$index
  "$consul_binary" agent -config-file="$node_dir/config.json" >"$node_dir/consul.log" 2>&1 &
  consul_pids+=("$!")
done

leader_ready=
for attempt in {1..360}; do
  if CONSUL_HTTP_ADDR="https://127.0.0.1:${https_ports[0]}" \
     CONSUL_CACERT="$ca_file" \
     CONSUL_CLIENT_CERT="$client_certificate" \
     CONSUL_CLIENT_KEY="$client_key" \
     CONSUL_HTTP_TOKEN_FILE="$token_file" \
     CONSUL_TLS_SERVER_NAME=server.too415-dc1.consul \
       "$consul_binary" operator raft list-peers >"$output_dir/raft-peers.txt" 2>/dev/null; then
    leader_ready=1
    break
  fi
  for process_id in "${consul_pids[@]}"; do
    kill -0 "$process_id" 2>/dev/null || { echo "Consul exited before secure cluster became ready" >&2; exit 1; }
  done
  sleep 0.25
done
[[ -n "$leader_ready" ]] || { echo "secure Consul cluster did not elect three voters" >&2; exit 1; }

set +e
curl --silent --show-error --fail --cacert "$ca_file" "https://127.0.0.1:${https_ports[0]}/v1/status/leader" >"$output_dir/no-client-cert.out" 2>"$output_dir/no-client-cert.err"
no_certificate_rc=$?
set -e
[[ $no_certificate_rc != 0 ]] || { echo "HTTPS request without client certificate unexpectedly passed" >&2; exit 1; }

(
  cd "$repo_root"
  ORCH_CONSUL_E2E_ADDRESSES="127.0.0.1:${https_ports[0]},127.0.0.1:${https_ports[1]},127.0.0.1:${https_ports[2]}" \
  ORCH_CONSUL_E2E_SCHEME=https \
  ORCH_CONSUL_E2E_DATACENTER=too415-dc1 \
  ORCH_CONSUL_E2E_TOKEN_FILE="$token_file" \
  ORCH_CONSUL_E2E_CA_FILE="$ca_file" \
  ORCH_CONSUL_E2E_CERT_FILE="$client_certificate" \
  ORCH_CONSUL_E2E_KEY_FILE="$client_key" \
  ORCH_CONSUL_E2E_SERVER_NAME=server.too415-dc1.consul \
    go test -mod=readonly ./internal/kv -run '^TestConsulExternalThreeServerLifecycle$' -count=1 -v
) >"$output_dir/go-client-e2e.log" 2>&1

jq -n \
  --arg status PASS \
  --arg consulVersion "$("$consul_binary" version -format=json | jq -r '.Version')" \
  --argjson voters 3 \
  --argjson tlsMutual true \
  --argjson anonymousDenied true \
  --argjson prefixAllowed true \
  --argjson outsidePrefixDenied true \
  '{status:$status,consulVersion:$consulVersion,voters:$voters,tlsMutual:$tlsMutual,anonymousDenied:$anonymousDenied,prefixAllowed:$prefixAllowed,outsidePrefixDenied:$outsidePrefixDenied}' >"$output_dir/result.json"

secret_leak=
while IFS= read -r retained_file; do
  if rg --quiet --fixed-strings "$management_token" "$retained_file"; then
    printf '%s\n' "$retained_file" >>"$runtime_dir/secret-files.txt"
    secret_leak=1
  fi
done < <(find "$output_dir" -maxdepth 1 -type f -print)
if [[ -n "$secret_leak" ]]; then
  echo "management token leaked into retained evidence" >&2
  exit 1
fi

echo "PASS voters=3 tls=verified-mtls acl=anonymous-denied prefix=allowed outside-prefix=denied"
