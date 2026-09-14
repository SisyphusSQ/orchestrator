package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/openark/orchestrator/internal/config"
)

func TestIsDuplicateKeyError(t *testing.T) {
	if !IsDuplicateKeyError(&mysql.MySQLError{Number: 1062, Message: "duplicate"}) {
		t.Fatal("MySQL duplicate key error not recognized")
	}
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("CREATE TABLE unique_values (value TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO unique_values VALUES ('same')"); err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec("INSERT INTO unique_values VALUES ('same')")
	if !IsDuplicateKeyError(fmt.Errorf("wrapped: %w", err)) {
		t.Fatalf("SQLite duplicate key error not recognized: %v", err)
	}
}

func TestSetupMySQLOrchestratorTLSReturnsCAFileError(t *testing.T) {
	previousConfigured := orchestratorTLSConfigured
	previousPassword := config.Current().Metadata.MySQL.Password
	previousCAFile := config.Current().Metadata.MySQL.SSLCAFile
	previousSkipVerify := config.Current().Metadata.MySQL.SSLSkipVerify
	orchestratorTLSConfigured = false
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.Password = "password-must-not-appear-in-error" })
	config.TestUpdate(func(cfg *config.Configuration) {
		cfg.Metadata.MySQL.SSLCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	})
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.SSLSkipVerify = false })
	t.Cleanup(func() {
		orchestratorTLSConfigured = previousConfigured
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.Password = previousPassword })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.SSLCAFile = previousCAFile })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.SSLSkipVerify = previousSkipVerify })
	})

	_, err := SetupMySQLOrchestratorTLS("user@tcp(localhost:3306)/orchestrator")
	if err == nil {
		t.Fatal("SetupMySQLOrchestratorTLS() returned nil for a missing CA file")
	}
	if !strings.Contains(err.Error(), "missing-ca.pem") {
		t.Fatalf("SetupMySQLOrchestratorTLS() error = %q; want missing CA path", err)
	}
	if strings.Contains(err.Error(), config.Current().Metadata.MySQL.Password) {
		t.Fatalf("SetupMySQLOrchestratorTLS() error exposed the configured password: %q", err)
	}
}

func TestConfigureOrchestratorTLSReturnsConfigurationError(t *testing.T) {
	previousConfigured := orchestratorTLSConfigured
	previousUseMutualTLS := config.Current().Metadata.MySQL.UseMutualTLS
	previousCAFile := config.Current().Metadata.MySQL.SSLCAFile
	previousSkipVerify := config.Current().Metadata.MySQL.SSLSkipVerify
	orchestratorTLSConfigured = false
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.UseMutualTLS = true })
	config.TestUpdate(func(cfg *config.Configuration) {
		cfg.Metadata.MySQL.SSLCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	})
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.SSLSkipVerify = false })
	t.Cleanup(func() {
		orchestratorTLSConfigured = previousConfigured
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.UseMutualTLS = previousUseMutualTLS })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.SSLCAFile = previousCAFile })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.MySQL.SSLSkipVerify = previousSkipVerify })
	})

	cfg := newOrchestratorMySQLConfig(config.Current().Metadata.MySQL.Database)
	if err := configureOrchestratorTLS(cfg); err == nil {
		t.Fatal("configureOrchestratorTLS() returned nil for an invalid TLS configuration")
	}
	if cfg.TLSConfig != "" {
		t.Fatalf("configureOrchestratorTLS() set TLSConfig %q after configuration failure", cfg.TLSConfig)
	}
}

func TestDeployStatementsReturnsExecutionError(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close sqlite fixture: %v", err)
		}
	})
	previousBackendDB := config.Current().Metadata.Type
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.Type = "sqlite3" })
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.Type = previousBackendDB })
	})

	err = deployStatements(database, []string{"not valid SQL"})
	if err == nil {
		t.Fatal("deployStatements() returned nil for invalid SQL")
	}
	if !strings.Contains(err.Error(), "not valid SQL") {
		t.Fatalf("deployStatements() error = %q; want failed query", err)
	}
}

func TestDeployStatementsContextPropagatesCancellation(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close sqlite fixture: %v", err)
		}
	})
	previousBackendDB := config.Current().Metadata.Type
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.Type = "sqlite3" })
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Metadata.Type = previousBackendDB })
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := deployStatementsContext(ctx, database, []string{"create table canceled(value integer)"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("deployStatementsContext() error = %v; want context.Canceled", err)
	}
}
