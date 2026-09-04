package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/openark/orchestrator/go/config"
)

func preserveMySQLConfig(t *testing.T) {
	t.Helper()
	previousOrchestratorUser := config.Config.MySQLOrchestratorUser
	previousOrchestratorPassword := config.Config.MySQLOrchestratorPassword
	previousOrchestratorHost := config.Config.MySQLOrchestratorHost
	previousOrchestratorPort := config.Config.MySQLOrchestratorPort
	previousOrchestratorDatabase := config.Config.MySQLOrchestratorDatabase
	previousOrchestratorReadTimeout := config.Config.MySQLOrchestratorReadTimeoutSeconds
	previousOrchestratorRejectReadOnly := config.Config.MySQLOrchestratorRejectReadOnly
	previousOrchestratorMaxAllowedPacket := config.Config.MySQLOrchestratorMaxAllowedPacket
	previousTopologyUser := config.Config.MySQLTopologyUser
	previousTopologyPassword := config.Config.MySQLTopologyPassword
	previousTopologyMaxAllowedPacket := config.Config.MySQLTopologyMaxAllowedPacket
	previousConnectTimeout := config.Config.MySQLConnectTimeoutSeconds
	t.Cleanup(func() {
		config.Config.MySQLOrchestratorUser = previousOrchestratorUser
		config.Config.MySQLOrchestratorPassword = previousOrchestratorPassword
		config.Config.MySQLOrchestratorHost = previousOrchestratorHost
		config.Config.MySQLOrchestratorPort = previousOrchestratorPort
		config.Config.MySQLOrchestratorDatabase = previousOrchestratorDatabase
		config.Config.MySQLOrchestratorReadTimeoutSeconds = previousOrchestratorReadTimeout
		config.Config.MySQLOrchestratorRejectReadOnly = previousOrchestratorRejectReadOnly
		config.Config.MySQLOrchestratorMaxAllowedPacket = previousOrchestratorMaxAllowedPacket
		config.Config.MySQLTopologyUser = previousTopologyUser
		config.Config.MySQLTopologyPassword = previousTopologyPassword
		config.Config.MySQLTopologyMaxAllowedPacket = previousTopologyMaxAllowedPacket
		config.Config.MySQLConnectTimeoutSeconds = previousConnectTimeout
	})
}

func TestNewOrchestratorMySQLConfigMapsApprovedFields(t *testing.T) {
	preserveMySQLConfig(t)
	config.Config.MySQLOrchestratorUser = "backend-user"
	config.Config.MySQLOrchestratorPassword = "backend-secret"
	config.Config.MySQLOrchestratorHost = "backend.example"
	config.Config.MySQLOrchestratorPort = 3307
	config.Config.MySQLOrchestratorDatabase = "orchestrator"
	config.Config.MySQLConnectTimeoutSeconds = 3
	config.Config.MySQLOrchestratorReadTimeoutSeconds = 7
	config.Config.MySQLOrchestratorRejectReadOnly = true
	config.Config.MySQLOrchestratorMaxAllowedPacket = 123456

	backend := newOrchestratorMySQLConfig(config.Config.MySQLOrchestratorDatabase)
	if backend.User != "backend-user" || backend.Passwd != "backend-secret" {
		t.Fatal("backend config did not preserve the configured credentials")
	}
	if backend.Net != "tcp" || backend.Addr != "backend.example:3307" || backend.DBName != "orchestrator" {
		t.Fatalf("backend endpoint = %s/%s/%s; want tcp/backend.example:3307/orchestrator", backend.Net, backend.Addr, backend.DBName)
	}
	if backend.Timeout != 3*time.Second || backend.ReadTimeout != 7*time.Second {
		t.Fatalf("backend timeouts = %s/%s; want 3s/7s", backend.Timeout, backend.ReadTimeout)
	}
	if !backend.RejectReadOnly || !backend.InterpolateParams {
		t.Fatal("backend config did not preserve rejectReadOnly and interpolateParams")
	}
	if backend.MaxAllowedPacket != 123456 {
		t.Fatalf("backend maxAllowedPacket = %d; want 123456", backend.MaxAllowedPacket)
	}

	bootstrap := newOrchestratorMySQLConfig("")
	if bootstrap.DBName != "" {
		t.Fatalf("bootstrap database = %q; want empty", bootstrap.DBName)
	}
	if bootstrap.RejectReadOnly {
		t.Fatal("bootstrap connection unexpectedly rejects a read-only server")
	}
}

