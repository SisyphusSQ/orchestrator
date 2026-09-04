package kv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/openark/orchestrator/go/config"
)

func TestGroupKVPairsByKeyPrefix(t *testing.T) {
	originalMax := config.Config.ConsulMaxKVsPerTransaction
	originalPrefix := config.Config.KVClusterMasterPrefix
	t.Cleanup(func() {
		config.Config.ConsulMaxKVsPerTransaction = originalMax
		config.Config.KVClusterMasterPrefix = originalPrefix
	})
	config.Config.ConsulMaxKVsPerTransaction = 12 // only 10 (5 x 2) KVs should fit into a max of 12
	config.Config.KVClusterMasterPrefix = "mysql/master"

	// make 100 KVs for 20 clusters
	kvPairs := consulapi.KVPairs{}
	var cluster int
	for cluster < 20 {
		kvPairs = append(kvPairs,
			&consulapi.KVPair{
				Key:   fmt.Sprintf("%s/cluster%d", config.Config.KVClusterMasterPrefix, cluster),
				Value: []byte("mysql.example.com:3306"),
			},
			&consulapi.KVPair{
				Key:   fmt.Sprintf("%s/cluster%d/hostname", config.Config.KVClusterMasterPrefix, cluster),
				Value: []byte("mysql.example.com"),
			},
			&consulapi.KVPair{
				Key:   fmt.Sprintf("%s/cluster%d/ipv4", config.Config.KVClusterMasterPrefix, cluster),
				Value: []byte("10.20.30.40"),
			},
			&consulapi.KVPair{
				Key:   fmt.Sprintf("%s/cluster%d/ipv6", config.Config.KVClusterMasterPrefix, cluster),
				Value: []byte("fdf0:7a53:0b88:d147:xxxx:xxxx:xxxx:xxxx"),
			},
			&consulapi.KVPair{
				Key:   fmt.Sprintf("%s/cluster%d/port", config.Config.KVClusterMasterPrefix, cluster),
				Value: []byte("3306"),
			},
		)
		cluster++
	}

	grouped := groupKVPairsByKeyPrefix(kvPairs)
	if len(grouped) != 10 {
		t.Fatalf("expected 10 groups, got %d: %v", len(grouped), grouped)
	}
	if len(grouped[0]) != 10 {
		t.Fatalf("expected 10 KVPairs in first group, got %d: %v", len(grouped[0]), grouped[0])
	}

	// check KVs for a cluster are in a single group
	clusterCounts := map[string]map[int]int{}
	for i, group := range grouped {
		for _, kvPair := range group {
			s := strings.Split(kvPair.Key, "/")
			clusterName := s[2]
			if _, ok := clusterCounts[clusterName]; ok {
				clusterCounts[clusterName][i]++
			} else {
				clusterCounts[clusterName] = map[int]int{i: 1}
			}
		}
	}
	for cluster, groups := range clusterCounts {
		if len(groups) != 1 {
			t.Fatalf("expected %s to be in a single group, found it in %d group(s): %v", cluster, len(groups), groups)
		}
		for _, count := range groups {
			if count != config.ConsulKVsPerCluster {
				t.Fatalf("expected group to contain %d x %s keys, found: %d", config.ConsulKVsPerCluster, cluster, count)
			}
		}
	}
}

func TestGroupKVPairsByKeyPrefixStableOrder(t *testing.T) {
	originalMax := config.Config.ConsulMaxKVsPerTransaction
	originalPrefix := config.Config.KVClusterMasterPrefix
	t.Cleanup(func() {
		config.Config.ConsulMaxKVsPerTransaction = originalMax
		config.Config.KVClusterMasterPrefix = originalPrefix
	})
	config.Config.ConsulMaxKVsPerTransaction = 5
	config.Config.KVClusterMasterPrefix = "mysql/master"

	forward := consulapi.KVPairs{}
	reverse := consulapi.KVPairs{}
	for _, cluster := range []string{"b-cluster", "a-cluster"} {
		for _, suffix := range []string{"", "/hostname", "/ipv4", "/ipv6", "/port"} {
			pair := &consulapi.KVPair{
				Key:   fmt.Sprintf("mysql/master/%s%s", cluster, suffix),
				Value: []byte("v"),
			}
			forward = append(forward, pair)
		}
	}
	for i := len(forward) - 1; i >= 0; i-- {
		reverse = append(reverse, forward[i])
	}

	gotForward := groupClusterNames(groupKVPairsByKeyPrefix(forward))
	gotReverse := groupClusterNames(groupKVPairsByKeyPrefix(reverse))
	if !reflect.DeepEqual(gotForward, gotReverse) {
		t.Fatalf("grouping is order-dependent:\nforward=%v\nreverse=%v", gotForward, gotReverse)
	}
	if len(gotForward) != 2 || gotForward[0][0] != "a-cluster" || gotForward[1][0] != "b-cluster" {
		t.Fatalf("expected sorted prefixes a-cluster then b-cluster, got %v", gotForward)
	}
}

