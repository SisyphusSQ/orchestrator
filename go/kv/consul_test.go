package kv

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

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

// sortTxnKVOps sort TxnOps by op.KV.Key to resolve random test failures
func sortTxnKVOps(txnOps []*consulapi.TxnOp) []*consulapi.TxnOp {
	sort.Slice(txnOps, func(a, b int) bool {
		return txnOps[a].KV.Key < txnOps[b].KV.Key
	})
	return txnOps
}

func buildConsulTestServer(t *testing.T, testOps []consulTestServerOp) *httptest.Server {
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBytes, _ := ioutil.ReadAll(r.Body)
		requestBody := strings.TrimSpace(string(requestBytes))

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
			if r.URL.String() == "/v1/catalog/datacenters" {
				w.WriteHeader(testOp.ResponseCode)
				json.NewEncoder(w).Encode(testOp.Response)
				return
			} else if strings.HasPrefix(r.URL.String(), "/v1/kv") && testOp.Response != nil {
				if expectedRequest, ok := testOp.Request.(string); ok && requestBody != expectedRequest {
					continue
				}
				w.WriteHeader(testOp.ResponseCode)
				json.NewEncoder(w).Encode(testOp.Response)
				return
			} else if strings.HasPrefix(r.URL.String(), "/v1/txn") {
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

	originalAddress := config.Config.ConsulAddress
	originalScheme := config.Config.ConsulScheme
	originalToken := config.Config.ConsulAclToken
	originalDistribution := config.Config.ConsulCrossDataCenterDistribution
	t.Cleanup(func() {
		config.Config.ConsulAddress = originalAddress
		config.Config.ConsulScheme = originalScheme
		config.Config.ConsulAclToken = originalToken
		config.Config.ConsulCrossDataCenterDistribution = originalDistribution
	})

	config.Config.ConsulAddress = strings.TrimPrefix(address, "http://")
	config.Config.ConsulScheme = "http"
	config.Config.ConsulAclToken = ""
	config.Config.ConsulCrossDataCenterDistribution = crossDataCenterDistribution
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

	store := NewConsulStore()
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
			Method:   http.MethodGet,
			URL:      "/v1/kv/mysql/master/cluster?dc=dc2",
			Response: consulapi.KVPairs{},
			Matches:  &valueReads,
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

	store := NewConsulStore()
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
