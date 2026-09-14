package discovery

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/kv"
)

func TestContinuousDiscoveryReturnsKVInitError(t *testing.T) {
	previous := *config.Current()
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { *cfg = previous })
		kv.ResetKVStoresForTest()
	})
	kv.ResetKVStoresForTest()
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Consul.Address = "https://127.0.0.1:8501" })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Consul.Scheme = "https" })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Consul.TLS.CAFile = filepath.Join(t.TempDir(), "missing-ca.pem") })

	err := ContinuousDiscovery(t.Context())
	if err == nil {
		t.Fatal("ContinuousDiscovery(t.Context()) returned nil for a Consul TLS initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize KV stores") {
		t.Fatalf("ContinuousDiscovery(t.Context()) error = %q; want initialize KV stores", err)
	}
}
