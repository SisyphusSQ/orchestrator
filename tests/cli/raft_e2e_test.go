package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type raftMetadataNode struct {
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Database              string `json:"database"`
	User                  string `json:"user"`
	PasswordFile          string `json:"passwordFile"`
	CredentialsConfigFile string `json:"credentialsConfigFile"`
}

type raftMetadataFixture struct {
	Nodes    []raftMetadataNode   `json:"nodes"`
	Topology *raftTopologyFixture `json:"topology"`
}

type raftTopologyFixture struct {
	Host                  string `json:"host"`
	Ports                 []int  `json:"ports"`
	User                  string `json:"user"`
	PasswordFile          string `json:"passwordFile"`
	CredentialsConfigFile string `json:"credentialsConfigFile"`
}

func loadRaftMetadataFixture(t *testing.T) *raftMetadataFixture {
	t.Helper()
	path := os.Getenv("ORCH_RAFT_E2E_MYSQL_CONFIG")
	if path == "" {
		return nil
	}
	assertPrivateFile := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("external MySQL fixture file must not be group/world accessible: %s (%#o)", path, info.Mode().Perm())
		}
	}
	assertPrivateFile(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture raftMetadataFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode external MySQL fixture: %v", err)
	}
	if len(fixture.Nodes) != 0 && len(fixture.Nodes) != 3 {
		t.Fatalf("external MySQL fixture has %d metadata nodes; want zero or exactly three", len(fixture.Nodes))
	}
	for i, node := range fixture.Nodes {
		if node.Host == "" || node.Port <= 0 || node.Database == "" || node.User == "" || node.PasswordFile == "" || node.CredentialsConfigFile == "" {
			t.Fatalf("external MySQL fixture node %d is incomplete", i)
		}
		assertPrivateFile(node.PasswordFile)
		assertPrivateFile(node.CredentialsConfigFile)
	}
	if fixture.Topology != nil {
		topology := fixture.Topology
		if topology.Host == "" || len(topology.Ports) != 3 || topology.User == "" || topology.PasswordFile == "" || topology.CredentialsConfigFile == "" {
			t.Fatal("external MySQL topology fixture must describe exactly three instances")
		}
		for _, port := range topology.Ports {
			if port <= 0 {
				t.Fatalf("external MySQL topology fixture has invalid port %d", port)
			}
		}
		assertPrivateFile(topology.PasswordFile)
		assertPrivateFile(topology.CredentialsConfigFile)
	}
	if len(fixture.Nodes) == 0 && fixture.Topology == nil {
		t.Fatal("external MySQL fixture must provide metadata nodes, a three-instance topology, or both")
	}
	return &fixture
}

