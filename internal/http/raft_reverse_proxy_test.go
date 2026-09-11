package http

import (
	nethttp "net/http"
	"testing"

	httpraft "github.com/openark/orchestrator/internal/http/raft"
	"github.com/openark/orchestrator/internal/http/transport"
)

func TestRaftProxyFallsThroughWhenRaftRuntimeIsDisabled(t *testing.T) {
	router := mustRouter(t, transport.RouterOptions{})
	handlerCalled := false
	router.Get("/proxy", httpraft.ReverseProxy, func(_ transport.Params, responder transport.Responder) {
		handlerCalled = true
		responder.JSON(nethttp.StatusOK, true)
	})

	response := serveRequest(t, router, nethttp.MethodGet, "/proxy", nil)
	if got, want := response.Code, nethttp.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !handlerCalled {
		t.Fatal("business handler did not run after disabled Raft proxy fell through")
	}
}
