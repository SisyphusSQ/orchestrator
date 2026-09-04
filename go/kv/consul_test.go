package kv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openark/orchestrator/go/config"

	consulapi "github.com/hashicorp/consul/api"
)

const consulTestDefaultDatacenter = "dc1"

type consulTestServerOp struct {
	Method       string
	URL          string
	Request      interface{}
	Response     interface{}
	ResponseCode int
	Matches      *atomic.Int64
}

type capturedConsulRequest struct {
	Method string
	URL    string
	Token  string
	Body   string
}

// sortTxnKVOps sort TxnOps by op.KV.Key to resolve random test failures
func sortTxnKVOps(txnOps []*consulapi.TxnOp) []*consulapi.TxnOp {
	sort.Slice(txnOps, func(a, b int) bool {
		return txnOps[a].KV.Key < txnOps[b].KV.Key
	})
	return txnOps
}

func buildConsulTestServer(t *testing.T, testOps []consulTestServerOp) *httptest.Server {
	t.Helper()
	return buildConsulTestServerWithObserver(t, testOps, nil)
}

func buildConsulTestServerWithObserver(t *testing.T, testOps []consulTestServerOp, observe func(capturedConsulRequest)) *httptest.Server {
	t.Helper()
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBytes, _ := io.ReadAll(r.Body)
		requestBody := strings.TrimSpace(string(requestBytes))
		if observe != nil {
			observe(capturedConsulRequest{
				Method: r.Method,
				URL:    r.URL.String(),
				Token:  r.Header.Get("X-Consul-Token"),
				Body:   requestBody,
			})
		}

		for _, testOp := range testOps {
			if r.Method != testOp.Method || r.URL.String() != testOp.URL {
				continue
			}
			if testOp.ResponseCode == 0 {
				testOp.ResponseCode = http.StatusOK
			}
			if testOp.Matches != nil {
				testOp.Matches.Add(1)
			}
			if r.URL.Path == "/v1/catalog/datacenters" {
				w.WriteHeader(testOp.ResponseCode)
				if testOp.Response != nil {
					json.NewEncoder(w).Encode(testOp.Response)
				}
				return
			} else if strings.HasPrefix(r.URL.Path, "/v1/kv") {
				if expectedRequest, ok := testOp.Request.(string); ok && requestBody != expectedRequest {
					continue
				}
				w.WriteHeader(testOp.ResponseCode)
				if testOp.Response != nil {
					json.NewEncoder(w).Encode(testOp.Response)
				}
				return
			} else if strings.HasPrefix(r.URL.Path, "/v1/txn") {
				var txnOps consulapi.TxnOps
				if err := json.Unmarshal(requestBytes, &txnOps); err != nil {
					t.Fatalf("Unable to unmarshal json request body: %v", err)
					continue
				}
				// https://github.com/openark/orchestrator/issues/1302
				// https://github.com/hashicorp/consul/blob/87f6617eecd23a64add1e79eb3cd8dc3da9e649e/agent/txn_endpoint.go#L121-L129
				if len(txnOps) > 64 {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					fmt.Fprintf(w, "Transaction contains too many operations (%d > 64)", len(txnOps))
					return
				}
				testOpRequest := sortTxnKVOps(testOp.Request.(consulapi.TxnOps))
				if testOp.Response != nil && reflect.DeepEqual(testOpRequest, sortTxnKVOps(txnOps)) {
					w.WriteHeader(testOp.ResponseCode)
					json.NewEncoder(w).Encode(testOp.Response)
					return
				}
			}
		}

		t.Fatalf("No requests matched setup. Got method %s, Path %s, body %s", r.Method, r.URL.String(), requestBody)
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintln(w, "")
	})
	return httptest.NewServer(handlerFunc)
}

