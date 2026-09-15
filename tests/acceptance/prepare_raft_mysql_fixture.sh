#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 3 ]]; then
  echo "usage: prepare_raft_mysql_fixture.sh OUTPUT_DIR METADATA_PASSWORD_FILE TOPOLOGY_PASSWORD_FILE" >&2
  exit 2
fi

output_dir=$1
password_file=$2
topology_password_file=$3
mysql_host=${TOO415_MYSQL_HOST:?TOO415_MYSQL_HOST is required}
mysql_port=${TOO415_MYSQL_PORT:-13306}
mysql_ports=${TOO415_MYSQL_PORTS:-13306,13307,13308}
metadata_prefix=${TOO415_METADATA_PREFIX:-too415_node}

[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }
[[ "$password_file" == /* ]] || { echo "METADATA_PASSWORD_FILE must be absolute" >&2; exit 2; }
[[ "$topology_password_file" == /* ]] || { echo "TOPOLOGY_PASSWORD_FILE must be absolute" >&2; exit 2; }
[[ -s "$password_file" ]] || { echo "metadata password file is missing" >&2; exit 2; }
[[ -s "$topology_password_file" ]] || { echo "topology password file is missing" >&2; exit 2; }
for secret_file in "$password_file" "$topology_password_file"; do
  secret_mode=$(stat -f '%Lp' "$secret_file")
  if [[ "$secret_mode" != 400 && "$secret_mode" != 600 ]]; then
    echo "password file must not be group/world accessible: $secret_file" >&2
    exit 2
  fi
done

umask 077
install -d -m 0700 "$output_dir"
password=$(tr -d '\n' <"$password_file")
topology_password=$(tr -d '\n' <"$topology_password_file")
[[ "$password" =~ ^[[:alnum:]_.-]+$ ]] || { echo "metadata password contains unsupported DSN characters" >&2; exit 2; }
[[ "$topology_password" =~ ^[[:alnum:]_.-]+$ ]] || { echo "topology password contains unsupported DSN characters" >&2; exit 2; }
IFS=, read -r topology_port_a topology_port_b topology_port_c extra <<<"$mysql_ports"
if [[ -n "${extra:-}" || -z "$topology_port_a" || -z "$topology_port_b" || -z "$topology_port_c" ]]; then
  echo "TOO415_MYSQL_PORTS must contain exactly three comma-separated ports" >&2
  exit 2
fi

credentials_file="$output_dir/metadata.cnf"
topology_credentials_file="$output_dir/topology.cnf"
fixture_file="$output_dir/raft-mysql.json"
printf '[client]\nuser = too415_metadata\npassword = %s\n' "$password" >"$credentials_file"
printf '[client]\nuser = too415_topology\npassword = %s\n' "$topology_password" >"$topology_credentials_file"
chmod 0600 "$credentials_file"
chmod 0600 "$topology_credentials_file"

printf '{\n  "nodes": [\n' >"$fixture_file"
for suffix in a b c; do
  separator=,
  [[ "$suffix" != c ]] || separator=
  printf '    {"host":"%s","port":%s,"database":"%s","user":"too415_metadata","passwordFile":"%s","credentialsConfigFile":"%s"}%s\n' \
    "$mysql_host" "$mysql_port" "${metadata_prefix}_$suffix" "$password_file" "$credentials_file" "$separator" >>"$fixture_file"
done
printf '  ],\n' >>"$fixture_file"
printf '  "topology": {"host":"%s","ports":[%s,%s,%s],"user":"too415_topology","passwordFile":"%s","credentialsConfigFile":"%s"}\n' \
  "$mysql_host" "$topology_port_a" "$topology_port_b" "$topology_port_c" "$topology_password_file" "$topology_credentials_file" >>"$fixture_file"
printf '}\n' >>"$fixture_file"
chmod 0600 "$fixture_file"
printf '%s\n' "$fixture_file"
