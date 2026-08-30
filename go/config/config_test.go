package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/golib/log"
	test "github.com/openark/golib/tests"
)

const (
	forceReadChildEnv      = "ORCHESTRATOR_FORCE_READ_CHILD"
	forceReadConfigPathEnv = "ORCHESTRATOR_FORCE_READ_CONFIG_PATH"
	forceReadStdinChildEnv = "ORCHESTRATOR_FORCE_READ_STDIN_CHILD"
)

func init() {
	Config.HostnameResolveMethod = "none"
	log.SetLevel(log.ERROR)
}

func runForceReadInChildProcess(t *testing.T, testName string, content string) ([]byte, error) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "orchestrator.conf.json")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(),
		forceReadChildEnv+"=1",
		forceReadConfigPathEnv+"="+configPath,
	)
	return cmd.CombinedOutput()
}

func forceReadChildConfig() bool {
	if os.Getenv(forceReadChildEnv) == "" {
		return false
	}
	ForceRead(os.Getenv(forceReadConfigPathEnv))
	return true
}

func TestForceReadRejectsRemovedZkAddress(t *testing.T) {
	if forceReadChildConfig() {
		return
	}

	testCases := map[string]string{
		"configured":       `{"ZkAddress":"zk-1:2181"}`,
		"empty":            `{"ZkAddress":""}`,
		"case-insensitive": `{"zkaddress":"zk-1:2181"}`,
	}
	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			output, err := runForceReadInChildProcess(t, "TestForceReadRejectsRemovedZkAddress", content)
			if err == nil {
				t.Fatalf("expected configuration containing ZkAddress to fail, output: %s", output)
			}
			for _, expected := range []string{"ZkAddress", "ZooKeeper", "Consul KV", "external failover hook"} {
				if !strings.Contains(string(output), expected) {
					t.Fatalf("expected failure output to contain %q, got: %s", expected, output)
				}
			}
		})
	}
}

func TestForceReadAllowsOtherUnknownFields(t *testing.T) {
	if forceReadChildConfig() {
		return
	}

	output, err := runForceReadInChildProcess(t, "TestForceReadAllowsOtherUnknownFields", `{"Debug":true,"FutureSetting":true}`)
	if err != nil {
		t.Fatalf("expected unrelated unknown configuration fields to remain accepted, got %v: %s", err, output)
	}
}

func TestForceReadAllowsNonSeekableInput(t *testing.T) {
	if os.Getenv(forceReadStdinChildEnv) != "" {
		ForceRead("/dev/stdin")
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestForceReadAllowsNonSeekableInput$")
	cmd.Env = append(os.Environ(), forceReadStdinChildEnv+"=1")
	cmd.Stdin = strings.NewReader(`{"Debug":true}`)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected non-seekable configuration input to remain accepted, got %v: %s", err, output)
	}
}

func TestReplicationLagQuery(t *testing.T) {
	{
		c := newConfiguration()
		c.SlaveLagQuery = "select 3"
		c.ReplicationLagQuery = "select 4"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.SlaveLagQuery = "select 3"
		c.ReplicationLagQuery = "select 3"
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.SlaveLagQuery = "select 3"
		c.ReplicationLagQuery = ""
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.ReplicationLagQuery, "select 3")
	}
}

func TestPostponeReplicaRecoveryOnLagMinutes(t *testing.T) {
	{
		c := newConfiguration()
		c.PostponeSlaveRecoveryOnLagMinutes = 3
		c.PostponeReplicaRecoveryOnLagMinutes = 5
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.PostponeSlaveRecoveryOnLagMinutes = 3
		c.PostponeReplicaRecoveryOnLagMinutes = 3
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.PostponeSlaveRecoveryOnLagMinutes = 3
		c.PostponeReplicaRecoveryOnLagMinutes = 0
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.PostponeReplicaRecoveryOnLagMinutes, uint(3))
	}
}

func TestMasterFailoverDetachReplicaMasterHost(t *testing.T) {
	{
		c := newConfiguration()
		c.MasterFailoverDetachSlaveMasterHost = false
		c.MasterFailoverDetachReplicaMasterHost = false
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectFalse(c.MasterFailoverDetachReplicaMasterHost)
	}
	{
		c := newConfiguration()
		c.MasterFailoverDetachSlaveMasterHost = false
		c.MasterFailoverDetachReplicaMasterHost = true
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectTrue(c.MasterFailoverDetachReplicaMasterHost)
	}
	{
		c := newConfiguration()
		c.MasterFailoverDetachSlaveMasterHost = true
		c.MasterFailoverDetachReplicaMasterHost = false
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectTrue(c.MasterFailoverDetachReplicaMasterHost)
	}
}

func TestMasterFailoverDetachDetachLostReplicasAfterMasterFailover(t *testing.T) {
	{
		c := newConfiguration()
		c.DetachLostSlavesAfterMasterFailover = false
		c.DetachLostReplicasAfterMasterFailover = false
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectFalse(c.DetachLostReplicasAfterMasterFailover)
	}
	{
		c := newConfiguration()
		c.DetachLostSlavesAfterMasterFailover = false
		c.DetachLostReplicasAfterMasterFailover = true
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectTrue(c.DetachLostReplicasAfterMasterFailover)
	}
	{
		c := newConfiguration()
		c.DetachLostSlavesAfterMasterFailover = true
		c.DetachLostReplicasAfterMasterFailover = false
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectTrue(c.DetachLostReplicasAfterMasterFailover)
	}
}

func TestRecoveryPeriodBlock(t *testing.T) {
	{
		c := newConfiguration()
		c.RecoveryPeriodBlockSeconds = 0
		c.RecoveryPeriodBlockMinutes = 0
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RecoveryPeriodBlockSeconds, 0)
	}
	{
		c := newConfiguration()
		c.RecoveryPeriodBlockSeconds = 30
		c.RecoveryPeriodBlockMinutes = 1
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RecoveryPeriodBlockSeconds, 30)
	}
	{
		c := newConfiguration()
		c.RecoveryPeriodBlockSeconds = 0
		c.RecoveryPeriodBlockMinutes = 2
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RecoveryPeriodBlockSeconds, 120)
	}
	{
		c := newConfiguration()
		c.RecoveryPeriodBlockSeconds = 15
		c.RecoveryPeriodBlockMinutes = 0
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RecoveryPeriodBlockSeconds, 15)
	}
}

func TestRaft(t *testing.T) {
	{
		c := newConfiguration()
		c.RaftBind = "1.2.3.4:1008"
		c.RaftDataDir = "/path/to/somewhere"
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftAdvertise, c.RaftBind)
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftBind = ""
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
}

func TestHttpAdvertise(t *testing.T) {
	{
		c := newConfiguration()
		c.HTTPAdvertise = ""
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.HTTPAdvertise = "http://127.0.0.1:1234"
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.HTTPAdvertise = "http://127.0.0.1"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.HTTPAdvertise = "127.0.0.1:1234"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.HTTPAdvertise = "http://127.0.0.1:1234/mypath"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
}