func TestNewTopologyMySQLConfigUsesTopologyPacketLimit(t *testing.T) {
	preserveMySQLConfig(t)
	config.Config.MySQLTopologyUser = "topology-user"
	config.Config.MySQLTopologyPassword = "topology-secret"
	config.Config.MySQLConnectTimeoutSeconds = 5
	config.Config.MySQLTopologyMaxAllowedPacket = 654321
	config.Config.MySQLOrchestratorMaxAllowedPacket = 111111

	topology := newTopologyMySQLConfig("mysql.example", 3310, 11*time.Second)
	if topology.User != "topology-user" || topology.Passwd != "topology-secret" {
		t.Fatal("topology config did not preserve the configured credentials")
	}
	if topology.Net != "tcp" || topology.Addr != "mysql.example:3310" || topology.DBName != "" {
		t.Fatalf("topology endpoint = %s/%s/%s; want tcp/mysql.example:3310/empty", topology.Net, topology.Addr, topology.DBName)
	}
	if topology.Timeout != 5*time.Second || topology.ReadTimeout != 11*time.Second {
		t.Fatalf("topology timeouts = %s/%s; want 5s/11s", topology.Timeout, topology.ReadTimeout)
	}
	if topology.MaxAllowedPacket != 654321 {
		t.Fatalf("topology maxAllowedPacket = %d; want 654321", topology.MaxAllowedPacket)
	}
}

func TestTopologyPoolKeySeparatesConnectionBehavior(t *testing.T) {
	preserveMySQLConfig(t)
	config.Config.MySQLTopologyUser = "topology-user"
	config.Config.MySQLTopologyPassword = "first-secret"
	firstConfig := newTopologyMySQLConfig("mysql.example", 3306, 2*time.Second)

	discoveryKey := newTopologyPoolKey(topologyConnectionDiscovery, firstConfig)
	operationKey := newTopologyPoolKey(topologyConnectionOperation, firstConfig)
	if discoveryKey == operationKey {
		t.Fatal("discovery and operation produced the same pool key")
	}

	config.Config.MySQLTopologyPassword = "second-secret"
	secondConfig := newTopologyMySQLConfig("mysql.example", 3306, 2*time.Second)
	secondKey := newTopologyPoolKey(topologyConnectionDiscovery, secondConfig)
	if discoveryKey == secondKey {
		t.Fatal("credential change did not change the topology pool key")
	}
	if rendered := fmt.Sprintf("%+v", discoveryKey); strings.Contains(rendered, "first-secret") {
		t.Fatal("topology pool key exposes the configured password")
	}
}

func TestPoolRegistryPublishesOnePoolForConcurrentInitialization(t *testing.T) {
	registry := newPoolRegistry[string]()
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Errorf("close pool registry: %v", err)
		}
	})

	var opens atomic.Int32
	opener := func() (*sql.DB, error) {
		opens.Add(1)
		return sql.Open("sqlite3", ":memory:")
	}

	const callers = 16
	results := make(chan *sql.DB, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			database, _, err := registry.GetOrCreate(context.Background(), "backend", opener)
			results <- database
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent GetOrCreate() error: %v", err)
		}
	}
	var first *sql.DB
	for database := range results {
		if first == nil {
			first = database
			continue
		}
		if database != first {
			t.Fatal("concurrent initialization published more than one pool")
		}
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("pool opener calls = %d; want 1", got)
	}
}

func TestPoolRegistryInitializesDifferentKeysConcurrently(t *testing.T) {
	registry := newPoolRegistry[string]()
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Errorf("close pool registry: %v", err)
		}
	})
	started := make(chan string, 2)
	release := make(chan struct{})
	errs := make(chan error, 2)

	for _, key := range []string{"first", "second"} {
		go func(key string) {
			_, _, err := registry.GetOrCreate(context.Background(), key, func() (*sql.DB, error) {
				started <- key
				<-release
				return sql.Open("sqlite3", ":memory:")
			})
			errs <- err
		}(key)
	}

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("different pool keys were initialized serially")
		}
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("GetOrCreate() error: %v", err)
		}
	}
}

