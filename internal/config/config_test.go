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
	Config.HostnameResolveMethod = "none"
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

func TestDecodeConfigurationSupportsJSONAndYAML(t *testing.T) {
	testCases := map[string]string{
		"json": `{"Debug":true,"BackendDB":"sqlite3","SQLite3DataFile":"/tmp/orchestrator.db","RecoverMasterClusterFilters":[".*"],"ClusterNameToAlias":{"production":"main"}}`,
		"yaml": "Debug: true\nBackendDB: sqlite3\nSQLite3DataFile: /tmp/orchestrator.db\nRecoverMasterClusterFilters:\n  - .*\nClusterNameToAlias:\n  production: main\n",
	}
	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			configuration := newConfiguration()
			if err := decodeConfiguration(strings.NewReader(content), configuration); err != nil {
				t.Fatal(err)
			}
			if !configuration.Debug || configuration.BackendDB != "sqlite3" || configuration.SQLite3DataFile != "/tmp/orchestrator.db" {
				t.Fatalf("decoded scalar values = %#v", configuration)
			}
			if len(configuration.RecoverMasterClusterFilters) != 1 || configuration.RecoverMasterClusterFilters[0] != ".*" {
				t.Fatalf("decoded list = %v", configuration.RecoverMasterClusterFilters)
			}
			if configuration.ClusterNameToAlias["production"] != "main" {
				t.Fatalf("decoded map = %v", configuration.ClusterNameToAlias)
			}
		})
	}
}

func TestDecodeConfigurationRejectsUnknownFields(t *testing.T) {
	for _, field := range []string{
		"FutureSetting",
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
		"json": `{"Debug": true, "Debug": false}`,
		"yaml": "Debug: true\nDebug: false\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := decodeConfiguration(strings.NewReader(content), newConfiguration()); err == nil {
				t.Fatal("duplicate configuration key accepted")
			}
		})
	}
}

func TestDecodeConfigurationRejectsMultipleYAMLDocuments(t *testing.T) {
	err := decodeConfiguration(strings.NewReader("Debug: true\n---\nDebug: false\n"), newConfiguration())
	if err == nil || !strings.Contains(err.Error(), "multiple configuration documents") {
		t.Fatalf("multiple documents error = %v", err)
	}
}

func TestDecodeConfigurationRejectsTrailingContent(t *testing.T) {
	for name, content := range map[string]string{
		"json": `{"Debug": true} {"Debug": false}`,
		"yaml": "Debug: true\ninvalid trailing scalar\n",
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
		"yaml in json file": {fileName: "orchestrator.conf.json", content: "Debug: true\n"},
		"json in yaml file": {fileName: "orchestrator.conf.yaml", content: `{"Debug": true}`},
	} {
		t.Run(name, func(t *testing.T) {
			configuration := newConfiguration()
			if err := readInto(writeNamedConfigFixture(t, fixture.fileName, fixture.content), configuration); err != nil {
				t.Fatal(err)
			}
			if !configuration.Debug {
				t.Fatal("configuration content was not applied")
			}
		})
	}
}

func TestReadIntoLayersJSONAndYAML(t *testing.T) {
	configuration := newConfiguration()
	if err := readInto(writeNamedConfigFixture(t, "base.json", `{"Debug": true, "ListenAddress": ":3000"}`), configuration); err != nil {
		t.Fatal(err)
	}
	if err := readInto(writeNamedConfigFixture(t, "override.yaml", "Debug: false\n"), configuration); err != nil {
		t.Fatal(err)
	}
	if configuration.Debug || configuration.ListenAddress != ":3000" {
		t.Fatalf("layered configuration = Debug:%t ListenAddress:%q", configuration.Debug, configuration.ListenAddress)
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
	Config.RaftNodeID = "existing-node"
	Config.RaftDataDir = t.TempDir()
	if err := Config.ValidateRaft(); err != nil {
		t.Fatal(err)
	}
	LockRaftConfiguration()
	if _, err := ForceRead(writeConfigFixture(t, `{"RaftNodeID":"replacement-node","Debug":false}`)); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("identity reload: %v", err)
	}
	if Config.RaftNodeID != "existing-node" || Config.Debug != previous.Debug {
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
	cmd.Stdin = strings.NewReader("Debug: true\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected non-seekable configuration input to remain accepted, got %v: %s", err, output)
	}
}

func TestForceReadReturnsMalformedConfigurationError(t *testing.T) {
	configPath := writeNamedConfigFixture(t, "malformed.conf.yaml", "Debug: [\n")
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

func TestDetachLostReplicasAfterMasterFailoverDefault(t *testing.T) {
	if !newConfiguration().DetachLostReplicasAfterMasterFailover {
		t.Fatal("DetachLostReplicasAfterMasterFailover default changed")
	}
}

func TestPostReadRejectsUnsupportedAuthenticationMethod(t *testing.T) {
	configuration := newConfiguration()
	configuration.AuthenticationMethod = "oauth"
	if err := configuration.postReadAdjustments(); err == nil || !strings.Contains(err.Error(), "unsupported AuthenticationMethod") {
		t.Fatalf("unsupported authentication method error = %v", err)
	}
}

func TestRaft(t *testing.T) {
	{
		c := newConfiguration()
		c.RaftBind = "1.2.3.4:1008"
		c.RaftNodeID = "node-1"
		c.RaftDataDir = "/path/to/somewhere"
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftAdvertise, c.RaftBind)
	}
	{
		c := newConfiguration()
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.RaftDataDir = "/path/to/somewhere"
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftAdvertise, c.RaftBind)
		test.S(t).ExpectEquals(c.RaftNodeID, "node-1")
	}
	{
		c := newConfiguration()
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		c.RaftBind = ""
		err := c.ValidateRaft()
		test.S(t).ExpectNotNil(err)
	}
	{
		c := newConfiguration()
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		c.RaftBind = "127.0.0.1"
		c.DefaultRaftPort = 10008
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftBind, "127.0.0.1:10008")
		test.S(t).ExpectEquals(c.RaftAdvertise, "127.0.0.1:10008")
	}
	{
		c := newConfiguration()
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node-1"
		c.RaftBind = "127.0.0.1:10008"
		c.RaftAdvertise = "10.0.0.1"
		c.DefaultRaftPort = 10008
		err := c.ValidateRaft()
		test.S(t).ExpectNil(err)
		test.S(t).ExpectEquals(c.RaftAdvertise, "10.0.0.1:10008")
		test.S(t).ExpectEquals(c.RaftNodeID, "node-1")
	}
	{
		c := newConfiguration()
		c.RaftDataDir = "/path/to/somewhere"
		c.RaftNodeID = "node 1"
		err := c.ValidateRaft()
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
