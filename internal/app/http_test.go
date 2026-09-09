package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/config"
)

func TestStandardHTTPReturnsInvalidMultiAuthConfiguration(t *testing.T) {
	previousMethod := config.Config.Authentication.Method
	previousUser := config.Config.Authentication.Basic.User
	config.Config.Authentication.Method = "multi"
	config.Config.Authentication.Basic.User = ""
	t.Cleanup(func() {
		config.Config.Authentication.Method = previousMethod
		config.Config.Authentication.Basic.User = previousUser
	})

	err := standardHttp(context.Background(), false, nil)
	if err == nil {
		t.Fatal("standardHttp() returned nil for multi auth without authentication.basic.user")
	}
	if !strings.Contains(err.Error(), "authentication.basic.user") {
		t.Fatalf("standardHttp() error = %q; want authentication.basic.user context", err)
	}
}

func TestHTTPRoutersApplyConfiguredMutualTLSVerification(t *testing.T) {
	previousUseMutualTLS := config.Config.Server.TLS.MutualTLS
	previousValidOUs := config.Config.Server.TLS.ValidOUs
	previousAgentsUseMutualTLS := config.Config.Agents.TLS.MutualTLS
	previousAgentValidOUs := config.Config.Agents.TLS.ValidOUs
	previousPrefix := config.Config.Server.URLPrefix
	previousMethod := config.Config.Authentication.Method
	config.Config.Server.TLS.MutualTLS = true
	config.Config.Server.TLS.ValidOUs = []string{"standard"}
	config.Config.Agents.TLS.MutualTLS = true
	config.Config.Agents.TLS.ValidOUs = []string{"agent"}
	config.Config.Server.URLPrefix = "/orchestrator"
	config.Config.Authentication.Method = ""
	t.Cleanup(func() {
		config.Config.Server.TLS.MutualTLS = previousUseMutualTLS
		config.Config.Server.TLS.ValidOUs = previousValidOUs
		config.Config.Agents.TLS.MutualTLS = previousAgentsUseMutualTLS
		config.Config.Agents.TLS.ValidOUs = previousAgentValidOUs
		config.Config.Server.URLPrefix = previousPrefix
		config.Config.Authentication.Method = previousMethod
	})

	standard, err := newStandardHTTPRouter()
	if err != nil {
		t.Fatalf("newStandardHTTPRouter() error = %v", err)
	}
	agents, err := newAgentsHTTPRouter()
	if err != nil {
		t.Fatalf("newAgentsHTTPRouter() error = %v", err)
	}

	for name, fixture := range map[string]struct {
		handler http.Handler
		target  string
	}{
		"standard": {handler: standard, target: "/orchestrator/api/lb-check"},
		"agent":    {handler: agents, target: "/orchestrator/api/agent-ping"},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, fixture.target, nil)
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if got, want := response.Code, http.StatusUnauthorized; got != want {
				t.Fatalf("status = %d, want %d; body = %q", got, want, response.Body.String())
			}
			if got, want := response.Body.String(), "No TLS\n"; got != want {
				t.Fatalf("body = %q, want %q", got, want)
			}
		})
	}
}

func TestStandardHTTPReturnsUnixListenerError(t *testing.T) {
	previousMethod := config.Config.Authentication.Method
	previousSocket := config.Config.Server.Listen.Socket
	previousUseSSL := config.Config.Server.TLS.Enabled
	config.Config.Authentication.Method = ""
	config.Config.Server.Listen.Socket = filepath.Join(t.TempDir(), "missing", "orchestrator.sock")
	config.Config.Server.TLS.Enabled = false
	t.Cleanup(func() {
		config.Config.Authentication.Method = previousMethod
		config.Config.Server.Listen.Socket = previousSocket
		config.Config.Server.TLS.Enabled = previousUseSSL
	})

	err := standardHttp(context.Background(), false, nil)
	if err == nil {
		t.Fatal("standardHttp() returned nil for an unavailable unix socket path")
	}
	if !strings.Contains(err.Error(), "orchestrator.sock") {
		t.Fatalf("standardHttp() error = %q; want unix socket path", err)
	}
}
