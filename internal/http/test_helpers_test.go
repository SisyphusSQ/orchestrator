package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/openark/orchestrator/internal/http/transport"
)

func mustRouter(t *testing.T, options transport.RouterOptions) *transport.Router {
	t.Helper()
	router, err := transport.NewRouter(options)
	if err != nil {
		t.Fatalf("transport.NewRouter() error = %v", err)
	}
	return router
}

func serveRequest(t *testing.T, handler nethttp.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
