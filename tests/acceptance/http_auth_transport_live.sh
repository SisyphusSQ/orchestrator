#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 1 ]]; then
  echo "usage: http_auth_transport_live.sh OUTPUT_DIR" >&2
  exit 2
fi
output_dir=$1
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
server_binary=$repo_root/bin/orchestrator
cli_binary=$repo_root/bin/orch
[[ -x "$server_binary" && -x "$cli_binary" ]] || { echo "run make build first" >&2; exit 1; }
for tool in curl jq openssl python3 rg sqlite3; do
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
socket_dir=
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  for process_id in "$proxy_pid" "$server_pid"; do
    if [[ -n "$process_id" ]] && kill -0 "$process_id" 2>/dev/null; then
      kill -TERM "$process_id" 2>/dev/null || true
      wait "$process_id" 2>/dev/null || true
    fi
  done
  if [[ -n "$socket_dir" && -d "$socket_dir" ]]; then rm -rf "$socket_dir"; fi
  rm -rf "$runtime_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

start_server() {
  local config_file=$1
  local log_file=$2
  "$server_binary" server --config="$config_file" --discovery=false >"$log_file" 2>&1 &
  server_pid=$!
}

stop_server() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid"
    wait "$server_pid"
  fi
  server_pid=
}

wait_http() {
  local target=$1
  shift
  for attempt in {1..120}; do
    if curl --silent --show-error --fail "$@" "$target" >/dev/null 2>&1; then return 0; fi
    kill -0 "$server_pid" 2>/dev/null || return 1
    sleep 0.25
  done
  return 1
}

expect_cli_failure() {
  local expected_rc=$1
  local output_file=$2
  shift 2
  set +e
  "$@" >"$output_file" 2>&1
  local rc=$?
  set -e
  [[ "$rc" == "$expected_rc" ]] || {
    echo "unexpected CLI exit code: got=$rc want=$expected_rc output=$output_file" >&2
    return 1
  }
}

password=$(openssl rand -hex 24)

# Multi authentication, read-only principal, configuration admin, and hot readOnly.
multi_dir=$runtime_dir/multi
install -d -m 0700 "$multi_dir"
multi_http=$(free_port)
multi_raft=$(free_port)
multi_config=$multi_dir/config.json
multi_reload=$multi_dir/read-only.json
multi_db=$multi_dir/metadata.sqlite
jq -n \
  --arg listen "127.0.0.1:$multi_http" \
  --arg advertise "http://127.0.0.1:$multi_http" \
  --arg raft "127.0.0.1:$multi_raft" \
  --arg raft_dir "$multi_dir/raft" \
  --arg data "$multi_db" \
  --arg password "$password" \
  '{server:{listen:{address:$listen},httpAdvertise:$advertise,urlPrefix:"/orchestrator"},raft:{nodeID:"too415-multi",bind:$raft,advertise:$raft,dataDir:$raft_dir},metadata:{type:"sqlite",sqlite:{dataFile:$data}},topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},authentication:{method:"multi",basic:{user:"writer",password:$password},power:{users:["writer"]},configurationAdmins:{users:["writer"]}},logging:{syslog:{enabled:false}},audit:{toSyslog:false}}' >"$multi_config"
jq -n '{server:{readOnly:true}}' >"$multi_reload"
multi_curl_config=$multi_dir/curl.conf
printf 'user = "writer:%s"\n' "$password" >"$multi_curl_config"
start_server "$multi_config" "$output_dir/multi-server.log"
multi_base=http://127.0.0.1:$multi_http/orchestrator
wait_http "$multi_base/health/live" --config "$multi_curl_config" || { tail -n 160 "$output_dir/multi-server.log" >&2; exit 1; }
[[ $(curl --silent --output /dev/null --write-out '%{http_code}' "$multi_base/health/live") == 401 ]]
[[ $(curl --silent --user readonly:anything --output /dev/null --write-out '%{http_code}' "$multi_base/health/live") == 200 ]]
ORCH_PASSWORD=$password "$cli_binary" --endpoint "$multi_base" --user writer --output json raft-bootstrap >"$output_dir/multi-bootstrap.json"
for attempt in {1..80}; do
  if ORCH_PASSWORD=$password "$cli_binary" --endpoint "$multi_base" --user writer --output json api leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done
