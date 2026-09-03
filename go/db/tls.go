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

package db

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/openark/golib/log"
	"github.com/patrickmn/go-cache"
	"github.com/rcrowley/go-metrics"

	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/ssl"
)

const Error3159 = "Error 3159:"
const Error1045 = "Access denied for user"

// Track if a TLS has already been configured for topology
var topologyTLSConfigured bool = false

// Track if a TLS has already been configured for Orchestrator
var orchestratorTLSConfigured bool = false
var mysqlTLSConfigMutex sync.Mutex

const (
	topologyTLSConfigName     = "topology"
	orchestratorTLSConfigName = "orchestrator"
)

var requireTLSCache *cache.Cache = cache.New(time.Duration(config.Config.TLSCacheTTLFactor*config.Config.InstancePollSeconds)*time.Second, time.Second)

var readInstanceTLSCounter = metrics.NewCounter()
var writeInstanceTLSCounter = metrics.NewCounter()
var readInstanceTLSCacheCounter = metrics.NewCounter()
var writeInstanceTLSCacheCounter = metrics.NewCounter()

func init() {
	metrics.Register("instance_tls.read", readInstanceTLSCounter)
	metrics.Register("instance_tls.write", writeInstanceTLSCounter)
	metrics.Register("instance_tls.read_cache", readInstanceTLSCacheCounter)
	metrics.Register("instance_tls.write_cache", writeInstanceTLSCacheCounter)
}

type SqlUtilsLogger struct {
	client_context     string
	backend_connection bool
}

func (logger SqlUtilsLogger) OnError(caller_context string, query string, err error) error {
	query = strings.Join(strings.Fields(query), " ") // trim whitespaces
	query = strings.Replace(query, "%", "%%", -1)    // escape %

	msg := fmt.Sprintf("%+v(%+v) %+v: %+v",
		caller_context,
		logger.client_context,
		query,
		err)

	return log.Errorf("%s", msg)
}

// This validator is for dev purposes only. Call of this validator is
// disabled in sqlutils.go
var query_whitelist = []string{
	"substring_index(host, ':', 1) as slave_hostname",
}

func (logger SqlUtilsLogger) ValidateQuery(query string) {
	if logger.backend_connection {
		return
	}

	// check if whitelisted
	for i := 0; i < len(query_whitelist); i++ {
		if strings.Contains(query, query_whitelist[i]) {
			return
		}
	}

	lquery := strings.ToLower(query)
	if strings.Contains(lquery, "master") || strings.Contains(lquery, "slave") {
		log.Error("QUERY CONTAINS MASTER / SLAVE: ")
		// panic("Query contains master/slave: " + query)
	}
}

func requiresTLSContext(ctx context.Context, host string, port int, cfg *mysql.Config) (bool, error) {
	poolKey := newTopologyPoolKey(topologyConnectionDiscovery, cfg)
	cacheKey := fmt.Sprintf("%s:%d:%x", host, port, poolKey.fingerprint)
	if value, found := requireTLSCache.Get(cacheKey); found {
		readInstanceTLSCacheCounter.Inc(1)
		return value.(bool), nil
	}

	required, conclusive, err := processDatabaseRuntime.probeTLSRequirement(ctx, cfg)
	if err != nil {
		return false, err
	}
	if !conclusive {
		return false, nil
	}

	query := `
			insert into
				database_instance_tls (
					hostname, port, required
				) values (
					?, ?, ?
				)
				on duplicate key update
					required=values(required)
				`
	if _, err := ExecOrchestratorContext(ctx, query, host, port, required); err != nil {
		log.Sugar().Warnw("persist topology TLS requirement failed", "host", host, "port", port, "error", err)
	}
	writeInstanceTLSCounter.Inc(1)
	requireTLSCache.Set(cacheKey, required, cache.DefaultExpiration)
	writeInstanceTLSCacheCounter.Inc(1)
	return required, nil
}

func (runtime *databaseRuntime) probeTLSRequirement(ctx context.Context, cfg *mysql.Config) (required bool, conclusive bool, err error) {
	database, err := runtime.openMySQL(cfg)
	if err != nil {
		return false, false, fmt.Errorf("create topology TLS probe connection: %w", err)
	}
	pingErr := database.PingContext(ctx)
	closeErr := database.Close()
	if pingErr == nil {
		if closeErr != nil {
			return false, false, fmt.Errorf("close topology TLS probe connection: %w", closeErr)
		}
		return false, true, nil
	}
	if errors.Is(pingErr, context.Canceled) || errors.Is(pingErr, context.DeadlineExceeded) {
		return false, false, errors.Join(pingErr, closeErr)
	}
	if strings.Contains(pingErr.Error(), Error3159) || strings.Contains(pingErr.Error(), Error1045) {
		return true, true, closeErr
	}
	if closeErr != nil {
		return false, false, fmt.Errorf("close topology TLS probe connection: %w", closeErr)
	}
	// The normal topology connection preserves the original non-TLS error.
	return false, false, nil
}

