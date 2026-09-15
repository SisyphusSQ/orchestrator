#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: $0 <start|stop|status> <consul-binary> <state-directory>" >&2
  exit 64
}

[[ $# -eq 3 ]] || usage

action=$1
consul_binary=$2
state_directory=$3

[[ -x "$consul_binary" ]] || { echo "consul binary is not executable: $consul_binary" >&2; exit 65; }
mkdir -p "$state_directory"

http_ports=(18501 18502 18503)
dns_ports=(18601 18602 18603)
lan_ports=(18301 18311 18321)
wan_ports=(18302 18312 18322)
server_ports=(18300 18310 18320)

stop_node() {
  local index=$1
  local node_directory="$state_directory/node-$index"
  local pid_file="$node_directory/consul.pid"
  [[ -f "$pid_file" ]] || return 0
  local pid
  pid=$(<"$pid_file")
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    for _ in $(seq 1 100); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    kill -0 "$pid" 2>/dev/null && { echo "consul node $index did not stop" >&2; return 1; }
  fi
  rm -f "$pid_file"
}

case "$action" in
  start)
    for index in 1 2 3; do
      node_directory="$state_directory/node-$index"
      [[ ! -e "$node_directory/consul.pid" ]] || { echo "pid file already exists: $node_directory/consul.pid" >&2; exit 66; }
      mkdir -p "$node_directory/data"
      offset=$((index - 1))
      args=(
        agent -server -bootstrap-expect=3
        -datacenter=too415-dc1
        -node="too415-consul-$index"
        -node-id="00000000-0000-0000-0000-00000000041$index"
        -data-dir="$node_directory/data"
        -bind=127.0.0.1 -advertise=127.0.0.1 -client=127.0.0.1
        -http-port="${http_ports[$offset]}" -dns-port="${dns_ports[$offset]}"
        -serf-lan-port="${lan_ports[$offset]}" -serf-wan-port="${wan_ports[$offset]}"
        -server-port="${server_ports[$offset]}"
        -grpc-port=-1 -grpc-tls-port=-1 -ui=false
      )
      if [[ $index -gt 1 ]]; then
        args+=(-retry-join=127.0.0.1:18301)
      fi
      "$consul_binary" "${args[@]}" >"$node_directory/consul.log" 2>&1 &
      echo $! >"$node_directory/consul.pid"
    done
    ready=
    for _ in $(seq 1 300); do
      leader=$(curl --connect-timeout 0.2 --max-time 1 --fail --silent http://127.0.0.1:18501/v1/status/leader 2>/dev/null || true)
      peers=$(curl --connect-timeout 0.2 --max-time 1 --fail --silent http://127.0.0.1:18501/v1/status/peers 2>/dev/null || true)
      if [[ "$leader" != '""' && $(grep -o '127.0.0.1:183' <<<"$peers" | wc -l | tr -d ' ') == 3 ]]; then
        ready=1
        break
      fi
      sleep 0.1
    done
    if [[ -z "$ready" ]]; then
      for index in 1 2 3; do tail -n 80 "$state_directory/node-$index/consul.log" >&2 || true; done
      exit 67
    fi
    "$consul_binary" members -http-addr=http://127.0.0.1:18501
    ;;
  stop)
    for index in 3 2 1; do stop_node "$index"; done
    ;;
  status)
    for index in 1 2 3; do
      offset=$((index - 1))
      curl --connect-timeout 1 --max-time 2 --fail --silent "http://127.0.0.1:${http_ports[$offset]}/v1/agent/self" >/dev/null
      echo "node-$index 127.0.0.1:${http_ports[$offset]} ready"
    done
    ;;
  *)
    usage
    ;;
esac
