package app

import (
	"net/http"
	"net/http/httptest"
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

func TestHTTPRoutersApplyConfiguredMutualTLSVerification(t *testing.T) {
	previousUseMutualTLS := config.Config.UseMutualTLS
	previousValidOUs := config.Config.SSLValidOUs
	previousAgentsUseMutualTLS := config.Config.AgentsUseMutualTLS
	previousAgentValidOUs := config.Config.AgentSSLValidOUs
	previousPrefix := config.Config.URLPrefix
	previousMethod := config.Config.AuthenticationMethod
	config.Config.UseMutualTLS = true
	config.Config.SSLValidOUs = []string{"standard"}
	config.Config.AgentsUseMutualTLS = true
	config.Config.AgentSSLValidOUs = []string{"agent"}
	config.Config.URLPrefix = "/orchestrator"
	config.Config.AuthenticationMethod = ""
	t.Cleanup(func() {
		config.Config.UseMutualTLS = previousUseMutualTLS
		config.Config.SSLValidOUs = previousValidOUs
		config.Config.AgentsUseMutualTLS = previousAgentsUseMutualTLS
		config.Config.AgentSSLValidOUs = previousAgentValidOUs
		config.Config.URLPrefix = previousPrefix
		config.Config.AuthenticationMethod = previousMethod
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
