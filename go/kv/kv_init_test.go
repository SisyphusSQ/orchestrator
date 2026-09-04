package kv

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/go/config"
)

func TestInitKVStoresOmitsExternalStoreWhenAddressEmpty(t *testing.T) {
	ResetKVStoresForTest()
	t.Cleanup(ResetKVStoresForTest)
	configureConsulTest(t, "", false)

	if err := InitKVStores(); err != nil {
		t.Fatalf("InitKVStores(): %v", err)
	}
	stores := getKVStores()
	if len(stores) != 1 {
		t.Fatalf("expected only the internal store, got %d", len(stores))
	}
}

func TestInitKVStoresAddsConsulStore(t *testing.T) {
	ResetKVStoresForTest()
	t.Cleanup(ResetKVStoresForTest)
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   "PUT",
			URL:      "/v1/kv/k",
			Request:  "v",
			Response: true,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)

	if err := InitKVStores(); err != nil {
		t.Fatalf("InitKVStores(): %v", err)
	}
	stores := getKVStores()
	if len(stores) != 2 {
		t.Fatalf("expected internal + consul stores, got %d", len(stores))
	}
	if _, ok := stores[1].(*consulStore); !ok {
		t.Fatalf("expected consulStore, got %T", stores[1])
	}
}

func TestInitKVStoresAddsTxnStoreForAlias(t *testing.T) {
	ResetKVStoresForTest()
	t.Cleanup(ResetKVStoresForTest)
	server := buildConsulTestServer(t, nil)
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulKVStoreProvider = "consul_txn"

	if err := InitKVStores(); err != nil {
		t.Fatalf("InitKVStores(): %v", err)
	}
	stores := getKVStores()
	if len(stores) != 2 {
		t.Fatalf("expected internal + consul-txn stores, got %d", len(stores))
	}
	if _, ok := stores[1].(*consulTxnStore); !ok {
		t.Fatalf("expected consulTxnStore, got %T", stores[1])
	}
}

func TestInitKVStoresReturnsTLSFileError(t *testing.T) {
	ResetKVStoresForTest()
	t.Cleanup(ResetKVStoresForTest)
	configureConsulTest(t, "https://127.0.0.1:8501", false)
	config.Config.ConsulTLSCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")

	err := InitKVStores()
	if err == nil {
		t.Fatal("expected TLS file error")
	}
	if !strings.Contains(err.Error(), "consul") && !strings.Contains(err.Error(), "CA") && !strings.Contains(strings.ToLower(err.Error()), "no such file") {
		t.Fatalf("error %q should mention consul TLS construction failure", err)
	}
}

func TestInitKVStoresOnceKeepsFirstResult(t *testing.T) {
	ResetKVStoresForTest()
	t.Cleanup(ResetKVStoresForTest)
	configureConsulTest(t, "", false)
	if err := InitKVStores(); err != nil {
		t.Fatalf("first init: %v", err)
	}
	config.Config.ConsulAddress = "https://127.0.0.1:8501"
	config.Config.ConsulScheme = "https"
	config.Config.ConsulTLSCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	if err := InitKVStores(); err != nil {
		t.Fatalf("second init should reuse the first success, got %v", err)
	}
	if len(getKVStores()) != 1 {
		t.Fatal("reload must not rebuild KV stores")
	}
}
