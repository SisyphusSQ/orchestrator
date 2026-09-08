package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestHTTPRaftLifecycle(t *testing.T) {
	if os.Getenv("ORCH_E2E") != "1" {
		t.Skip("set ORCH_E2E=1; launches isolated Raft processes")
	}
	server, err := filepath.Abs("../../bin/orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := filepath.Abs("../../bin/orch")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	type node struct {
		endpoint, address string
		cmd               *exec.Cmd
		db                *sql.DB
		start             func() *exec.Cmd
	}
	nodes := []node{}
	invoke := func(endpoint string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, cli, append([]string{"--endpoint", endpoint, "--timeout", "10s", "--output", "json"}, args...)...).CombinedOutput()
	}
	must := func(endpoint string, args ...string) []byte {
		t.Helper()
		out, err := invoke(endpoint, args...)
		if err != nil {
			t.Fatalf("orch %s: %v: %s", args[0], err, out)
		}
		return out
	}
	for i := range 3 {
		home := filepath.Join(dir, fmt.Sprint(i))
		if err := os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		endpoint := fmt.Sprintf("http://127.0.0.1:%d", freePort(t))
		address := fmt.Sprintf("127.0.0.1:%d", freePort(t))
		dbfile := filepath.Join(home, "backend.db")
		config := map[string]any{"BackendDB": "sqlite", "SQLite3DataFile": dbfile, "ListenAddress": strings.TrimPrefix(endpoint, "http://"), "HTTPAdvertise": endpoint, "HostnameResolveMethod": "none", "RaftNodeID": fmt.Sprintf("e2e-%d", i), "RaftBind": address, "RaftAdvertise": address, "RaftDataDir": home, "Debug": false, "EnableSyslog": false, "AuditToSyslog": false, "InstancePollSeconds": 60}
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
		eventually(t, 15*time.Second, func() bool { _, err := invoke(endpoint, "raft-configuration"); return err == nil })
		db, err := sql.Open("sqlite3", "file:"+dbfile+"?mode=ro&_busy_timeout=1000")
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
	t.Log("follower routing, SQL readback, leadership transfer, snapshot/restart, leader replacement and no-quorum rejection passed")
}
