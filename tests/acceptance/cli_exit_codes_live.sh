#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 1 ]]; then
  echo "usage: cli_exit_codes_live.sh OUTPUT_DIR" >&2
  exit 2
fi
output_dir=$1
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
server_binary=$repo_root/bin/orchestrator
cli_binary=$repo_root/bin/orch
[[ -x "$server_binary" && -x "$cli_binary" ]] || { echo "run make build first" >&2; exit 1; }
for tool in curl jq nc python3 sqlite3; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done

umask 077
install -d -m 0700 "$output_dir"
runtime_dir=$output_dir/runtime
[[ ! -e "$runtime_dir" ]] || { echo "runtime directory already exists: $runtime_dir" >&2; exit 1; }
install -d -m 0700 "$runtime_dir"

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

server_pid=
proxy_pid=
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  for process_id in "$proxy_pid" "$server_pid"; do
    if [[ -n "$process_id" ]] && kill -0 "$process_id" 2>/dev/null; then
      kill -TERM "$process_id" 2>/dev/null || true
      wait "$process_id" 2>/dev/null || true
    fi
  done
  rm -rf "$runtime_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

http_port=$(free_port)
raft_port=$(free_port)
database_file=$runtime_dir/metadata.sqlite
config_file=$runtime_dir/config.json
jq -n \
  --arg listen "127.0.0.1:$http_port" \
  --arg advertise "http://127.0.0.1:$http_port" \
  --arg raft "127.0.0.1:$raft_port" \
  --arg raft_dir "$runtime_dir/raft" \
  --arg data "$database_file" \
  '{server:{listen:{address:$listen},httpAdvertise:$advertise,urlPrefix:"/orchestrator"},raft:{nodeID:"too415-cli-exit",bind:$raft,advertise:$raft,dataDir:$raft_dir},metadata:{type:"sqlite",sqlite:{dataFile:$data}},topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},logging:{syslog:{enabled:false}},audit:{toSyslog:false}}' >"$config_file"
"$server_binary" server --config="$config_file" --discovery=false >"$output_dir/server.log" 2>&1 &
server_pid=$!
backend=http://127.0.0.1:$http_port/orchestrator
for attempt in {1..120}; do
  if curl --silent --show-error --fail "$backend/health/live" >/dev/null 2>&1; then break; fi
  kill -0 "$server_pid" 2>/dev/null || { tail -n 160 "$output_dir/server.log" >&2; exit 1; }
  sleep 0.25
done
"$cli_binary" --endpoint "$backend" --output json raft-bootstrap >"$output_dir/bootstrap.json"
for attempt in {1..80}; do
  if "$cli_binary" --endpoint "$backend" --output json api leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done

capture_rc() {
  local output_file=$1
  shift
  set +e
  "$@" >"$output_file" 2>&1
  local rc=$?
  set -e
  printf '%s' "$rc"
}

