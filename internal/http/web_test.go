package http

import (
	"encoding/json"
	"github.com/openark/orchestrator/internal/http/transport"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/openark/orchestrator/internal/config"
	httpweb "github.com/openark/orchestrator/internal/http/web"
)

func TestWebPageDeepLinksAndPrefix(t *testing.T) {
	router := mustRouter(t, transport.RouterOptions{})
	web := httpweb.New("/orchestrator", fstest.MapFS{
		"index.html": {Data: []byte(`<head><!--ORCHESTRATOR_BASE--><script src="./assets/main.js"></script></head><div id="root"></div>`)},
	})
	web.RegisterRequests(router)
	for _, path := range []string{"/web/clusters", "/web/cluster/mysql%2Fteam%2Bblue%253A3306", "/web/cluster/alias/team%2Fblue", "/web/audit-recovery/uid/abc", "/web/seed-details/1"} {
		response := serveRequest(t, router, http.MethodGet, "/orchestrator"+path, nil)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `<base href="/orchestrator/web/">`) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid SPA response for %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/orchestrator/api/unknown", "/orchestrator/web/unknown", "/orchestrator/web/assets/missing.js"} {
		response := serveRequest(t, router, http.MethodGet, path, nil)
		if response.Code != 404 {
			t.Fatalf("%s must remain 404, got %d", path, response.Code)
		}
	}
	head := serveRequest(t, router, http.MethodHead, "/orchestrator/web/clusters", nil)
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatal("HEAD must have no body")
	}
}

func TestEmbeddedStaticFilesWithoutWorkingDirectoryResources(t *testing.T) {
	t.Chdir(t.TempDir())
	router := mustRouter(t, transport.RouterOptions{Authentication: transport.AuthenticationOptions{Method: "basic", Username: "reader", Password: "fixture"}})
	router.StaticFS("/prefix/web/assets", http.FS(fstest.MapFS{
		"main.js": {Data: []byte("console.log('embedded')")},
	}))
	unauthorized := serveRequest(t, router, http.MethodGet, "/prefix/web/assets/main.js", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("assets bypassed authentication: %d", unauthorized.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		request := httptest.NewRequest(method, "/prefix/web/assets/main.js", nil)
		request.SetBasicAuth("reader", "fixture")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("%s static response: %d %s", method, response.Code, response.Body.String())
		}
		if method == http.MethodHead && response.Body.Len() != 0 {
			t.Fatal("HEAD returned an asset body")
		}
		if method == http.MethodGet && response.Body.String() != "console.log('embedded')" {
			t.Fatal("GET did not serve the in-memory asset")
		}
	}
	for _, path := range []string{"/prefix/web/assets/", "/prefix/web/assets/missing.js", "/prefix/web/assets/../private.txt"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.SetBasicAuth("reader", "fixture")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), "main.js") || strings.Contains(response.Body.String(), "embedded") {
			t.Fatalf("unexpected file exposure at %s: %s", path, response.Body.String())
		}
		if path != "/prefix/web/assets/" && response.Code != http.StatusNotFound {
			t.Fatalf("missing asset returned %d for %s", response.Code, path)
		}
	}
}

func TestWebPageReportsMissingEmbeddedBuild(t *testing.T) {
	web := httpweb.New("", fstest.MapFS{})
	response := httptest.NewRecorder()
	web.Page(response, httptest.NewRequest(http.MethodGet, "/web/clusters", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "make binary") {
		t.Fatalf("missing build response: %d %s", response.Code, response.Body.String())
	}
}

func TestWebConfigPublishesOnlyUICapabilities(t *testing.T) {
	previous := config.Config
	copy := *previous
	config.Config = &copy
	config.Config.Server.ReadOnly = true
	config.Config.Topology.MySQL.Password = "never-publish-this"
	config.Config.Server.Web.Message = "maintenance </script><script>alert(1)</script>"
	t.Cleanup(func() { config.Config = previous })
	router := mustRouter(t, transport.RouterOptions{})
	web := httpweb.New("/prefix", nil)
	web.RegisterRequests(router)
	response := serveRequest(t, router, http.MethodGet, "/prefix/api/web-config", nil)
	var result httpweb.Config
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.AuthorizedForAction || result.URLPrefix != "/prefix" || result.WebMessage != config.Config.Server.Web.Message {
		t.Fatalf("incorrect public capabilities: %+v", result)
	}
	if strings.Contains(response.Body.String(), "never-publish-this") || strings.Contains(response.Body.String(), "</script>") {
		t.Fatal("credentials or unescaped HTML in public config")
	}
}

func TestWebPostActionsRejectCrossOriginAndReadonlyBeforeExecution(t *testing.T) {
	previous := config.Config
	copy := *previous
	config.Config = &copy
	config.Config.Authentication.Method = ""
	t.Cleanup(func() { config.Config = previous })
	calls := 0
	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{URLPrefix: "/prefix"}
	api.registerSingleAPIRequest(router, "begin-maintenance/:host/:port/:owner/:reason", func(params transport.Params, r transport.Responder) {
		calls++
		r.JSON(200, params["reason"])
	}, false)
	for _, test := range []struct {
		name, origin, site string
		readOnly           bool
		want               int
	}{
		{"same-origin", "http://example.com", "same-origin", false, 200},
		{"cross-origin", "https://other.example", "", false, 403},
		{"cross-site", "", "cross-site", false, 403},
		{"read-only", "", "", true, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			config.Config.Server.ReadOnly = test.readOnly
			request := httptest.NewRequest(http.MethodPost, "http://example.com/prefix/api/begin-maintenance/mysql/3306/owner/a%2Fb%2Bc%25d", nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.site)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if test.want == 200 && response.Body.String() != `"a/b+c%d"` {
				t.Fatal("path must decode exactly once")
			}
		})
	}
	if calls != 1 {
		t.Fatalf("mutating handler ran %d times, want one", calls)
	}
}

func TestRaftLeadershipTransferRejectsCrossSiteBeforeExecution(t *testing.T) {
	previous := config.Config
	copy := *previous
	config.Config = &copy
	config.Config.Authentication.Method = ""
	config.Config.Server.ReadOnly = false
	t.Cleanup(func() { config.Config = previous })
	calls := 0
	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{}
	api.registerAPIMethod(router, http.MethodPost, "raft/leadership/transfer", func(_ transport.Params, r transport.Responder) {
		calls++
		r.JSON(http.StatusOK, "transferred")
	}, true)
	for _, origin := range []string{"https://other.example", "http://example.com"} {
		request := httptest.NewRequest(http.MethodPost, "http://example.com/api/raft/leadership/transfer", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusOK
		if origin == "https://other.example" {
			want = http.StatusForbidden
		}
		if response.Code != want {
			t.Fatalf("origin=%s status=%d want=%d body=%s", origin, response.Code, want, response.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("leadership transfer handler ran %d times, want one", calls)
	}
}
