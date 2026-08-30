package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/openark/orchestrator/go/config"
)

// TestMatchDSN tests that the dsns we match don't expose the password
func TestMatchDSN(t *testing.T) {
	var tests = []struct {
		dsn    string
		output string
		err    error
	}{
		{"user@tcp(host:3306)/", "user@tcp(host:3306)/", nil},
		{"user:pass@tcp(host:3306)/", "user:?@tcp(host:3306)/", nil},
		{"user@tcp(host:3306)/db", "user@tcp(host:3306)/db", nil},
		{"user:pass@tcp(host:3306)/db", "user:?@tcp(host:3306)/db", nil},
		{"user:pass@tcp(host:3306)/db?param1=true", "user:?@tcp(host:3306)/db?param1=true", nil},
		{"user:pass@tcp(host:3306)/db?param1=true&param2=10", "user:?@tcp(host:3306)/db?param1=true&param2=10", nil},
		// tricky ones
		{"user:user:pass@tcp(host:3306)/db?param1=true&param2=10", "user:?@tcp(host:3306)/db?param1=true&param2=10", nil},
		{"user:pass@pass@tcp(host:3306)/db?param1=true&param2=10", "user:?@tcp(host:3306)/db?param1=true&param2=10", nil},
	}

	for i := range tests {
		match, err := matchDSN(tests[i].dsn)
		if match != tests[i].output || err != tests[i].err {
			t.Errorf("Failed to match %q: expected(%q,%v), got(%q,%v)",
				tests[i].dsn, tests[i].output, tests[i].err, match, err)
		}
	}
}

func TestSetupMySQLOrchestratorTLSReturnsCAFileError(t *testing.T) {
	previousConfigured := orchestratorTLSConfigured
	previousCAFile := config.Config.MySQLOrchestratorSSLCAFile
	previousSkipVerify := config.Config.MySQLOrchestratorSSLSkipVerify
	orchestratorTLSConfigured = false
	config.Config.MySQLOrchestratorSSLCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	config.Config.MySQLOrchestratorSSLSkipVerify = false
	t.Cleanup(func() {
		orchestratorTLSConfigured = previousConfigured
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
}

func TestGetMySQLURIReturnsTLSConfigurationError(t *testing.T) {
	previousURI := mysqlURI
	previousConfigured := orchestratorTLSConfigured
	previousUseMutualTLS := config.Config.MySQLOrchestratorUseMutualTLS
	previousCAFile := config.Config.MySQLOrchestratorSSLCAFile
	previousSkipVerify := config.Config.MySQLOrchestratorSSLSkipVerify
	mysqlURI = ""
	orchestratorTLSConfigured = false
	config.Config.MySQLOrchestratorUseMutualTLS = true
	config.Config.MySQLOrchestratorSSLCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
	config.Config.MySQLOrchestratorSSLSkipVerify = false
	t.Cleanup(func() {
		mysqlURI = previousURI
		orchestratorTLSConfigured = previousConfigured
		config.Config.MySQLOrchestratorUseMutualTLS = previousUseMutualTLS
		config.Config.MySQLOrchestratorSSLCAFile = previousCAFile
		config.Config.MySQLOrchestratorSSLSkipVerify = previousSkipVerify
	})

	if _, err := getMySQLURI(); err == nil {
		t.Fatal("getMySQLURI() returned nil for an invalid TLS configuration")
	}
	if mysqlURI != "" {
		t.Fatalf("getMySQLURI() cached DSN %q after TLS configuration failure", mysqlURI)
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
