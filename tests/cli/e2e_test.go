package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// TestHTTPTopologyLifecycle 只在显式启用时运行，所有数据库/服务均为回环地址上的临时进程。
func TestHTTPTopologyLifecycle(t *testing.T) {
	if os.Getenv("ORCH_E2E") != "1" {
		t.Skip("set ORCH_E2E=1; requires existing mysqld/mysql installation")
	}
	serverBinary, err := filepath.Abs("../../bin/orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	clientBinary, err := filepath.Abs("../../bin/orch")
	if err != nil {
		t.Fatal(err)
	}
	mysqld, err := exec.LookPath("mysqld")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "orch-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	type node struct {
		port    int
		db      *sql.DB
		process *exec.Cmd
	}
	start := func(binary string, args ...string) *exec.Cmd {
		t.Helper()
		cmd := exec.Command(binary, args...)
		log, err := os.CreateTemp(dir, "process-*.log")
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout = log
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = log.Close()
			if t.Failed() {
				data, _ := os.ReadFile(log.Name())
				if len(data) > 6000 {
					data = data[len(data)-6000:]
				}
				t.Logf("isolated process log: %s", data)
			}
		})
		return cmd
	}
	nodes := []node{}
	for i := range 4 {
		data := filepath.Join(dir, fmt.Sprint(i))
		if err := os.Mkdir(data, 0700); err != nil {
			t.Fatal(err)
		}
		init := exec.CommandContext(t.Context(), mysqld, "--no-defaults", "--initialize-insecure", "--datadir="+data)
		if output, err := init.CombinedOutput(); err != nil {
			t.Fatalf("initialize isolated mysql: %v: %s", err, output)
		}
		port := freePort(t)
		cmd := start(mysqld, "--no-defaults", "--datadir="+data, "--socket="+filepath.Join(data, "mysql.sock"), fmt.Sprintf("--port=%d", port), "--bind-address=127.0.0.1", "--mysqlx=0", fmt.Sprintf("--server-id=%d", i+501), "--log-bin="+filepath.Join(data, "binlog"), "--gtid-mode=ON", "--enforce-gtid-consistency=ON", "--log-replica-updates=ON", "--report-host=127.0.0.1", fmt.Sprintf("--report-port=%d", port), "--innodb-buffer-pool-size=32M", "--performance-schema=OFF")
		db, err := sql.Open("mysql", fmt.Sprintf("root@tcp(127.0.0.1:%d)/?timeout=1s&readTimeout=3s", port))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		eventually(t, 30*time.Second, func() bool { return db.PingContext(t.Context()) == nil })
		nodes = append(nodes, node{port, db, cmd})
	}
	sqlExec := func(index int, query string) {
		t.Helper()
		if _, err := nodes[index].db.ExecContext(t.Context(), query); err != nil {
			t.Fatalf("isolated node %d SQL: %v", index, err)
		}
	}
	// The fourth MySQL is a private orchestrator backend, outside the managed replication topology.
	sqlExec(3, "CREATE DATABASE orchestrator_backend")
	sqlExec(0, "CREATE DATABASE orch_e2e")
	sqlExec(0, "CREATE TABLE orch_e2e.marker (id INT PRIMARY KEY)")
	sqlExec(0, "INSERT INTO orch_e2e.marker VALUES (1)")
	for i := 1; i < 3; i++ {
		sqlExec(i, fmt.Sprintf("CHANGE REPLICATION SOURCE TO SOURCE_HOST='127.0.0.1', SOURCE_PORT=%d, SOURCE_USER='root', SOURCE_PASSWORD='', SOURCE_AUTO_POSITION=1, GET_SOURCE_PUBLIC_KEY=1", nodes[0].port))
		sqlExec(i, "START REPLICA")
		sqlExec(i, "SET GLOBAL read_only=1")
		eventually(t, 20*time.Second, func() bool {
			var count int
			return nodes[i].db.QueryRow("SELECT COUNT(*) FROM orch_e2e.marker").Scan(&count) == nil && count == 1
		})
	}
	httpPort := freePort(t)
	config := map[string]any{
		"metadata": map[string]any{
			"type": "mysql",
			"mysql": map[string]any{
				"host": "127.0.0.1", "port": nodes[3].port, "user": "root", "database": "orchestrator_backend",
			},
		},
		"server": map[string]any{"listen": map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", httpPort)}},
		"topology": map[string]any{
			"mysql":     map[string]any{"user": "root"},
			"hostname":  map[string]any{"resolveMethod": "none", "mysqlResolveMethod": "none"},
			"discovery": map[string]any{"useShowReplicaHosts": true, "pollSeconds": 1},
		},
		"mysql":   map[string]any{"connectTimeoutSeconds": 1},
		"logging": map[string]any{"debug": false, "syslog": map[string]any{"enabled": false}},
		"audit":   map[string]any{"toSyslog": false},
		"raft": map[string]any{
			"nodeID": "topology-e2e", "dataDir": filepath.Join(dir, "raft"), "bind": fmt.Sprintf("127.0.0.1:%d", freePort(t)),
		},
	}
	configPath := filepath.Join(dir, "server.json")
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	start(serverBinary, "server", "--discovery=false", "--config="+configPath)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	invoke := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, clientBinary, append([]string{"--endpoint", endpoint, "--output", "json", "--timeout", "40s"}, args...)...)
		return command.CombinedOutput()
	}
	orch := func(args ...string) []byte {
		t.Helper()
		out, err := invoke(args...)
		if err != nil {
			t.Fatalf("orch %s: %v: %s", args[0], err, out)
		}
		if !json.Valid(out) {
			t.Fatalf("invalid JSON from %s: %s", args[0], out)
		}
		return out
	}
	eventually(t, 15*time.Second, func() bool { _, err := invoke("api", "lb-check"); return err == nil })
	if out, err := invoke("register-candidate", "-i", "127.0.0.1:19999", "--promotion-rule", "prefer"); err == nil {
		t.Fatalf("write before bootstrap succeeded: %s", out)
	}
	orch("raft-bootstrap")
	eventually(t, 10*time.Second, func() bool { _, err := invoke("api", "leader-check"); return err == nil })
	instance := func(i int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(nodes[i].port)) }
	for i := range 3 {
		orch("discover", "-i", instance(i))
	}
	orch("begin-maintenance", "-i", instance(2), "--owner", "e2e", "--reason", "isolated / + % test", "--duration", "1h")
	orch("in-maintenance", "-i", instance(2))
	orch("end-maintenance", "-i", instance(2))
	orch("submit-pool-instances", "--pool", "e2e", "--instances", instance(1)+","+instance(2))
	var pools []map[string]any
	if err := json.Unmarshal(orch("cluster-pool-instances", "--pool", "e2e"), &pools); err != nil || len(pools) != 2 {
		t.Fatalf("pool membership readback: %v, %v", pools, err)
	}
	orch("submit-pool-instances", "--pool", "e2e", "--instances", "")
	if out := orch("cluster-pool-instances", "--pool", "e2e"); string(out) != "[]\n" {
		t.Fatalf("pool clear readback: %s", out)
	}
	orch("tag", "-i", instance(2), "--tag", "purpose=e2e")
	if out := orch("tag-value", "-i", instance(2), "--tag", "purpose"); strings.TrimSpace(string(out)) != `"e2e"` {
		t.Fatalf("tag readback: %s", out)
	}
	orch("relocate", "-i", instance(2), "-d", instance(1))
	// 直接读取真实 MySQL，证明 source 已改变而不是只相信 HTTP 成功。
	eventually(t, 15*time.Second, func() bool { return replicaSourcePort(t, nodes[2].db) == nodes[1].port })
	sqlExec(0, "INSERT INTO orch_e2e.marker VALUES (2)")
	eventually(t, 15*time.Second, func() bool {
		var count int
		return nodes[2].db.QueryRow("SELECT COUNT(*) FROM orch_e2e.marker").Scan(&count) == nil && count == 2
	})
	orch("register-candidate", "-i", instance(1), "--promotion-rule", "prefer")
	orch("discover", "-i", instance(1))
	orch("discover", "-i", instance(2))
	// 停止本测试创建的主库，再通过服务端恢复业务选举替代主库。
	if err := nodes[0].process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	orch("force-master-failover", "--cluster", instance(0))
	eventually(t, 15*time.Second, func() bool {
		var readOnly int
		return nodes[1].db.QueryRow("SELECT @@read_only").Scan(&readOnly) == nil && readOnly == 0
	})
	eventually(t, 15*time.Second, func() bool { return replicaSourcePort(t, nodes[2].db) == nodes[1].port })
	orch("discover", "-i", instance(1))
	orch("which-cluster-master", "--cluster", instance(1))
	t.Log("independent MySQL backend and single-node Raft: discovery, maintenance, tags, GTID relocation, replication data readback and forced failover passed")
}
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}
func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("isolated runtime condition did not converge")
}
func replicaSourcePort(t *testing.T, db *sql.DB) int {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SHOW REPLICA STATUS")
	if err != nil {
		return 0
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || !rows.Next() {
		return 0
	}
	values := make([]sql.RawBytes, len(columns))
	args := make([]any, len(columns))
	for i := range values {
		args[i] = &values[i]
	}
	if err := rows.Scan(args...); err != nil {
		return 0
	}
	for i, name := range columns {
		if name == "Source_Port" {
			port, _ := strconv.Atoi(string(values[i]))
			return port
		}
	}
	return 0
}
