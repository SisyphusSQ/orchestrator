#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: container_mysql_cluster.sh {mariadb|percona} {start|status|stop|cleanup} SECRETS_FILE [PORT_BASE]" >&2
  exit 2
}

[[ $# -ge 3 && $# -le 4 ]] || usage
product=$1
action=$2
secrets_file=$3

case "$product" in
  mariadb)
    image_ref=docker.io/library/mariadb:11.8.6
    default_port_base=23306
    client=mariadb
    root_password_variable=MARIADB_ROOT_PASSWORD
    root_host_variable=MARIADB_ROOT_HOST
    ;;
  percona)
    image_ref=docker.io/percona/percona-server:8.4.11
    default_port_base=24306
    client=mysql
    root_password_variable=MYSQL_ROOT_PASSWORD
    root_host_variable=MYSQL_ROOT_HOST
    ;;
  *) usage ;;
esac

case "$action" in
  start|status|stop|cleanup) ;;
  *) usage ;;
esac

[[ "$secrets_file" == /* ]] || { echo "SECRETS_FILE must be absolute" >&2; exit 2; }
[[ -s "$secrets_file" ]] || { echo "SECRETS_FILE is missing" >&2; exit 2; }
secrets_mode=$(stat -f '%Lp' "$secrets_file")
if [[ "$secrets_mode" != 400 && "$secrets_mode" != 600 ]]; then
  echo "SECRETS_FILE must not be group/world accessible" >&2
  exit 2
fi

# shellcheck disable=SC1090
source "$secrets_file"
: "${ROOT_PASSWORD:?ROOT_PASSWORD is required}"
: "${REPLICATION_PASSWORD:?REPLICATION_PASSWORD is required}"
: "${TOPOLOGY_PASSWORD:?TOPOLOGY_PASSWORD is required}"
: "${METADATA_PASSWORD:?METADATA_PASSWORD is required}"
for secret in "$ROOT_PASSWORD" "$REPLICATION_PASSWORD" "$TOPOLOGY_PASSWORD" "$METADATA_PASSWORD"; do
  [[ "$secret" =~ ^[[:alnum:]]+$ ]] || { echo "secrets must be non-empty alphanumeric strings" >&2; exit 2; }
done

port_base=${4:-$default_port_base}
[[ "$port_base" =~ ^[0-9]+$ && "$port_base" -ge 1024 && "$port_base" -le 65533 ]] || {
  echo "PORT_BASE must leave room for three non-privileged ports" >&2
  exit 2
}

prefix=too415-$product
network=${prefix}-net
containers=("${prefix}-a" "${prefix}-b" "${prefix}-c")
ports=("$port_base" "$((port_base + 1))" "$((port_base + 2))")
bind_address=${TOO415_CONTAINER_BIND_ADDRESS:-127.0.0.1}
advertised_host=${TOO415_CONTAINER_ADVERTISED_HOST:-host.containers.internal}
[[ "$bind_address" != *[[:space:]]* && -n "$bind_address" ]] || { echo "invalid bind address" >&2; exit 2; }
[[ "$advertised_host" != *[[:space:]]* && -n "$advertised_host" ]] || { echo "invalid advertised host" >&2; exit 2; }

container_exists() {
  podman container exists "$1"
}

exec_sql() {
  local container_name=$1
  shift
  podman exec -i "$container_name" sh -c "exec $client -uroot -p\"\$$root_password_variable\" --protocol=socket \"\$@\"" -- "$@"
}

wait_ready() {
  local container_name=$1
  local attempt
  for attempt in {1..120}; do
    if podman exec "$container_name" sh -c "$client -uroot -p\"\$$root_password_variable\" --protocol=socket -Nse 'SELECT 1'" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "$container_name did not become ready" >&2
  podman logs --tail 80 "$container_name" >&2 || true
  return 1
}

status_cluster() {
  local index container_name
  for index in 0 1 2; do
    container_name=${containers[$index]}
    container_exists "$container_name" || { echo "$container_name missing" >&2; return 1; }
    podman inspect --format '{{.Name}} {{.State.Status}} {{.ImageName}}' "$container_name"
    exec_sql "$container_name" <<'SQL'
SELECT @@version AS version, @@server_id AS server_id, @@read_only AS read_only;
SQL
    if (( index > 0 )); then
      if [[ "$product" == mariadb ]]; then
        exec_sql "$container_name" <<'SQL'
SHOW SLAVE STATUS\G
SQL
      else
        exec_sql "$container_name" <<'SQL'
SHOW REPLICA STATUS\G
SQL
      fi
    fi
  done
}

case "$action" in
  stop)
    for container_name in "${containers[@]}"; do
      if container_exists "$container_name"; then
        podman stop --time 20 "$container_name"
      fi
    done
    exit 0
    ;;
  cleanup)
    for container_name in "${containers[@]}"; do
      if container_exists "$container_name"; then
        podman rm -f --time 20 "$container_name"
      fi
    done
    if podman network exists "$network"; then
      podman network rm "$network"
    fi
    exit 0
    ;;
  status)
    status_cluster
    exit 0
    ;;
esac

for container_name in "${containers[@]}"; do
  if container_exists "$container_name"; then
    echo "$container_name already exists; run cleanup before start" >&2
    exit 1
  fi
done
if podman network exists "$network"; then
  echo "$network already exists; run cleanup before start" >&2
  exit 1
fi
for port in "${ports[@]}"; do
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "host port $port is already listening" >&2
    exit 1
  fi
done

podman network create "$network" >/dev/null
started=()
rollback_start() {
  local rc=$?
  local container_name
  trap - EXIT
  if (( rc != 0 && ${#started[@]} > 0 )); then
    for container_name in "${started[@]}"; do
      echo "--- $container_name log tail ---" >&2
      podman logs --tail 60 "$container_name" >&2 || true
      podman rm -f --time 10 "$container_name" >/dev/null 2>&1 || true
    done
  fi
  if (( rc != 0 )); then
    podman network rm "$network" >/dev/null 2>&1 || true
  fi
  exit "$rc"
}
trap rollback_start EXIT

for index in 0 1 2; do
  container_name=${containers[$index]}
  port=${ports[$index]}
  server_id=$((41500 + index + 1))
  database_args=(
    --server-id="$server_id"
    --log-bin=mysql-bin
    --binlog-format=ROW
    --skip-name-resolve
  )
  if [[ "$product" == mariadb ]]; then
    database_args+=(--gtid-strict-mode=ON --gtid-domain-id="$server_id" --log-slave-updates=ON)
  else
    database_args+=(--gtid-mode=ON --enforce-gtid-consistency=ON --log-replica-updates=ON)
  fi
  podman run -d \
    --name "$container_name" \
    --network "$network" \
    --publish "${bind_address}:${port}:3306" \
    --env "$root_password_variable=$ROOT_PASSWORD" \
    --env "$root_host_variable=%" \
    --env "TOO415_REPLICATION_PASSWORD=$REPLICATION_PASSWORD" \
    --env "TOO415_TOPOLOGY_PASSWORD=$TOPOLOGY_PASSWORD" \
    --env "TOO415_METADATA_PASSWORD=$METADATA_PASSWORD" \
    "$image_ref" "${database_args[@]}" >/dev/null
  started+=("$container_name")
done

for container_name in "${containers[@]}"; do
  wait_ready "$container_name"
  exec_sql "$container_name" <<SQL
SET sql_log_bin=0;
CREATE USER IF NOT EXISTS 'too415_topology'@'%' IDENTIFIED BY '${TOPOLOGY_PASSWORD}';
ALTER USER 'too415_topology'@'%' IDENTIFIED BY '${TOPOLOGY_PASSWORD}';
GRANT ALL PRIVILEGES ON *.* TO 'too415_topology'@'%' WITH GRANT OPTION;
CREATE USER IF NOT EXISTS 'too415_repl'@'%' IDENTIFIED BY '${REPLICATION_PASSWORD}';
ALTER USER 'too415_repl'@'%' IDENTIFIED BY '${REPLICATION_PASSWORD}';
GRANT REPLICATION SLAVE ON *.* TO 'too415_repl'@'%';
CREATE USER IF NOT EXISTS 'too415_metadata'@'%' IDENTIFIED BY '${METADATA_PASSWORD}';
ALTER USER 'too415_metadata'@'%' IDENTIFIED BY '${METADATA_PASSWORD}';
GRANT ALL PRIVILEGES ON *.* TO 'too415_metadata'@'%';
CREATE DATABASE IF NOT EXISTS too415_app;
SET sql_log_bin=1;
FLUSH PRIVILEGES;
SQL
done

source_gtid=
if [[ "$product" == mariadb ]]; then
  source_gtid=$(exec_sql "${containers[0]}" -Nse 'SELECT @@global.gtid_binlog_pos')
  [[ "$source_gtid" =~ ^[0-9]+-[0-9]+-[0-9]+(,[0-9]+-[0-9]+-[0-9]+)*$ ]] || {
    echo "unexpected MariaDB source GTID position" >&2
    exit 1
  }
fi

for index in 1 2; do
  container_name=${containers[$index]}
  if [[ "$product" == mariadb ]]; then
    exec_sql "$container_name" <<SQL
RESET MASTER;
SET GLOBAL gtid_slave_pos='${source_gtid}';
CHANGE MASTER TO MASTER_HOST='${advertised_host}', MASTER_PORT=${ports[0]}, MASTER_USER='too415_repl', MASTER_PASSWORD='${REPLICATION_PASSWORD}', MASTER_USE_GTID=slave_pos;
START SLAVE;
SET GLOBAL read_only=ON;
SQL
  else
    exec_sql "$container_name" <<SQL
CHANGE REPLICATION SOURCE TO SOURCE_HOST='${advertised_host}', SOURCE_PORT=${ports[0]}, SOURCE_USER='too415_repl', SOURCE_PASSWORD='${REPLICATION_PASSWORD}', SOURCE_AUTO_POSITION=1;
START REPLICA;
SET GLOBAL read_only=ON;
SQL
  fi
done

exec_sql "${containers[0]}" <<'SQL'
CREATE TABLE IF NOT EXISTS too415_app.fixture_marker (id INT PRIMARY KEY, note VARCHAR(64));
REPLACE INTO too415_app.fixture_marker VALUES (415, 'one-primary-two-replicas');
SQL

for index in 1 2; do
  container_name=${containers[$index]}
  replicated=0
  for attempt in {1..60}; do
    if exec_sql "$container_name" -Nse "SELECT COUNT(*) FROM too415_app.fixture_marker WHERE id=415" 2>/dev/null | grep -qx 1; then
      replicated=1
      break
    fi
    sleep 1
  done
  if (( replicated != 1 )); then
    echo "$container_name did not replicate the fixture marker" >&2
    if [[ "$product" == mariadb ]]; then
      exec_sql "$container_name" -e 'SHOW SLAVE STATUS\G' >&2 || true
    else
      exec_sql "$container_name" -e 'SHOW REPLICA STATUS\G' >&2 || true
    fi
    exit 1
  fi
done

trap - EXIT
status_cluster