ORCH_PASSWORD=$password "$cli_binary" --endpoint "$multi_base" --user writer --output json register-candidate -i 127.0.0.1:19991 --promotion-rule prefer >"$output_dir/multi-writer.json"
[[ $(sqlite3 "$multi_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19991 AND promotion_rule='prefer';") == 1 ]]
curl --silent --show-error --fail --user readonly:anything "$multi_base/api/web-config" >"$output_dir/multi-readonly-web-config.json"
jq -e '.authorizedForAction == false and .authorizedForConfiguration == false' "$output_dir/multi-readonly-web-config.json" >/dev/null
expect_cli_failure 1 "$output_dir/multi-readonly-denied.json" env ORCH_PASSWORD=anything "$cli_binary" --endpoint "$multi_base" --user readonly --output json register-candidate -i 127.0.0.1:19992 --promotion-rule prefer
[[ $(sqlite3 "$multi_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19992;") == 0 ]]
curl --silent --show-error --fail --config "$multi_curl_config" --get --data-urlencode "config=$multi_reload" "$multi_base/api/reload-configuration" >"$output_dir/multi-readonly-reload.json"
jq -e '.Code == "OK"' "$output_dir/multi-readonly-reload.json" >/dev/null
curl --silent --show-error --fail --config "$multi_curl_config" "$multi_base/api/web-config" >"$output_dir/server-readonly-web-config.json"
jq -e '.authorizedForAction == false and .authorizedForConfiguration == false' "$output_dir/server-readonly-web-config.json" >/dev/null
expect_cli_failure 1 "$output_dir/server-readonly-denied.json" env ORCH_PASSWORD="$password" "$cli_binary" --endpoint "$multi_base" --user writer --output json register-candidate -i 127.0.0.1:19993 --promotion-rule prefer
[[ $(sqlite3 "$multi_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19993;") == 0 ]]
stop_server

# Proxy authentication with a controlled proxy that overwrites client identity.
proxy_dir=$runtime_dir/proxy
install -d -m 0700 "$proxy_dir"
proxy_backend_port=$(free_port)
proxy_frontend_port=$(free_port)
proxy_raft=$(free_port)
proxy_config=$proxy_dir/config.json
proxy_db=$proxy_dir/metadata.sqlite
jq -n \
  --arg listen "127.0.0.1:$proxy_backend_port" \
  --arg advertise "http://127.0.0.1:$proxy_backend_port" \
  --arg raft "127.0.0.1:$proxy_raft" \
  --arg raft_dir "$proxy_dir/raft" \
  --arg data "$proxy_db" \
  '{server:{listen:{address:$listen},httpAdvertise:$advertise,urlPrefix:"/orchestrator"},raft:{nodeID:"too415-proxy",bind:$raft,advertise:$raft,dataDir:$raft_dir},metadata:{type:"sqlite",sqlite:{dataFile:$data}},topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},authentication:{method:"proxy",proxy:{userHeader:"X-Forwarded-User"},power:{users:["proxy-writer"]},configurationAdmins:{users:["proxy-writer"]}},logging:{syslog:{enabled:false}},audit:{toSyslog:false}}' >"$proxy_config"
start_server "$proxy_config" "$output_dir/proxy-server.log"
proxy_backend=http://127.0.0.1:$proxy_backend_port/orchestrator
wait_http "$proxy_backend/health/live" || { tail -n 160 "$output_dir/proxy-server.log" >&2; exit 1; }
"$cli_binary" --endpoint "$proxy_backend" --header 'X-Forwarded-User: proxy-writer' --output json raft-bootstrap >"$output_dir/proxy-bootstrap.json"
for attempt in {1..80}; do
  if "$cli_binary" --endpoint "$proxy_backend" --header 'X-Forwarded-User: proxy-writer' --output json api leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done
