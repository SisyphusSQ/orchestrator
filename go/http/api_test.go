package http

import (
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/openark/golib/log"
	test "github.com/openark/golib/tests"
	"github.com/openark/orchestrator/go/config"
)

func init() {
	config.Config.HostnameResolveMethod = "none"
	config.MarkConfigurationLoaded()
	log.SetLevel(log.ERROR)
}

func TestGetSynonymPath(t *testing.T) {
	api := HttpAPI{}

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
	m, err := NewRouter(RouterOptions{})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	api := HttpAPI{}

	api.RegisterRequests(m)

	pathsMap := make(map[string]bool)
	for _, path := range registeredPaths {
		pathBase := strings.Split(path, "/")[0]
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
	previousStatusEndpoint := config.Config.StatusEndpoint
	previousRaftEnabled := config.Config.RaftEnabled
	config.Config.StatusEndpoint = config.DefaultStatusAPIEndpoint
	config.Config.RaftEnabled = true
	t.Cleanup(func() {
		config.Config.StatusEndpoint = previousStatusEndpoint
		config.Config.RaftEnabled = previousRaftEnabled
	})

	standard := mustRouter(t, RouterOptions{})
	api := HttpAPI{URLPrefix: "/orchestrator"}
	web := HttpWeb{URLPrefix: "/orchestrator"}
	registeredAPIsBefore := len(registeredPaths)
	api.RegisterRequests(standard)
	if got, want := len(registeredPaths)-registeredAPIsBefore, 249; got != want {
		t.Fatalf("registered API routes = %d, want %d", got, want)
	}
	web.RegisterRequests(standard)
	if got, want := len(standard.logicalRoutes), 299; got != want {
		t.Fatalf("standard logical routes = %d, want %d", got, want)
	}

	for _, directory := range []string{"bootstrap", "css", "images", "js"} {
		standard.Static("/orchestrator/"+directory, "resources/public/"+directory)
	}
	if got, want := len(standard.staticMounts), 4; got != want {
		t.Fatalf("static mounts = %d, want %d", got, want)
	}

	agents := mustRouter(t, RouterOptions{})
	agentAPI := HttpAgentsAPI{URLPrefix: "/orchestrator"}
	agentAPI.RegisterRequests(agents)
	if got, want := len(agents.logicalRoutes), 6; got != want {
		t.Fatalf("agent logical routes = %d, want %d", got, want)
	}
	if got, want := len(standard.logicalRoutes)+len(agents.logicalRoutes), 305; got != want {
		t.Fatalf("total logical routes = %d, want %d", got, want)
	}

	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/api/topology/:host/:port")
	assertExactRouteRegistered(t, standard, nethttp.MethodHead, "/orchestrator/api/topology/:host/:port/")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/debug/pprof/symbol")
	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/js/*filepath")
	assertExactRouteRegistered(t, standard, nethttp.MethodGet, "/orchestrator/api/raft/configuration")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/bootstrap")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/members")
	assertExactRouteRegistered(t, standard, nethttp.MethodDelete, "/orchestrator/api/raft/members/:id")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/leadership/transfer")
	assertExactRouteRegistered(t, standard, nethttp.MethodPost, "/orchestrator/api/raft/snapshot")
	assertExactRouteRegistered(t, agents, nethttp.MethodHead, "/orchestrator/api/agent-ping/")
}

func assertExactRouteRegistered(t *testing.T, router *Router, method, path string) {
	t.Helper()
	enginePath, _ := normalizeEnginePath(path)
	if _, found := router.registered[method+" "+enginePath]; !found {
		t.Fatalf("route %s %s is not registered", method, path)
	}
}

func TestCustomStatusEndpointRegistration(t *testing.T) {
	previousStatusEndpoint := config.Config.StatusEndpoint
	config.Config.StatusEndpoint = "/custom-status"
	t.Cleanup(func() {
		config.Config.StatusEndpoint = previousStatusEndpoint
	})

	router := mustRouter(t, RouterOptions{})
	api := HttpAPI{URLPrefix: "/orchestrator"}
	api.RegisterRequests(router)
	assertExactRouteRegistered(t, router, nethttp.MethodGet, "/custom-status")
	assertExactRouteRegistered(t, router, nethttp.MethodHead, "/custom-status/")
}

func TestDebugEndpointsRespond(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	web := HttpWeb{URLPrefix: "/orchestrator"}
	web.RegisterDebug(router)

	for _, target := range []string{
		"/orchestrator/debug/vars",
		"/orchestrator/debug/pprof",
		"/orchestrator/debug/metrics",
	} {
		response := serveRequest(t, router, nethttp.MethodGet, target, nil)
		if got, want := response.Code, nethttp.StatusOK; got != want {
			t.Fatalf("GET %s status = %d, want %d; body = %q", target, got, want, response.Body.String())
		}
	}
}
