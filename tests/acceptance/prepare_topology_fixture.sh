#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 2 ]]; then
  echo "usage: prepare_topology_fixture.sh OUTPUT_DIR TOPOLOGY_PASSWORD_FILE" >&2
  exit 2
fi

output_dir=$1
password_file=$2
mysql_host=${TOO415_MYSQL_HOST:?TOO415_MYSQL_HOST is required}
mysql_ports=${TOO415_MYSQL_PORTS:-13306,13307,13308}

[[ "$output_dir" == /* ]] || { echo "OUTPUT_DIR must be absolute" >&2; exit 2; }
[[ "$password_file" == /* ]] || { echo "TOPOLOGY_PASSWORD_FILE must be absolute" >&2; exit 2; }
[[ -s "$password_file" ]] || { echo "topology password file is missing" >&2; exit 2; }
password_mode=$(stat -f '%Lp' "$password_file")
if [[ "$password_mode" != 400 && "$password_mode" != 600 ]]; then
  echo "topology password file must not be group/world accessible" >&2
  exit 2
fi
IFS=, read -r port_a port_b port_c extra <<<"$mysql_ports"
if [[ -n "${extra:-}" || -z "$port_a" || -z "$port_b" || -z "$port_c" ]]; then
  echo "TOO415_MYSQL_PORTS must contain exactly three comma-separated ports" >&2
  exit 2
fi

umask 077
install -d -m 0700 "$output_dir"
password=$(tr -d '\n' <"$password_file")
[[ "$password" =~ ^[[:alnum:]_.-]+$ ]] || { echo "topology password contains unsupported DSN characters" >&2; exit 2; }
credentials_file="$output_dir/topology.cnf"
fixture_file="$output_dir/topology.json"
printf '[client]\nuser = too415_topology\npassword = %s\n' "$password" >"$credentials_file"
chmod 0600 "$credentials_file"
printf '{\n  "nodes": [],\n  "topology": {"host":"%s","ports":[%s,%s,%s],"user":"too415_topology","passwordFile":"%s","credentialsConfigFile":"%s"}\n}\n' \
  "$mysql_host" "$port_a" "$port_b" "$port_c" "$password_file" "$credentials_file" >"$fixture_file"
chmod 0600 "$fixture_file"
printf '%s\n' "$fixture_file"
