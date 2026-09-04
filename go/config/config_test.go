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
	forceReadStdinChildEnv = "ORCHESTRATOR_FORCE_READ_STDIN_CHILD"
)

func init() {
	Config.HostnameResolveMethod = "none"
	log.SetLevel(log.ERROR)
}

func writeConfigFixture(t *testing.T, content string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "orchestrator.conf.json")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return configPath
}

func TestForceReadRejectsRemovedZkAddress(t *testing.T) {
	testCases := map[string]string{
		"configured":       `{"ZkAddress":"zk-1:2181"}`,
		"empty":            `{"ZkAddress":""}`,
		"case-insensitive": `{"zkaddress":"zk-1:2181"}`,
	}
	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := ForceRead(writeConfigFixture(t, content))
			if err == nil {
				t.Fatal("expected configuration containing ZkAddress to fail")
			}
			for _, expected := range []string{"ZkAddress", "ZooKeeper", "Consul KV", "external failover hook"} {
				if !strings.Contains(err.Error(), expected) {
					t.Fatalf("expected failure error to contain %q, got: %v", expected, err)
				}
			}
		})
	}
}

func TestForceReadAllowsOtherUnknownFields(t *testing.T) {
	previous := *Config
	previousReadFileNames := append([]string(nil), readFileNames...)
	t.Cleanup(func() {
		*Config = previous
		readFileNames = previousReadFileNames
	})
	_, err := ForceRead(writeConfigFixture(t, `{"Debug":true,"FutureSetting":true}`))
	if err != nil {
		t.Fatalf("expected unrelated unknown configuration fields to remain accepted: %v", err)
	}
}

func TestForceReadAllowsNonSeekableInput(t *testing.T) {
	if os.Getenv(forceReadStdinChildEnv) != "" {
		if _, err := ForceRead("/dev/stdin"); err != nil {
			t.Fatalf("ForceRead(/dev/stdin) error: %v", err)
		}
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestForceReadAllowsNonSeekableInput$")
	cmd.Env = append(os.Environ(), forceReadStdinChildEnv+"=1")
	cmd.Stdin = strings.NewReader(`{"Debug":true}`)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected non-seekable configuration input to remain accepted, got %v: %s", err, output)
	}
}

func TestForceReadReturnsMalformedJSONError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "malformed.conf.json")
	if err := os.WriteFile(configPath, []byte(`{"Debug":`), 0600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	_, err := ForceRead(configPath)
	if err == nil {
		t.Fatal("ForceRead() returned nil for malformed JSON")
	}
	if !strings.Contains(err.Error(), "malformed.conf.json") {
		t.Fatalf("ForceRead() error = %q; want config path", err)
	}
}

func TestForceReadDoesNotApplyInvalidConfigurationPartially(t *testing.T) {
	previous := *Config
	Config.Debug = false
	Config.BackendDB = "mysql"
	Config.ClusterNameToAlias = map[string]string{"existing": "cluster"}
	t.Cleanup(func() {
		*Config = previous
	})

	configPath := writeConfigFixture(t, `{"Debug":true,"BackendDB":"sqlite3","SQLite3DataFile":"","ClusterNameToAlias":{"new":"cluster"}}`)
	_, err := ForceRead(configPath)
	if err == nil {
		t.Fatal("ForceRead() returned nil for invalid sqlite configuration")
	}
	if Config.Debug {
		t.Fatal("ForceRead() partially applied Debug from an invalid configuration")
	}
	if Config.BackendDB != "mysql" {
		t.Fatalf("BackendDB = %q; want previous value %q", Config.BackendDB, "mysql")
	}
	if _, found := Config.ClusterNameToAlias["new"]; found {
		t.Fatal("ForceRead() partially applied a map entry from an invalid configuration")
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
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftAdvertise, c.RaftBind)
		test.S(t).ExpectEquals(c.RaftNodeID, "node-1")
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		c.RaftBind = ""
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		c.RaftBind = "127.0.0.1"
		c.DefaultRaftPort = 10008
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftBind, "127.0.0.1:10008")
		test.S(t).ExpectEquals(c.RaftAdvertise, "127.0.0.1:10008")
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		c.RaftBind = "127.0.0.1:10008"
		c.RaftAdvertise = "10.0.0.1"
		c.DefaultRaftPort = 10008
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftAdvertise, "10.0.0.1:10008")
		test.S(t).ExpectEquals(c.RaftNodeID, "node-1")
	}
	{
		c := newConfiguration()
		c.RaftEnabled = true
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node 1"
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