func groupClusterNames(groups []consulapi.KVPairs) [][]string {
	out := make([][]string, len(groups))
	for i, group := range groups {
		seen := map[string]struct{}{}
		for _, pair := range group {
			name := strings.Split(pair.Key, "/")[2]
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out[i] = append(out[i], name)
		}
	}
	return out
}

func TestConsulTxnStorePutKVPairs(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   "PUT",
			URL:      "/v1/kv/kv1",
			Request:  "test",
			Response: &consulapi.KVPair{Key: "kv1", Value: []byte("test")},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "kv1", Value: []byte("test")},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "kv2", Value: []byte("test2")},
				},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{
						KV: &consulapi.KVPair{Key: "kv1", Value: []byte("test")},
					},
					{
						KV: &consulapi.KVPair{Key: "kv2", Value: []byte("test2")},
					},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "fail1", Value: []byte("test")},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "fail2", Value: []byte("test2")},
				},
			},
			Response: &consulapi.TxnResponse{
				Errors: consulapi.TxnErrors{
					{
						What: "test error",
					},
				},
			},
			ResponseCode: http.StatusConflict, // PUT /v1/txn returns a HTTP 409 code on txn failure
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	store := newTestConsulTxnStore(t)

	t.Run("single-kv", func(t *testing.T) {
		if err := store.PutKVPairs([]*KVPair{
			{Key: "kv1", Value: "test"},
		}); err != nil {
			t.Fatalf("Unable to run .PutKVPairs(): %v", err)
		}
	})

	t.Run("multi-kv", func(t *testing.T) {
		if err := store.PutKVPairs([]*KVPair{
			{Key: "kv1", Value: "test"},
			{Key: "kv2", Value: "test2"},
		}); err != nil {
			t.Fatalf("Unable to run .PutKVPairs(): %v", err)
		}
	})

	t.Run("multi-kv-failed", func(t *testing.T) {
		if err := store.PutKVPairs([]*KVPair{
			{Key: "fail1", Value: "test"},
			{Key: "fail2", Value: "test2"},
		}); err == nil || err.Error() != "test error" {
			t.Fatalf("Expected %q error from .PutKVPairs(), got: %q", "test error", err)
		}
	})
}

func TestConsulTxnStoreMissingKeyReturnsNotFound(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:       http.MethodGet,
			URL:          "/v1/kv/missing",
			ResponseCode: http.StatusNotFound,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	store := newTestConsulTxnStore(t)
	value, found, err := store.GetKeyValue("missing")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if found || value != "" {
		t.Fatalf("expected found=false, got %q %t", value, found)
	}
}

