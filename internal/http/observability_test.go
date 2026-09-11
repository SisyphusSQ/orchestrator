package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpobservability "github.com/openark/orchestrator/internal/http/observability"
	"github.com/openark/orchestrator/internal/http/transport"
	httpweb "github.com/openark/orchestrator/internal/http/web"
	"github.com/openark/orchestrator/internal/models/vo"
	"github.com/openark/orchestrator/internal/observability"
)

func TestObservabilityRoutesStayLocalAndAuthenticated(t *testing.T) {
	runtime, err := observability.New(t.Context(), "", 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	runtime.Install()
	t.Cleanup(func() { _ = runtime.Close() })
	router := mustRouter(t, transport.RouterOptions{EnableGzip: true, Authentication: transport.AuthenticationOptions{Method: "basic", Username: "monitor", Password: "test"}})
	httpobservability.Register(router, "/orchestrator")
	server := httptest.NewServer(router)
	defer server.Close()
	req := httptest.NewRequest("GET", server.URL+"/orchestrator/metrics", nil)
	req.RequestURI = ""
	req.SetBasicAuth("monitor", "test")
	result, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != 200 || !strings.HasPrefix(string(body), "# HELP") {
		t.Fatalf("invalid or double-compressed Prometheus response: status=%d", result.StatusCode)
	}
	cases := []struct {
		path   string
		health vo.Health
		code   int
	}{
		{"/metrics", vo.Health{}, 200},
		{"/health/live", vo.Health{}, 200},
		{"/health/ready", vo.Health{}, 503},
		{"/health/ready", vo.Health{Ready: true, CheckedAt: time.Now()}, 200},
		{"/health/leader-ready", vo.Health{Ready: true, CheckedAt: time.Now()}, 503},
		{"/health/leader-ready", vo.Health{Ready: true, LeaderReady: true, CheckedAt: time.Now()}, 200},
		{"/health/leader-ready", vo.Health{Ready: true, LeaderReady: true, CheckedAt: time.Now().Add(-time.Minute)}, 503},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			observability.SetHealth(tc.health)
			for _, method := range []string{"GET", "HEAD"} {
				req := httptest.NewRequest(method, "/orchestrator"+tc.path, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != 401 {
					t.Fatalf("unauthenticated=%d", w.Code)
				}
				req.SetBasicAuth("monitor", "test")
				w = httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != tc.code {
					t.Fatalf("%s %s = %d body %s", method, tc.path, w.Code, w.Body)
				}
			}
		})
	}
}

func TestRemovedMetricsEndpoints(t *testing.T) {
	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{}
	api.RegisterRequests(router)
	web := httpweb.New("", nil)
	web.RegisterDebug(router)
	for _, path := range []string{"/debug/metrics", "/api/discovery-metrics-raw/60", "/api/discovery-metrics-aggregated/60", "/api/discovery-queue-metrics-raw/DEFAULT/60", "/api/backend-query-metrics-aggregated/60", "/api/write-buffer-metrics-raw/60"} {
		if w := serveRequest(t, router, http.MethodGet, path, nil); w.Code != 404 {
			t.Fatalf("%s=%d", path, w.Code)
		}
	}
}
