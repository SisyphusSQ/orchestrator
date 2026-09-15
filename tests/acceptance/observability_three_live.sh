#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 1 ]]; then
  echo "usage: observability_three_live.sh OUTPUT_DIR" >&2
  exit 2
fi
output_dir=$1
[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
for tool in brew curl grafana go jq prometheus promtool python3; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done
[[ -x "$repo_root/bin/orchestrator" && -x "$repo_root/bin/orch" ]] || {
  echo "run make build first" >&2
  exit 1
}

umask 077
install -d -m 0700 "$output_dir"
runtime_dir=$output_dir/runtime
[[ ! -e "$runtime_dir" ]] || { echo "runtime directory already exists: $runtime_dir" >&2; exit 1; }
install -d -m 0700 "$runtime_dir"

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

ready_file=$runtime_dir/raft.ready.json
release_file=$ready_file.release
e2e_log=$output_dir/three-voter.log
prometheus_log=$output_dir/prometheus.log
grafana_log=$output_dir/grafana.log
result_file=$output_dir/result.json
prometheus_port=$(free_port)
grafana_port=$(free_port)
while [[ "$grafana_port" == "$prometheus_port" ]]; do grafana_port=$(free_port); done

test_pid=
prometheus_pid=
grafana_pid=
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  [[ -n "$test_pid" && -f "$ready_file" ]] && : >"$release_file"
  for process_id in "$grafana_pid" "$prometheus_pid" "$test_pid"; do
    if [[ -n "$process_id" ]] && kill -0 "$process_id" 2>/dev/null; then
      kill -TERM "$process_id" 2>/dev/null || true
      wait "$process_id" 2>/dev/null || true
    fi
  done
  rm -rf "$runtime_dir"
  exit "$rc"
}
trap cleanup EXIT INT TERM

(
  cd "$repo_root"
  ORCH_E2E=1 ORCH_RAFT_E2E_READY_FILE="$ready_file" \
    go test -mod=readonly ./tests/cli -run '^TestHTTPRaftLifecycle$' -count=1 -v
) >"$e2e_log" 2>&1 &
test_pid=$!

for attempt in {1..480}; do
  [[ -s "$ready_file" ]] && break
  kill -0 "$test_pid" 2>/dev/null || { tail -n 160 "$e2e_log" >&2; exit 1; }
  sleep 0.25
done
[[ -s "$ready_file" ]] || { echo "three-voter fixture was not ready" >&2; exit 1; }
jq -e '.endpoints | length == 3 and all(.[]; startswith("http://127.0.0.1:"))' "$ready_file" >/dev/null

prometheus_config=$runtime_dir/prometheus.yml
{
  printf '%s\n' \
    'global:' \
    '  scrape_interval: 1s' \
    '  evaluation_interval: 1s' \
    'rule_files:' \
    "  - '$repo_root/resources/metrics/alerts.yml'" \
    'scrape_configs:' \
    '  - job_name: orchestrator' \
    '    metrics_path: /metrics' \
    '    static_configs:' \
    '      - targets:'
  jq -r '.endpoints[] | sub("^http://"; "") | "          - \u0027" + . + "\u0027"' "$ready_file"
  printf '%s\n' \
    '        labels:' \
    "          orchestrator_cluster: 'too415-local-three'"
} >"$prometheus_config"
promtool check config "$prometheus_config" >"$output_dir/promtool-config.log" 2>&1

prometheus \
  --config.file="$prometheus_config" \
  --web.listen-address="127.0.0.1:$prometheus_port" \
  --storage.tsdb.path="$runtime_dir/prometheus-data" \
  --storage.tsdb.retention.time=1h \
  >"$prometheus_log" 2>&1 &
prometheus_pid=$!
prometheus_base=http://127.0.0.1:$prometheus_port
for attempt in {1..120}; do
  if curl --silent --show-error --fail "$prometheus_base/-/ready" >/dev/null 2>&1; then break; fi
  kill -0 "$prometheus_pid" 2>/dev/null || { tail -n 160 "$prometheus_log" >&2; exit 1; }
  sleep 0.25
done

targets_file=$output_dir/prometheus-targets.json
for attempt in {1..120}; do
  curl --silent --show-error --fail "$prometheus_base/api/v1/targets" >"$targets_file"
  if jq -e '[.data.activeTargets[] | select(.labels.job == "orchestrator" and .health == "up")] | length == 3' "$targets_file" >/dev/null; then
    break
  fi
  sleep 0.25
done
jq -e '[.data.activeTargets[] | select(.labels.job == "orchestrator" and .health == "up")] | length == 3' "$targets_file" >/dev/null

rules_file=$output_dir/prometheus-rules.json
curl --silent --show-error --fail "$prometheus_base/api/v1/rules" >"$rules_file"
jq -e '[.data.groups[].rules[] | select(.type == "alerting")] | length == 8' "$rules_file" >/dev/null

grafana_provisioning=$runtime_dir/grafana-provisioning
grafana_dashboards=$runtime_dir/grafana-dashboards
install -d -m 0700 "$grafana_provisioning/datasources" "$grafana_provisioning/dashboards" "$grafana_dashboards" \
  "$runtime_dir/grafana-data" "$runtime_dir/grafana-logs" "$runtime_dir/grafana-plugins"
