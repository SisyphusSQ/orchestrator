package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/models/domain"
	"gorm.io/gorm"

	"github.com/openark/orchestrator/internal/observability"
)

// ErrDatabaseRuntimeClosed indicates that process shutdown has already closed
// the runtime-owned database pools.
var ErrDatabaseRuntimeClosed = errors.New("database runtime is closed")

type topologyConnectionRole string

const (
	topologyConnectionDiscovery topologyConnectionRole = "discovery"
	topologyConnectionOperation topologyConnectionRole = "operation"
)

func newOrchestratorMySQLConfig(database string) *mysql.Config {
	cfg := mysql.NewConfig()
	cfg.User = config.Config.Metadata.MySQL.User
	cfg.Passwd = config.Config.Metadata.MySQL.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(config.Config.Metadata.MySQL.Host, strconv.FormatUint(uint64(config.Config.Metadata.MySQL.Port), 10))
	cfg.DBName = database
	cfg.Timeout = time.Duration(config.Config.MySQL.ConnectTimeoutSeconds) * time.Second
	cfg.ReadTimeout = time.Duration(config.Config.Metadata.MySQL.ReadTimeoutSeconds) * time.Second
	cfg.InterpolateParams = true
	cfg.RejectReadOnly = database != "" && config.Config.Metadata.MySQL.RejectReadOnly
	if config.Config.Metadata.MySQL.MaxAllowedPacket >= 0 {
		cfg.MaxAllowedPacket = int(config.Config.Metadata.MySQL.MaxAllowedPacket)
	}
	return cfg
}

func newTopologyMySQLConfig(host string, port int, readTimeout time.Duration) *mysql.Config {
	cfg := mysql.NewConfig()
	cfg.User = config.Config.Topology.MySQL.User
	cfg.Passwd = config.Config.Topology.MySQL.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, strconv.Itoa(port))
	cfg.Timeout = time.Duration(config.Config.MySQL.ConnectTimeoutSeconds) * time.Second
	cfg.ReadTimeout = readTimeout
	cfg.InterpolateParams = true
	if config.Config.Topology.MySQL.MaxAllowedPacket >= 0 {
		cfg.MaxAllowedPacket = int(config.Config.Topology.MySQL.MaxAllowedPacket)
	}
	return cfg
}

type topologyPoolKey struct {
	role        topologyConnectionRole
	fingerprint [sha256.Size]byte
}

func newTopologyPoolKey(role topologyConnectionRole, cfg *mysql.Config) topologyPoolKey {
	identity := stringsForHash(
		cfg.User,
		cfg.Passwd,
		cfg.Net,
		cfg.Addr,
		cfg.DBName,
		cfg.TLSConfig,
		cfg.Timeout.String(),
		cfg.ReadTimeout.String(),
		cfg.WriteTimeout.String(),
		strconv.Itoa(cfg.MaxAllowedPacket),
		strconv.FormatBool(cfg.InterpolateParams),
		strconv.FormatBool(cfg.RejectReadOnly),
	)
	return topologyPoolKey{
		role:        role,
		fingerprint: sha256.Sum256(identity),
	}
}

func stringsForHash(values ...string) []byte {
	length := 0
	for _, value := range values {
		length += len(value) + 1
	}
	result := make([]byte, 0, length)
	for _, value := range values {
		result = append(result, value...)
		result = append(result, 0)
	}
	return result
}

type poolRegistry[K comparable] struct {
	mu             sync.Mutex
	closed         bool
	pools          map[K]*sql.DB
	initializing   map[K]*poolInitialization
	initialization sync.WaitGroup
	closePool      func(*sql.DB) error
}

type poolInitialization struct {
	done     chan struct{}
	database *sql.DB
	err      error
}

func newPoolRegistry[K comparable]() *poolRegistry[K] {
	return newPoolRegistryWithCloser[K](func(database *sql.DB) error {
		return database.Close()
	})
}

func newPoolRegistryWithCloser[K comparable](closePool func(*sql.DB) error) *poolRegistry[K] {
	return &poolRegistry[K]{
		pools:        make(map[K]*sql.DB),
		initializing: make(map[K]*poolInitialization),
		closePool:    closePool,
	}
}

func (registry *poolRegistry[K]) GetOrCreate(
	ctx context.Context,
	key K,
	open func() (*sql.DB, error),
) (*sql.DB, bool, error) {
	return registry.getOrCreate(ctx, key, open, func(ctx context.Context, database *sql.DB) error {
		return database.PingContext(ctx)
	})
}