func configureConsulTest(t *testing.T, address string, crossDataCenterDistribution bool) {
	t.Helper()

	original := *config.Config
	t.Cleanup(func() {
		*config.Config = original
	})

	config.Config.ConsulAddress = address
	if strings.HasPrefix(address, "https://") {
		config.Config.ConsulScheme = "https"
	} else {
		config.Config.ConsulScheme = "http"
	}
	config.Config.ConsulAclToken = ""
	config.Config.ConsulDatacenter = ""
	config.Config.ConsulTLSCAFile = ""
	config.Config.ConsulTLSCAPath = ""
	config.Config.ConsulTLSCertFile = ""
	config.Config.ConsulTLSPrivateKeyFile = ""
	config.Config.ConsulTLSServerName = ""
	config.Config.ConsulTLSSkipVerify = false
	config.Config.ConsulHttpTimeoutSeconds = 60
	config.Config.ConsulCrossDataCenterDistribution = crossDataCenterDistribution
	config.Config.ConsulKVStoreProvider = "consul"
}

func mustConsulClient(t *testing.T) *consulapi.Client {
	t.Helper()
	client, err := newConsulClientFromConfig(config.Config)
	if err != nil {
		t.Fatalf("create consul client: %v", err)
	}
	if client == nil {
		t.Fatal("expected consul client")
	}
	return client
}

func newTestConsulStore(t *testing.T) KVStore {
	t.Helper()
	return NewConsulStore(mustConsulClient(t))
}

func newTestConsulTxnStore(t *testing.T) KVStore {
	t.Helper()
	return NewConsulTxnStore(mustConsulClient(t))
}

func TestConsulStorePutAndGetKVPairs(t *testing.T) {
	var masterWrites atomic.Int64
	var hostnameWrites atomic.Int64
	var masterReads atomic.Int64
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster",
			Request:  "mysql.example.com:3306",
			Response: true,
			Matches:  &masterWrites,
		},
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster/hostname",
			Request:  "mysql.example.com",
			Response: true,
			Matches:  &hostnameWrites,
		},
		{
			Method: http.MethodGet,
			URL:    "/v1/kv/mysql/master/cluster",
			Response: consulapi.KVPairs{
				{Key: "mysql/master/cluster", Value: []byte("mysql.example.com:3306")},
			},
			Matches: &masterReads,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)

	store := newTestConsulStore(t)
	if err := store.PutKVPairs([]*KVPair{
		{Key: "mysql/master/cluster", Value: "mysql.example.com:3306"},
		{Key: "mysql/master/cluster/hostname", Value: "mysql.example.com"},
	}); err != nil {
		t.Fatalf("put Consul KV pairs: %v", err)
	}

	value, found, err := store.GetKeyValue("mysql/master/cluster")
	if err != nil {
		t.Fatalf("get Consul KV: %v", err)
	}
	if !found || value != "mysql.example.com:3306" {
		t.Fatalf("expected stored master identity, got value %q, found %t", value, found)
	}
	if masterWrites.Load() != 1 || hostnameWrites.Load() != 1 || masterReads.Load() != 1 {
		t.Fatalf("expected one master write, hostname write, and master read; got %d, %d, %d", masterWrites.Load(), hostnameWrites.Load(), masterReads.Load())
	}
}

func TestConsulStoreMissingKeyReturnsNotFound(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:       http.MethodGet,
			URL:          "/v1/kv/mysql/master/missing",
			ResponseCode: http.StatusNotFound,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)

	store := newTestConsulStore(t)
	value, found, err := store.GetKeyValue("mysql/master/missing")
	if err != nil {
		t.Fatalf("get missing Consul KV: %v", err)
	}
	if found || value != "" {
		t.Fatalf("expected found=false and empty value, got value %q found %t", value, found)
	}
}

func TestConsulStoreNilClientMissingKey(t *testing.T) {
	store := NewConsulStore(nil)
	value, found, err := store.GetKeyValue("mysql/master/missing")
	if err != nil || found || value != "" {
		t.Fatalf("nil client GetKeyValue = (%q, %t, %v)", value, found, err)
	}
}

