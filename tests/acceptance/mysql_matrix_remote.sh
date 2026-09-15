#!/usr/bin/env bash

set -euo pipefail

task_root="${TOO415_REMOTE_ROOT:?TOO415_REMOTE_ROOT is required}"
bind_address="${TOO415_BIND_ADDRESS:?TOO415_BIND_ADDRESS is required}"
compat_lib_dir="$task_root/packages/compat-libnsl/lib64"
if [[ -d "$compat_lib_dir" ]]; then
  export LD_LIBRARY_PATH="$compat_lib_dir${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
fi

declare -A package_urls=(
  [5.1.73]="https://downloads.mysql.com/archives/get/p/23/file/mysql-5.1.73-linux-x86_64-glibc23.tar.gz"
  [5.5.62]="https://downloads.mysql.com/archives/get/p/23/file/mysql-5.5.62-linux-glibc2.12-x86_64.tar.gz"
  [5.6.51]="https://cdn.mysql.com/Downloads/MySQL-5.6/mysql-5.6.51-linux-glibc2.12-x86_64.tar.gz"
  [5.7.44]="https://cdn.mysql.com/Downloads/MySQL-5.7/mysql-5.7.44-linux-glibc2.12-x86_64.tar.gz"
  [8.0.46]="https://cdn.mysql.com/Downloads/MySQL-8.0/mysql-8.0.46-linux-glibc2.17-x86_64-minimal.tar.xz"
  [8.1.0]="https://cdn.mysql.com/Downloads/MySQL-8.1/mysql-8.1.0-linux-glibc2.17-x86_64-minimal.tar.xz"
  [8.2.0]="https://cdn.mysql.com/Downloads/MySQL-8.2/mysql-8.2.0-linux-glibc2.17-x86_64-minimal.tar.xz"
  [8.3.0]="https://cdn.mysql.com/Downloads/MySQL-8.3/mysql-8.3.0-linux-glibc2.17-x86_64-minimal.tar.xz"
  [8.4.11]="https://cdn.mysql.com/Downloads/MySQL-8.4/mysql-8.4.11-linux-glibc2.28-x86_64-minimal.tar.xz"
  [9.0.1]="https://cdn.mysql.com/Downloads/MySQL-9.0/mysql-9.0.1-linux-glibc2.17-x86_64-minimal.tar.xz"
  [9.1.0]="https://cdn.mysql.com/Downloads/MySQL-9.1/mysql-9.1.0-linux-glibc2.17-x86_64-minimal.tar.xz"
  [9.2.0]="https://cdn.mysql.com/Downloads/MySQL-9.2/mysql-9.2.0-linux-glibc2.17-x86_64-minimal.tar.xz"
  [9.3.0]="https://downloads.mysql.com/archives/get/p/23/file/mysql-9.3.0-linux-glibc2.28-x86_64-minimal.tar.xz"
  [9.4.0]="https://downloads.mysql.com/archives/get/p/23/file/mysql-9.4.0-linux-glibc2.28-x86_64-minimal.tar.xz"
  [9.5.0]="https://downloads.mysql.com/archives/get/p/23/file/mysql-9.5.0-linux-glibc2.28-x86_64-minimal.tar.xz"
  [9.6.0]="https://downloads.mysql.com/archives/get/p/23/file/mysql-9.6.0-linux-glibc2.28-x86_64-minimal.tar.xz"
  [9.7.2]="https://cdn.mysql.com/Downloads/MySQL-9.7/mysql-9.7.2-linux-glibc2.28-x86_64-minimal.tar.xz"
  [26.7.0]="https://cdn.mysql.com/Downloads/MySQL-26.7/mysql-26.7.0-linux-glibc2.28-x86_64-minimal.tar.xz"
  [percona-8.4.11-11]="https://downloads.percona.com/downloads/Percona-Server-8.4/Percona-Server-8.4.11-11/binary/tarball/Percona-Server-8.4.11-11-Linux.x86_64.glibc2.28-minimal.tar.gz"
)