// Create a TLS configuration from the config supplied CA, Certificate, and Private key.
// Register the TLS config with the mysql drivers as the "topology" config
// Modify the supplied URI to call the TLS config
func SetupMySQLTopologyTLS(uri string) (string, error) {
	name, err := ensureMySQLTopologyTLSConfig()
	if err != nil {
		return "", err
	}
	separator := "?"
	if strings.Contains(uri, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%stls=%s", uri, separator, name), nil
}

// Create a TLS configuration from the config supplied CA, Certificate, and Private key.
// Register the TLS config with the mysql drivers as the "orchestrator" config
// Modify the supplied URI to call the TLS config
func SetupMySQLOrchestratorTLS(uri string) (string, error) {
	name, err := ensureMySQLOrchestratorTLSConfig()
	if err != nil {
		return "", err
	}
	separator := "?"
	if strings.Contains(uri, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%stls=%s", uri, separator, name), nil
}

func ensureMySQLTopologyTLSConfig() (string, error) {
	mysqlTLSConfigMutex.Lock()
	defer mysqlTLSConfigMutex.Unlock()
	if topologyTLSConfigured {
		return topologyTLSConfigName, nil
	}
	tlsConfig, err := ssl.NewTLSConfig(config.Config.MySQLTopologySSLCAFile, !config.Config.MySQLTopologySSLSkipVerify)
	if err != nil {
		return "", fmt.Errorf("create TLS configuration for topology connection: %w", err)
	}
	// Preserve compatibility with MySQL deployments that still negotiate TLS 1.0.
	tlsConfig.MinVersion = tls.VersionTLS10
	tlsConfig.InsecureSkipVerify = config.Config.MySQLTopologySSLSkipVerify
	if config.Config.MySQLTopologyUseMutualTLS && !config.Config.MySQLTopologySSLSkipVerify &&
		config.Config.MySQLTopologySSLCertFile != "" && config.Config.MySQLTopologySSLPrivateKeyFile != "" {
		if err := ssl.AppendKeyPair(tlsConfig, config.Config.MySQLTopologySSLCertFile, config.Config.MySQLTopologySSLPrivateKeyFile); err != nil {
			return "", fmt.Errorf("set up TLS key pair for topology connection: %w", err)
		}
	}
	if err := mysql.RegisterTLSConfig(topologyTLSConfigName, tlsConfig); err != nil {
		return "", fmt.Errorf("register MySQL TLS config for topology: %w", err)
	}
	topologyTLSConfigured = true
	return topologyTLSConfigName, nil
}

func ensureMySQLOrchestratorTLSConfig() (string, error) {
	mysqlTLSConfigMutex.Lock()
	defer mysqlTLSConfigMutex.Unlock()
	if orchestratorTLSConfigured {
		return orchestratorTLSConfigName, nil
	}
	tlsConfig, err := ssl.NewTLSConfig(config.Config.MySQLOrchestratorSSLCAFile, !config.Config.MySQLOrchestratorSSLSkipVerify)
	if err != nil {
		return "", fmt.Errorf("create TLS configuration for orchestrator connection: %w", err)
	}
	// Preserve compatibility with MySQL deployments that still negotiate TLS 1.0.
	tlsConfig.MinVersion = tls.VersionTLS10
	tlsConfig.InsecureSkipVerify = config.Config.MySQLOrchestratorSSLSkipVerify
	if !config.Config.MySQLOrchestratorSSLSkipVerify &&
		config.Config.MySQLOrchestratorSSLCertFile != "" && config.Config.MySQLOrchestratorSSLPrivateKeyFile != "" {
		if err := ssl.AppendKeyPair(tlsConfig, config.Config.MySQLOrchestratorSSLCertFile, config.Config.MySQLOrchestratorSSLPrivateKeyFile); err != nil {
			return "", fmt.Errorf("set up TLS key pair for orchestrator connection: %w", err)
		}
	}
	if err := mysql.RegisterTLSConfig(orchestratorTLSConfigName, tlsConfig); err != nil {
		return "", fmt.Errorf("register MySQL TLS config for orchestrator: %w", err)
	}
	orchestratorTLSConfigured = true
	return orchestratorTLSConfigName, nil
}

func configureOrchestratorTLS(cfg *mysql.Config) error {
	if !config.Config.MySQLOrchestratorUseMutualTLS {
		return nil
	}
	name, err := ensureMySQLOrchestratorTLSConfig()
	if err != nil {
		return err
	}
	cfg.TLSConfig = name
	return nil
}

func configureTopologyTLS(cfg *mysql.Config) error {
	name, err := ensureMySQLTopologyTLSConfig()
	if err != nil {
		return err
	}
	cfg.TLSConfig = name
	return nil
}
