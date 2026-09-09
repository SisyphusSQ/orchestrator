package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/golib/log"
	test "github.com/openark/orchestrator/internal/golib/tests"
)

const (
	forceReadStdinChildEnv = "ORCHESTRATOR_FORCE_READ_STDIN_CHILD"
)

func init() {
	Config.Topology.Hostname.ResolveMethod = "none"
	log.SetLevel(log.ERROR)
}

func writeConfigFixture(t *testing.T, content string) string {
	t.Helper()
	return writeNamedConfigFixture(t, "orchestrator.conf.json", content)
}

func writeNamedConfigFixture(t *testing.T, name, content string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return configPath
}

func TestDecodeConfigurationSupportsLayeredJSONAndYAML(t *testing.T) {
	testCases := map[string]string{
		"json": `{"logging":{"debug":true},"metadata":{"type":"sqlite3","sqlite":{"dataFile":"/tmp/orchestrator.db"}},"topology":{"discovery":{"seeds":["db:3306"]},"classification":{"clusterNameToAlias":{"production":"main"}}}}`,
		"yaml": "logging:\n  debug: true\nmetadata:\n  type: sqlite3\n  sqlite:\n    dataFile: /tmp/orchestrator.db\ntopology:\n  discovery:\n    seeds:\n      - db:3306\n  classification:\n    clusterNameToAlias:\n      production: main\n",
	}
	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			configuration := newConfiguration()
			if err := decodeConfiguration(strings.NewReader(content), configuration); err != nil {
				t.Fatal(err)
			}
			if !configuration.Logging.Debug || configuration.Metadata.Type != "sqlite3" || configuration.Metadata.SQLite.DataFile != "/tmp/orchestrator.db" {
				t.Fatalf("decoded scalar values = %#v", configuration)
			}
			if len(configuration.Topology.Discovery.Seeds) != 1 || configuration.Topology.Discovery.Seeds[0] != "db:3306" {
				t.Fatalf("decoded list = %v", configuration.Topology.Discovery.Seeds)
			}
			if configuration.Topology.Classification.ClusterNameToAlias["production"] != "main" {
				t.Fatalf("decoded map = %v", configuration.Topology.Classification.ClusterNameToAlias)
			}
		})
	}
}

func TestDecodeConfigurationRejectsFlatAndWrongCaseFields(t *testing.T) {
	for name, content := range map[string]string{
		"flat":             "RaftNodeID: node-1\n",
		"wrong root case":  "Raft:\n  nodeID: node-1\n",
		"wrong field case": "raft:\n  NodeID: node-1\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := decodeConfiguration(strings.NewReader(content), newConfiguration()); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("decode invalid configuration error = %v", err)
			}
		})
	}
}

func TestDecodeConfigurationRejectsUnknownFields(t *testing.T) {
	for _, field := range []string{
		"FutureSetting",
		"ReasonableReplicationLagSeconds",
		"ProblemIgnoreHostnameFilters",
		"VerifyReplicationFilters",
		"ReasonableMaintenanceReplicationLagSeconds",
		"PromotionIgnoreHostnameFilters",
		"FailureDetectionPeriodBlockMinutes",
		"RecoveryPeriodBlockSeconds",
		"RecoveryIgnoreHostnameFilters",
		"RecoverMasterClusterFilters",
		"RecoverIntermediateMasterClusterFilters",
		"OnFailureDetectionProcesses",
		"PreGracefulTakeoverProcesses",
		"PreFailoverProcesses",
		"PostFailoverProcesses",
		"PostUnsuccessfulFailoverProcesses",
		"PostMasterFailoverProcesses",
		"PostIntermediateMasterFailoverProcesses",
		"PostGracefulTakeoverProcesses",
		"PostTakeMasterProcesses",
		"RecoverNonWriteableMaster",
		"CoMasterRecoveryMustPromoteOtherCoMaster",
		"DetachLostReplicasAfterMasterFailover",
		"ApplyMySQLPromotionAfterMasterFailover",
		"PreventCrossDataCenterMasterFailover",
		"PreventCrossRegionMasterFailover",
		"MasterFailoverDetachReplicaMasterHost",
		"FailMasterPromotionOnLagMinutes",
		"FailMasterPromotionIfSQLThreadNotUpToDate",
		"DelayMasterPromotionIfSQLThreadNotUpToDate",
		"PostponeReplicaRecoveryOnLagMinutes",
		"EnforceExactSemiSyncReplicas",
		"RecoverLockedSemiSyncMaster",
		"ReasonableLockedSemiSyncMasterSeconds",
		"RaftEnabled",
		"ZkAddress",
		"SlaveLagQuery",
		"RecoveryPeriodBlockMinutes",
		"DetachLostSlavesAfterMasterFailover",
		"MasterFailoverDetachSlaveMasterHost",
		"PostponeSlaveRecoveryOnLagMinutes",
		"OAuthClientId",
		"OAuthClientSecret",
		"OAuthScopes",
		"ExpectFailureAnalysisConcensus",
		"SeedAcceptableBytesDiff",
		"MasterFailoverLostInstancesDowntimeMinutes",
	} {
		t.Run(field, func(t *testing.T) {
			for format, content := range map[string]string{
				"json": `{"` + field + `": null}`,
				"yaml": field + ": null\n",
			} {
				t.Run(format, func(t *testing.T) {
					err := decodeConfiguration(strings.NewReader(content), newConfiguration())
					if err == nil || !strings.Contains(err.Error(), "unknown field") {
						t.Fatalf("decode unknown field error = %v", err)
					}
				})
			}
		})
	}
}

