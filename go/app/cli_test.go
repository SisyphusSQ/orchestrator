package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/golib/log"
	test "github.com/openark/golib/tests"
	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/kv"
)

func init() {
	config.Config.HostnameResolveMethod = "none"
	config.MarkConfigurationLoaded()
	log.SetLevel(log.ERROR)
}

func TestHelp(t *testing.T) {
	if err := Cli("help", false, "localhost:9999", "localhost:9999", "orc", "no-reason", "1m", ".", "no-alias", "no-pool", ""); err != nil {
		t.Fatalf("Cli(help) error: %v", err)
	}
	test.S(t).ExpectTrue(len(knownCommands) > 0)
}

func TestKnownCommands(t *testing.T) {
	if err := Cli("help", false, "localhost:9999", "localhost:9999", "orc", "no-reason", "1m", ".", "no-alias", "no-pool", ""); err != nil {
		t.Fatalf("Cli(help) error: %v", err)
	}

	commandsMap := make(map[string]string)
	for _, command := range knownCommands {
		commandsMap[command.Command] = command.Section
	}
	test.S(t).ExpectEquals(commandsMap["no-such-command"], "")
	test.S(t).ExpectEquals(commandsMap["relocate"], "Smart relocation")
	test.S(t).ExpectEquals(commandsMap["relocate-slaves"], "")
	test.S(t).ExpectEquals(commandsMap["relocate-replicas"], "Smart relocation")

	for _, synonym := range commandSynonyms {
		test.S(t).ExpectNotEquals(commandsMap[synonym], "")
	}
}

func TestCliWrapperReturnsRaftConfigurationError(t *testing.T) {
	previousRaftEnabled := config.Config.RaftEnabled
	previousIgnoreRaftSetup := config.RuntimeCLIFlags.IgnoreRaftSetup
	ignoreRaftSetup := false
	config.Config.RaftEnabled = true
	config.RuntimeCLIFlags.IgnoreRaftSetup = &ignoreRaftSetup
	t.Cleanup(func() {
		config.Config.RaftEnabled = previousRaftEnabled
		config.RuntimeCLIFlags.IgnoreRaftSetup = previousIgnoreRaftSetup
	})

	err := CliWrapper("help", false, "", "", "orc", "", "", "", "", "", "")
	if err == nil {
		t.Fatal("CliWrapper() returned nil for a Raft CLI invocation")
	}
	if !strings.Contains(err.Error(), "RaftEnabled") {
		t.Fatalf("CliWrapper() error = %q; want RaftEnabled context", err)
	}
}

func TestCliReturnsKVInitError(t *testing.T) {
	previous := *config.Config
	t.Cleanup(func() {
		*config.Config = previous
		kv.ResetKVStoresForTest()
	})
	kv.ResetKVStoresForTest()
	config.Config.ConsulAddress = "https://127.0.0.1:8501"
	config.Config.ConsulScheme = "https"
	config.Config.ConsulTLSCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")

	err := Cli("help", false, "localhost:9999", "localhost:9999", "orc", "no-reason", "1m", ".", "no-alias", "no-pool", "")
	if err == nil {
		t.Fatal("Cli() returned nil for a Consul TLS initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize KV stores") {
		t.Fatalf("Cli() error = %q; want initialize KV stores", err)
	}
}

func TestValidateInstanceIsFoundReturnsNilKeyError(t *testing.T) {
	_, err := validateInstanceIsFound(nil)
	if err == nil {
		t.Fatal("validateInstanceIsFound(nil) returned nil")
	}
	if !strings.Contains(err.Error(), "instance") {
		t.Fatalf("validateInstanceIsFound(nil) error = %q; want instance context", err)
	}
}
