#!/bin/bash

# Local integration tests. To be used by CI.
# See https://github.com/openark/orchestrator/tree/doc/local-tests.md
#

# Usage: localtests/test/sh [mysql|sqlite] [filter]
# By default, runs all tests. Given filter, will only run tests matching given regep

tests_path=$(dirname $0)
task_tmp=$(mktemp -d /tmp/orch-integration.XXXXXX)
server_pid=
trap '[ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null; rm -rf "$task_tmp"' EXIT
cli_binary="$task_tmp/orch"
test_logfile=${task_tmp}/orchestrator-test.log
test_outfile=${task_tmp}/orchestrator-test.out
test_diff_file=${task_tmp}/orchestrator-test.diff
test_query_file=${task_tmp}/orchestrator-test.sql
test_config_file=${task_tmp}/orchestrator.conf.json
orchestrator_binary=${task_tmp}/orchestrator-test
exec_command_file=${task_tmp}/orchestrator-test.bash
test_mysql_defaults_file=${task_tmp}/orchestrator-test-my.cnf
db_type=""
sqlite_file="${task_tmp}/orchestrator.db"
mysql_args="--defaults-extra-file=${test_mysql_defaults_file} --default-character-set=utf8mb4 -s -s"

function run_queries() {
  queries_file="$1"

  if [ "$db_type" == "sqlite" ] ; then
    cat $queries_file |
      sed -e "s/last_checked - interval 1 minute/datetime('last_checked', '-1 minute')/g" |
      sed -e "s/current_timestamp + interval 1 minute/datetime('now', '+1 minute')/g" |
      sed -e "s/current_timestamp - interval 1 minute/datetime('now', '-1 minute')/g" |
      sqlite3 $sqlite_file
  else
    # Assume mysql
    mysql $mysql_args test < $queries_file
  fi
}

setup_mysql() {
  local mysql_user=""
  local mysql_password=""
  echo "one time setup of mysql"
  if mysql --default-character-set=utf8mb4 -ss -e "select 16 + 1" -u root -proot 2> /dev/null | grep -q 17 ; then
    mysql_user="root"
    mysql_password="root"
  fi
  if mysql --default-character-set=utf8mb4 -ss -e "select 16 + 1" -u root -pmsandbox 2> /dev/null | grep -q 17 ; then
    mysql_user="root"
    mysql_password="msandbox"
  fi
  if mysql --default-character-set=utf8mb4 -ss -e "select 16 + 1" -u "$(whoami)" 2> /dev/null | grep -q 17 ; then
    mysql_user="$(whoami)"
  fi
  echo "[client]"                   >  $test_mysql_defaults_file
  echo "user=${mysql_user}"         >> $test_mysql_defaults_file
  echo "password=${mysql_password}" >> $test_mysql_defaults_file

  echo "mysql args: $mysql_args"
  echo "mysql config (${test_mysql_defaults_file})"
  # Credentials remain in the test-owned defaults file.
  mysql $mysql_args -e "create database if not exists test"
}

check_db() {
  if [ "$db_type" == "mysql" ] ; then
    setup_mysql
  fi
  echo "select 1;" > $test_query_file
  query_result="$(run_queries $test_query_file)"
  if [ "$query_result" != "1" ] ; then
    echo "Cannot execute queries"
    exit 1
  fi
  echo "- check_db OK"
}

exec_cmd() {
  echo "$@"
  command "$@" 1> $test_outfile 2> $test_logfile
  return $?
}

echo_dot() {
  echo -n "."
}

