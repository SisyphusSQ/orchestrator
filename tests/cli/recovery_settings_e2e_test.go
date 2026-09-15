package cli_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestRecoverySettingsLifecycle exercises the real HTTP, Raft and SQLite path.
// It is opt-in because it starts the built server binary on loopback ports.
func TestRecoverySettingsLifecycle(t *testing.T) {
	if os.Getenv("ORCH_RECOVERY_SETTINGS_E2E") != "1" {
		t.Skip("set ORCH_RECOVERY_SETTINGS_E2E=1 and build bin/orchestrator first")
	}
	binary, err := filepath.Abs("../../bin/orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	databasePath := filepath.Join(workDir, "orchestrator.db")
	auditPath := filepath.Join(workDir, "audit.log")
	httpPort, raftPort := freePort(t), freePort(t)
	configuration := map[string]any{
		"metadata": map[string]any{
			"type":   "sqlite3",
			"sqlite": map[string]any{"dataFile": databasePath},
		},
		"server": map[string]any{
			"listen": map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", httpPort)},
		},
		"topology": map[string]any{
			"hostname": map[string]any{"resolveMethod": "none"},
		},
		"raft": map[string]any{
			"nodeID":  "recovery-settings-e2e",
			"dataDir": filepath.Join(workDir, "raft"),
			"bind":    fmt.Sprintf("127.0.0.1:%d", raftPort),
		},
		"logging": map[string]any{"syslog": map[string]any{"enabled": false}},
		"audit":   map[string]any{"logFile": auditPath, "toSyslog": false},
	}
	configurationBytes, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	configurationPath := filepath.Join(workDir, "orchestrator.json")
	if err := os.WriteFile(configurationPath, configurationBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(workDir, "orchestrator.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	server := exec.Command(binary, "server", "--discovery=false", "--config="+configurationPath)
	server.Stdout, server.Stderr = logFile, logFile
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
		_ = logFile.Close()
		if t.Failed() {
			contents, _ := os.ReadFile(logPath)
			t.Logf("isolated server log:\n%s", contents)
		}
	})

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			payload, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			reader = bytes.NewReader(payload)
		}
		req, requestErr := http.NewRequestWithContext(t.Context(), method, baseURL+path, reader)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, requestErr := client.Do(req)
		if requestErr != nil {
			return 0, nil
		}
		defer response.Body.Close()
		var envelope map[string]any
		if decodeErr := json.NewDecoder(response.Body).Decode(&envelope); decodeErr != nil {
			t.Fatalf("decode %s %s: %v", method, path, decodeErr)
		}
		return response.StatusCode, envelope
	}
	ok := func(method, path string, body any) map[string]any {
		t.Helper()
		status, envelope := request(method, path, body)
		if status != http.StatusOK || envelope["Code"] != "OK" {
			t.Fatalf("%s %s status=%d response=%v", method, path, status, envelope)
		}
		return envelope
	}
	statusOnly := func(path string) int {
		t.Helper()
		response, requestErr := client.Get(baseURL + path)
		if requestErr != nil {
			return 0
		}
		defer response.Body.Close()
		return response.StatusCode
	}

	eventually(t, 15*time.Second, func() bool {
		return statusOnly("/api/lb-check") == http.StatusOK
	})
	ok(http.MethodPost, "/api/raft/bootstrap", nil)
	eventually(t, 15*time.Second, func() bool {
		return statusOnly("/api/leader-check") == http.StatusOK
	})

	global := ok(http.MethodGet, "/api/recovery-policy/global/*", nil)["Details"].(map[string]any)
	if global["revision"].(float64) != 0 || global["inherited"].(map[string]any)["recoveryPeriodBlockSeconds"].(float64) != 3600 {
		t.Fatalf("unexpected code defaults: %v", global)
	}
	globalSave := map[string]any{
		"scopeType": "global", "scopeKey": "*", "expectedRevision": 0,
		"overrides":    map[string]any{"autoMasterRecovery": true, "recoveryPeriodBlockSeconds": 12},
		"changeReason": "live E2E global policy",
	}
	ok(http.MethodPost, "/api/recovery-policy", globalSave)
	status, conflict := request(http.MethodPost, "/api/recovery-policy", globalSave)
	if status != http.StatusConflict || conflict["Code"] != "ERROR" {
		t.Fatalf("stale policy update status=%d response=%v", status, conflict)
	}

	ok(http.MethodGet, "/api/set-cluster-alias/orders?alias=orders-prod", nil)
	ok(http.MethodPost, "/api/recovery-policy", map[string]any{
		"scopeType": "cluster", "scopeKey": "orders-prod", "expectedRevision": 0,
		"overrides":    map[string]any{"recoveryPeriodBlockSeconds": 7},
		"changeReason": "live E2E cluster override",
	})
	cluster := ok(http.MethodGet, "/api/recovery-policy/cluster/orders-prod", nil)["Details"].(map[string]any)
	if cluster["inherited"].(map[string]any)["autoMasterRecovery"] != true || cluster["effective"].(map[string]any)["recoveryPeriodBlockSeconds"].(float64) != 7 {
		t.Fatalf("cluster precedence readback: %v", cluster)
	}

	ok(http.MethodPost, "/api/recovery-hook-profiles", map[string]any{
		"profile": map[string]any{
			"id": "e2e-hook", "name": "E2E Hook",
			"commands":       []string{"printf 'password=supersecret ready=yes'"},
			"timeoutSeconds": 5, "failurePolicy": "abort", "outputLimitBytes": 1024,
			"enabled": true, "changeReason": "live E2E hook profile",
		},
		"expectedRevision": 0,
	})
	testResult := ok(http.MethodPost, "/api/recovery-hook-test", map[string]any{"profileId": "e2e-hook"})
	result := testResult["Details"].([]any)[0].(map[string]any)
	output := result["output"].(string)
	if strings.Contains(output, "supersecret") || !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("hook output was not redacted: %q", output)
	}
	saveHook := func(id, failurePolicy string, commands []string, timeoutSeconds, outputLimitBytes int) {
		t.Helper()
		ok(http.MethodPost, "/api/recovery-hook-profiles", map[string]any{
			"profile": map[string]any{
				"id": id, "name": id, "commands": commands,
				"timeoutSeconds": timeoutSeconds, "failurePolicy": failurePolicy, "outputLimitBytes": outputLimitBytes,
				"enabled": true, "changeReason": "TOO-415 hook boundary E2E",
			},
			"expectedRevision": 0,
		})
	}
	saveHook("e2e-timeout-abort", "abort", []string{"sleep 2; printf late", "printf must-not-run"}, 1, 1024)
	timeoutResults := ok(http.MethodPost, "/api/recovery-hook-test", map[string]any{"profileId": "e2e-timeout-abort"})["Details"].([]any)
	if len(timeoutResults) != 1 || timeoutResults[0].(map[string]any)["error"] == nil || strings.Contains(timeoutResults[0].(map[string]any)["output"].(string), "must-not-run") {
		t.Fatalf("abort-on-timeout hook results: %v", timeoutResults)
	}

	saveHook("e2e-failure-continue", "continue", []string{"printf failed-output; exit 7", "printf continued-output"}, 5, 1024)
	continueResults := ok(http.MethodPost, "/api/recovery-hook-test", map[string]any{"profileId": "e2e-failure-continue"})["Details"].([]any)
	if len(continueResults) != 2 || continueResults[0].(map[string]any)["error"] == nil || continueResults[1].(map[string]any)["error"] != nil || continueResults[1].(map[string]any)["output"] != "continued-output" {
		t.Fatalf("continue-on-failure hook results: %v", continueResults)
	}

	saveHook("e2e-output-limit", "abort", []string{"printf '%02048d' 0"}, 5, 1024)
	limitedResults := ok(http.MethodPost, "/api/recovery-hook-test", map[string]any{"profileId": "e2e-output-limit"})["Details"].([]any)
	limitedOutput := limitedResults[0].(map[string]any)["output"].(string)
	if len(limitedResults) != 1 || len(limitedOutput) != 1024 || limitedResults[0].(map[string]any)["error"] != nil {
		t.Fatalf("bounded hook output len=%d results=%v", len(limitedOutput), limitedResults)
	}
	for _, assignment := range []map[string]any{
		{"scopeType": "global", "scopeKey": "*", "phase": "post_failover", "mode": "replace", "profileIds": []string{"e2e-hook"}, "revision": 0, "changeReason": "live E2E global hook"},
		{"scopeType": "cluster", "scopeKey": "orders-prod", "phase": "post_failover", "mode": "disable", "profileIds": []string{}, "revision": 0, "changeReason": "live E2E cluster hook override"},
	} {
		ok(http.MethodPost, "/api/recovery-hook-assignments", map[string]any{"assignment": assignment, "expectedRevision": 0})
	}
	clusterAssignments := ok(http.MethodGet, "/api/recovery-hook-assignments/cluster/orders-prod", nil)["Details"].([]any)
	if len(clusterAssignments) != 1 || clusterAssignments[0].(map[string]any)["mode"] != "disable" {
		t.Fatalf("cluster hook override readback: %v", clusterAssignments)
	}

	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for table, want := range map[string]int{"recovery_policy": 2, "recovery_hook_profile": 4, "recovery_hook_assignment": 2} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s row count=%d, want=%d, error=%v", table, count, want, err)
		}
	}

	response, err := client.Get(baseURL + "/web/recovery-settings")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("recovery settings web route status=%d", response.StatusCode)
	}
	auditContents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(auditContents), "test-recovery-hook") != 5 {
		t.Fatalf("audit file hook entries=%d, want 5", strings.Count(string(auditContents), "test-recovery-hook"))
	}
	t.Log("live recovery settings E2E passed: defaults, sparse global/cluster precedence, revision conflict, hook redaction/timeout/output-limit/continue/abort, audit file, inherit override storage and web route")
}