func TestConsulStoreDistributePairs(t *testing.T) {
	var datacenterReads atomic.Int64
	var valueReads atomic.Int64
	var valueWrites atomic.Int64
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   http.MethodGet,
			URL:      "/v1/catalog/datacenters",
			Response: []string{"dc2"},
			Matches:  &datacenterReads,
		},
		{
			Method:       http.MethodGet,
			URL:          "/v1/kv/mysql/master/cluster?dc=dc2",
			ResponseCode: http.StatusNotFound,
			Matches:      &valueReads,
		},
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster?dc=dc2",
			Request:  "mysql.example.com:3306",
			Response: true,
			Matches:  &valueWrites,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, true)

	store := newTestConsulStore(t)
	if err := store.DistributePairs([]*KVPair{{
		Key:   "mysql/master/cluster",
		Value: "mysql.example.com:3306",
	}}); err != nil {
		t.Fatalf("distribute Consul KV pair: %v", err)
	}
	if datacenterReads.Load() != 1 || valueReads.Load() != 1 || valueWrites.Load() != 1 {
		t.Fatalf("expected one datacenter read, value read, and value write; got %d, %d, %d", datacenterReads.Load(), valueReads.Load(), valueWrites.Load())
	}
}

func TestConsulStoreDistributePairsPartialFailure(t *testing.T) {
	var dc1Writes atomic.Int64
	var dc2Writes atomic.Int64
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   http.MethodGet,
			URL:      "/v1/catalog/datacenters",
			Response: []string{"dc1", "dc2"},
		},
		{
			Method:       http.MethodGet,
			URL:          "/v1/kv/mysql/master/cluster?dc=dc1",
			ResponseCode: http.StatusNotFound,
		},
		{
			Method:       http.MethodGet,
			URL:          "/v1/kv/mysql/master/cluster?dc=dc2",
			ResponseCode: http.StatusNotFound,
		},
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster?dc=dc1",
			Request:  "mysql.example.com:3306",
			Response: true,
			Matches:  &dc1Writes,
		},
		{
			Method:       http.MethodPut,
			URL:          "/v1/kv/mysql/master/cluster?dc=dc2",
			Request:      "mysql.example.com:3306",
			ResponseCode: http.StatusInternalServerError,
			Response:     "write failed",
			Matches:      &dc2Writes,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, true)

	store := newTestConsulStore(t)
	err := store.DistributePairs([]*KVPair{{
		Key:   "mysql/master/cluster",
		Value: "mysql.example.com:3306",
	}})
	if err == nil {
		t.Fatal("expected aggregated datacenter failure")
	}
	if !strings.Contains(err.Error(), "dc2") {
		t.Fatalf("error %q should mention failed datacenter dc2", err)
	}
	if dc1Writes.Load() != 1 || dc2Writes.Load() != 1 {
		t.Fatalf("expected both datacenters to be attempted, got dc1=%d dc2=%d", dc1Writes.Load(), dc2Writes.Load())
	}
}

func TestConsulStoreReadFailureStillWrites(t *testing.T) {
	var valueWrites atomic.Int64
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   http.MethodGet,
			URL:      "/v1/catalog/datacenters",
			Response: []string{"dc2"},
		},
		{
			Method:       http.MethodGet,
			URL:          "/v1/kv/mysql/master/cluster?dc=dc2",
			ResponseCode: http.StatusInternalServerError,
			Response:     "read failed",
		},
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster?dc=dc2",
			Request:  "mysql.example.com:3306",
			Response: true,
			Matches:  &valueWrites,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, true)

	store := newTestConsulStore(t)
	if err := store.DistributePairs([]*KVPair{{
		Key:   "mysql/master/cluster",
		Value: "mysql.example.com:3306",
	}}); err != nil {
		t.Fatalf("distribute after read failure: %v", err)
	}
	if valueWrites.Load() != 1 {
		t.Fatalf("expected intended write after read failure, got %d writes", valueWrites.Load())
	}
}

func TestConsulStoreDistributePairsNotReentrant(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var catalogReads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/catalog/datacenters" {
			catalogReads.Add(1)
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]string{"dc1"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	configureConsulTest(t, server.URL, true)

	store := newTestConsulStore(t)
	done := make(chan error, 1)
	go func() {
		done <- store.DistributePairs([]*KVPair{{Key: "mysql/master/cluster", Value: "v"}})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first DistributePairs")
	}
	if err := store.DistributePairs([]*KVPair{{Key: "mysql/master/cluster", Value: "v"}}); err != nil {
		t.Fatalf("reentrant DistributePairs: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first DistributePairs: %v", err)
	}
	if catalogReads.Load() != 1 {
		t.Fatalf("expected one in-flight distribution, got %d catalog reads", catalogReads.Load())
	}
}