test_single() {
  local test_name
  test_name="$1"

  echo -n "Testing: $test_name"

  echo_dot
  run_queries $tests_path/create-per-test.sql

  echo_dot
  if [ -f $tests_path/$test_name/create.sql ] ; then
    run_queries $tests_path/$test_name/create.sql
  fi

  extra_args=""
  if [ -f $tests_path/$test_name/extra_args ] ; then
    extra_args=$(cat $tests_path/$test_name/extra_args)
  fi
  
  test_config_file_save="$test_config_file"
  if [ -f "$tests_path/$test_name/config.json" ]; then
    # There is no way to reload the configuration (partially) for integration tests
    # so we need to provide a single JSON file.
    # Let's merge the base config file with the provided one.
    echo "- applying configuration: $tests_path/$test_name/config.json"
    merged_config_path="$task_tmp/config_final.json"
    real_config_path="$(realpath "$tests_path/$test_name/config.json")"

    python3 - <<EOF
import json

with open("${test_config_file}") as f:
    base = json.load(f)

with open("$real_config_path") as f:
    override = json.load(f)

base.update(override)

with open("$merged_config_path", "w") as out:
    json.dump(base, out, indent=2)
EOF

    test_config_file="$merged_config_path"
  fi

  if [[ "$extra_args" == admin* ]]; then
    cmd="$orchestrator_binary --config=${test_config_file} ${extra_args}"
  else
    # 每个 fixture 独立启动服务端，禁止自动发现改写预置拓扑。
    listen_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
    python3 - "$test_config_file" "$task_tmp/server.json" "$listen_port" <<'PYCONFIG'
import json,sys,socket,os
with open(sys.argv[1]) as f: config=json.load(f)
sock=socket.socket(); sock.bind(("127.0.0.1",0)); raft_port=sock.getsockname()[1]; sock.close()
config.pop("RaftEnabled",None)
config.update(ListenAddress="127.0.0.1:"+sys.argv[3],HostnameResolveMethod="none",Debug=False,AuditLogFile="",RaftNodeID="integration",RaftDataDir=os.path.join(os.path.dirname(sys.argv[2]),"raft-"+sys.argv[3]),RaftBind="127.0.0.1:"+str(raft_port),RaftAdvertise="127.0.0.1:"+str(raft_port))
with open(sys.argv[2],"w") as f: json.dump(config,f)
PYCONFIG
    "$orchestrator_binary" server --config="$task_tmp/server.json" --discovery=false >"$task_tmp/server.log" 2>&1 &
    server_pid=$!
    ready=
    for attempt in $(seq 1 100); do
      if "$cli_binary" --endpoint="http://127.0.0.1:$listen_port" api lb-check >/dev/null 2>&1; then ready=1; break; fi
      kill -0 "$server_pid" 2>/dev/null || break
      sleep 0.1
    done
    if [ -z "$ready" ]; then cat "$task_tmp/server.log"; return 1; fi
    "$cli_binary" --endpoint="http://127.0.0.1:$listen_port" raft-bootstrap >/dev/null || return 1
    ready=
    for attempt in $(seq 1 100); do
      if "$cli_binary" --endpoint="http://127.0.0.1:$listen_port" api leader-check >/dev/null 2>&1; then ready=1; break; fi
      kill -0 "$server_pid" 2>/dev/null || break
      sleep 0.1
    done
    if [ -z "$ready" ]; then cat "$task_tmp/server.log"; return 1; fi
    cmd="$cli_binary --endpoint=http://127.0.0.1:$listen_port ${extra_args}"
  fi
  echo "$cmd" > "$exec_command_file"
  bash "$exec_command_file" > "$test_outfile" 2> "$test_logfile"
  execution_result=$?
  if [ -n "$server_pid" ]; then kill "$server_pid"; wait "$server_pid" 2>/dev/null; server_pid=; fi

  # restore the original config
  if [ "$test_config_file" != "$test_config_file_save" ] ; then
    rm -f "$test_config_file"
  fi
  test_config_file="$test_config_file_save"
 
  if [ -f $tests_path/$test_name/destroy.sql ] ; then
    run_queries $tests_path/$test_name/destroy.sql
  fi

  if [ -f $tests_path/$test_name/expect_failure ] ; then
    if [ $execution_result -eq 0 ] ; then
      echo
      echo "ERROR $test_name execution was expected to exit on error but did not. cat $test_logfile"
      return 1
    fi
    if [ -s $tests_path/$test_name/expect_failure ] ; then
      # 'expect_failure' file has content. We expect to find this content in the log.
      expected_error_message="$(cat $tests_path/$test_name/expect_failure)"
      if grep -q "$expected_error_message" $test_logfile ; then
          return 0
      fi
      echo
      echo "ERROR $test_name execution was expected to exit with error message '${expected_error_message}' but did not. cat $test_logfile"
      return 1
    fi
    # 'expect_failure' file has no content. We generally agree that the failure is correct
    return 0
  fi

  if [ $execution_result -ne 0 ] ; then
    echo
    echo "ERROR $test_name execution failure"
    cat "$test_logfile"
    return 1
  fi

  if [ -f $tests_path/$test_name/expect_output ] ; then
    diff -b $tests_path/$test_name/expect_output $test_outfile > $test_diff_file
    diff_result=$?
    if [ $diff_result -ne 0 ] ; then
      echo
      echo "ERROR $test_name diff failure. cat $test_diff_file"
      echo "---"
      cat $test_diff_file
      echo "---"
      return 1
    fi
  fi

  # all is well
  return 0
}