usage() {
  cat <<'EOF'
usage: mysql_matrix_remote.sh COMMAND [ARGS]

Commands:
  prepare
  fetch VERSION
  init VERSION SLOT PORT SERVER_ID
  start VERSION SLOT
  stop VERSION SLOT
  status VERSION SLOT
  rotate-passwords VERSION SLOT
  replicate VERSION REPLICA_SLOT SOURCE_SLOT
  manifest

The script owns only TOO415_REMOTE_ROOT. It never removes a datadir or package.
EOF
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

prepare() {
  umask 077
  mkdir -p "$task_root"/{downloads,packages,runs,secrets,checksums}
  chmod 0700 "$task_root" "$task_root"/runs "$task_root"/secrets
  for name in root topology metadata replication; do
    if [[ ! -s "$task_root/secrets/$name.password" ]]; then
      local random_bytes=24
      # MySQL replication source passwords are limited to 32 characters.
      [[ "$name" != replication ]] || random_bytes=16
      openssl rand -hex "$random_bytes" >"$task_root/secrets/$name.password"
    fi
    chmod 0600 "$task_root/secrets/$name.password"
  done
}

version_parts() {
  local version=$1
  version=${version#percona-}
  version_major=${version%%.*}
  local rest=${version#*.}
  version_minor=${rest%%.*}
}

package_path() {
  local version=$1
  local url=${package_urls[$version]:-}
  [[ -n "$url" ]] || fail "unsupported version: $version"
  printf '%s/downloads/%s\n' "$task_root" "${url##*/}"
}

basedir_path() {
  printf '%s/packages/%s\n' "$task_root" "$1"
}

instance_path() {
  printf '%s/runs/%s/%s\n' "$task_root" "$1" "$2"
}

fetch() {
  local version=$1
  local url=${package_urls[$version]:-}
  [[ -n "$url" ]] || fail "unsupported version: $version"
  prepare
  local archive
  archive=$(package_path "$version")
  if [[ ! -s "$archive" ]]; then
    local download_ok=0
    for attempt in $(seq 1 5); do
      if curl --fail --location --retry 3 --continue-at - --output "$archive.part" "$url"; then
        download_ok=1
        break
      fi
      sleep $((attempt * 2))
    done
    [[ "$download_ok" == 1 ]] || fail "download failed after resumable retries: $version"
    mv "$archive.part" "$archive"
  fi
  sha256sum "$archive" | tee "$task_root/checksums/$version.sha256"
  local basedir
  basedir=$(basedir_path "$version")
  if [[ ! -x "$basedir/bin/mysqld" ]]; then
    local unpack="$task_root/packages/.unpack-$version"
    [[ ! -e "$unpack" ]] || fail "stale unpack directory: $unpack"
    mkdir -p "$unpack"
    tar -xf "$archive" -C "$unpack" --strip-components=1
    mv "$unpack" "$basedir"
  fi
  "$basedir/bin/mysqld" --no-defaults --version
  ldd "$basedir/bin/mysqld" | tee "$task_root/checksums/$version.ldd"
  if grep -q 'not found' "$task_root/checksums/$version.ldd"; then
    fail "missing shared libraries for $version"
  fi
}

write_config() {
  local version=$1 slot=$2 port=$3 server_id=$4
  local basedir instance
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  version_parts "$version"
  cat >"$instance/my.cnf" <<EOF
[mysqld]
basedir=$basedir
datadir=$instance/data
socket=$instance/mysql.sock
pid-file=$instance/mysqld.pid
log-error=$instance/error.log
port=$port
bind-address=$bind_address
server-id=$server_id
report-host=$bind_address
report-port=$port
skip-name-resolve
log-bin=$instance/binlog
binlog-format=ROW
log-slave-updates
max-connections=100
innodb-buffer-pool-size=64M
EOF
  if ((version_major >= 8)); then
    cat >>"$instance/my.cnf" <<'EOF'
mysqlx=0
EOF
  fi
  if ((version_major > 5 || (version_major == 5 && version_minor >= 6))); then
    cat >>"$instance/my.cnf" <<'EOF'
gtid-mode=ON
enforce-gtid-consistency=ON
EOF
  fi
  if ((version_major == 8 && version_minor >= 4)); then
    cat >>"$instance/my.cnf" <<'EOF'
mysql-native-password=ON
EOF
  fi
  chmod 0600 "$instance/my.cnf"
}

wait_ready() {
  local version=$1 slot=$2
  local basedir instance
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  for _ in $(seq 1 120); do
    if "$basedir/bin/mysqladmin" --no-defaults --socket="$instance/mysql.sock" --user=root ping >/dev/null 2>&1; then
      return 0
    fi
    if [[ -s "$instance/mysqld.pid" ]] && ! kill -0 "$(<"$instance/mysqld.pid")" 2>/dev/null; then
      break
    fi
    sleep 1
  done
  tail -n 120 "$instance/error.log" >&2 || true
  fail "mysqld did not become ready: $version/$slot"
}

bootstrap_accounts() {
  local version=$1 slot=$2
  local basedir instance sql_file
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  sql_file="$instance/bootstrap.sql"
  version_parts "$version"
  local root_password topology_password metadata_password replication_password
  local replication_user="too415_replication"
  local database_charset="utf8mb4 COLLATE utf8mb4_general_ci"
  if ((version_major == 5 && version_minor == 1)); then
    database_charset="utf8 COLLATE utf8_general_ci"
  fi
  if ((version_major == 5 && version_minor <= 6)); then
    replication_user="too415_repl"
  fi
  root_password=$(<"$task_root/secrets/root.password")
  topology_password=$(<"$task_root/secrets/topology.password")
  metadata_password=$(<"$task_root/secrets/metadata.password")
  replication_password=$(<"$task_root/secrets/replication.password")
  if ((version_major >= 8)); then
    local auth_clause=""
    if ((version_major == 8)); then
      auth_clause=" WITH mysql_native_password"
    fi
    cat >"$sql_file" <<EOF
CREATE USER 'too415_topology'@'%' IDENTIFIED$auth_clause BY '$topology_password';
GRANT ALL PRIVILEGES ON *.* TO 'too415_topology'@'%' WITH GRANT OPTION;
CREATE USER 'too415_metadata'@'%' IDENTIFIED$auth_clause BY '$metadata_password';
CREATE DATABASE too415_metadata_a CHARACTER SET $database_charset;
CREATE DATABASE too415_metadata_b CHARACTER SET $database_charset;
CREATE DATABASE too415_metadata_c CHARACTER SET $database_charset;
CREATE DATABASE too415_app CHARACTER SET $database_charset;
GRANT ALL PRIVILEGES ON too415_metadata_a.* TO 'too415_metadata'@'%';
GRANT ALL PRIVILEGES ON too415_metadata_b.* TO 'too415_metadata'@'%';
GRANT ALL PRIVILEGES ON too415_metadata_c.* TO 'too415_metadata'@'%';
CREATE USER '$replication_user'@'%' IDENTIFIED$auth_clause BY '$replication_password';
GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO '$replication_user'@'%';
ALTER USER 'root'@'localhost' IDENTIFIED$auth_clause BY '$root_password';
FLUSH PRIVILEGES;
EOF
  else
    cat >"$sql_file" <<EOF
GRANT ALL PRIVILEGES ON *.* TO 'too415_topology'@'%' IDENTIFIED BY '$topology_password' WITH GRANT OPTION;
CREATE DATABASE too415_metadata_a CHARACTER SET $database_charset;
CREATE DATABASE too415_metadata_b CHARACTER SET $database_charset;
CREATE DATABASE too415_metadata_c CHARACTER SET $database_charset;
CREATE DATABASE too415_app CHARACTER SET $database_charset;
GRANT ALL PRIVILEGES ON too415_metadata_a.* TO 'too415_metadata'@'%' IDENTIFIED BY '$metadata_password';
GRANT ALL PRIVILEGES ON too415_metadata_b.* TO 'too415_metadata'@'%' IDENTIFIED BY '$metadata_password';
GRANT ALL PRIVILEGES ON too415_metadata_c.* TO 'too415_metadata'@'%' IDENTIFIED BY '$metadata_password';
GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO '$replication_user'@'%' IDENTIFIED BY '$replication_password';
SET PASSWORD FOR 'root'@'localhost' = PASSWORD('$root_password');
FLUSH PRIVILEGES;
EOF
  fi
  chmod 0600 "$sql_file"
  "$basedir/bin/mysql" --no-defaults --socket="$instance/mysql.sock" --user=root <"$sql_file"
  rm -f "$sql_file"
  cat >"$instance/root.cnf" <<EOF
[client]
user=root
password=$root_password
socket=$instance/mysql.sock
EOF
  chmod 0600 "$instance/root.cnf"
}

reset_binary_state() {
  local version=$1 slot=$2
  local basedir instance statement="RESET MASTER"
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  version_parts "$version"
  if ((version_major > 8 || (version_major == 8 && version_minor >= 4))); then
    statement="RESET BINARY LOGS AND GTIDS"
  fi
  "$basedir/bin/mysql" --defaults-extra-file="$instance/root.cnf" --execute="$statement"
}

init_instance() {
  local version=$1 slot=$2 port=$3 server_id=$4
  prepare
  local basedir instance
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  [[ -x "$basedir/bin/mysqld" ]] || fail "fetch $version first"
  [[ ! -e "$instance" ]] || fail "instance already exists: $instance"
  mkdir -p "$instance/data"
  chmod 0700 "$instance" "$instance/data"
  write_config "$version" "$slot" "$port" "$server_id"
  version_parts "$version"
  if ((version_major > 5 || (version_major == 5 && version_minor >= 7))); then
    "$basedir/bin/mysqld" --no-defaults --initialize-insecure --user="$(id -un)" --basedir="$basedir" --datadir="$instance/data"
  else
    "$basedir/scripts/mysql_install_db" --no-defaults --user="$(id -un)" --basedir="$basedir" --datadir="$instance/data"
  fi
  "$basedir/bin/mysqld" --defaults-file="$instance/my.cnf" --user="$(id -un)" --skip-networking &
  wait_ready "$version" "$slot"
  bootstrap_accounts "$version" "$slot"
  # Every node bootstraps the same task users. Do not expose those local GTIDs
  # to a later source/replica relationship, where they would replay as conflicts.
  reset_binary_state "$version" "$slot"
  "$basedir/bin/mysqladmin" --defaults-extra-file="$instance/root.cnf" shutdown
  for _ in $(seq 1 60); do
    [[ ! -e "$instance/mysqld.pid" ]] && break
    sleep 1
  done
  start_instance "$version" "$slot"
}

start_instance() {
  local version=$1 slot=$2
  local basedir instance
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  [[ -f "$instance/my.cnf" ]] || fail "instance is not initialized: $version/$slot"
  if [[ -s "$instance/mysqld.pid" ]] && kill -0 "$(<"$instance/mysqld.pid")" 2>/dev/null; then
    fail "instance is already running: $version/$slot"
  fi
  "$basedir/bin/mysqld" --defaults-file="$instance/my.cnf" --user="$(id -un)" &
  for _ in $(seq 1 120); do
    if "$basedir/bin/mysqladmin" --defaults-extra-file="$instance/root.cnf" ping >/dev/null 2>&1; then
      status_instance "$version" "$slot"
      return 0
    fi
    sleep 1
  done
  tail -n 120 "$instance/error.log" >&2 || true
  fail "mysqld did not restart: $version/$slot"
}

stop_instance() {
  local version=$1 slot=$2
  local basedir instance
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  [[ -f "$instance/root.cnf" ]] || fail "missing task-owned root config: $instance/root.cnf"
  if [[ ! -s "$instance/mysqld.pid" ]]; then
    printf 'STOPPED %s/%s\n' "$version" "$slot"
    return 0
  fi
  "$basedir/bin/mysqladmin" --defaults-extra-file="$instance/root.cnf" shutdown
  for _ in $(seq 1 60); do
    [[ ! -e "$instance/mysqld.pid" ]] && break
    sleep 1
  done
  [[ ! -e "$instance/mysqld.pid" ]] || fail "pid file remains after shutdown: $instance/mysqld.pid"
  printf 'STOPPED %s/%s\n' "$version" "$slot"
}

status_instance() {
  local version=$1 slot=$2
  local basedir instance
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  [[ -f "$instance/root.cnf" ]] || fail "missing task-owned root config: $instance/root.cnf"
  "$basedir/bin/mysql" --defaults-extra-file="$instance/root.cnf" --batch --skip-column-names \
    --execute="SELECT VERSION(), @@hostname, @@port, @@server_id, @@read_only"
}

rotate_passwords() {
  local version=$1 slot=$2
  local basedir instance sql_file topology_password metadata_password replication_password
  local replication_user="too415_replication"
  basedir=$(basedir_path "$version")
  instance=$(instance_path "$version" "$slot")
  topology_password=$(<"$task_root/secrets/topology.password")
  metadata_password=$(<"$task_root/secrets/metadata.password")
  replication_password=$(<"$task_root/secrets/replication.password")
  sql_file="$instance/rotate-passwords.sql"
  version_parts "$version"
  if ((version_major == 5 && version_minor <= 6)); then
    replication_user="too415_repl"
  fi
  if ((version_major >= 8)); then
    local auth_clause=""
    if ((version_major == 8)); then
      auth_clause=" WITH mysql_native_password"
    fi
    cat >"$sql_file" <<EOF
ALTER USER 'too415_topology'@'%' IDENTIFIED$auth_clause BY '$topology_password';
ALTER USER 'too415_metadata'@'%' IDENTIFIED$auth_clause BY '$metadata_password';
ALTER USER '$replication_user'@'%' IDENTIFIED$auth_clause BY '$replication_password';
FLUSH PRIVILEGES;
EOF
  else
    cat >"$sql_file" <<EOF
SET PASSWORD FOR 'too415_topology'@'%' = PASSWORD('$topology_password');
SET PASSWORD FOR 'too415_metadata'@'%' = PASSWORD('$metadata_password');
SET PASSWORD FOR '$replication_user'@'%' = PASSWORD('$replication_password');
FLUSH PRIVILEGES;
EOF
  fi
  chmod 0600 "$sql_file"
  "$basedir/bin/mysql" --defaults-extra-file="$instance/root.cnf" <"$sql_file"
  rm -f "$sql_file"
  printf 'ROTATED %s/%s\n' "$version" "$slot"
}

replicate_instance() {
  local version=$1 replica_slot=$2 source_slot=$3
  local basedir replica source source_port sql_file replication_password
  local replication_user="too415_replication"
  basedir=$(basedir_path "$version")
  replica=$(instance_path "$version" "$replica_slot")
  source=$(instance_path "$version" "$source_slot")
  source_port=$(awk -F= '$1 == "port" {print $2}' "$source/my.cnf")
  replication_password=$(<"$task_root/secrets/replication.password")
  sql_file="$replica/replicate.sql"
  version_parts "$version"
  if ((version_major == 5 && version_minor <= 6)); then
    replication_user="too415_repl"
  fi
  if ((version_major >= 8)); then
    cat >"$sql_file" <<EOF
STOP REPLICA;
RESET REPLICA ALL;
CHANGE REPLICATION SOURCE TO SOURCE_HOST='$bind_address', SOURCE_PORT=$source_port, SOURCE_USER='$replication_user', SOURCE_PASSWORD='$replication_password', SOURCE_AUTO_POSITION=1, GET_SOURCE_PUBLIC_KEY=1;
START REPLICA;
SET GLOBAL read_only=1;
EOF
  elif ((version_major > 5 || (version_major == 5 && version_minor >= 6))); then
    cat >"$sql_file" <<EOF
STOP SLAVE;
RESET SLAVE ALL;
CHANGE MASTER TO MASTER_HOST='$bind_address', MASTER_PORT=$source_port, MASTER_USER='$replication_user', MASTER_PASSWORD='$replication_password', MASTER_AUTO_POSITION=1;
START SLAVE;
SET GLOBAL read_only=1;
EOF
  else
    local source_file source_position reset_replica="RESET SLAVE ALL"
    read -r source_file source_position _ < <(
      "$basedir/bin/mysql" --defaults-extra-file="$source/root.cnf" --batch --skip-column-names --execute="SHOW MASTER STATUS"
    )
    [[ -n "$source_file" && "$source_position" =~ ^[0-9]+$ ]] || fail "cannot read source binlog coordinates"
    if ((version_major == 5 && version_minor == 1)); then
      reset_replica="RESET SLAVE"
    fi
    cat >"$sql_file" <<EOF
STOP SLAVE;
$reset_replica;
CHANGE MASTER TO MASTER_HOST='$bind_address', MASTER_PORT=$source_port, MASTER_USER='$replication_user', MASTER_PASSWORD='$replication_password', MASTER_LOG_FILE='$source_file', MASTER_LOG_POS=$source_position;
START SLAVE;
SET GLOBAL read_only=1;
EOF
  fi
  chmod 0600 "$sql_file"
  "$basedir/bin/mysql" --defaults-extra-file="$replica/root.cnf" <"$sql_file"
  rm -f "$sql_file"
  status_instance "$version" "$replica_slot"
}

manifest() {
  local version
  for version in "${!package_urls[@]}"; do
    printf '%s\t%s\n' "$version" "${package_urls[$version]}"
  done | sort -V
}

command=${1:-}
case "$command" in
  prepare) prepare ;;
  fetch) [[ $# == 2 ]] || fail "fetch requires VERSION"; fetch "$2" ;;
  init) [[ $# == 5 ]] || fail "init requires VERSION SLOT PORT SERVER_ID"; init_instance "$2" "$3" "$4" "$5" ;;
  start) [[ $# == 3 ]] || fail "start requires VERSION SLOT"; start_instance "$2" "$3" ;;
  stop) [[ $# == 3 ]] || fail "stop requires VERSION SLOT"; stop_instance "$2" "$3" ;;
  status) [[ $# == 3 ]] || fail "status requires VERSION SLOT"; status_instance "$2" "$3" ;;
  rotate-passwords) [[ $# == 3 ]] || fail "rotate-passwords requires VERSION SLOT"; rotate_passwords "$2" "$3" ;;
  replicate) [[ $# == 4 ]] || fail "replicate requires VERSION REPLICA_SLOT SOURCE_SLOT"; replicate_instance "$2" "$3" "$4" ;;
  manifest) manifest ;;
  *) usage; exit 2 ;;
esac
