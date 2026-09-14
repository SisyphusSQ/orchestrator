package http

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/http/transport"

	"github.com/openark/orchestrator/internal/config"
	httpcli "github.com/openark/orchestrator/internal/http/cli"
	"github.com/openark/orchestrator/internal/http/contract"
)

// 将独立客户端的静态契约与实际注册路由对照，避免命令存在却调用不存在的 API。
func TestClientCatalogRoutesExist(t *testing.T) {
	data, err := os.ReadFile("../../tools/orch-cli/internal/cmd/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var specs []struct {
		Name, Path, Method string
		ReadOnly           bool
	}
	if err := json.Unmarshal(data, &specs); err != nil {
		t.Fatal(err)
	}
	canonical, err := contract.ReadSpecification()
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]bool{}
	for _, endpoint := range canonical.Endpoints {
		metadata[endpoint.Method+" "+endpoint.Path] = endpoint.ReadOnly
	}
	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{}
	api.RegisterRequests(router)
	placeholder := regexp.MustCompile(`\{([^}]+)\}`)
	for _, spec := range specs {
		method := spec.Method
		if method == "" {
			method = "GET"
		}
		path := placeholder.ReplaceAllStringFunc(spec.Path, func(p string) string {
			if strings.HasSuffix(p, "?}") {
				return ""
			}
			if p == "{instance}" || p == "{destination}" {
				return "host/3306"
			}
			return "value"
		})
		path = "/api/" + strings.TrimRight(path, "/")
		found := false
		for _, route := range router.LogicalRoutes() {
			if route.Method != method {
				continue
			}
			pattern := "^" + regexp.MustCompile(`:[^/]+`).ReplaceAllString(route.Path, `[^/]+`) + "$"
			if regexp.MustCompile(pattern).MatchString(path) {
				found = true
				if readOnly, ok := metadata[method+" "+route.Path]; !ok || readOnly != spec.ReadOnly {
					t.Errorf("%s: CLI readOnly=%v differs from contract %v", spec.Name, spec.ReadOnly, readOnly)
				}
				break
			}
		}
		if !found {
			t.Errorf("%s: route missing: %s %s", spec.Name, method, path)
		}
	}
}
func TestDiagnosticsRequireAuthorizationBeforeDatabaseAccess(t *testing.T) {
	old := config.Current().Server.ReadOnly
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = true })
	t.Cleanup(func() { config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = old }) })
	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{}
	httpcli.Register(func(path string, handler transport.Handler) {
		api.registerAPIRequest(router, path, handler)
	})
	response := serveRequest(t, router, http.MethodGet, "/api/cli/rematch/db/3306", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMigratedMutationsRejectReadOnlyBeforeDatabaseAccess(t *testing.T) {
	old := config.Current().Server.ReadOnly
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = true })
	t.Cleanup(func() { config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = old }) })
	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{}
	api.RegisterRequests(router)
	for _, path := range []string{"tag/db/3306?tag=x=y", "untag/db/3306?tag=x", "untag-all?tag=x=y", "submit-masters-to-kv-stores", "snapshot-topologies"} {
		response := serveRequest(t, router, http.MethodGet, "/api/"+path, nil)
		if !strings.Contains(response.Body.String(), "Unauthorized") {
			t.Errorf("%s: %s", path, response.Body)
		}
	}
}

func TestEncodedPathParameterRoundTrip(t *testing.T) {
	router := mustRouter(t, transport.RouterOptions{})
	router.Get("/api/echo/:value", func(params transport.Params, r transport.Responder) { r.JSON(http.StatusOK, params["value"]) })
	for _, path := range []string{"a%2Fb+%25%20%E4%B8%AD", "%252F"} {
		response := serveRequest(t, router, http.MethodGet, "/api/echo/"+path, nil)
		var got string
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		want := "a/b+% 中"
		if path == "%252F" {
			want = "%2F"
		}
		if response.Code != 200 || got != want {
			t.Fatalf("path=%s status=%d value=%q", path, response.Code, got)
		}
	}
}
