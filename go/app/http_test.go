package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/go/config"
)

func TestStandardHTTPReturnsInvalidMultiAuthConfiguration(t *testing.T) {
	previousMethod := config.Config.AuthenticationMethod
	previousUser := config.Config.HTTPAuthUser
	config.Config.AuthenticationMethod = "multi"
	config.Config.HTTPAuthUser = ""
	t.Cleanup(func() {
		config.Config.AuthenticationMethod = previousMethod
		config.Config.HTTPAuthUser = previousUser
	})

	err := standardHttp(false, nil)
	if err == nil {
		t.Fatal("standardHttp() returned nil for multi auth without HTTPAuthUser")
	}
	if !strings.Contains(err.Error(), "HTTPAuthUser") {
		t.Fatalf("standardHttp() error = %q; want HTTPAuthUser context", err)
	}
}

func TestStandardHTTPReturnsUnixListenerError(t *testing.T) {
	previousMethod := config.Config.AuthenticationMethod
	previousSocket := config.Config.ListenSocket
	previousUseSSL := config.Config.UseSSL
	config.Config.AuthenticationMethod = ""
	config.Config.ListenSocket = filepath.Join(t.TempDir(), "missing", "orchestrator.sock")
	config.Config.UseSSL = false
	t.Cleanup(func() {
		config.Config.AuthenticationMethod = previousMethod
		config.Config.ListenSocket = previousSocket
		config.Config.UseSSL = previousUseSSL
	})

	err := standardHttp(false, nil)
	if err == nil {
		t.Fatal("standardHttp() returned nil for an unavailable unix socket path")
	}
	if !strings.Contains(err.Error(), "orchestrator.sock") {
		t.Fatalf("standardHttp() error = %q; want unix socket path", err)
	}
}
