package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/openark/orchestrator/go/config"
)

func TestSetupMySQLOrchestratorTLSReturnsCAFileError(t *testing.T) {
	previousConfigured := orchestratorTLSConfigured
	previousPassword := config.Config.MySQLOrchestratorPassword
	previousCAFile := config.Config.MySQLOrchestratorSSLCAFile
	previousSkipVerify := config.Config.MySQLOrchestratorSSLSkipVerify
	orchestratorTLSConfigured = false
	config.Config.MySQLOrchestratorPassword = "password-must-not-appear-in-error"
	config.Config.MySQLOrchestratorSSLCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	config.Config.MySQLOrchestratorSSLSkipVerify = false
	t.Cleanup(func() {
		orchestratorTLSConfigured = previousConfigured
		config.Config.MySQLOrchestratorPassword = previousPassword
		config.Config.MySQLOrchestratorSSLCAFile = previousCAFile
		config.Config.MySQLOrchestratorSSLSkipVerify = previousSkipVerify
	})

	_, err := SetupMySQLOrchestratorTLS("user@tcp(localhost:3306)/orchestrator")
	if err == nil {
		t.Fatal("SetupMySQLOrchestratorTLS() returned nil for a missing CA file")
	}
	if !strings.Contains(err.Error(), "missing-ca.pem") {
		t.Fatalf("SetupMySQLOrchestratorTLS() error = %q; want missing CA path", err)
	}
	if strings.Contains(err.Error(), config.Config.MySQLOrchestratorPassword) {
		t.Fatalf("SetupMySQLOrchestratorTLS() error exposed the configured password: %q", err)
	}
}

func TestConfigureOrchestratorTLSReturnsConfigurationError(t *testing.T) {
	previousConfigured := orchestratorTLSConfigured
	previousUseMutualTLS := config.Config.MySQLOrchestratorUseMutualTLS
	previousCAFile := config.Config.MySQLOrchestratorSSLCAFile
	previousSkipVerify := config.Config.MySQLOrchestratorSSLSkipVerify
	orchestratorTLSConfigured = false
	config.Config.MySQLOrchestratorUseMutualTLS = true
	config.Config.MySQLOrchestratorSSLCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	config.Config.MySQLOrchestratorSSLSkipVerify = false
	t.Cleanup(func() {
		orchestratorTLSConfigured = previousConfigured
		config.Config.MySQLOrchestratorUseMutualTLS = previousUseMutualTLS
		config.Config.MySQLOrchestratorSSLCAFile = previousCAFile
		config.Config.MySQLOrchestratorSSLSkipVerify = previousSkipVerify
	})

	cfg := newOrchestratorMySQLConfig(config.Config.MySQLOrchestratorDatabase)
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
	previousBackendDB := config.Config.BackendDB
	config.Config.BackendDB = "sqlite3"
	t.Cleanup(func() {
		config.Config.BackendDB = previousBackendDB
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
	previousBackendDB := config.Config.BackendDB
	config.Config.BackendDB = "sqlite3"
	t.Cleanup(func() {
		config.Config.BackendDB = previousBackendDB
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := deployStatementsContext(ctx, database, []string{"create table canceled(value integer)"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("deployStatementsContext() error = %v; want context.Canceled", err)
	}
}