func TestDecodeConfigurationRejectsDuplicateKeys(t *testing.T) {
	for name, content := range map[string]string{
		"json": `{"logging":{"debug":true,"debug":false}}`,
		"yaml": "logging:\n  debug: true\n  debug: false\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := decodeConfiguration(strings.NewReader(content), newConfiguration()); err == nil {
				t.Fatal("duplicate configuration key accepted")
			}
		})
	}
}

func TestDecodeConfigurationRejectsMultipleYAMLDocuments(t *testing.T) {
	err := decodeConfiguration(strings.NewReader("logging:\n  debug: true\n---\nlogging:\n  debug: false\n"), newConfiguration())
	if err == nil || !strings.Contains(err.Error(), "multiple configuration documents") {
		t.Fatalf("multiple documents error = %v", err)
	}
}

func TestDecodeConfigurationRejectsTrailingContent(t *testing.T) {
	for name, content := range map[string]string{
		"json": `{"logging":{"debug":true}} {"logging":{"debug":false}}`,
		"yaml": "logging:\n  debug: true\ninvalid trailing scalar\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := decodeConfiguration(strings.NewReader(content), newConfiguration()); err == nil {
				t.Fatal("trailing configuration content accepted")
			}
		})
	}
}

func TestReadIntoUsesContentInsteadOfFileExtension(t *testing.T) {
	for name, fixture := range map[string]struct {
		fileName string
		content  string
	}{
		"yaml in json file": {fileName: "orchestrator.conf.json", content: "logging:\n  debug: true\n"},
		"json in yaml file": {fileName: "orchestrator.conf.yaml", content: `{"logging":{"debug":true}}`},
	} {
		t.Run(name, func(t *testing.T) {
			configuration := newConfiguration()
			if err := readInto(writeNamedConfigFixture(t, fixture.fileName, fixture.content), configuration); err != nil {
				t.Fatal(err)
			}
			if !configuration.Logging.Debug {
				t.Fatal("configuration content was not applied")
			}
		})
	}
}

func TestReadIntoLayersJSONAndYAML(t *testing.T) {
	configuration := newConfiguration()
	if err := readInto(writeNamedConfigFixture(t, "base.json", `{"logging":{"debug":true},"server":{"listen":{"address":":3000"}}}`), configuration); err != nil {
		t.Fatal(err)
	}
	if err := readInto(writeNamedConfigFixture(t, "override.yaml", "logging:\n  debug: false\n"), configuration); err != nil {
		t.Fatal(err)
	}
	if configuration.Logging.Debug || configuration.Server.Listen.Address != ":3000" {
		t.Fatalf("layered configuration = logging.debug:%t server.listen.address:%q", configuration.Logging.Debug, configuration.Server.Listen.Address)
	}
}

