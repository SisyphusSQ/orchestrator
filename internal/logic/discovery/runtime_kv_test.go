package discovery

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/kv"
)

func TestContinuousDiscoveryReturnsKVInitError(t *testing.T) {
	previous := *config.Config
	t.Cleanup(func() {
		*config.Config = previous
		kv.ResetKVStoresForTest()
	})
	kv.ResetKVStoresForTest()
	config.Config.Consul.Address = "https://127.0.0.1:8501"
	config.Config.Consul.Scheme = "https"
	config.Config.Consul.TLS.CAFile = filepath.Join(t.TempDir(), "missing-ca.pem")

	err := ContinuousDiscovery(t.Context())
	if err == nil {
		t.Fatal("ContinuousDiscovery(t.Context()) returned nil for a Consul TLS initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize KV stores") {
		t.Fatalf("ContinuousDiscovery(t.Context()) error = %q; want initialize KV stores", err)
	}
}