build_binary() {
  echo "Building"
  make binary cli BINARY="$orchestrator_binary" CLI_BINARY="$cli_binary"
}


deploy_internal_db() {
  echo "Deploying db"
  cmd="$orchestrator_binary \
    --config=${test_config_file}
    --debug \
    --stack \
    admin redeploy-internal-db"
  echo_dot
  echo $cmd > $exec_command_file
  echo_dot
  bash $exec_command_file 1> $test_outfile 2> $test_logfile
  if [ $? -ne 0 ] ; then
    echo "ERROR deploy internal db failed"
    cat $test_logfile
    return 1
  fi
  echo "- deploy_internal_db result: $?"
}

generate_config_file() {
  python3 - "$tests_path/orchestrator.conf.json" "$test_config_file" "$test_mysql_defaults_file" "$db_type" "$sqlite_file" <<'PYCONFIG'
import json,re,sys
text=open(sys.argv[1]).read(); config=json.loads(re.sub(r",\s*([}\]])",r"\1",text))
config["MySQLOrchestratorCredentialsConfigFile"]=sys.argv[3]
config["AuditLogFile"]=""
config["BackendDB"]=sys.argv[4]
config["SQLite3DataFile"]=sys.argv[5]
with open(sys.argv[2],"w") as f: json.dump(config,f)
PYCONFIG
  touch "$test_mysql_defaults_file" # required even for sqlite because config file references the my.cnf cgf file
  echo "- generate_config_file OK"
}

test_all() {
  deploy_internal_db
  if [ $? -ne 0 ] ; then
    echo "ERROR deploy failed"
    return 1
  fi
  echo "- deploy_internal_db OK"

  test_pattern="${1:-.}"
  local matched=0
  for test_dir in "$tests_path"/*/; do
    test_name=$(basename "$test_dir")
    [[ "$test_name" =~ $test_pattern ]] || continue
    matched=$((matched + 1))
    test_single "$test_name"
    if [ $? -ne 0 ] ; then
      echo "+ FAIL"
      return 1
    else
      echo
      echo "+ pass"
    fi
  done
  if [ "$matched" -eq 0 ]; then echo "No fixtures match: $test_pattern"; return 1; fi
}

test_db() {
  db_type="$1"
  echo "### testing via $db_type"
  check_db
  generate_config_file
  test_all ${@:2}
  if [ $? -ne 0 ] ; then
    echo "test_db failed"
    return 1
  fi
  echo "- done testing via $db_type"
}

main() {
  build_binary
  if [ $? -ne 0 ] ; then
    echo "ERROR build failed"
    return 1
  fi

  test_dbs=""
  if [ "$1" == "mysql" ] ; then
    test_dbs="mysql"
    shift
  elif [ "$1" == "sqlite" ] ; then
    test_dbs="sqlite"
    shift
  fi
  test_dbs=${test_dbs:-"mysql sqlite"}
  for db in $(echo $test_dbs) ; do
    test_db $db "$@"
    if [ $? -ne 0 ] ; then
      echo "+ FAIL: test_db $db $@"
      return 1
    fi
  done
}

main "$@"
