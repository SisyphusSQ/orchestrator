package logic

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/kv"
)

func TestContinuousDiscoveryReturnsKVInitError(t *testing.T) {
	previous := *config.Config
	t.Cleanup(func() {
		*config.Config = previous
		kv.ResetKVStoresForTest()
	})
	kv.ResetKVStoresForTest()
	config.Config.ConsulAddress = "https://127.0.0.1:8501"
	config.Config.ConsulScheme = "https"
	config.Config.ConsulTLSCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	config.Config.RaftEnabled = false

	err := ContinuousDiscovery()
	if err == nil {
		t.Fatal("ContinuousDiscovery() returned nil for a Consul TLS initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize KV stores") {
		t.Fatalf("ContinuousDiscovery() error = %q; want initialize KV stores", err)
	}
}