func (registry *poolRegistry[K]) getOrCreate(
	ctx context.Context,
	key K,
	open func() (*sql.DB, error),
	initialize func(context.Context, *sql.DB) error,
) (*sql.DB, bool, error) {
	if ctx == nil {
		return nil, false, errors.New("database pool context is nil")
	}
	registry.mu.Lock()
	if registry.closed {
		registry.mu.Unlock()
		return nil, false, ErrDatabaseRuntimeClosed
	}
	if database, ok := registry.pools[key]; ok {
		registry.mu.Unlock()
		return database, true, nil
	}
	if pending, ok := registry.initializing[key]; ok {
		registry.mu.Unlock()
		select {
		case <-pending.done:
			return pending.database, pending.database != nil, pending.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	pending := &poolInitialization{done: make(chan struct{})}
	registry.initializing[key] = pending
	registry.initialization.Add(1)
	registry.mu.Unlock()

	database, err := open()
	if err != nil {
		if database != nil {
			err = errors.Join(err, registry.closePool(database))
			database = nil
		}
	} else if database == nil {
		err = errors.New("database pool opener returned nil")
	} else if initializeErr := initialize(ctx, database); initializeErr != nil {
		err = errors.Join(
			fmt.Errorf("validate database pool: %w", initializeErr),
			registry.closePool(database),
		)
		database = nil
	}

	registry.mu.Lock()
	closed := registry.closed
	if err == nil && !closed {
		registry.pools[key] = database
	}
	delete(registry.initializing, key)
	registry.mu.Unlock()

	if err == nil && closed {
		err = errors.Join(ErrDatabaseRuntimeClosed, registry.closePool(database))
		database = nil
	}
	pending.database = database
	pending.err = err
	close(pending.done)
	registry.initialization.Done()
	return database, false, err
}

func (registry *poolRegistry[K]) Close() error {
	registry.mu.Lock()
	if registry.closed {
		registry.mu.Unlock()
		return nil
	}
	registry.closed = true
	pools := registry.pools
	registry.pools = nil
	registry.mu.Unlock()
	registry.initialization.Wait()

	errs := make([]error, 0, len(pools))
	for _, database := range pools {
		errs = append(errs, registry.closePool(database))
	}
	return errors.Join(errs...)
}

type mysqlPoolOpener func(*mysql.Config) (*sql.DB, error)
type sqlitePoolOpener func(string) (*sql.DB, error)

type databaseRuntime struct {
	backend    *poolRegistry[string]
	topology   *poolRegistry[topologyPoolKey]
	openMySQL  mysqlPoolOpener
	openSQLite sqlitePoolOpener
	gormMu     sync.Mutex
	gorm       *gorm.DB
}

func newDatabaseRuntime() *databaseRuntime {
	return &databaseRuntime{
		backend:   newPoolRegistry[string](),
		topology:  newPoolRegistry[topologyPoolKey](),
		openMySQL: openMySQLConnectorPool,
		openSQLite: func(dataSourceName string) (*sql.DB, error) {
			return sql.Open("sqlite3", dataSourceName)
		},
	}
}

var processDatabaseRuntime = newDatabaseRuntime()

func openMySQLConnectorPool(cfg *mysql.Config) (*sql.DB, error) {
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("create MySQL connector: %w", err)
	}
	return sql.OpenDB(connector), nil
}

func (runtime *databaseRuntime) openBackend(ctx context.Context) (*sql.DB, error) {
	database, cached, err := runtime.backend.getOrCreate(
		ctx,
		"orchestrator",
		func() (*sql.DB, error) {
			if IsSQLite() {
				database, err := runtime.openSQLite(config.Config.Metadata.SQLite.DataFile)
				if err != nil {
					return nil, fmt.Errorf("open SQLite backend: %w", err)
				}
				database.SetMaxOpenConns(1)
				database.SetMaxIdleConns(1)
				return database, nil
			}
			if err := runtime.ensureMySQLBackendDatabase(ctx); err != nil {
				return nil, err
			}
			cfg := newOrchestratorMySQLConfig(config.Config.Metadata.MySQL.Database)
			if err := configureOrchestratorTLS(cfg); err != nil {
				return nil, err
			}
			database, err := runtime.openMySQL(cfg)
			if err != nil {
				return nil, err
			}
			configureBackendPool(database)
			return database, nil
		},
		func(ctx context.Context, database *sql.DB) error {
			if err := database.PingContext(ctx); err != nil {
				return fmt.Errorf("ping orchestrator backend: %w", err)
			}
			if config.Config.Metadata.Schema.SkipUpdate {
				return nil
			}
			if err := initOrchestratorDBContext(ctx, database); err != nil {
				return fmt.Errorf("initialize orchestrator backend: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	if !cached {
		if IsSQLite() {
			log.Debugf("Connected to orchestrator backend: sqlite on %v", config.Config.Metadata.SQLite.DataFile)
		} else {
			log.Debugf("Connected to orchestrator backend: mysql on %s:%d/%s",
				config.Config.Metadata.MySQL.Host,
				config.Config.Metadata.MySQL.Port,
				config.Config.Metadata.MySQL.Database)
			maxIdleConns := max(config.Config.Metadata.MySQL.MaxPoolConnections*25/100, 10)
			log.Infof("Connecting to backend %s:%d: maxConnections: %d, maxIdleConns: %d",
				config.Config.Metadata.MySQL.Host,
				config.Config.Metadata.MySQL.Port,
				config.Config.Metadata.MySQL.MaxPoolConnections,
				maxIdleConns)
		}
	}
	observability.SetBackendStatsProvider(func() domain.DatabasePoolStats {
		stats := database.Stats()
		return domain.DatabasePoolStats{
			InUse:              stats.InUse,
			Idle:               stats.Idle,
			MaxOpenConnections: stats.MaxOpenConnections,
			WaitCount:          stats.WaitCount,
			WaitDuration:       stats.WaitDuration,
		}
	})
	return database, nil
}

func (runtime *databaseRuntime) openBackendGORM(ctx context.Context) (*gorm.DB, error) {
	runtime.gormMu.Lock()
	defer runtime.gormMu.Unlock()
	database, err := runtime.openBackend(ctx)
	if err != nil {
		return nil, err
	}
	if runtime.gorm == nil {
		runtime.gorm, err = newBackendGORM(database, IsSQLite())
		if err != nil {
			return nil, err
		}
	}
	return runtime.gorm.WithContext(ctx), nil
}

func (runtime *databaseRuntime) ensureMySQLBackendDatabase(ctx context.Context) (returnErr error) {
	cfg := newOrchestratorMySQLConfig("")
	if err := configureOrchestratorTLS(cfg); err != nil {
		return err
	}
	database, err := runtime.openMySQL(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := database.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close orchestrator bootstrap pool: %w", err))
		}
	}()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("ping orchestrator bootstrap connection: %w", err)
	}
	databaseName := "`" + strings.ReplaceAll(config.Config.Metadata.MySQL.Database, "`", "``") + "`"
	query := fmt.Sprintf(
		"create database if not exists %s default character set utf8mb4 collate utf8mb4_general_ci",
		databaseName,
	)
	if _, err := database.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("create orchestrator database: %w", err)
	}
	return nil
}

func configureBackendPool(database *sql.DB) {
	if config.Config.Metadata.MySQL.MaxPoolConnections > 0 {
		database.SetMaxOpenConns(config.Config.Metadata.MySQL.MaxPoolConnections)
	}
	if config.Config.MySQL.ConnectionLifetimeSeconds > 0 {
		database.SetConnMaxLifetime(time.Duration(config.Config.MySQL.ConnectionLifetimeSeconds) * time.Second)
	}
	maxIdleConns := max(config.Config.Metadata.MySQL.MaxPoolConnections*25/100, 10)
	database.SetMaxIdleConns(maxIdleConns)
}

func (runtime *databaseRuntime) openTopologyPool(
	ctx context.Context,
	role topologyConnectionRole,
	cfg *mysql.Config,
) (*sql.DB, error) {
	key := newTopologyPoolKey(role, cfg)
	database, _, err := runtime.topology.GetOrCreate(ctx, key, func() (*sql.DB, error) {
		database, err := runtime.openMySQL(cfg)
		if err != nil {
			return nil, err
		}
		if config.Config.MySQL.ConnectionLifetimeSeconds > 0 {
			database.SetConnMaxLifetime(time.Duration(config.Config.MySQL.ConnectionLifetimeSeconds) * time.Second)
		}
		database.SetMaxOpenConns(config.MySQLTopologyMaxPoolConnections)
		database.SetMaxIdleConns(config.MySQLTopologyMaxPoolConnections)
		return database, nil
	})
	if err != nil {
		return nil, fmt.Errorf("open %s topology connection to %s: %w", role, cfg.Addr, err)
	}
	return database, nil
}

// OpenOrchestratorContext returns the process-owned backend pool after it has
// been connected and, when enabled, its schema has been initialized.
func OpenOrchestratorContext(ctx context.Context) (*sql.DB, error) {
	return processDatabaseRuntime.openBackend(ctx)
}

// Close closes all runtime-owned backend and topology pools. It is safe to call
// more than once during process shutdown.
func Close() error {
	return processDatabaseRuntime.Close()
}

func (runtime *databaseRuntime) Close() error {
	runtime.gormMu.Lock()
	defer runtime.gormMu.Unlock()
	runtime.gorm = nil
	var errs []error
	if err := runtime.topology.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close topology pools: %w", err))
	}
	if err := runtime.backend.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close backend pool: %w", err))
	}
	return errors.Join(errs...)
}
