/*
   Copyright 2014 Outbrain Inc.

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

// Package resolve resolves database instance addresses and persists resolution state.
package resolve

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	"github.com/patrickmn/go-cache"
)

// ResolveInstanceKey resolves key.Hostname in place and returns the same key.
func ResolveInstanceKey(key *instmodel.InstanceKey) (*instmodel.InstanceKey, error) {
	if !key.IsValid() {
		return key, nil
	}
	hostname, err := ResolveHostname(key.Hostname)
	if err == nil {
		key.Hostname = hostname
	}
	return key, err
}

// NewInstanceKey constructs and resolves an instance key.
func NewInstanceKey(hostname string, port int) (*instmodel.InstanceKey, error) {
	key, err := instmodel.NewInstanceKey(hostname, port)
	if err != nil {
		return nil, err
	}
	return ResolveInstanceKey(key)
}

// NewInstanceKeyStrings constructs and resolves an instance key from string values.
func NewInstanceKeyStrings(hostname string, port string) (*instmodel.InstanceKey, error) {
	key, err := instmodel.NewInstanceKeyStrings(hostname, port)
	if err != nil {
		return nil, err
	}
	return ResolveInstanceKey(key)
}

// NewRawInstanceKeyStrings constructs an instance key without resolving its hostname.
func NewRawInstanceKeyStrings(hostname string, port string) (*instmodel.InstanceKey, error) {
	return instmodel.NewInstanceKeyStrings(hostname, port)
}

// ParseInstanceKey parses an address using the configured default port and resolves its hostname.
func ParseInstanceKey(hostPort string) (*instmodel.InstanceKey, error) {
	key, err := ParseRawInstanceKey(hostPort)
	if err != nil {
		return nil, err
	}
	return ResolveInstanceKey(key)
}

// ParseRawInstanceKey parses an address using the configured default port without hostname resolution.
func ParseRawInstanceKey(hostPort string) (*instmodel.InstanceKey, error) {
	return instmodel.ParseInstanceKey(hostPort, config.Config.Topology.MySQL.DefaultPort)
}

// ReadCommaDelimitedList resolves a comma-delimited key list into keyMap.
func ReadCommaDelimitedList(keyMap *instmodel.InstanceKeyMap, list string) error {
	for token := range strings.SplitSeq(list, ",") {
		key, err := ParseInstanceKey(token)
		if err != nil {
			return err
		}
		keyMap.AddKey(*key)
	}
	return nil
}

type HostnameResolve struct {
	hostname         string
	resolvedHostname string
}

func (resolve HostnameResolve) String() string {
	return fmt.Sprintf("%s %s", resolve.hostname, resolve.resolvedHostname)
}

type HostnameUnresolve struct {
	hostname           string
	unresolvedHostname string
}

func (unresolve HostnameUnresolve) String() string {
	return fmt.Sprintf("%s %s", unresolve.hostname, unresolve.unresolvedHostname)
}

type HostnameRegistration struct {
	CreatedAt time.Time
	Key       instmodel.InstanceKey
	Hostname  string
}

func NewHostnameRegistration(instanceKey *instmodel.InstanceKey, hostname string) *HostnameRegistration {
	return &HostnameRegistration{
		CreatedAt: time.Now(),
		Key:       *instanceKey,
		Hostname:  hostname,
	}
}

func NewHostnameDeregistration(instanceKey *instmodel.InstanceKey) *HostnameRegistration {
	return &HostnameRegistration{
		CreatedAt: time.Now(),
		Key:       *instanceKey,
		Hostname:  "",
	}
}

var hostnameResolvesLightweightCache *cache.Cache
var hostnameResolvesLightweightCacheInit sync.Mutex
var hostnameResolvesLightweightCacheLoadedOnceFromDB bool
var hostnameIPsCache = cache.New(10*time.Minute, time.Minute)

func init() {
	if config.Config.Topology.Hostname.ResolveExpiryMinutes < 1 {
		config.Config.Topology.Hostname.ResolveExpiryMinutes = 1
	}
}

func getHostnameResolvesLightweightCache() *cache.Cache {
	hostnameResolvesLightweightCacheInit.Lock()
	defer hostnameResolvesLightweightCacheInit.Unlock()
	if hostnameResolvesLightweightCache == nil {
		hostnameResolvesLightweightCache = cache.New(time.Duration(config.Config.Topology.Hostname.ResolveExpiryMinutes)*time.Minute, time.Minute)
	}
	return hostnameResolvesLightweightCache
}

func HostnameResolveMethodIsNone() bool {
	return strings.ToLower(config.Config.Topology.Hostname.ResolveMethod) == "none"
}

// GetCNAME resolves an IP or hostname into a normalized valid CNAME
func GetCNAME(hostname string) (string, error) {
	res, err := net.LookupCNAME(hostname)
	if err != nil {
		return hostname, err
	}
	res = strings.TrimRight(res, ".")
	return res, nil
}

func resolveHostname(hostname string) (string, error) {
	switch strings.ToLower(config.Config.Topology.Hostname.ResolveMethod) {
	case "none":
		return hostname, nil
	case "default":
		return hostname, nil
	case "cname":
		return GetCNAME(hostname)
	case "ip":
		return getHostnameIP(hostname)
	}
	return hostname, nil
}

// Attempt to resolve a hostname. This may return a database cached hostname or otherwise
// it may resolve the hostname via CNAME
func ResolveHostname(hostname string) (string, error) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return hostname, errors.New("will not resolve empty hostname")
	}
	if strings.Contains(hostname, ",") {
		return hostname, fmt.Errorf("will not resolve multi-hostname: %+v", hostname)
	}
	if (&instmodel.InstanceKey{Hostname: hostname}).IsDetached() {
		// quietly abort. Nothing to do. The hostname is detached for a reason: it
		// will not be resolved, for sure.
		return hostname, nil
	}

	// First go to lightweight cache
	if resolvedHostname, found := getHostnameResolvesLightweightCache().Get(hostname); found {
		return resolvedHostname.(string), nil
	}

	if !hostnameResolvesLightweightCacheLoadedOnceFromDB {
		// A continuous-discovery will first make sure to load all resolves from DB.
		// However cli does not do so.
		// Anyway, it seems like the cache was not loaded from DB. Before doing real resolves,
		// let's try and get the resolved hostname from database.
		if !HostnameResolveMethodIsNone() {
			go func() {
				if resolvedHostname, err := ReadResolvedHostname(hostname); err == nil && resolvedHostname != "" {
					getHostnameResolvesLightweightCache().Set(hostname, resolvedHostname, 0)
				}
			}()
		}
	}

	// Unfound: resolve!
	log.Debugf("Hostname unresolved yet: %s", hostname)
	resolvedHostname, err := resolveHostname(hostname)
	if config.Config.Topology.Hostname.RejectResolvePattern != "" {
		// Reject, don't even cache
		if matched, _ := regexp.MatchString(config.Config.Topology.Hostname.RejectResolvePattern, resolvedHostname); matched {
			log.Warningf("ResolveHostname: %+v resolved to %+v but rejected due to RejectHostnameResolvePattern '%+v'", hostname, resolvedHostname, config.Config.Topology.Hostname.RejectResolvePattern)
			return hostname, nil
		}
	}

	if err != nil {
		// Problem. What we'll do is cache the hostname for just one minute, so as to avoid flooding requests
		// on one hand, yet make it refresh shortly on the other hand. Anyway do not write to database.
		getHostnameResolvesLightweightCache().Set(hostname, resolvedHostname, time.Minute)
		return hostname, err
	}
	// Good result! Cache it, also to DB
	log.Debugf("Cache hostname resolve %s as %s", hostname, resolvedHostname)
	go UpdateResolvedHostname(hostname, resolvedHostname)
	return resolvedHostname, nil
}

// UpdateResolvedHostname will store the given resolved hostname in cache
// Returns false when the key already existed with same resolved value (similar
// to AFFECTED_ROWS() in mysql)
func UpdateResolvedHostname(hostname string, resolvedHostname string) bool {
	if resolvedHostname == "" {
		return false
	}
	if existingResolvedHostname, found := getHostnameResolvesLightweightCache().Get(hostname); found && (existingResolvedHostname == resolvedHostname) {
		return false
	}
	getHostnameResolvesLightweightCache().Set(hostname, resolvedHostname, 0)
	if !HostnameResolveMethodIsNone() {
		WriteResolvedHostname(hostname, resolvedHostname)
	}
	return true
}

func LoadHostnameResolveCache() error {
	if !HostnameResolveMethodIsNone() {
		return loadHostnameResolveCacheFromDatabase()
	}
	return nil
}

func loadHostnameResolveCacheFromDatabase() error {
	allHostnamesResolves, err := ReadAllHostnameResolves()
	if err != nil {
		return err
	}
	for _, hostnameResolve := range allHostnamesResolves {
		getHostnameResolvesLightweightCache().Set(hostnameResolve.hostname, hostnameResolve.resolvedHostname, 0)
	}
	hostnameResolvesLightweightCacheLoadedOnceFromDB = true
	return nil
}

func FlushNontrivialResolveCacheToDatabase() error {
	if HostnameResolveMethodIsNone() {
		return nil
	}
	items, _ := HostnameResolveCache()
	for hostname := range items {
		resolvedHostname, found := getHostnameResolvesLightweightCache().Get(hostname)
		if found && (resolvedHostname.(string) != hostname) {
			WriteResolvedHostname(hostname, resolvedHostname.(string))
		}
	}
	return nil
}

func ResetHostnameResolveCache() error {
	err := deleteHostnameResolves()
	getHostnameResolvesLightweightCache().Flush()
	hostnameResolvesLightweightCacheLoadedOnceFromDB = false
	return err
}

func HostnameResolveCache() (map[string]cache.Item, error) {
	return getHostnameResolvesLightweightCache().Items(), nil
}

// UnresolveHostname reverses a hostname mapping and verifies it through the supplied topology reader.
func UnresolveHostname(
	instanceKey *instmodel.InstanceKey,
	readTopologyInstance func(*instmodel.InstanceKey) (*instmodel.Instance, error),
) (instmodel.InstanceKey, bool, error) {
	if *config.RuntimeCLIFlags.SkipUnresolve {
		return *instanceKey, false, nil
	}
	unresolvedHostname, err := readUnresolvedHostname(instanceKey.Hostname)
	if err != nil {
		return *instanceKey, false, log.Errore(err)
	}
	if unresolvedHostname == instanceKey.Hostname {
		// unchanged. Nothing to do
		return *instanceKey, false, nil
	}
	// We unresovled to a different hostname. We will now re-resolve to double-check!
	unresolvedKey := &instmodel.InstanceKey{Hostname: unresolvedHostname, Port: instanceKey.Port}

	instance, err := readTopologyInstance(unresolvedKey)
	if err != nil {
		return *instanceKey, false, log.Errore(err)
	}
	if instance.IsBinlogServer() && config.Config.Topology.Hostname.SkipBinlogServerUnresolveCheck {
		// Do nothing. Everything is assumed to be fine.
	} else if instance.Key.Hostname != instanceKey.Hostname {
		// Resolve(Unresolve(hostname)) != hostname ==> Bad; reject
		if *config.RuntimeCLIFlags.SkipUnresolveCheck {
			return *instanceKey, false, nil
		}
		return *instanceKey, false, log.Errorf("Error unresolving; hostname=%s, unresolved=%s, re-resolved=%s; mismatch. Skip/ignore with --skip-unresolve-check", instanceKey.Hostname, unresolvedKey.Hostname, instance.Key.Hostname)
	}
	return *unresolvedKey, true, nil
}

func RegisterHostnameUnresolve(registration *HostnameRegistration) (err error) {
	if registration.Hostname == "" {
		return DeleteHostnameUnresolve(&registration.Key)
	}
	if registration.CreatedAt.Add(time.Duration(config.Config.Topology.Hostname.ResolveExpiryMinutes) * time.Minute).Before(time.Now()) {
		// already expired.
		return nil
	}
	return WriteHostnameUnresolve(&registration.Key, registration.Hostname)
}

func extractIPs(ips []net.IP) (ipv4String string, ipv6String string) {
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			ipv4String = ip.String()
		} else {
			ipv6String = ip.String()
		}
	}
	return ipv4String, ipv6String
}

func getHostnameIPs(hostname string) (ips []net.IP, fromCache bool, err error) {
	if ips, found := hostnameIPsCache.Get(hostname); found {
		return ips.([]net.IP), true, nil
	}
	ips, err = net.LookupIP(hostname)
	if err != nil {
		return ips, false, log.Errore(err)
	}
	hostnameIPsCache.Set(hostname, ips, cache.DefaultExpiration)
	return ips, false, nil
}

func getHostnameIP(hostname string) (ipString string, err error) {
	ips, _, err := getHostnameIPs(hostname)
	if err != nil {
		return ipString, err
	}
	ipv4String, ipv6String := extractIPs(ips)
	if ipv4String != "" {
		return ipv4String, nil
	}
	return ipv6String, nil
}

func ResolveHostnameIPs(hostname string) error {
	ips, fromCache, err := getHostnameIPs(hostname)
	if err != nil {
		return err
	}
	if fromCache {
		return nil
	}
	ipv4String, ipv6String := extractIPs(ips)
	return writeHostnameIPs(hostname, ipv4String, ipv6String)
}
