package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openark/orchestrator/internal/config"
)

func TestStandardHTTPReturnsInvalidMultiAuthConfiguration(t *testing.T) {
	previousMethod := config.Current().Authentication.Method
	previousUser := config.Current().Authentication.Basic.User
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = "multi" })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Basic.User = "" })
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = previousMethod })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Basic.User = previousUser })
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
	previousUseMutualTLS := config.Current().Server.TLS.MutualTLS
	previousValidOUs := config.Current().Server.TLS.ValidOUs
	previousAgentsUseMutualTLS := config.Current().Agents.TLS.MutualTLS
	previousAgentValidOUs := config.Current().Agents.TLS.ValidOUs
	previousPrefix := config.Current().Server.URLPrefix
	previousMethod := config.Current().Authentication.Method
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.TLS.MutualTLS = true })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.TLS.ValidOUs = []string{"standard"} })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Agents.TLS.MutualTLS = true })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Agents.TLS.ValidOUs = []string{"agent"} })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.URLPrefix = "/orchestrator" })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = "" })
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.TLS.MutualTLS = previousUseMutualTLS })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.TLS.ValidOUs = previousValidOUs })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Agents.TLS.MutualTLS = previousAgentsUseMutualTLS })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Agents.TLS.ValidOUs = previousAgentValidOUs })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.URLPrefix = previousPrefix })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = previousMethod })
	})

	standard, err := newStandardHTTPRouter(nil)
	if err != nil {
		t.Fatalf("newStandardHTTPRouter(nil) error = %v", err)
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
	previousMethod := config.Current().Authentication.Method
	previousSocket := config.Current().Server.Listen.Socket
	previousUseSSL := config.Current().Server.TLS.Enabled
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = "" })
	config.TestUpdate(func(cfg *config.Configuration) {
		cfg.Server.Listen.Socket = filepath.Join(t.TempDir(), "missing", "orchestrator.sock")
	})
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.TLS.Enabled = false })
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = previousMethod })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.Listen.Socket = previousSocket })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.TLS.Enabled = previousUseSSL })
	})

	err := standardHttp(context.Background(), false, nil)
	if err == nil {
		t.Fatal("standardHttp() returned nil for an unavailable unix socket path")
	}
	if !strings.Contains(err.Error(), "orchestrator.sock") {
		t.Fatalf("standardHttp() error = %q; want unix socket path", err)
	}
}

func TestHTTPServiceCancellationStopsListenerAndRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- serveHTTPContext(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
	}()
	requestFinished := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if response != nil {
			response.Body.Close()
		}
		requestFinished <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-requestFinished:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not stop")
	}
}