func TestConsulStoreDistributePairsRace(t *testing.T) {
	var mu sync.Mutex
	writes := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/catalog/datacenters":
			json.NewEncoder(w).Encode([]string{"dc1", "dc2"})
		case strings.HasPrefix(r.URL.Path, "/v1/kv/"):
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			mu.Lock()
			writes[r.URL.String()]++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(true)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	configureConsulTest(t, server.URL, true)

	store := newTestConsulStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.DistributePairs([]*KVPair{
				{Key: "mysql/master/cluster", Value: "mysql.example.com:3306"},
				{Key: "mysql/master/cluster/hostname", Value: "mysql.example.com"},
			})
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(writes) == 0 {
		t.Fatal("expected concurrent distribution to write at least once")
	}
}

func TestConsulTokenSentAsHeaderNotQuery(t *testing.T) {
	var captured []capturedConsulRequest
	var mu sync.Mutex
	server := buildConsulTestServerWithObserver(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster",
			Request:  "mysql.example.com:3306",
			Response: true,
		},
	}, func(req capturedConsulRequest) {
		mu.Lock()
		captured = append(captured, req)
		mu.Unlock()
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulAclToken = "secret-token"

	store := newTestConsulStore(t)
	if err := store.PutKeyValue("mysql/master/cluster", "mysql.example.com:3306"); err != nil {
		t.Fatalf("put: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("expected 1 request, got %d", len(captured))
	}
	if captured[0].Token != "secret-token" {
		t.Fatalf("X-Consul-Token = %q", captured[0].Token)
	}
	if strings.Contains(captured[0].URL, "secret-token") || strings.Contains(captured[0].URL, "token=") {
		t.Fatalf("token leaked into URL %q", captured[0].URL)
	}
}

func TestConsulJSONTokenWinsOverEnv(t *testing.T) {
	t.Setenv("CONSUL_HTTP_TOKEN", "env-token")
	t.Setenv("CONSUL_HTTP_TOKEN_FILE", "")
	var captured []capturedConsulRequest
	var mu sync.Mutex
	server := buildConsulTestServerWithObserver(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster",
			Request:  "v",
			Response: true,
		},
	}, func(req capturedConsulRequest) {
		mu.Lock()
		captured = append(captured, req)
		mu.Unlock()
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulAclToken = "json-token"

	store := newTestConsulStore(t)
	if err := store.PutKeyValue("mysql/master/cluster", "v"); err != nil {
		t.Fatalf("put: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if captured[0].Token != "json-token" {
		t.Fatalf("token = %q, want json-token", captured[0].Token)
	}
}

func TestConsulEnvTokenUsedWhenJSONEmpty(t *testing.T) {
	t.Setenv("CONSUL_HTTP_TOKEN", "env-token")
	var captured []capturedConsulRequest
	var mu sync.Mutex
	server := buildConsulTestServerWithObserver(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster",
			Request:  "v",
			Response: true,
		},
	}, func(req capturedConsulRequest) {
		mu.Lock()
		captured = append(captured, req)
		mu.Unlock()
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)

	store := newTestConsulStore(t)
	if err := store.PutKeyValue("mysql/master/cluster", "v"); err != nil {
		t.Fatalf("put: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if captured[0].Token != "env-token" {
		t.Fatalf("token = %q, want env-token", captured[0].Token)
	}
}

func TestConsulDatacenterQueryParameter(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster?dc=east",
			Request:  "v",
			Response: true,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulDatacenter = "east"

	store := newTestConsulStore(t)
	if err := store.PutKeyValue("mysql/master/cluster", "v"); err != nil {
		t.Fatalf("put: %v", err)
	}
}

func TestNewConsulClientEmptyAddress(t *testing.T) {
	client, err := newConsulClient(consulClientOptions{})
	if err != nil {
		t.Fatalf("empty address should not error, got %v", err)
	}
	if client != nil {
		t.Fatal("empty address should not construct a client")
	}
}