func TestPoolRegistryDoesNotCacheFailedInitialization(t *testing.T) {
	registry := newPoolRegistry[string]()
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Errorf("close pool registry: %v", err)
		}
	})
	expectedErr := errors.New("open unavailable")
	calls := 0
	opener := func() (*sql.DB, error) {
		calls++
		if calls == 1 {
			return nil, expectedErr
		}
		return sql.Open("sqlite3", ":memory:")
	}

	if _, _, err := registry.GetOrCreate(context.Background(), "backend", opener); !errors.Is(err, expectedErr) {
		t.Fatalf("first GetOrCreate() error = %v; want %v", err, expectedErr)
	}
	database, cached, err := registry.GetOrCreate(context.Background(), "backend", opener)
	if err != nil {
		t.Fatalf("second GetOrCreate() error: %v", err)
	}
	if cached {
		t.Fatal("successful retry was reported as cached")
	}
	if database == nil || calls != 2 {
		t.Fatalf("successful retry database/calls = %v/%d; want non-nil/2", database != nil, calls)
	}
}

func TestPoolRegistryPropagatesCanceledContextAndCloses(t *testing.T) {
	registry := newPoolRegistry[string]()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := registry.GetOrCreate(ctx, "backend", func() (*sql.DB, error) {
		return sql.Open("sqlite3", ":memory:")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrCreate() error = %v; want context.Canceled", err)
	}

	database, _, err := registry.GetOrCreate(context.Background(), "backend", func() (*sql.DB, error) {
		return sql.Open("sqlite3", ":memory:")
	})
	if err != nil {
		t.Fatalf("retry GetOrCreate() error: %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := database.PingContext(context.Background()); err == nil {
		t.Fatal("published pool remained usable after Close()")
	}
	if _, _, err := registry.GetOrCreate(context.Background(), "other", func() (*sql.DB, error) {
		return sql.Open("sqlite3", ":memory:")
	}); !errors.Is(err, ErrDatabaseRuntimeClosed) {
		t.Fatalf("GetOrCreate() after Close() error = %v; want ErrDatabaseRuntimeClosed", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestOpenOrchestratorContextReusesAndClosesRuntimeOwnedSQLitePool(t *testing.T) {
	previousBackendDB := config.Config.BackendDB
	previousSQLiteFile := config.Config.SQLite3DataFile
	previousSkipUpdate := config.Config.SkipOrchestratorDatabaseUpdate
	previousRuntime := processDatabaseRuntime
	config.Config.BackendDB = "sqlite3"
	config.Config.SQLite3DataFile = ":memory:"
	config.Config.SkipOrchestratorDatabaseUpdate = true
	processDatabaseRuntime = newDatabaseRuntime()
	t.Cleanup(func() {
		_ = processDatabaseRuntime.Close()
		processDatabaseRuntime = previousRuntime
		config.Config.BackendDB = previousBackendDB
		config.Config.SQLite3DataFile = previousSQLiteFile
		config.Config.SkipOrchestratorDatabaseUpdate = previousSkipUpdate
	})

	first, err := OpenOrchestratorContext(context.Background())
	if err != nil {
		t.Fatalf("first OpenOrchestratorContext() error: %v", err)
	}
	second, err := OpenOrchestratorContext(context.Background())
	if err != nil {
		t.Fatalf("second OpenOrchestratorContext() error: %v", err)
	}
	if first != second {
		t.Fatal("OpenOrchestratorContext() published more than one backend pool")
	}
	if got := first.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("SQLite MaxOpenConnections = %d; want 1", got)
	}

	if err := Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := first.PingContext(context.Background()); err == nil {
		t.Fatal("backend pool remained usable after Close()")
	}
	if _, err := OpenOrchestratorContext(context.Background()); !errors.Is(err, ErrDatabaseRuntimeClosed) {
		t.Fatalf("OpenOrchestratorContext() after Close() error = %v; want ErrDatabaseRuntimeClosed", err)
	}
}

func TestOpenOrchestratorContextInitializesSQLiteSchema(t *testing.T) {
	previousBackendDB := config.Config.BackendDB
	previousSQLiteFile := config.Config.SQLite3DataFile
	previousSkipUpdate := config.Config.SkipOrchestratorDatabaseUpdate
	previousPanicIfDifferent := config.Config.PanicIfDifferentDatabaseDeploy
	previousConfiguredVersion := config.RuntimeCLIFlags.ConfiguredVersion
	previousRuntime := processDatabaseRuntime
	config.Config.BackendDB = "sqlite3"
	config.Config.SQLite3DataFile = ":memory:"
	config.Config.SkipOrchestratorDatabaseUpdate = false
	config.Config.PanicIfDifferentDatabaseDeploy = false
	config.RuntimeCLIFlags.ConfiguredVersion = "runtime-test"
	processDatabaseRuntime = newDatabaseRuntime()
	t.Cleanup(func() {
		_ = processDatabaseRuntime.Close()
		processDatabaseRuntime = previousRuntime
		config.Config.BackendDB = previousBackendDB
		config.Config.SQLite3DataFile = previousSQLiteFile
		config.Config.SkipOrchestratorDatabaseUpdate = previousSkipUpdate
		config.Config.PanicIfDifferentDatabaseDeploy = previousPanicIfDifferent
		config.RuntimeCLIFlags.ConfiguredVersion = previousConfiguredVersion
	})

	database, err := OpenOrchestratorContext(context.Background())
	if err != nil {
		t.Fatalf("OpenOrchestratorContext() error: %v", err)
	}
	var deployedVersion string
	if err := database.QueryRowContext(
		context.Background(),
		"select deployed_version from orchestrator_db_deployments where deployed_version = ?",
		config.RuntimeCLIFlags.ConfiguredVersion,
	).Scan(&deployedVersion); err != nil {
		t.Fatalf("read initialized deployment metadata: %v", err)
	}
	if deployedVersion != config.RuntimeCLIFlags.ConfiguredVersion {
		t.Fatalf("deployed version = %q; want %q", deployedVersion, config.RuntimeCLIFlags.ConfiguredVersion)
	}
}

func TestDatabaseRuntimeSeparatesAndClosesTopologyPools(t *testing.T) {
	preserveMySQLConfig(t)
	config.Config.MySQLTopologyUser = "topology-user"
	config.Config.MySQLTopologyPassword = "first-secret"
	runtime := newDatabaseRuntime()
	runtime.openMySQL = func(*mysql.Config) (*sql.DB, error) {
		return sql.Open("sqlite3", ":memory:")
	}

	firstConfig := newTopologyMySQLConfig("mysql.example", 3306, 2*time.Second)
	discovery, err := runtime.openTopologyPool(context.Background(), topologyConnectionDiscovery, firstConfig)
	if err != nil {
		t.Fatalf("open discovery pool: %v", err)
	}
	reused, err := runtime.openTopologyPool(context.Background(), topologyConnectionDiscovery, firstConfig)
	if err != nil {
		t.Fatalf("reuse discovery pool: %v", err)
	}
	if discovery != reused {
		t.Fatal("identical discovery configuration did not reuse its pool")
	}

	operation, err := runtime.openTopologyPool(context.Background(), topologyConnectionOperation, firstConfig)
	if err != nil {
		t.Fatalf("open operation pool: %v", err)
	}
	if operation == discovery {
		t.Fatal("discovery and operation reused the same topology pool")
	}

	secondConfig := firstConfig.Clone()
	secondConfig.Passwd = "second-secret"
	rotated, err := runtime.openTopologyPool(context.Background(), topologyConnectionDiscovery, secondConfig)
	if err != nil {
		t.Fatalf("open pool after credential rotation: %v", err)
	}
	if rotated == discovery {
		t.Fatal("credential rotation reused the previous topology pool")
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("close database runtime: %v", err)
	}
	for name, database := range map[string]*sql.DB{
		"discovery": discovery,
		"operation": operation,
		"rotated":   rotated,
	} {
		if err := database.PingContext(context.Background()); err == nil {
			t.Fatalf("%s pool remained usable after runtime close", name)
		}
	}
}

func TestOpenTopologyContextUsesRoleSpecificRuntimePools(t *testing.T) {
	preserveMySQLConfig(t)
	previousMutualTLS := config.Config.MySQLTopologyUseMutualTLS
	previousMixedTLS := config.Config.MySQLTopologyUseMixedTLS
	previousDiscoveryTimeout := config.Config.MySQLDiscoveryReadTimeoutSeconds
	previousTopologyTimeout := config.Config.MySQLTopologyReadTimeoutSeconds
	previousRuntime := processDatabaseRuntime
	config.Config.MySQLTopologyUser = "topology-user"
	config.Config.MySQLTopologyPassword = "topology-secret"
	config.Config.MySQLTopologyUseMutualTLS = false
	config.Config.MySQLTopologyUseMixedTLS = false
	config.Config.MySQLDiscoveryReadTimeoutSeconds = 2
	config.Config.MySQLTopologyReadTimeoutSeconds = 2
	processDatabaseRuntime = newDatabaseRuntime()
	processDatabaseRuntime.openMySQL = func(*mysql.Config) (*sql.DB, error) {
		return sql.Open("sqlite3", ":memory:")
	}
	t.Cleanup(func() {
		_ = processDatabaseRuntime.Close()
		processDatabaseRuntime = previousRuntime
		config.Config.MySQLTopologyUseMutualTLS = previousMutualTLS
		config.Config.MySQLTopologyUseMixedTLS = previousMixedTLS
		config.Config.MySQLDiscoveryReadTimeoutSeconds = previousDiscoveryTimeout
		config.Config.MySQLTopologyReadTimeoutSeconds = previousTopologyTimeout
	})

	discovery, err := OpenDiscoveryContext(context.Background(), "mysql.example", 3306)
	if err != nil {
		t.Fatalf("OpenDiscoveryContext() error: %v", err)
	}
	reused, err := OpenDiscoveryContext(context.Background(), "mysql.example", 3306)
	if err != nil {
		t.Fatalf("second OpenDiscoveryContext() error: %v", err)
	}
	if discovery != reused {
		t.Fatal("OpenDiscoveryContext() did not reuse the matching pool")
	}
	operation, err := OpenTopologyContext(context.Background(), "mysql.example", 3306)
	if err != nil {
		t.Fatalf("OpenTopologyContext() error: %v", err)
	}
	if operation == discovery {
		t.Fatal("OpenTopologyContext() reused the discovery pool when timeouts matched")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenDiscoveryContext(canceled, "other.example", 3306); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenDiscoveryContext() canceled error = %v; want context.Canceled", err)
	}
}

func TestProbeTLSRequirementClosesTemporaryPool(t *testing.T) {
	runtime := newDatabaseRuntime()
	var probe *sql.DB
	runtime.openMySQL = func(*mysql.Config) (*sql.DB, error) {
		probe, _ = sql.Open("sqlite3", ":memory:")
		return probe, nil
	}

	required, conclusive, err := runtime.probeTLSRequirement(context.Background(), mysql.NewConfig())
	if err != nil {
		t.Fatalf("probeTLSRequirement() error: %v", err)
	}
	if required || !conclusive {
		t.Fatalf("probeTLSRequirement() required/conclusive = %t/%t; want false/true", required, conclusive)
	}
	if probe == nil {
		t.Fatal("probeTLSRequirement() did not open a temporary pool")
	}
	if err := probe.PingContext(context.Background()); err == nil {
		t.Fatal("probeTLSRequirement() left its temporary pool open")
	}
}

func TestProbeTLSRequirementPropagatesCancellationAndCloses(t *testing.T) {
	runtime := newDatabaseRuntime()
	var probe *sql.DB
	runtime.openMySQL = func(*mysql.Config) (*sql.DB, error) {
		probe, _ = sql.Open("sqlite3", ":memory:")
		return probe, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := runtime.probeTLSRequirement(ctx, mysql.NewConfig())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("probeTLSRequirement() error = %v; want context.Canceled", err)
	}
	if probe == nil {
		t.Fatal("probeTLSRequirement() did not open a temporary pool")
	}
	if err := probe.PingContext(context.Background()); err == nil {
		t.Fatal("probeTLSRequirement() left its canceled temporary pool open")
	}
}