func TestConsulTxnStoreUpdateDatacenterKVPairs(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test"},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test2"},
				},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{
						KV: &consulapi.KVPair{Key: "test", Value: []byte("test")},
					},
					{
						KV: &consulapi.KVPair{Key: "test2", Value: []byte("not-equal")},
					},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test"},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "doesnt-exist"},
				},
			},
			Response: &consulapi.TxnResponse{
				Errors: consulapi.TxnErrors{
					{
						OpIndex: 1,
						What:    `key "doesnt-exist" doesn't exist`,
					},
				},
			},
			ResponseCode: http.StatusConflict, // PUT /v1/txn returns a HTTP 409 code on txn failure
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "test2", Value: []byte("test")},
				},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{
						KV: &consulapi.KVPair{Key: "test2", Value: []byte("test")},
					},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "test", Value: []byte("test")},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "doesnt-exist", Value: []byte("test")},
				},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{
						KV: &consulapi.KVPair{Key: "test", Value: []byte("test")},
					},
					{
						KV: &consulapi.KVPair{Key: "doesnt-exist", Value: []byte("test")},
					},
				},
			},
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	store := newTestConsulTxnStore(t).(*consulTxnStore)

	t.Run("success-cached", func(t *testing.T) {
		cacheKey := getConsulKVCacheKey(consulTestDefaultDatacenter, "cached")
		store.kvCache.SetDefault(cacheKey, "cached") // pre-cache the 'cached' key-value
		defer store.kvCache.Flush()

		kvPairs := []*consulapi.KVPair{
			{Key: "cached", Value: []byte("cached")}, // already cached key-value
			{Key: "test", Value: []byte("test")},     // already correct on consul server
			{Key: "test2", Value: []byte("test")},    // not equal on consul server
		}

		resp := store.updateDatacenterKVPairs(consulTestDefaultDatacenter, kvPairs)
		if resp.err != nil {
			t.Fatalf(".updateDatacenterKVPairs() should not return an error, got: %v", resp.err)
		}
		if resp.skipped != 1 || resp.existing != 1 || resp.written != 1 || resp.failed != 0 {
			t.Fatalf("expected: existing/skipped/written=1 and failed=0, got: skipped=%d, existing=%d, written=%d, failed=%d",
				resp.skipped, resp.existing, resp.written, resp.failed,
			)
		}

		for _, pair := range kvPairs {
			cacheKey := getConsulKVCacheKey(consulTestDefaultDatacenter, pair.Key)
			if cached, found := store.kvCache.Get(cacheKey); !found || cached != string(pair.Value) {
				t.Fatalf("expected cache key %q to equal %q, got %v", cacheKey, string(pair.Value), cached)
			}
		}
	})

	t.Run("success-missing-kv", func(t *testing.T) {
		kvPairs := []*consulapi.KVPair{
			{Key: "test", Value: []byte("test")},         // already correct on consul server
			{Key: "doesnt-exist", Value: []byte("test")}, // does not exist on consul server
		}
		resp := store.updateDatacenterKVPairs(consulTestDefaultDatacenter, kvPairs)

		if resp.err != nil {
			t.Fatalf(".updateDatacenterKVPairs() should not return an error, got: %v", resp.err)
		}
		if resp.skipped != 0 || resp.existing != 0 || resp.written != 2 || resp.failed != 0 { // confirm all KVs are updated if one does not exist
			t.Fatalf("expected: existing/skipped/failed=0 and written=2, got: skipped=%d, existing=%d, written=%d, failed=%d",
				resp.skipped, resp.existing, resp.written, resp.failed,
			)
		}
	})
}

func TestConsulTxnStoreReadFailureStillWrites(t *testing.T) {
	var setTxns atomic.Int64
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:       "PUT",
			URL:          "/v1/txn?dc=dc1",
			Request:      consulapi.TxnOps{{KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test"}}},
			Response:     "read failed",
			ResponseCode: http.StatusInternalServerError,
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "test", Value: []byte("test")}},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{KV: &consulapi.KVPair{Key: "test", Value: []byte("test")}},
				},
			},
			Matches: &setTxns,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)
	store := newTestConsulTxnStore(t).(*consulTxnStore)
	resp := store.updateDatacenterKVPairs(consulTestDefaultDatacenter, []*consulapi.KVPair{
		{Key: "test", Value: []byte("test")},
	})
	if resp.err != nil {
		t.Fatalf("intended write after read failure: %v", resp.err)
	}
	if resp.getTxns != 1 || resp.setTxns != 1 || resp.written != 1 {
		t.Fatalf("expected one get txn and one intended write, got get=%d set=%d written=%d", resp.getTxns, resp.setTxns, resp.written)
	}
	if setTxns.Load() != 1 {
		t.Fatalf("expected the target write to run once, got %d", setTxns.Load())
	}
}