func TestRepositoryConfigurationFilesDecode(t *testing.T) {
	patterns := []string{
		"../../conf/*.conf.json",
		"../../conf/*.conf.yaml",
		"../../tests/integration/orchestrator.conf.json",
		"../../tests/system/orchestrator-ci-system.conf.json",
		"../../tests/*/*/config.json",
		"../../tests/*/*/*/config.json",
	}
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no repository configuration files found")
	}
	for _, fileName := range files {
		t.Run(filepath.Base(fileName), func(t *testing.T) {
			file, err := os.Open(fileName)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := decodeConfiguration(file, newConfiguration()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunningRaftRejectsIdentityReloadWithoutPartialChanges(t *testing.T) {
	previous, locked := *Config, raftConfigurationLocked
	t.Cleanup(func() { *Config = previous; raftConfigurationLocked = locked })
	Config.Raft.NodeID = "existing-node"
	Config.Raft.DataDir = t.TempDir()
	if err := Config.ValidateRaft(); err != nil {
		t.Fatal(err)
	}
	LockRaftConfiguration()
	if _, err := ForceRead(writeConfigFixture(t, `{"raft":{"nodeID":"replacement-node"},"logging":{"debug":false}}`)); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("identity reload: %v", err)
	}
	if Config.Raft.NodeID != "existing-node" || Config.Logging.Debug != previous.Logging.Debug {
		t.Fatal("invalid reload partially changed running configuration")
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
	cmd.Stdin = strings.NewReader("logging:\n  debug: true\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected non-seekable configuration input to remain accepted, got %v: %s", err, output)
	}
}

func TestForceReadReturnsMalformedConfigurationError(t *testing.T) {
	configPath := writeNamedConfigFixture(t, "malformed.conf.yaml", "logging:\n  debug: [\n")
	_, err := ForceRead(configPath)
	if err == nil {
		t.Fatal("ForceRead() returned nil for malformed configuration")
	}
	if !strings.Contains(err.Error(), "malformed.conf.yaml") {
		t.Fatalf("ForceRead() error = %q; want config path", err)
	}
}

func TestForceReadDoesNotApplyInvalidConfigurationPartially(t *testing.T) {
	previous := *Config
	Config.Logging.Debug = false
	Config.Metadata.Type = "mysql"
	Config.Topology.Classification.ClusterNameToAlias = map[string]string{"existing": "cluster"}
	t.Cleanup(func() {
		*Config = previous
	})

	configPath := writeConfigFixture(t, `{"logging":{"debug":true},"metadata":{"type":"sqlite3","sqlite":{"dataFile":""}},"topology":{"classification":{"clusterNameToAlias":{"new":"cluster"}}}}`)
	_, err := ForceRead(configPath)
	if err == nil {
		t.Fatal("ForceRead() returned nil for invalid sqlite configuration")
	}
	if Config.Logging.Debug {
		t.Fatal("ForceRead() partially applied Debug from an invalid configuration")
	}
	if Config.Metadata.Type != "mysql" {
		t.Fatalf("metadata.type = %q; want previous value %q", Config.Metadata.Type, "mysql")
	}
	if _, found := Config.Topology.Classification.ClusterNameToAlias["new"]; found {
		t.Fatal("ForceRead() partially applied a map entry from an invalid configuration")
	}
}

func TestPostReadRejectsUnsupportedAuthenticationMethod(t *testing.T) {
	configuration := newConfiguration()
	configuration.Authentication.Method = "oauth"
	if err := configuration.postReadAdjustments(); err == nil || !strings.Contains(err.Error(), "unsupported authentication.method") {
		t.Fatalf("unsupported authentication method error = %v", err)
	}
}

func TestRaft(t *testing.T) {
	{
		c := newConfiguration()
		c.Raft.Bind = "1.2.3.4:1008"
		c.Raft.NodeID = "node-1"
		c.Raft.DataDir = "/path/to/somewhere"
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.Raft.Advertise, c.Raft.Bind)
	}
	{
		c := newConfiguration()
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.Raft.DataDir = "/path/to/somewhere"
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.Raft.DataDir = "/path/to/somewhere"
		c.Raft.NodeID = "node-1"
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.Raft.Advertise, c.Raft.Bind)
		test.S(t).ExpectEquals(c.Raft.NodeID, "node-1")
	}
	{
		c := newConfiguration()
		c.Raft.DataDir = "/path/to/somewhere"
		c.Raft.NodeID = "node-1"
		c.Raft.Bind = ""
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.Raft.DataDir = "/path/to/somewhere"
		c.Raft.NodeID = "node-1"
		c.Raft.Bind = "127.0.0.1"
		c.Raft.DefaultPort = 10008
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.Raft.Bind, "127.0.0.1:10008")
		test.S(t).ExpectEquals(c.Raft.Advertise, "127.0.0.1:10008")
	}
	{
		c := newConfiguration()
		c.Raft.DataDir = "/path/to/somewhere"
		c.Raft.NodeID = "node-1"
		c.Raft.Bind = "127.0.0.1:10008"
		c.Raft.Advertise = "10.0.0.1"
		c.Raft.DefaultPort = 10008
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.Raft.Advertise, "10.0.0.1:10008")
		test.S(t).ExpectEquals(c.Raft.NodeID, "node-1")
	}
	{
		c := newConfiguration()
		c.Raft.DataDir = "/path/to/somewhere"
		c.Raft.NodeID = "node 1"
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
}

func TestHttpAdvertise(t *testing.T) {
	{
		c := newConfiguration()
		c.Server.HTTPAdvertise = ""
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.Server.HTTPAdvertise = "http://127.0.0.1:1234"
		err := c.postReadAdjustments()
		test.S(t).ExpectNil(err)
	}
	{
		c := newConfiguration()
		c.Server.HTTPAdvertise = "http://127.0.0.1"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.Server.HTTPAdvertise = "127.0.0.1:1234"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.Server.HTTPAdvertise = "http://127.0.0.1:1234/mypath"
		err := c.postReadAdjustments()
		test.S(t).ExpectNotNil(err)
	}
}
