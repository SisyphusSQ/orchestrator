/*
   Copyright 2021 Shlomi Noach, GitHub Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package kv

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/openark/orchestrator/go/config"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/patrickmn/go-cache"

	"github.com/openark/golib/log"
)

func clusterKeyPrefix(key, clusterMasterPrefix string) string {
	prefix := key
	if strings.HasPrefix(prefix, clusterMasterPrefix) {
		prefix = strings.Replace(prefix, clusterMasterPrefix, "", 1)
		if path := strings.Split(prefix, "/"); len(path) > 0 {
			prefix = path[0]
		}
	}
	return prefix
}

// groupKVPairsByKeyPrefix groups Consul Transaction operations by KV key prefix. This
// ensures KVs for a single cluster are grouped into a single transaction. Prefixes
// are sorted so grouping is stable across runs.
func groupKVPairsByKeyPrefix(kvPairs consulapi.KVPairs) (groups []consulapi.KVPairs) {
	maxOpsPerTxn := config.Config.ConsulMaxKVsPerTransaction
	clusterMasterPrefix := config.Config.KVClusterMasterPrefix + "/"
	groupsMap := map[string]consulapi.KVPairs{}
	var prefixes []string
	seen := map[string]struct{}{}
	for _, pair := range kvPairs {
		prefix := clusterKeyPrefix(pair.Key, clusterMasterPrefix)
		if _, found := seen[prefix]; !found {
			seen[prefix] = struct{}{}
			prefixes = append(prefixes, prefix)
		}
		groupsMap[prefix] = append(groupsMap[prefix], pair)
	}
	sort.Strings(prefixes)

	pairsBuf := consulapi.KVPairs{}
	for _, prefix := range prefixes {
		group := groupsMap[prefix]
		groupLen := len(group)
		pairsBufLen := len(pairsBuf)
		if (pairsBufLen + groupLen) > maxOpsPerTxn {
			groups = append(groups, pairsBuf)
			pairsBuf = consulapi.KVPairs{}
		}
		pairsBuf = append(pairsBuf, group...)
	}
	if len(pairsBuf) > 0 {
		groups = append(groups, pairsBuf)
	}
	return groups
}

// A Consul store based on config's `ConsulAddress`, `ConsulScheme`, and `ConsulKVPrefix`
type consulTxnStore struct {
	client              *consulapi.Client
	kvCache             *cache.Cache
	distributionReentry int64
}

// NewConsulTxnStore creates a Consul store that uses Consul Transactions to read/write multiple KVPairs.
// A nil client is valid and makes every operation a no-op.
func NewConsulTxnStore(client *consulapi.Client) KVStore {
	return &consulTxnStore{
		client:  client,
		kvCache: cache.New(cache.NoExpiration, cache.DefaultExpiration),
	}
}

// doWriteTxn performs one or many of write operations using a Consul Transaction and handles any client/server
// or transaction-level errors. Updates are all-or-nothing - all operations are rolled-back on any txn error
func (this *consulTxnStore) doWriteTxn(txnOps consulapi.TxnOps, queryOptions *consulapi.QueryOptions) (err error) {
	ok, resp, _, err := this.client.Txn().Txn(txnOps, queryOptions)
	if err != nil {
		log.Errorf("consulTxnStore.doWriteTxn(): %v", err)
		return err
	} else if !ok {
		for _, terr := range resp.Errors {
			txnOp := txnOps[terr.OpIndex]
			log.Errorf("consulTxnStore.doWriteTxn(): transaction error %q for KV %s on %s", terr.What, txnOp.KV.Verb, txnOp.KV.Key)
			err = fmt.Errorf("%v", terr.What)
		}
	}
	return err
}

// updateDatacenterKVPairsResponse contains a response from .updateDatacenterKVPairs()
type updateDatacenterKVPairsResponse struct {
	err      error
	existing int
	failed   int
	skipped  int
	written  int
	getTxns  int
	setTxns  int
}

// updateDatacenterKVPairs handles updating a list of Consul KV pairs for a single datacenter. Current values are
// read from the server in a single transaction and any necessary updates are made in a second transaction. If a
// KVPair from a group is missing on the server all KVPairs will be updated. A failed read is only an optimization
// miss: the originally intended writes still run once. Failed writes are not retried.
func (this *consulTxnStore) updateDatacenterKVPairs(dc string, kvPairs []*consulapi.KVPair) updateDatacenterKVPairsResponse {
	queryOptions := &consulapi.QueryOptions{Datacenter: dc}
	kcCacheKeys := make([]string, 0)

	resp := updateDatacenterKVPairsResponse{}
	var getTxnOps consulapi.TxnOps
	var getTxnResp *consulapi.TxnResponse
	var possibleSetKVPairs []*consulapi.KVPair
	for _, kvPair := range kvPairs {
		val := string(kvPair.Value)
		kcCacheKey := getConsulKVCacheKey(dc, kvPair.Key)
		kcCacheKeys = append(kcCacheKeys, kcCacheKey)
		if value, found := this.kvCache.Get(kcCacheKey); found && val == value {
			resp.skipped++
			continue
		}
		getTxnOps = append(getTxnOps, &consulapi.TxnOp{
			KV: &consulapi.KVTxnOp{
				Verb: consulapi.KVGet,
				Key:  kvPair.Key,
			},
		})
		possibleSetKVPairs = append(possibleSetKVPairs, kvPair)
	}
	readOK := false
	if len(getTxnOps) > 0 {
		ok, txnResp, _, terr := this.client.Txn().Txn(getTxnOps, queryOptions)
		resp.getTxns++
		if terr != nil {
			log.Errorf("consulTxnStore.DistributePairs(): read-before-write optimization failed; proceeding with intended writes: %v", terr)
		} else if ok {
			readOK = true
			getTxnResp = txnResp
		} else {
			log.Debugf("consulTxnStore.DistributePairs(): read-before-write transaction did not succeed; proceeding with intended writes")
		}
	}

	var setTxnOps consulapi.TxnOps
	for _, pair := range possibleSetKVPairs {
		kvExistsAndEqual := false
		if readOK && getTxnResp != nil {
			for _, result := range getTxnResp.Results {
				if pair.Key == result.KV.Key && string(pair.Value) == string(result.KV.Value) {
					this.kvCache.SetDefault(getConsulKVCacheKey(dc, pair.Key), string(pair.Value))
					kvExistsAndEqual = true
					resp.existing++
					break
				}
			}
		}
		if !kvExistsAndEqual {
			setTxnOps = append(setTxnOps, &consulapi.TxnOp{
				KV: &consulapi.KVTxnOp{
					Verb:  consulapi.KVSet,
					Key:   pair.Key,
					Value: pair.Value,
				},
			})
		}
	}

	if len(setTxnOps) > 0 {
		if resp.err = this.doWriteTxn(setTxnOps, queryOptions); resp.err != nil {
			log.Errorf("consulTxnStore.DistributePairs(): failed %v, error %v", kcCacheKeys, resp.err)
			resp.failed = len(setTxnOps)
		} else {
			for _, txnOp := range setTxnOps {
				this.kvCache.SetDefault(getConsulKVCacheKey(dc, txnOp.KV.Key), string(txnOp.KV.Value))
				resp.written++
			}
		}
		resp.setTxns++
	}

	return resp
}

// GetKeyValue returns the value of a Consul KV if it exists
func (this *consulTxnStore) GetKeyValue(key string) (value string, found bool, err error) {
	if this.client == nil {
		return "", false, nil
	}
	pair, _, err := this.client.KV().Get(key, nil)
	if err != nil {
		return "", false, err
	}
	if pair == nil {
		return "", false, nil
	}
	return string(pair.Value), true, nil
}

// PutKeyValue performs a Consul KV put operation for a key/value
func (this *consulTxnStore) PutKeyValue(key string, value string) (err error) {
	if this.client == nil {
		return nil
	}
	pair := &consulapi.KVPair{Key: key, Value: []byte(value)}
	_, err = this.client.KV().Put(pair, nil)
	return err
}

// PutKVPairs updates one or more KV pairs in a single, atomic Consul operation.
// If a single KV pair is provided PutKeyValue is used to update the pair
func (this *consulTxnStore) PutKVPairs(kvPairs []*KVPair) (err error) {
	if this.client == nil {
		return nil
	}
	if len(kvPairs) == 1 {
		return this.PutKeyValue(kvPairs[0].Key, kvPairs[0].Value)
	}
	var txnOps consulapi.TxnOps
	for _, pair := range kvPairs {
		txnOps = append(txnOps, &consulapi.TxnOp{
			KV: &consulapi.KVTxnOp{
				Verb:  consulapi.KVSet,
				Key:   pair.Key,
				Value: []byte(pair.Value),
			},
		})
	}
	return this.doWriteTxn(txnOps, nil)
}

// DistributePairs updates all known Consul Datacenters with one or more KV pairs
func (this *consulTxnStore) DistributePairs(kvPairs [](*KVPair)) (err error) {
	// This function is non re-entrant (it can only be running once at any point in time)
	if atomic.CompareAndSwapInt64(&this.distributionReentry, 0, 1) {
		defer atomic.StoreInt64(&this.distributionReentry, 0)
	} else {
		return nil
	}

	if !config.Config.ConsulCrossDataCenterDistribution {
		return nil
	}
	if this.client == nil {
		return nil
	}

	datacenters, err := this.client.Catalog().Datacenters()
	if err != nil {
		return err
	}
	log.Debugf("consulTxnStore.DistributePairs(): distributing %d pairs to %d datacenters", len(kvPairs), len(datacenters))
	consulPairs := []*consulapi.KVPair{}
	for _, kvPair := range kvPairs {
		consulPairs = append(consulPairs, &consulapi.KVPair{Key: kvPair.Key, Value: []byte(kvPair.Value)})
	}
	groups := groupKVPairsByKeyPrefix(consulPairs)

	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	for _, datacenter := range datacenters {
		datacenter := datacenter
		wg.Add(1)
		go func() {
			defer wg.Done()

			sum := updateDatacenterKVPairsResponse{}
			var dcErrs []error
			var dcWg sync.WaitGroup
			var dcMu sync.Mutex
			for _, kvPairGroup := range groups {
				kvPairGroup := kvPairGroup
				dcWg.Add(1)
				go func() {
					defer dcWg.Done()
					resp := this.updateDatacenterKVPairs(datacenter, kvPairGroup)
					dcMu.Lock()
					defer dcMu.Unlock()
					sum.existing += resp.existing
					sum.failed += resp.failed
					sum.skipped += resp.skipped
					sum.written += resp.written
					sum.getTxns += resp.getTxns
					sum.setTxns += resp.setTxns
					if resp.err != nil {
						dcErrs = append(dcErrs, resp.err)
					}
				}()
			}
			dcWg.Wait()
			log.Debugf("consulTxnStore.DistributePairs(): datacenter: %s; getTxns: %d, setTxns: %d, skipped: %d, existing: %d, written: %d, failed: %d",
				datacenter, sum.getTxns, sum.setTxns, sum.skipped, sum.existing, sum.written, sum.failed,
			)
			if joined := errors.Join(dcErrs...); joined != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("consul datacenter %s: %w", datacenter, joined))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