cp "$repo_root/resources/metrics/orchestrator-grafana.json" "$grafana_dashboards/orchestrator.json"
{
  printf '%s\n' \
    'apiVersion: 1' \
    'datasources:' \
    '  - name: Prometheus' \
    '    uid: prometheus' \
    '    type: prometheus' \
    '    access: proxy' \
    "    url: $prometheus_base" \
    '    isDefault: true' \
    '    jsonData:' \
    '      timeInterval: 1s'
} >"$grafana_provisioning/datasources/default.yml"
{
  printf '%s\n' \
    'apiVersion: 1' \
    'providers:' \
    '  - name: orchestrator' \
    '    folder: Orchestrator' \
    '    type: file' \
    '    options:' \
    "      path: $grafana_dashboards"
} >"$grafana_provisioning/dashboards/default.yml"

grafana_home=$(brew --prefix grafana)/share/grafana
grafana_config=$runtime_dir/grafana.ini
{
  printf '%s\n' \
    'app_mode = production' \
    '[paths]' \
    "data = $runtime_dir/grafana-data" \
    "logs = $runtime_dir/grafana-logs" \
    "plugins = $runtime_dir/grafana-plugins" \
    "provisioning = $grafana_provisioning" \
    '[server]' \
    'http_addr = 127.0.0.1' \
    "http_port = $grafana_port" \
    '[auth]' \
    'disable_login_form = true' \
    '[auth.anonymous]' \
    'enabled = true' \
    'org_role = Viewer' \
    '[analytics]' \
    'reporting_enabled = false' \
    'check_for_updates = false' \
    '[plugins]' \
    'plugin_admin_enabled = false' \
    'preinstall_disabled = true'
} >"$grafana_config"
grafana server --homepath "$grafana_home" --config "$grafana_config" >"$grafana_log" 2>&1 &
grafana_pid=$!
grafana_base=http://127.0.0.1:$grafana_port
for attempt in {1..240}; do
  if curl --silent --show-error --fail "$grafana_base/api/health" >"$output_dir/grafana-health.json" 2>/dev/null; then break; fi
  kill -0 "$grafana_pid" 2>/dev/null || { tail -n 200 "$grafana_log" >&2; exit 1; }
  sleep 0.25
done
jq -e '.database == "ok"' "$output_dir/grafana-health.json" >/dev/null

for attempt in {1..120}; do
  if curl --silent --show-error --fail "$grafana_base/api/dashboards/uid/orchestrator-observability" >"$output_dir/grafana-dashboard.json" 2>/dev/null && \
     jq -e '.dashboard.uid == "orchestrator-observability"' "$output_dir/grafana-dashboard.json" >/dev/null; then
    break
  fi
  sleep 0.25
done
jq -e '.dashboard.uid == "orchestrator-observability" and (.dashboard.panels | length) == 44 and ([.dashboard.panels[] | select(.type == "row")] | length) == 6' "$output_dir/grafana-dashboard.json" >/dev/null

expressions_file=$runtime_dir/dashboard-expressions.txt
jq -r '.dashboard | .. | objects | select(has("expr")) | .expr' "$output_dir/grafana-dashboard.json" >"$expressions_file"
query_results=$output_dir/dashboard-query-results.jsonl
: >"$query_results"
query_total=0
query_success=0
query_nonempty=0
while IFS= read -r expression; do
  [[ -n "$expression" ]] || continue
  ((query_total += 1))
  expression=${expression//\$__rate_interval/1m}
  expression=${expression//\$orchestrator_cluster/too415-local-three}
  expression=${expression//\$instance/.\*}
  expression=${expression//\$queue/.\*}
  expression=${expression//\$recovery_kind/.\*}
  response=$(curl --silent --show-error --fail --get --data-urlencode "query=$expression" "$prometheus_base/api/v1/query")
  if [[ $(jq -r '.status' <<<"$response") == success ]]; then
    ((query_success += 1))
  fi
  result_count=$(jq -r '.data.result | length' <<<"$response")
  if (( result_count > 0 )); then ((query_nonempty += 1)); fi
  jq -cn --arg status "$(jq -r '.status' <<<"$response")" --argjson resultCount "$result_count" '{status:$status,resultCount:$resultCount}' >>"$query_results"
done <"$expressions_file"
[[ "$query_total" == 43 && "$query_success" == 43 ]] || {
  echo "dashboard query result total=$query_total success=$query_success" >&2
  exit 1
}

: >"$release_file"
wait "$test_pid"
test_pid=
kill -TERM "$grafana_pid"
wait "$grafana_pid"
grafana_pid=
kill -TERM "$prometheus_pid"
wait "$prometheus_pid"
prometheus_pid=

jq -n \
  --arg status PASS \
  --argjson targets 3 \
  --argjson alertRules 8 \
  --argjson panels 44 \
  --argjson rows 6 \
  --argjson queries "$query_total" \
  --argjson successfulQueries "$query_success" \
  --argjson nonemptyQueries "$query_nonempty" \
  '{status:$status,targets:$targets,alertRules:$alertRules,panels:$panels,rows:$rows,queries:$queries,successfulQueries:$successfulQueries,nonemptyQueries:$nonemptyQueries}' \
  >"$result_file"
rm -rf "$runtime_dir"
trap - EXIT INT TERM
jq -r '"PASS targets=\(.targets) alert_rules=\(.alertRules) panels=\(.panels) rows=\(.rows) queries=\(.queries) query_success=\(.successfulQueries) query_nonempty=\(.nonemptyQueries)"' "$result_file"