func TestConsulTxnStoreDistributePairs(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   "GET",
			URL:      "/v1/catalog/datacenters",
			Response: []string{"dc1"},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1"},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1/hostname"},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1/ipv4"},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1/ipv6"},
				},
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1/port"},
				},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{
						KV: &consulapi.KVPair{Key: "test/cluster1", Value: []byte("not-equal")},
					},
					{
						KV: &consulapi.KVPair{Key: "test/cluster1/hostname", Value: []byte("mysql.example.com")},
					},
					{
						KV: &consulapi.KVPair{Key: "test/cluster1/ipv4", Value: []byte("10.20.30.40")},
					},
					{
						KV: &consulapi.KVPair{Key: "test/cluster1/ipv6", Value: []byte("fdf0:7a53:0b88:d147:xxxx:xxxx:xxxx:xxxx")},
					},
					{
						KV: &consulapi.KVPair{Key: "test/cluster1/port", Value: []byte("3306")},
					},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{
					KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "test/cluster1", Value: []byte("mysql.example.com:3306")},
				},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{
						KV: &consulapi.KVPair{Key: "test/cluster1", Value: []byte("mysql.example.com:3306")},
					},
				},
			},
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, true)
	config.Config.KVClusterMasterPrefix = "test"

	store := newTestConsulTxnStore(t)
	if err := store.DistributePairs([]*KVPair{
		{Key: "test/cluster1", Value: "mysql.example.com:3306"},
		{Key: "test/cluster1/hostname", Value: "mysql.example.com"},
		{Key: "test/cluster1/ipv4", Value: "10.20.30.40"},
		{Key: "test/cluster1/ipv6", Value: "fdf0:7a53:0b88:d147:xxxx:xxxx:xxxx:xxxx"},
		{Key: "test/cluster1/port", Value: "3306"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConsulTxnStoreDistributePairsReturnsFailure(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   "GET",
			URL:      "/v1/catalog/datacenters",
			Response: []string{"dc1", "dc2"},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1"}},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{KV: &consulapi.KVPair{Key: "test/cluster1", Value: []byte("old")}},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc1",
			Request: consulapi.TxnOps{
				{KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "test/cluster1", Value: []byte("new")}},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{KV: &consulapi.KVPair{Key: "test/cluster1", Value: []byte("new")}},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc2",
			Request: consulapi.TxnOps{
				{KV: &consulapi.KVTxnOp{Verb: consulapi.KVGet, Key: "test/cluster1"}},
			},
			Response: &consulapi.TxnResponse{
				Results: consulapi.TxnResults{
					{KV: &consulapi.KVPair{Key: "test/cluster1", Value: []byte("old")}},
				},
			},
		},
		{
			Method: "PUT",
			URL:    "/v1/txn?dc=dc2",
			Request: consulapi.TxnOps{
				{KV: &consulapi.KVTxnOp{Verb: consulapi.KVSet, Key: "test/cluster1", Value: []byte("new")}},
			},
			Response: &consulapi.TxnResponse{
				Errors: consulapi.TxnErrors{
					{What: "dc2 write failed"},
				},
			},
			ResponseCode: http.StatusConflict,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, true)
	config.Config.KVClusterMasterPrefix = "test"
	config.Config.ConsulMaxKVsPerTransaction = 5

	store := newTestConsulTxnStore(t)
	err := store.DistributePairs([]*KVPair{{Key: "test/cluster1", Value: "new"}})
	if err == nil {
		t.Fatal("expected transaction failure to be returned")
	}
	if !strings.Contains(err.Error(), "dc2") || !strings.Contains(err.Error(), "dc2 write failed") {
		t.Fatalf("error %q should include datacenter and txn failure", err)
	}
}

func TestConsulTxnStoreDistributePairsNotReentrant(t *testing.T) {
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
		if r.URL.Path == "/v1/txn" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(&consulapi.TxnResponse{})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	configureConsulTest(t, server.URL, true)
	store := newTestConsulTxnStore(t)
	done := make(chan error, 1)
	go func() {
		done <- store.DistributePairs([]*KVPair{{Key: "test/cluster1", Value: "v"}})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first DistributePairs")
	}
	if err := store.DistributePairs([]*KVPair{{Key: "test/cluster1", Value: "v"}}); err != nil {
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

func TestConsulTxnStoreDistributePairsRace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/catalog/datacenters":
			json.NewEncoder(w).Encode([]string{"dc1", "dc2"})
		case r.URL.Path == "/v1/txn":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(&consulapi.TxnResponse{})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	configureConsulTest(t, server.URL, true)
	store := newTestConsulTxnStore(t)
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
}