"$cli_binary" --endpoint "$proxy_backend" --header 'X-Forwarded-User: proxy-writer' --output json register-candidate -i 127.0.0.1:19994 --promotion-rule prefer >"$output_dir/proxy-writer.json"
[[ $(sqlite3 "$proxy_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19994;") == 1 ]]
proxy_program=$proxy_dir/proxy.py
cat >"$proxy_program" <<'PY'
import http.client
import http.server
import sys

frontend_port = int(sys.argv[1])
backend_port = int(sys.argv[2])

class Handler(http.server.BaseHTTPRequestHandler):
    def handle_request(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else None
        headers = {}
        for name, value in self.headers.items():
            if name.lower() not in {"connection", "content-length", "host", "x-forwarded-user"}:
                headers[name] = value
        headers["X-Forwarded-User"] = "proxy-reader"
        connection = http.client.HTTPConnection("127.0.0.1", backend_port, timeout=10)
        connection.request(self.command, self.path, body=body, headers=headers)
        response = connection.getresponse()
        payload = response.read()
        self.send_response(response.status)
        for name, value in response.getheaders():
            if name.lower() not in {"connection", "content-length", "transfer-encoding"}:
                self.send_header(name, value)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(payload)
        connection.close()

    do_GET = handle_request
    do_HEAD = handle_request
    do_POST = handle_request
    do_DELETE = handle_request

    def log_message(self, format, *args):
        return

http.server.ThreadingHTTPServer(("127.0.0.1", frontend_port), Handler).serve_forever()
PY
python3 "$proxy_program" "$proxy_frontend_port" "$proxy_backend_port" >"$output_dir/proxy.log" 2>&1 &
proxy_pid=$!
proxy_frontend=http://127.0.0.1:$proxy_frontend_port/orchestrator
for attempt in {1..80}; do
  if curl --silent --show-error --fail "$proxy_frontend/health/live" >/dev/null 2>&1; then break; fi
  kill -0 "$proxy_pid" 2>/dev/null || { cat "$output_dir/proxy.log" >&2; exit 1; }
  sleep 0.25
done
curl --silent --show-error --fail --header 'X-Forwarded-User: proxy-writer' "$proxy_frontend/api/web-config" >"$output_dir/proxy-stripped-web-config.json"
jq -e '.authorizedForAction == false and .authorizedForConfiguration == false' "$output_dir/proxy-stripped-web-config.json" >/dev/null
expect_cli_failure 1 "$output_dir/proxy-forged-denied.json" "$cli_binary" --endpoint "$proxy_frontend" --header 'X-Forwarded-User: proxy-writer' --output json register-candidate -i 127.0.0.1:19995 --promotion-rule prefer
[[ $(sqlite3 "$proxy_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19995;") == 0 ]]
kill -TERM "$proxy_pid"
wait "$proxy_pid" 2>/dev/null || true
proxy_pid=
stop_server

# Access-token authentication and invalid-token rejection.
token_dir=$runtime_dir/token
install -d -m 0700 "$token_dir"
token_http=$(free_port)
token_raft=$(free_port)
token_config=$token_dir/config.json
token_db=$token_dir/metadata.sqlite
jq -n \
  --arg listen "127.0.0.1:$token_http" \
  --arg advertise "http://127.0.0.1:$token_http" \
  --arg raft "127.0.0.1:$token_raft" \
  --arg raft_dir "$token_dir/raft" \
  --arg data "$token_db" \
  '{server:{listen:{address:$listen},httpAdvertise:$advertise,urlPrefix:"/orchestrator"},raft:{nodeID:"too415-token",bind:$raft,advertise:$raft,dataDir:$raft_dir},metadata:{type:"sqlite",sqlite:{dataFile:$data}},topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},authentication:{method:"token",accessToken:{useExpirySeconds:60,expiryMinutes:60}},logging:{syslog:{enabled:false}},audit:{toSyslog:false}}' >"$token_config"
start_server "$token_config" "$output_dir/token-server.log"
token_base=http://127.0.0.1:$token_http/orchestrator
wait_http "$token_base/health/live" || { tail -n 160 "$output_dir/token-server.log" >&2; exit 1; }
public_token=$("$server_binary" admin access-token --owner too415 --config="$token_config" 2>"$output_dir/token-issue.log")
cookie_jar=$token_dir/cookies.txt
curl --silent --show-error --fail --cookie-jar "$cookie_jar" "$token_base/web/access-token?publicToken=$public_token" >/dev/null
access_token=$(awk '$6 == "access-token" {print $7}' "$cookie_jar")
[[ "$access_token" == "$public_token:"* && "$access_token" != "$public_token:" ]]
ORCH_TOKEN=$access_token "$cli_binary" --endpoint "$token_base" --output json raft-bootstrap >"$output_dir/token-bootstrap.json"
for attempt in {1..80}; do
  if ORCH_TOKEN=$access_token "$cli_binary" --endpoint "$token_base" --output json api leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done
ORCH_TOKEN=$access_token "$cli_binary" --endpoint "$token_base" --output json register-candidate -i 127.0.0.1:19996 --promotion-rule prefer >"$output_dir/token-writer.json"
[[ $(sqlite3 "$token_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19996;") == 1 ]]
curl --silent --show-error --fail --cookie "$cookie_jar" "$token_base/api/web-config" >"$output_dir/token-web-config.json"
jq -e '.authorizedForAction == true and .authorizedForConfiguration == false' "$output_dir/token-web-config.json" >/dev/null
expect_cli_failure 1 "$output_dir/token-invalid-denied.json" env ORCH_TOKEN=invalid:invalid "$cli_binary" --endpoint "$token_base" --output json register-candidate -i 127.0.0.1:19997 --promotion-rule prefer
[[ $(sqlite3 "$token_db" "SELECT COUNT(*) FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19997;") == 0 ]]
stop_server

# Mutual TLS, OU authorization, custom status bypass, and CLI certificate verification.
tls_dir=$runtime_dir/mtls
install -d -m 0700 "$tls_dir"
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 -subj '/CN=too415-ca' -keyout "$tls_dir/ca.key" -out "$tls_dir/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj '/CN=localhost/OU=server' -keyout "$tls_dir/server.key" -out "$tls_dir/server.csr" >/dev/null 2>&1
printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' >"$tls_dir/server.ext"
openssl x509 -req -sha256 -days 1 -in "$tls_dir/server.csr" -CA "$tls_dir/ca.crt" -CAkey "$tls_dir/ca.key" -CAcreateserial -extfile "$tls_dir/server.ext" -out "$tls_dir/server.crt" >/dev/null 2>&1
for identity in allowed denied; do
  openssl req -newkey rsa:2048 -nodes -sha256 -subj "/CN=$identity/OU=$identity" -keyout "$tls_dir/$identity.key" -out "$tls_dir/$identity.csr" >/dev/null 2>&1
  printf 'extendedKeyUsage=clientAuth\n' >"$tls_dir/$identity.ext"
  openssl x509 -req -sha256 -days 1 -in "$tls_dir/$identity.csr" -CA "$tls_dir/ca.crt" -CAkey "$tls_dir/ca.key" -CAcreateserial -extfile "$tls_dir/$identity.ext" -out "$tls_dir/$identity.crt" >/dev/null 2>&1
done
tls_http=$(free_port)
tls_raft=$(free_port)
tls_config=$tls_dir/config.json
jq -n \
  --arg listen "127.0.0.1:$tls_http" \
  --arg advertise "https://localhost:$tls_http" \
  --arg raft "127.0.0.1:$tls_raft" \
  --arg raft_dir "$tls_dir/raft" \
  --arg data "$tls_dir/metadata.sqlite" \
  --arg ca "$tls_dir/ca.crt" \
  --arg cert "$tls_dir/server.crt" \
  --arg key "$tls_dir/server.key" \
  '{server:{listen:{address:$listen},httpAdvertise:$advertise,urlPrefix:"/orchestrator",tls:{enabled:true,mutualTLS:true,skipVerify:false,privateKeyFile:$key,certFile:$cert,caFile:$ca,validOUs:["allowed"]},status:{endpoint:"/too415-status",verifyOU:false}},raft:{nodeID:"too415-mtls",bind:$raft,advertise:$raft,dataDir:$raft_dir},metadata:{type:"sqlite",sqlite:{dataFile:$data}},topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},logging:{syslog:{enabled:false}},audit:{toSyslog:false}}' >"$tls_config"
start_server "$tls_config" "$output_dir/mtls-server.log"
tls_base=https://localhost:$tls_http/orchestrator
wait_http "https://localhost:$tls_http/too415-status" --cacert "$tls_dir/ca.crt" || { tail -n 160 "$output_dir/mtls-server.log" >&2; exit 1; }
[[ $(curl --silent --cacert "$tls_dir/ca.crt" --output /dev/null --write-out '%{http_code}' "$tls_base/health/live") == 401 ]]
[[ $(curl --silent --cacert "$tls_dir/ca.crt" --cert "$tls_dir/denied.crt" --key "$tls_dir/denied.key" --output /dev/null --write-out '%{http_code}' "$tls_base/health/live") == 401 ]]
[[ $(curl --silent --cacert "$tls_dir/ca.crt" --cert "$tls_dir/allowed.crt" --key "$tls_dir/allowed.key" --output /dev/null --write-out '%{http_code}' "$tls_base/health/live") == 200 ]]
[[ $(curl --silent --cacert "$tls_dir/ca.crt" --cert "$tls_dir/denied.crt" --key "$tls_dir/denied.key" --output /dev/null --write-out '%{http_code}' "https://localhost:$tls_http/too415-status") == 200 ]]
[[ $(curl --silent --cacert "$tls_dir/ca.crt" --cert "$tls_dir/allowed.crt" --key "$tls_dir/allowed.key" --output /dev/null --write-out '%{http_code}' "$tls_base/api/status") == 404 ]]
ORCH_CA="$tls_dir/ca.crt" ORCH_CERT="$tls_dir/allowed.crt" ORCH_KEY="$tls_dir/allowed.key" "$cli_binary" --endpoint "$tls_base" --output json raft-bootstrap >"$output_dir/mtls-bootstrap.json"
for attempt in {1..80}; do
  if ORCH_CA="$tls_dir/ca.crt" ORCH_CERT="$tls_dir/allowed.crt" ORCH_KEY="$tls_dir/allowed.key" "$cli_binary" --endpoint "$tls_base" --output json api leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done
ORCH_CA="$tls_dir/ca.crt" ORCH_CERT="$tls_dir/allowed.crt" ORCH_KEY="$tls_dir/allowed.key" "$cli_binary" --endpoint "$tls_base" --output json api leader-check >"$output_dir/mtls-cli-leader.json"
"$cli_binary" --help >"$output_dir/orch-help.txt"
rg -q -- '--ca' "$output_dir/orch-help.txt"
if rg -qi -- 'skip.verify|insecure' "$output_dir/orch-help.txt"; then
  echo "CLI unexpectedly exposes TLS verification bypass" >&2
  exit 1
fi
stop_server

# Production Unix socket, custom status, debug, metrics, prefix, and embedded Web.
unix_dir=$runtime_dir/unix
install -d -m 0700 "$unix_dir"
socket_dir=$(mktemp -d /tmp/too415-unix.XXXXXX)
chmod 0700 "$socket_dir"
unix_socket=$socket_dir/orchestrator.sock
unix_raft=$(free_port)
unix_config=$unix_dir/config.json
jq -n \
  --arg socket "$unix_socket" \
  --arg raft "127.0.0.1:$unix_raft" \
  --arg raft_dir "$unix_dir/raft" \
  --arg data "$unix_dir/metadata.sqlite" \
  '{server:{listen:{socket:$socket},httpAdvertise:"http://unix:80",urlPrefix:"/orchestrator",status:{endpoint:"/too415-unix-status"}},raft:{nodeID:"too415-unix",bind:$raft,advertise:$raft,dataDir:$raft_dir},metadata:{type:"sqlite",sqlite:{dataFile:$data}},topology:{hostname:{resolveMethod:"none",mysqlResolveMethod:"none"},discovery:{pollSeconds:60}},logging:{syslog:{enabled:false}},audit:{toSyslog:false}}' >"$unix_config"
start_server "$unix_config" "$output_dir/unix-server.log"
for attempt in {1..120}; do
  if [[ -S "$unix_socket" ]] && curl --silent --show-error --fail --unix-socket "$unix_socket" http://localhost/orchestrator/health/live >/dev/null 2>&1; then break; fi
  kill -0 "$server_pid" 2>/dev/null || { tail -n 160 "$output_dir/unix-server.log" >&2; exit 1; }
  sleep 0.25
done
curl --silent --show-error --fail --unix-socket "$unix_socket" --request POST http://localhost/orchestrator/api/raft/bootstrap >"$output_dir/unix-bootstrap.json"
jq -e '.Code == "OK"' "$output_dir/unix-bootstrap.json" >/dev/null
for attempt in {1..80}; do
  if curl --silent --show-error --fail --unix-socket "$unix_socket" http://localhost/orchestrator/api/leader-check >/dev/null 2>&1; then break; fi
  sleep 0.25
done
[[ $(curl --silent --unix-socket "$unix_socket" --output /dev/null --write-out '%{http_code}' http://localhost/too415-unix-status) == 200 ]]
[[ $(curl --silent --unix-socket "$unix_socket" --output /dev/null --write-out '%{http_code}' http://localhost/orchestrator/api/status) == 404 ]]
curl --silent --show-error --fail --unix-socket "$unix_socket" http://localhost/orchestrator/debug/vars >"$output_dir/unix-debug-vars.json"
jq -e 'type == "object"' "$output_dir/unix-debug-vars.json" >/dev/null
curl --silent --show-error --fail --unix-socket "$unix_socket" http://localhost/orchestrator/metrics >"$output_dir/unix-metrics.txt"
rg -q '^# HELP (go_|process_|orchestrator_)' "$output_dir/unix-metrics.txt"
curl --silent --show-error --fail --unix-socket "$unix_socket" http://localhost/orchestrator/web/audit/2 >"$output_dir/unix-web.html"
rg -q '<div id="root"></div>' "$output_dir/unix-web.html"
stop_server
rm -rf "$socket_dir"
socket_dir=

jq -n '{status:"PASS",multi:{writer:true,readonlyDenied:true,serverReadOnlyDenied:true},proxy:{trustedWriter:true,forgedHeaderStripped:true},token:{valid:true,invalidDenied:true,configurationAdmin:false},mtls:{validOU:true,invalidOURejected:true,statusBypass:true,cliVerified:true},unix:{socket:true,customStatus:true,debug:true,metrics:true,embeddedWeb:true}}' >"$output_dir/result.json"
rm -rf "$runtime_dir"
trap - EXIT INT TERM
jq -r '"PASS multi=writer+readonly+server-readonly proxy=forged-header-stripped token=valid+invalid-denied mtls=valid-ou+invalid-ou-denied+cli-verified unix=status+debug+metrics+web"' "$output_dir/result.json"