rc0=$(capture_rc "$output_dir/exit-0.json" "$cli_binary" --endpoint "$backend" --output json register-candidate -i 127.0.0.1:20000 --promotion-rule prefer)
[[ "$rc0" == 0 ]]
[[ $(sqlite3 "$database_file" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=20000;") == 1 ]]

rc1=$(capture_rc "$output_dir/exit-1.log" "$cli_binary" --endpoint "$backend" --output json api definitely-missing)
[[ "$rc1" == 1 ]]

rc2=$(capture_rc "$output_dir/exit-2.log" "$cli_binary" --endpoint "$backend" --output yaml clusters)
[[ "$rc2" == 2 ]]

start_fault_proxy() {
  local mode=$1
  local frontend_port=$2
  python3 - "$frontend_port" "$http_port" "$mode" >"$output_dir/proxy-$mode.log" 2>&1 <<'PY' &
import http.client
import http.server
import socket
import sys
import threading

frontend_port = int(sys.argv[1])
backend_port = int(sys.argv[2])
mode = sys.argv[3]

class Handler(http.server.BaseHTTPRequestHandler):
    mutation_count = 0
    lock = threading.Lock()

    def handle_request(self):
        is_mutation = "/api/register-candidate/" in self.path
        if is_mutation:
            with Handler.lock:
                Handler.mutation_count += 1
                mutation_number = Handler.mutation_count
        else:
            mutation_number = 0
        if mode == "partial" and mutation_number == 2:
            payload = b"{}"
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return

        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else None
        headers = {name: value for name, value in self.headers.items() if name.lower() not in {"connection", "content-length", "host"}}
        connection = http.client.HTTPConnection("127.0.0.1", backend_port, timeout=10)
        connection.request(self.command, self.path, body=body, headers=headers)
        response = connection.getresponse()
        payload = response.read()
        response_headers = response.getheaders()
        response_status = response.status
        connection.close()

        if mode == "unknown" and is_mutation:
            self.close_connection = True
            try:
                self.connection.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            self.connection.close()
            return

        self.send_response(response_status)
        for name, value in response_headers:
            if name.lower() not in {"connection", "content-length", "transfer-encoding"}:
                self.send_header(name, value)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(payload)

    do_GET = handle_request
    do_HEAD = handle_request
    do_POST = handle_request
    do_DELETE = handle_request

    def log_message(self, format, *args):
        return

http.server.ThreadingHTTPServer(("127.0.0.1", frontend_port), Handler).serve_forever()
PY
  proxy_pid=$!
  for attempt in {1..80}; do
    if nc -z 127.0.0.1 "$frontend_port" >/dev/null 2>&1; then return 0; fi
    kill -0 "$proxy_pid" 2>/dev/null || return 1
    sleep 0.25
  done
  return 1
}

unknown_port=$(free_port)
start_fault_proxy unknown "$unknown_port"
unknown_endpoint=http://127.0.0.1:$unknown_port/orchestrator
rc3=$(capture_rc "$output_dir/exit-3.log" "$cli_binary" --endpoint "$unknown_endpoint" --output json register-candidate -i 127.0.0.1:20003 --promotion-rule prefer)
[[ "$rc3" == 3 ]]
[[ $(sqlite3 "$database_file" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=20003;") == 1 ]]
kill -TERM "$proxy_pid"
wait "$proxy_pid" 2>/dev/null || true
proxy_pid=

partial_port=$(free_port)
start_fault_proxy partial "$partial_port"
partial_endpoint=http://127.0.0.1:$partial_port/orchestrator
rc4=$(capture_rc "$output_dir/exit-4.log" "$cli_binary" --endpoint "$partial_endpoint" --output json register-candidate -i 127.0.0.1:20004,127.0.0.1:20005 --promotion-rule prefer)
[[ "$rc4" == 4 ]]
[[ $(sqlite3 "$database_file" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=20004;") == 1 ]]
[[ $(sqlite3 "$database_file" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=20005;") == 0 ]]
kill -TERM "$proxy_pid"
wait "$proxy_pid" 2>/dev/null || true
proxy_pid=

kill -TERM "$server_pid"
wait "$server_pid"
server_pid=

jq -n --argjson exit0 "$rc0" --argjson exit1 "$rc1" --argjson exit2 "$rc2" --argjson exit3 "$rc3" --argjson exit4 "$rc4" '{status:"PASS",exitCodes:[$exit0,$exit1,$exit2,$exit3,$exit4],unknownMutationCommitted:true,partialFirstCommitted:true,partialSecondRejected:true,noRetry:true}' >"$output_dir/result.json"
rm -rf "$runtime_dir"
trap - EXIT INT TERM
jq -r '"PASS exit_codes=\(.exitCodes|join(",")) unknown_committed=\(.unknownMutationCommitted) partial=\(.partialFirstCommitted)/\(.partialSecondRejected) no_retry=\(.noRetry)"' "$output_dir/result.json"
