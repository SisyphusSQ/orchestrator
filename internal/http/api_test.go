package http

import (
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	test "github.com/openark/orchestrator/internal/golib/tests"
	httpagent "github.com/openark/orchestrator/internal/http/agent"
	httpobservability "github.com/openark/orchestrator/internal/http/observability"
	"github.com/openark/orchestrator/internal/http/transport"
	httpweb "github.com/openark/orchestrator/internal/http/web"
)

func init() {
	config.Config.Topology.Hostname.ResolveMethod = "none"
	config.MarkConfigurationLoaded()
	log.SetLevel(log.ERROR)
}

func TestGetSynonymPath(t *testing.T) {
	api := Routes{}

	{
		path := "relocate-slaves"
		synonym := api.getSynonymPath(path)
		test.S(t).ExpectEquals(synonym, "relocate-replicas")
	}
	{
		path := "relocate-slaves/:host/:port"
		synonym := api.getSynonymPath(path)
		test.S(t).ExpectEquals(synonym, "relocate-replicas/:host/:port")
	}
}

func TestKnownPaths(t *testing.T) {
	m, err := transport.NewRouter(transport.RouterOptions{})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	api := Routes{}

	api.RegisterRequests(m)

	pathsMap := make(map[string]bool)
	for _, path := range registeredPaths {
		pathBase, _, _ := strings.Cut(path, "/")
		pathsMap[pathBase] = true
	}
	test.S(t).ExpectTrue(pathsMap["health"])
	test.S(t).ExpectTrue(pathsMap["lb-check"])
	test.S(t).ExpectTrue(pathsMap["relocate"])
	test.S(t).ExpectTrue(pathsMap["relocate-slaves"])

	for path, synonym := range apiSynonyms {
		test.S(t).ExpectTrue(pathsMap[path])
		test.S(t).ExpectTrue(pathsMap[synonym])
	}
}

func TestCompleteRouteRegistrationContract(t *testing.T) {
	previousStatusEndpoint := config.Config.Server.Status.Endpoint
	config.Config.Server.Status.Endpoint = config.DefaultStatusAPIEndpoint
	t.Cleanup(func() {
		config.Config.Server.Status.Endpoint = previousStatusEndpoint
	})

	standard := mustRouter(t, transport.RouterOptions{})
	api := Routes{URLPrefix: "/orchestrator"}
	web := httpweb.New("/orchestrator", nil)
	registeredAPIsBefore := len(registeredPaths)
	api.RegisterRequests(standard)
	if got, want := len(registeredPaths)-registeredAPIsBefore, 263; got != want {
		t.Fatalf("registered API routes = %d, want %d", got, want)
	}
	httpobservability.Register(standard, "/orchestrator")
	web.RegisterRequests(standard)
	if got, want := len(standard.LogicalRoutes()), 390; got != want {
		t.Fatalf("standard logical routes = %d, want %d", got, want)
	}

	standard.Static("/orchestrator/web/assets", t.TempDir())
	if got, want := len(standard.StaticMounts()), 1; got != want {
		t.Fatalf("static mounts = %d, want %d", got, want)
	}

	agents := mustRouter(t, transport.RouterOptions{})
	agentAPI := httpagent.RegistrationAPI{URLPrefix: "/orchestrator"}
	agentAPI.RegisterRequests(agents)
	if got, want := len(agents.LogicalRoutes()), 6; got != want {
		t.Fatalf("agent logical routes = %d, want %d", got, want)
	}
	if got, want := len(standard.LogicalRoutes())+len(agents.LogicalRoutes()), 396; got != want {
		t.Fatalf("total logical routes = %d, want %d", got, want)
	}

	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/api/topology/:host/:port")
	assertExactRouteRegistered(t, standard, nethttp.MethodHead, "/orchestrator/api/topology/:host/:port/")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/debug/pprof/symbol")
	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/web/assets/*filepath")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/relocate/:host/:port/:belowHost/:belowPort")
	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/api/web-config")
	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/api/raft/configuration")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/bootstrap")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/members")
	assertExactRouteRegistered(t, standard, nethttp.MethodDelete, "/orchestrator/api/raft/members/:id")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/leadership/transfer")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/snapshot")
	assertExactRouteRegistered(t, agents, nethttp.MethodHead, "/orchestrator/api/agent-ping/")
}

func assertExactRouteRegistered(t *testing.T, router *transport.Router, method, path string) {
	t.Helper()
	if !router.HasRoute(method, path) {
		t.Fatalf("route %s %s is not registered", method, path)
	}
}

func TestCustomStatusEndpointRegistration(t *testing.T) {
	previousStatusEndpoint := config.Config.Server.Status.Endpoint
	config.Config.Server.Status.Endpoint = "/custom-status"
	t.Cleanup(func() {
		config.Config.Server.Status.Endpoint = previousStatusEndpoint
	})

	router := mustRouter(t, transport.RouterOptions{})
	api := Routes{URLPrefix: "/orchestrator"}
	api.RegisterRequests(router)
	assertExactRouteRegistered(t, router, nethttp.MethodGet, "/custom-status")
	assertExactRouteRegistered(t, router, nethttp.MethodHead, "/custom-status/")
}

func TestDebugEndpointsRespond(t *testing.T) {
	router := mustRouter(t, transport.RouterOptions{})
	web := httpweb.New("/orchestrator", nil)
	web.RegisterDebug(router)

	for _, target := range []string{
		"/orchestrator/debug/vars",
		"/orchestrator/debug/pprof",
	} {
		response := serveRequest(t, router, nethttp.MethodGet, target, nil)
		if got, want := response.Code, nethttp.StatusOK; got != want {
			t.Fatalf("GET %s status = %d, want %d; body = %q", target, got, want, response.Body.String())
		}
	}
}
