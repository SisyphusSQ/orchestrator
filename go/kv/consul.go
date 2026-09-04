/*
   Copyright 2017 Shlomi Noach, GitHub Inc.

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
	"sync/atomic"

	"github.com/openark/orchestrator/go/config"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/patrickmn/go-cache"

	"github.com/openark/golib/log"
)

// getConsulKVCacheKey returns a Consul KV cache key for a given datacenter
func getConsulKVCacheKey(dc, key string) string {
	return fmt.Sprintf("%s;%s", dc, key)
}

// A Consul store based on config's `ConsulAddress`, `ConsulScheme`, and `ConsulKVPrefix`
type consulStore struct {
	client              *consulapi.Client
	kvCache             *cache.Cache
	distributionReentry int64
}

// NewConsulStore creates a Consul KV store that uses an already constructed official client.
// A nil client is valid and makes every operation a no-op.
func NewConsulStore(client *consulapi.Client) KVStore {
	return &consulStore{
		client:  client,
		kvCache: cache.New(cache.NoExpiration, cache.DefaultExpiration),
	}
}

func (this *consulStore) PutKeyValue(key string, value string) (err error) {
	if this.client == nil {
		return nil
	}
	pair := &consulapi.KVPair{Key: key, Value: []byte(value)}
	_, err = this.client.KV().Put(pair, nil)
	return err
}

func (this *consulStore) GetKeyValue(key string) (value string, found bool, err error) {
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

func (this *consulStore) PutKVPairs(kvPairs []*KVPair) (err error) {
	if this.client == nil {
		return nil
	}
	for _, pair := range kvPairs {
		if err := this.PutKeyValue(pair.Key, pair.Value); err != nil {
			return err
		}
	}
	return nil
}

func (this *consulStore) DistributePairs(kvPairs [](*KVPair)) (err error) {
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
	log.Debugf("consulStore.DistributePairs(): distributing %d pairs to %d datacenters", len(kvPairs), len(datacenters))
	consulPairs := make([]*consulapi.KVPair, 0, len(kvPairs))
	for _, kvPair := range kvPairs {
		consulPairs = append(consulPairs, &consulapi.KVPair{Key: kvPair.Key, Value: []byte(kvPair.Value)})
	}

	errCh := make(chan error, len(datacenters))
	for _, datacenter := range datacenters {
		datacenter := datacenter
		go func() {
			errCh <- this.distributePairsToDatacenter(datacenter, consulPairs)
		}()
	}
	var errs []error
	for range datacenters {
		if dcErr := <-errCh; dcErr != nil {
			errs = append(errs, dcErr)
		}
	}
	return errors.Join(errs...)
}

func (this *consulStore) distributePairsToDatacenter(datacenter string, consulPairs []*consulapi.KVPair) error {
	writeOptions := &consulapi.WriteOptions{Datacenter: datacenter}
	queryOptions := &consulapi.QueryOptions{Datacenter: datacenter}
	skipped := 0
	existing := 0
	written := 0
	failed := 0
	var errs []error

	for _, consulPair := range consulPairs {
		val := string(consulPair.Value)
		kcCacheKey := getConsulKVCacheKey(datacenter, consulPair.Key)

		if value, found := this.kvCache.Get(kcCacheKey); found && val == value {
			skipped++
			continue
		}
		pair, _, err := this.client.KV().Get(consulPair.Key, queryOptions)
		if err != nil {
			log.Debugf("consulStore.DistributePairs(): read-before-write optimization failed for %s; proceeding with intended write: %v", kcCacheKey, err)
		} else if pair != nil && val == string(pair.Value) {
			existing++
			this.kvCache.SetDefault(kcCacheKey, val)
			continue
		}

		if _, err := this.client.KV().Put(consulPair, writeOptions); err != nil {
			log.Errorf("consulStore.DistributePairs(): failed %s", kcCacheKey)
			failed++
			errs = append(errs, fmt.Errorf("consul datacenter %s key %s: %w", datacenter, consulPair.Key, err))
			continue
		}
		log.Debugf("consulStore.DistributePairs(): written %s=%s", kcCacheKey, val)
		written++
		this.kvCache.SetDefault(kcCacheKey, val)
	}
	log.Debugf("consulStore.DistributePairs(): datacenter: %s; skipped: %d, existing: %d, written: %d, failed: %d", datacenter, skipped, existing, written, failed)
	return errors.Join(errs...)
}