func TestHTTPRaftLifecycle(t *testing.T) {
	if os.Getenv("ORCH_E2E") != "1" {
		t.Skip("set ORCH_E2E=1; launches isolated Raft processes")
	}
	specialTopology := os.Getenv("ORCH_RAFT_E2E_SPECIAL_TOPOLOGY") == "1"
	server, err := filepath.Abs("../../bin/orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := filepath.Abs("../../bin/orch")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mysqlFixture := loadRaftMetadataFixture(t)
	hasExternalMetadata := mysqlFixture != nil && len(mysqlFixture.Nodes) == 3
	startupTimeout := 15 * time.Second
	if hasExternalMetadata {
		startupTimeout = 2 * time.Minute
	}
	operationTimeout, cliTimeout := 15*time.Second, "10s"
	if mysqlFixture != nil && mysqlFixture.Topology != nil {
		operationTimeout, cliTimeout = 55*time.Second, "50s"
	}
	var topologyDBs []*sql.DB
	if mysqlFixture != nil && mysqlFixture.Topology != nil {
		topology := mysqlFixture.Topology
		passwordBytes, err := os.ReadFile(topology.PasswordFile)
		if err != nil {
			t.Fatal(err)
		}
		password := strings.TrimSpace(string(passwordBytes))
		if password == "" || strings.ContainsAny(topology.User+password, ":@/") {
			t.Fatal("external MySQL topology fixture has an empty or unsupported user/password")
		}
		for _, port := range topology.Ports {
			dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=5s&readTimeout=10s&writeTimeout=10s&parseTime=true", topology.User, password, topology.Host, port)
			db, err := sql.Open("mysql", dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err := db.PingContext(t.Context()); err != nil {
				t.Fatalf("connect external MySQL topology port %d: %v", port, err)
			}
			topologyDBs = append(topologyDBs, db)
		}
		if specialTopology {
			if _, err := topologyDBs[0].ExecContext(t.Context(), "CREATE DATABASE IF NOT EXISTS `_pseudo_gtid_`"); err != nil {
				t.Fatalf("create pseudo-GTID schema: %v", err)
			}
		}
	}
	type node struct {
		endpoint, address string
		cmd               *exec.Cmd
		db                *sql.DB
		start             func() *exec.Cmd
	}
	nodes := []node{}
	invoke := func(endpoint string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
		defer cancel()
		return exec.CommandContext(ctx, cli, append([]string{"--endpoint", endpoint, "--timeout", cliTimeout, "--output", "json"}, args...)...).CombinedOutput()
	}
	must := func(endpoint string, args ...string) []byte {
		t.Helper()
		out, err := invoke(endpoint, args...)
		if err != nil {
			t.Fatalf("orch %s: %v: %s", args[0], err, out)
		}
		return out
	}
	requestJSON := func(endpoint, method, path string, body any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
		request, err := http.NewRequestWithContext(t.Context(), method, endpoint+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := (&http.Client{Timeout: operationTimeout}).Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer response.Body.Close()
		var envelope map[string]any
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
		return response.StatusCode, envelope
	}
	expectOK := func(endpoint, method, path string, body any) map[string]any {
		t.Helper()
		status, envelope := requestJSON(endpoint, method, path, body)
		if status != http.StatusOK || envelope["Code"] != "OK" {
			t.Fatalf("%s %s status=%d response=%v", method, path, status, envelope)
		}
		return envelope
	}
	for i := range 3 {
		home := filepath.Join(dir, fmt.Sprint(i))
		if err := os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		endpoint := fmt.Sprintf("http://127.0.0.1:%d", freePort(t))
		address := fmt.Sprintf("127.0.0.1:%d", freePort(t))
		dbfile := filepath.Join(home, "backend.db")
		metadataConfig := map[string]any{"type": "sqlite", "sqlite": map[string]any{"dataFile": dbfile}}
		dbDriver, dbDSN := "sqlite3", "file:"+dbfile+"?mode=ro&_busy_timeout=1000"
		if hasExternalMetadata {
			fixtureNode := mysqlFixture.Nodes[i]
			passwordBytes, err := os.ReadFile(fixtureNode.PasswordFile)
			if err != nil {
				t.Fatal(err)
			}
			password := strings.TrimSpace(string(passwordBytes))
			if password == "" || strings.ContainsAny(fixtureNode.User+password, ":@/") {
				t.Fatalf("external MySQL fixture node %d has an empty or unsupported user/password", i)
			}
			metadataConfig = map[string]any{
				"type": "mysql",
				"mysql": map[string]any{
					"host":                  fixtureNode.Host,
					"port":                  fixtureNode.Port,
					"database":              fixtureNode.Database,
					"credentialsConfigFile": fixtureNode.CredentialsConfigFile,
					"readTimeoutSeconds":    30,
				},
			}
			dbDriver = "mysql"
			dbDSN = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=5s&readTimeout=10s&writeTimeout=10s&parseTime=true", fixtureNode.User, password, fixtureNode.Host, fixtureNode.Port, fixtureNode.Database)
		}
		topologyConfig := map[string]any{
			"hostname":  map[string]any{"resolveMethod": "none", "mysqlResolveMethod": "none"},
			"discovery": map[string]any{"pollSeconds": 60},
		}
		if mysqlFixture != nil && mysqlFixture.Topology != nil {
			topologyConfig["mysql"] = map[string]any{
				"credentialsConfigFile":       mysqlFixture.Topology.CredentialsConfigFile,
				"discoveryReadTimeoutSeconds": 10,
				"readTimeoutSeconds":          30,
			}
			topologyConfig["discovery"].(map[string]any)["useShowReplicaHosts"] = true
		}
		config := map[string]any{
			"metadata": metadataConfig,
			"server": map[string]any{
				"listen":        map[string]any{"address": strings.TrimPrefix(endpoint, "http://")},
				"httpAdvertise": endpoint,
			},
			"topology": topologyConfig,
			"raft": map[string]any{
				"nodeID": fmt.Sprintf("e2e-%d", i), "bind": address, "advertise": address, "dataDir": home,
			},
			"logging": map[string]any{"debug": false, "syslog": map[string]any{"enabled": false}},
			"audit":   map[string]any{"toBackend": true, "toSyslog": false},
		}
		if specialTopology {
			config["pseudoGTID"] = map[string]any{"auto": true}
		}
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		configfile := filepath.Join(home, "config.json")
		if err := os.WriteFile(configfile, raw, 0600); err != nil {
			t.Fatal(err)
		}
		start := func() *exec.Cmd {
			log, err := os.Create(filepath.Join(home, "server.log"))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(server, "server", "--config="+configfile)
			if i == 2 {
				cmd.Args = append(cmd.Args, "--discovery=false")
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
					raw, _ := os.ReadFile(log.Name())
					if len(raw) > 4000 {
						raw = raw[len(raw)-4000:]
					}
					t.Logf("node %d log: %s", i, raw)
				}
			})
			return cmd
		}
		cmd := start()
		eventually(t, startupTimeout, func() bool { _, err := invoke(endpoint, "raft-configuration"); return err == nil })
		db, err := sql.Open(dbDriver, dbDSN)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		nodes = append(nodes, node{endpoint, address, cmd, db, start})
	}
	must(nodes[0].endpoint, "raft-bootstrap")
	eventually(t, 10*time.Second, func() bool { _, err := invoke(nodes[0].endpoint, "api", "leader-check"); return err == nil })
	for i := 1; i < 3; i++ {
		body := fmt.Sprintf(`{"id":"e2e-%d","address":%q,"suffrage":"voter"}`, i, nodes[i].address)
		must(nodes[0].endpoint, "raft-add-member", "--body", body)
	}
	// Wait for follower routing readiness before making a single write through it.
	eventually(t, 15*time.Second, func() bool { _, err := invoke(nodes[1].endpoint, "api", "routed-leader-check"); return err == nil })
	if mysqlFixture != nil && mysqlFixture.Topology != nil {
		topology := mysqlFixture.Topology
		instances := make([]string, 0, len(topology.Ports))
		for _, port := range topology.Ports {
			instance := fmt.Sprintf("%s:%d", topology.Host, port)
			instances = append(instances, instance)
			must(nodes[1].endpoint, "discover", "-i", instance)
		}
		for i := range nodes {
			eventually(t, 30*time.Second, func() bool {
				var count int
				return nodes[i].db.QueryRow("SELECT COUNT(*) FROM database_instance WHERE hostname=?", topology.Host).Scan(&count) == nil && count == 3
			})
		}

		// Exercise the full operator-facing discovery lifecycle against the real
		// three-copy topology and verify every Raft metadata copy converges.
		asyncAuditCount := func() (int, bool) {
			count := 0
			for i := range nodes {
				var nodeCount int
				if nodes[i].db.QueryRow("SELECT COUNT(*) FROM audit WHERE audit_type='async-discover' AND hostname=? AND port=? AND message='completed'", topology.Host, topology.Ports[2]).Scan(&nodeCount) != nil {
					return 0, false
				}
				count += nodeCount
			}
			return count, true
		}
		asyncAuditBefore, ok := asyncAuditCount()
		if !ok {
			t.Fatal("read asynchronous discovery audit baseline")
		}
		must(nodes[2].endpoint, "async-discover", "-i", instances[2])
		for i := range nodes {
			eventually(t, 30*time.Second, func() bool {
				var count int
				return nodes[i].db.QueryRow("SELECT COUNT(*) FROM database_instance WHERE hostname=? AND port=?", topology.Host, topology.Ports[2]).Scan(&count) == nil && count == 1
			})
		}
		eventually(t, 30*time.Second, func() bool {
			count, ok := asyncAuditCount()
			return ok && count > asyncAuditBefore
		})

		invalidDowntimePath := fmt.Sprintf("/api/begin-downtime/%s/%d/too415/invalid/%s", url.PathEscape(topology.Host), topology.Ports[2], url.PathEscape("-1s"))
		_, invalidDowntime := requestJSON(nodes[0].endpoint, http.MethodPost, invalidDowntimePath, nil)
		if invalidDowntime["Code"] != "ERROR" {
			t.Fatalf("negative downtime duration accepted: %v", invalidDowntime)
		}
		beginDowntimePath := fmt.Sprintf("/api/begin-downtime/%s/%d/too415/%s/%s", url.PathEscape(topology.Host), topology.Ports[2], url.PathEscape("discovery lifecycle"), url.PathEscape("1h"))
		expectOK(nodes[1].endpoint, http.MethodPost, beginDowntimePath, nil)
		for i := range nodes {
			eventually(t, 30*time.Second, func() bool {
				var count int
				return nodes[i].db.QueryRow("SELECT COUNT(*) FROM database_instance_downtime WHERE hostname=? AND port=? AND downtime_active=1", topology.Host, topology.Ports[2]).Scan(&count) == nil && count == 1
			})
		}
		must(nodes[2].endpoint, "which-downtimed-instances")
		must(nodes[0].endpoint, "end-downtime", "-i", instances[2])
		for i := range nodes {
			eventually(t, 30*time.Second, func() bool {
				var count int
				return nodes[i].db.QueryRow("SELECT COUNT(*) FROM database_instance_downtime WHERE hostname=? AND port=?", topology.Host, topology.Ports[2]).Scan(&count) == nil && count == 0
			})
		}

		var clusterName string
		if err := nodes[0].db.QueryRow("SELECT cluster_name FROM database_instance WHERE hostname=? AND port=?", topology.Host, topology.Ports[0]).Scan(&clusterName); err != nil {
			t.Fatal(err)
		}
		aliasPath := "/api/set-cluster-alias/" + url.PathEscape(clusterName)
		_, invalidAlias := requestJSON(nodes[0].endpoint, http.MethodPost, aliasPath+"?alias=", nil)
		if invalidAlias["Code"] != "ERROR" {
			t.Fatalf("empty cluster alias accepted: %v", invalidAlias)
		}
		for _, alias := range []string{"too415-discovery-a", "too415-discovery-b"} {
			expectOK(nodes[1].endpoint, http.MethodPost, aliasPath+"?alias="+url.QueryEscape(alias), nil)
			for i := range nodes {
				eventually(t, 30*time.Second, func() bool {
					var got string
					return nodes[i].db.QueryRow("SELECT alias FROM cluster_alias_override WHERE cluster_name=?", clusterName).Scan(&got) == nil && got == alias
				})
			}
		}
		must(nodes[0].endpoint, "discover", "-i", instances[0])
		aliasOutput := must(nodes[2].endpoint, "which-cluster-alias", "-i", instances[0])
		if !strings.Contains(string(aliasOutput), "too415-discovery-b") {
			t.Fatalf("updated cluster alias not visible: %s", aliasOutput)
		}
		t.Log("discovery lifecycle passed: asynchronous completion with request-independent audit, downtime create/read/delete with invalid-duration rejection, and alias create/update/read with empty-alias rejection")
		if specialTopology {
			for index := range instances {
				eventually(t, 30*time.Second, func() bool {
					output, err := invoke(nodes[index].endpoint, "last-pseudo-gtid", "-i", instances[index], "--strict")
					return err == nil && strings.Contains(string(output), "_pseudo_gtid_")
				})
			}

			must(nodes[1].endpoint, "set-read-only", "-i", instances[1])
			must(nodes[2].endpoint, "set-read-only", "-i", instances[2])
			if mysqlInt(t, topologyDBs[2], "SELECT AUTO_POSITION FROM performance_schema.replication_connection_configuration WHERE CHANNEL_NAME=''") == 1 {
				must(nodes[0].endpoint, "disable-gtid", "-i", instances[2])
			}
			if mysqlInt(t, topologyDBs[2], "SELECT AUTO_POSITION FROM performance_schema.replication_connection_configuration WHERE CHANNEL_NAME=''") != 0 {
				t.Fatal("disable-gtid did not switch c to file-position replication")
			}

			must(nodes[1].endpoint, "delay-replication", "-i", instances[1], "--seconds", "3")
			eventually(t, 20*time.Second, func() bool {
				return mysqlInt(t, topologyDBs[1], "SELECT DESIRED_DELAY FROM performance_schema.replication_applier_configuration WHERE CHANNEL_NAME=''") == 3
			})
			must(nodes[2].endpoint, "delay-replication", "-i", instances[1], "--seconds", "0")
			eventually(t, 20*time.Second, func() bool {
				return mysqlInt(t, topologyDBs[1], "SELECT DESIRED_DELAY FROM performance_schema.replication_applier_configuration WHERE CHANNEL_NAME=''") == 0
			})

			must(nodes[1].endpoint, "enable-semi-sync-master", "-i", instances[0])
			must(nodes[2].endpoint, "enable-semi-sync-replica", "-i", instances[1])
			must(nodes[0].endpoint, "enable-semi-sync-replica", "-i", instances[2])
			for index, query := range []string{
				"SELECT @@global.rpl_semi_sync_source_enabled",
				"SELECT @@global.rpl_semi_sync_replica_enabled",
				"SELECT @@global.rpl_semi_sync_replica_enabled",
			} {
				if got := mysqlInt(t, topologyDBs[index], query); got != 1 {
					t.Fatalf("semi-sync enable readback node=%d value=%d", index, got)
				}
			}
			must(nodes[2].endpoint, "disable-semi-sync-replica", "-i", instances[2])
			must(nodes[1].endpoint, "disable-semi-sync-replica", "-i", instances[1])
			must(nodes[0].endpoint, "disable-semi-sync-master", "-i", instances[0])
			for index, query := range []string{
				"SELECT @@global.rpl_semi_sync_source_enabled",
				"SELECT @@global.rpl_semi_sync_replica_enabled",
				"SELECT @@global.rpl_semi_sync_replica_enabled",
			} {
				if got := mysqlInt(t, topologyDBs[index], query); got != 0 {
					t.Fatalf("semi-sync disable readback node=%d value=%d", index, got)
				}
			}

			errantGTID := uuid.NewString() + ":1"
			errantConnection, err := topologyDBs[2].Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				fmt.Sprintf("SET SESSION GTID_NEXT='%s'", errantGTID),
				"BEGIN",
				"COMMIT",
				"SET SESSION GTID_NEXT='AUTOMATIC'",
			} {
				if _, err := errantConnection.ExecContext(t.Context(), statement); err != nil {
					_ = errantConnection.Close()
					t.Fatalf("inject errant GTID statement %q: %v", statement, err)
				}
			}
			if err := errantConnection.Close(); err != nil {
				t.Fatal(err)
			}
			must(nodes[0].endpoint, "discover", "-i", instances[2])
			eventually(t, 30*time.Second, func() bool {
				output, err := invoke(nodes[0].endpoint, "which-gtid-errant", "-i", instances[2])
				return err == nil && strings.Contains(string(output), errantGTID)
			})
			eventually(t, 30*time.Second, func() bool {
				output, err := invoke(nodes[0].endpoint, "locate-gtid-errant", "-i", instances[2])
				return err == nil && strings.Contains(string(output), "binlog")
			})
			must(nodes[0].endpoint, "gtid-errant-inject-empty", "-i", instances[2])
			for index := 0; index < 3; index++ {
				eventually(t, 30*time.Second, func() bool {
					return mysqlInt(t, topologyDBs[index], fmt.Sprintf("SELECT GTID_SUBSET('%s', @@global.gtid_executed)", errantGTID)) == 1
				})
			}
			must(nodes[0].endpoint, "discover", "-i", instances[0])
			must(nodes[0].endpoint, "discover", "-i", instances[2])
			eventually(t, 30*time.Second, func() bool {
				output, err := invoke(nodes[0].endpoint, "which-gtid-errant", "-i", instances[2])
				return err == nil && !strings.Contains(string(output), errantGTID)
			})

			beforeBinlog := firstMySQLColumn(t, topologyDBs[0], "SHOW MASTER STATUS")
			must(nodes[1].endpoint, "flush-binary-logs", "-i", instances[0])
			must(nodes[2].endpoint, "flush-binary-logs", "-i", instances[0])
			afterBinlog := firstMySQLColumn(t, topologyDBs[0], "SHOW MASTER STATUS")
			if beforeBinlog == afterBinlog {
				t.Fatalf("flush-binary-logs did not rotate: %s", beforeBinlog)
			}
			if _, err := topologyDBs[0].ExecContext(t.Context(), "CREATE TABLE IF NOT EXISTS too415_app.special_marker (id INT PRIMARY KEY, note VARCHAR(64))"); err != nil {
				t.Fatal(err)
			}
			if _, err := topologyDBs[0].ExecContext(t.Context(), "REPLACE INTO too415_app.special_marker VALUES (415, 'before-purge')"); err != nil {
				t.Fatal(err)
			}
			for index := 1; index < 3; index++ {
				eventually(t, 30*time.Second, func() bool {
					return mysqlInt(t, topologyDBs[index], "SELECT COUNT(*) FROM too415_app.special_marker WHERE id=415 AND note='before-purge'") == 1
				})
			}
			must(nodes[1].endpoint, "discover", "-i", instances[1])
			must(nodes[2].endpoint, "discover", "-i", instances[2])
			must(nodes[0].endpoint, "purge-binary-logs", "-i", instances[0], "--binlog", afterBinlog)
			if oldestBinlog := firstMySQLColumn(t, topologyDBs[0], "SHOW BINARY LOGS"); oldestBinlog != afterBinlog {
				t.Fatalf("purge-binary-logs oldest=%s, want %s", oldestBinlog, afterBinlog)
			}
			t.Log("special topology operations passed: delay 3->0, semi-sync source/replicas enable->disable, errant GTID locate/inject-empty/remediation, two binlog flushes and safe purge after three-copy readback")
		}
		must(nodes[1].endpoint, "begin-maintenance", "-i", instances[2], "--owner", "too415", "--reason", "remote three-copy acceptance", "--duration", "1h")
		must(nodes[2].endpoint, "in-maintenance", "-i", instances[2])
		must(nodes[0].endpoint, "end-maintenance", "-i", instances[2])
		must(nodes[1].endpoint, "submit-pool-instances", "--pool", "too415", "--instances", strings.Join(instances[1:], ","))
		var pools []map[string]any
		if err := json.Unmarshal(must(nodes[2].endpoint, "cluster-pool-instances", "--pool", "too415"), &pools); err != nil || len(pools) != 2 {
			t.Fatalf("remote pool membership readback: %v, %v", pools, err)
		}
		must(nodes[0].endpoint, "submit-pool-instances", "--pool", "too415", "--instances", "")
		must(nodes[1].endpoint, "stop-replica", "-i", instances[2])
		must(nodes[2].endpoint, "start-replica", "-i", instances[2])
		must(nodes[1].endpoint, "set-writeable", "-i", instances[2])
		var readOnly int
		if err := topologyDBs[2].QueryRowContext(t.Context(), "SELECT @@read_only").Scan(&readOnly); err != nil || readOnly != 0 {
			t.Fatalf("remote set-writeable readback: read_only=%d error=%v", readOnly, err)
		}
		must(nodes[2].endpoint, "set-read-only", "-i", instances[2])
		if specialTopology {
			must(nodes[1].endpoint, "match", "-i", instances[2], "-d", instances[1])
		} else {
			must(nodes[1].endpoint, "relocate", "-i", instances[2], "-d", instances[1])
		}
		eventually(t, 30*time.Second, func() bool { return replicaSourcePort(t, topologyDBs[2]) == topology.Ports[1] })
		if specialTopology && mysqlInt(t, topologyDBs[2], "SELECT AUTO_POSITION FROM performance_schema.replication_connection_configuration WHERE CHANNEL_NAME=''") != 0 {
			t.Fatal("Pseudo-GTID match did not switch c to file-position replication")
		}
		if _, err := topologyDBs[0].ExecContext(t.Context(), "CREATE TABLE IF NOT EXISTS too415_app.raft_marker (id INT PRIMARY KEY, note VARCHAR(64))"); err != nil {
			t.Fatal(err)
		}
		if _, err := topologyDBs[0].ExecContext(t.Context(), "REPLACE INTO too415_app.raft_marker VALUES (415, 'three-voter-chain')"); err != nil {
			t.Fatal(err)
		}
		eventually(t, 30*time.Second, func() bool {
			var count int
			return topologyDBs[2].QueryRowContext(t.Context(), "SELECT COUNT(*) FROM too415_app.raft_marker WHERE id=415").Scan(&count) == nil && count == 1
		})
		if specialTopology {
			must(nodes[0].endpoint, "move-up", "-i", instances[2])
		} else {
			must(nodes[0].endpoint, "relocate", "-i", instances[2], "-d", instances[0])
		}
		eventually(t, 30*time.Second, func() bool { return replicaSourcePort(t, topologyDBs[2]) == topology.Ports[0] })
		if specialTopology && mysqlInt(t, topologyDBs[2], "SELECT AUTO_POSITION FROM performance_schema.replication_connection_configuration WHERE CHANNEL_NAME=''") != 0 {
			t.Fatal("classic move-up did not preserve file-position replication")
		}
		if specialTopology {
			for index := range instances {
				must(nodes[index].endpoint, "discover", "-i", instances[index])
			}
			must(nodes[0].endpoint, "api", "leader-check")
			if output, err := invoke(nodes[0].endpoint, "graceful-master-takeover", "-i", instances[0], "-d", instances[1]); err != nil {
				var recoveries, detections int
				_ = nodes[0].db.QueryRow("SELECT COUNT(*) FROM topology_recovery").Scan(&recoveries)
				_ = nodes[0].db.QueryRow("SELECT COUNT(*) FROM topology_failure_detection").Scan(&detections)
				_, leaderErr := invoke(nodes[0].endpoint, "api", "leader-check")
				t.Fatalf("graceful non-auto failed: %v: %s; leader-check=%v recoveries=%d detections=%d", err, output, leaderErr, recoveries, detections)
			}
			eventually(t, 30*time.Second, func() bool {
				return mysqlInt(t, topologyDBs[1], "SELECT @@read_only") == 0 &&
					replicaSourcePort(t, topologyDBs[0]) == topology.Ports[1] &&
					mysqlInt(t, topologyDBs[0], "SELECT COUNT(*) FROM performance_schema.replication_applier_status WHERE SERVICE_STATE='ON'") == 0
			})
			if _, err := topologyDBs[1].ExecContext(t.Context(), "REPLACE INTO too415_app.raft_marker VALUES (416, 'non-auto-takeover')"); err != nil {
				t.Fatal(err)
			}
			eventually(t, 30*time.Second, func() bool {
				return mysqlInt(t, topologyDBs[2], "SELECT COUNT(*) FROM too415_app.raft_marker WHERE id=416 AND note='non-auto-takeover'") == 1
			})
			must(nodes[2].endpoint, "start-replica", "-i", instances[0])
			eventually(t, 30*time.Second, func() bool {
				return mysqlInt(t, topologyDBs[0], "SELECT COUNT(*) FROM too415_app.raft_marker WHERE id=416 AND note='non-auto-takeover'") == 1
			})
			if replicaSourcePort(t, topologyDBs[2]) == topology.Ports[1] {
				must(nodes[0].endpoint, "match", "-i", instances[2], "-d", instances[0])
				eventually(t, 30*time.Second, func() bool { return replicaSourcePort(t, topologyDBs[2]) == topology.Ports[0] })
			}
			for index := range instances {
				must(nodes[index].endpoint, "discover", "-i", instances[index])
			}
			must(nodes[0].endpoint, "ack-all-recoveries", "--reason", "TOO-415 reverse graceful takeover")
			must(nodes[0].endpoint, "graceful-master-takeover-auto", "-i", instances[1], "-d", instances[0])
			eventually(t, 30*time.Second, func() bool {
				return mysqlInt(t, topologyDBs[0], "SELECT @@read_only") == 0 &&
					replicaSourcePort(t, topologyDBs[1]) == topology.Ports[0] &&
					mysqlInt(t, topologyDBs[1], "SELECT COUNT(*) FROM performance_schema.replication_applier_status WHERE SERVICE_STATE='ON'") == 1
			})
			if _, err := topologyDBs[0].ExecContext(t.Context(), "REPLACE INTO too415_app.raft_marker VALUES (417, 'auto-takeover-restored')"); err != nil {
				t.Fatal(err)
			}
			for index := 1; index < 3; index++ {
				eventually(t, 30*time.Second, func() bool {
					return mysqlInt(t, topologyDBs[index], "SELECT COUNT(*) FROM too415_app.raft_marker WHERE id=417 AND note='auto-takeover-restored'") == 1
				})
			}
			must(nodes[1].endpoint, "ack-all-recoveries", "--reason", "TOO-415 graceful takeover complete")
			t.Log("graceful takeover passed: non-auto left demoted a stopped, auto reverse restored a and started demoted b, with three-copy marker readback")
		}
	}
	if hasExternalMetadata {
		global := expectOK(nodes[1].endpoint, http.MethodGet, "/api/recovery-policy/global/*", nil)["Details"].(map[string]any)
		if global["revision"].(float64) != 0 {
			t.Fatalf("unexpected initial global recovery policy: %v", global)
		}
		globalSave := map[string]any{
			"scopeType": "global", "scopeKey": "*", "expectedRevision": 0,
			"overrides":    map[string]any{"autoMasterRecovery": true, "recoveryPeriodBlockSeconds": 12},
			"changeReason": "TOO-415 three-voter recovery policy",
		}
		expectOK(nodes[1].endpoint, http.MethodPost, "/api/recovery-policy", globalSave)
		status, conflict := requestJSON(nodes[2].endpoint, http.MethodPost, "/api/recovery-policy", globalSave)
		if status != http.StatusConflict || conflict["Code"] != "ERROR" {
			t.Fatalf("stale recovery policy update status=%d response=%v", status, conflict)
		}
		expectOK(nodes[1].endpoint, http.MethodGet, "/api/set-cluster-alias/too415?alias=too415-remote", nil)
		expectOK(nodes[2].endpoint, http.MethodPost, "/api/recovery-policy", map[string]any{
			"scopeType": "cluster", "scopeKey": "too415-remote", "expectedRevision": 0,
			"overrides":    map[string]any{"recoveryPeriodBlockSeconds": 7},
			"changeReason": "TOO-415 three-voter cluster override",
		})
		cluster := expectOK(nodes[0].endpoint, http.MethodGet, "/api/recovery-policy/cluster/too415-remote", nil)["Details"].(map[string]any)
		if cluster["inherited"].(map[string]any)["autoMasterRecovery"] != true || cluster["effective"].(map[string]any)["recoveryPeriodBlockSeconds"].(float64) != 7 {
			t.Fatalf("three-voter recovery policy precedence: %v", cluster)
		}
		expectOK(nodes[1].endpoint, http.MethodPost, "/api/recovery-hook-profiles", map[string]any{
			"profile": map[string]any{
				"id": "too415-hook", "name": "TOO-415 Hook", "commands": []string{"printf 'password=supersecret ready=yes'"},
				"timeoutSeconds": 5, "failurePolicy": "abort", "outputLimitBytes": 1024, "enabled": true,
				"changeReason": "TOO-415 three-voter hook",
			},
			"expectedRevision": 0,
		})
		hookResults := expectOK(nodes[2].endpoint, http.MethodPost, "/api/recovery-hook-test", map[string]any{"profileId": "too415-hook"})["Details"].([]any)
		hookOutput := hookResults[0].(map[string]any)["output"].(string)
		if strings.Contains(hookOutput, "supersecret") || !strings.Contains(hookOutput, "[REDACTED]") {
			t.Fatalf("three-voter hook output was not redacted: %q", hookOutput)
		}
		for i := range nodes {
			eventually(t, 30*time.Second, func() bool {
				var policies, profiles int
				policyErr := nodes[i].db.QueryRow("SELECT COUNT(*) FROM recovery_policy").Scan(&policies)
				profileErr := nodes[i].db.QueryRow("SELECT COUNT(*) FROM recovery_hook_profile").Scan(&profiles)
				return policyErr == nil && profileErr == nil && policies == 2 && profiles == 1
			})
		}
	}
	if readyPath := os.Getenv("ORCH_RAFT_E2E_READY_FILE"); readyPath != "" {
		ready := map[string]any{"endpoints": []string{nodes[0].endpoint, nodes[1].endpoint, nodes[2].endpoint}}
		raw, err := json.Marshal(ready)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(readyPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		releasePath := readyPath + ".release"
		readyTimeout := 5 * time.Minute
		if raw := os.Getenv("ORCH_RAFT_E2E_READY_TIMEOUT"); raw != "" {
			parsed, err := time.ParseDuration(raw)
			if err != nil || parsed <= 0 {
				t.Fatalf("invalid ORCH_RAFT_E2E_READY_TIMEOUT %q", raw)
			}
			readyTimeout = parsed
		}
		eventually(t, readyTimeout, func() bool {
			_, err := os.Stat(releasePath)
			return err == nil
		})
		_ = os.Remove(releasePath)
	}
	if mysqlFixture != nil && mysqlFixture.Topology != nil {
		topology := mysqlFixture.Topology
		forgotten := fmt.Sprintf("%s:%d", topology.Host, topology.Ports[2])
		must(nodes[1].endpoint, "forget", "-i", forgotten)
		for i := range nodes {
			eventually(t, 30*time.Second, func() bool {
				var count int
				return nodes[i].db.QueryRow("SELECT COUNT(*) FROM database_instance WHERE hostname=? AND port=?", topology.Host, topology.Ports[2]).Scan(&count) == nil && count == 0
			})
		}
		if output, err := invoke(nodes[0].endpoint, "forget", "-i", forgotten); err == nil {
			t.Fatalf("forget accepted an already absent instance: %s", output)
		}
		t.Log("forget lifecycle passed: three metadata copies removed the instance and repeated removal was rejected")
	}
	must(nodes[1].endpoint, "register-candidate", "-i", "127.0.0.1:19999", "--promotion-rule", "prefer")
	readRule := func(i int, want string) bool {
		var rule string
		return nodes[i].db.QueryRow("SELECT promotion_rule FROM candidate_database_instance WHERE hostname='127.0.0.1' AND port=19999").Scan(&rule) == nil && rule == want
	}
	for i := range nodes {
		eventually(t, 10*time.Second, func() bool { return readRule(i, "prefer") })
	}
	// Tag mutations must replicate as well, including removal on a follower.
	must(nodes[1].endpoint, "tag", "-i", "127.0.0.1:19999", "--tag", "purpose=raft-e2e")
	tagCount := func(i int) int {
		var count int
		if err := nodes[i].db.QueryRow("SELECT COUNT(*) FROM database_instance_tags WHERE tag_name='purpose'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	for i := range nodes {
		eventually(t, 10*time.Second, func() bool { return tagCount(i) == 1 })
	}
	must(nodes[1].endpoint, "untag", "-i", "127.0.0.1:19999", "--tag", "purpose")
	for i := range nodes {
		eventually(t, 10*time.Second, func() bool { return tagCount(i) == 0 })
	}
	must(nodes[1].endpoint, "tag", "-i", "127.0.0.1:19999", "--tag", "purpose=raft-e2e")
	must(nodes[1].endpoint, "untag-all", "--tag", "purpose=raft-e2e")
	for i := range nodes {
		eventually(t, 10*time.Second, func() bool { return tagCount(i) == 0 })
	}
	all := nodes[0].endpoint + "," + nodes[1].endpoint + "," + nodes[2].endpoint
	must(all, "register-candidate", "-i", "127.0.0.1:19999", "--promotion-rule", "neutral")
	for i := range nodes {
		eventually(t, 10*time.Second, func() bool { return readRule(i, "neutral") })
	}
	must(nodes[0].endpoint, "raft-transfer-leadership", "--body", `{"id":"e2e-1"}`)
	eventually(t, 10*time.Second, func() bool { _, err := invoke(nodes[1].endpoint, "api", "leader-check"); return err == nil })
	must(nodes[1].endpoint, "raft-transfer-leadership", "--body", `{"id":"e2e-0"}`)
	eventually(t, 10*time.Second, func() bool { _, err := invoke(nodes[0].endpoint, "api", "leader-check"); return err == nil })
	// Persist a real SQL snapshot, gracefully restart the follower with the same identity, and read it back.
	must(nodes[2].endpoint, "raft-snapshot")
	if err := nodes[2].cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := nodes[2].cmd.Wait(); err != nil {
		t.Fatalf("graceful shutdown: %v", err)
	}
	nodes[2].cmd = nodes[2].start()
	eventually(t, 15*time.Second, func() bool { _, err := invoke(nodes[2].endpoint, "api", "routed-leader-check"); return err == nil })
	eventually(t, 10*time.Second, func() bool { return readRule(2, "neutral") })
	if err := nodes[0].cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	leader := -1
	eventually(t, 15*time.Second, func() bool {
		for i := 1; i < 3; i++ {
			if _, err := invoke(nodes[i].endpoint, "api", "leader-check"); err == nil {
				leader = i
				return true
			}
		}
		return false
	})
	must(all, "register-candidate", "-i", "127.0.0.1:19999", "--promotion-rule", "prefer_not")
	for i := 1; i < 3; i++ {
		eventually(t, 10*time.Second, func() bool { return readRule(i, "prefer_not") })
	}
	other := 3 - leader
	if err := nodes[other].cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, func() bool { _, err := invoke(nodes[leader].endpoint, "api", "leader-check"); return err != nil })
	if out, err := invoke(nodes[leader].endpoint, "register-candidate", "-i", "127.0.0.1:19999", "--promotion-rule", "must_not"); err == nil {
		t.Fatalf("write without quorum succeeded: %s", out)
	}
	if !readRule(leader, "prefer_not") {
		t.Fatal("write without quorum changed the backend")
	}
	if out, err := invoke(all, "register-candidate", "-i", "127.0.0.1:19999", "--promotion-rule", "must_not"); err == nil || !strings.Contains(string(out), "no available leader") {
		t.Fatalf("multi-address quorum failure: %v: %s", err, out)
	}
	if mysqlFixture != nil && mysqlFixture.Topology != nil {
		if specialTopology {
			t.Log("remote three-copy topology and three-voter Raft passed: discovery, maintenance, pools, replication control, read-only changes, Pseudo-GTID match, classic file-position move-up, chained data readback/restore, SQL state replication, leadership transfer, snapshot/restart, leader replacement and no-quorum rejection")
		} else {
			t.Log("remote three-copy topology and three-voter Raft passed: discovery, maintenance, pools, replication control, read-only changes, chained replication relocation/data readback/restore, SQL state replication, leadership transfer, snapshot/restart, leader replacement and no-quorum rejection")
		}
	} else {
		t.Log("follower routing, SQL readback, leadership transfer, snapshot/restart, leader replacement and no-quorum rejection passed")
	}
}

func mysqlInt(t *testing.T, database *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := database.QueryRowContext(t.Context(), query).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func firstMySQLColumn(t *testing.T, database *sql.DB, query string) string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("query %q returned no rows", query)
	}
	values := make([]sql.RawBytes, len(columns))
	destinations := make([]any, len(columns))
	for index := range values {
		destinations[index] = &values[index]
	}
	if err := rows.Scan(destinations...); err != nil {
		t.Fatal(err)
	}
	return string(values[0])
}
