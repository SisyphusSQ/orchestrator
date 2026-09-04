package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orcraft "github.com/openark/orchestrator/go/raft"
)

func newJSONRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func serveHTTPRequest(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestRaftHTTPErrorClassesWhenDisabled(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	api := HttpAPI{URLPrefix: ""}
	api.RegisterRequests(router)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/raft/configuration"},
		{method: http.MethodPost, path: "/api/raft/bootstrap"},
		{method: http.MethodPost, path: "/api/raft/members", body: `{"id":"n2","address":"127.0.0.1:2","suffrage":"voter"}`},
		{method: http.MethodDelete, path: "/api/raft/members/n2"},
		{method: http.MethodPost, path: "/api/raft/leadership/transfer", body: `{}`},
		{method: http.MethodPost, path: "/api/raft/snapshot"},
	}
	for _, tc := range tests {
		req := newJSONRequest(t, tc.method, tc.path, tc.body)
		resp := serveHTTPRequest(t, router, req)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status = %d, want 400; body=%s", tc.method, tc.path, resp.Code, resp.Body.String())
		}
		var payload struct {
			Code       string
			Message    string
			ErrorClass string
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s %s: %v body=%s", tc.method, tc.path, err, resp.Body.String())
		}
		if payload.ErrorClass != string(orcraft.ClassDisabled) {
			t.Fatalf("%s %s ErrorClass = %q, want %q", tc.method, tc.path, payload.ErrorClass, orcraft.ClassDisabled)
		}
	}
}

func TestRaftAddMemberRejectsInvalidJSON(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	api := HttpAPI{URLPrefix: ""}
	api.RegisterRequests(router)
	req := newJSONRequest(t, http.MethodPost, "/api/raft/members", `{`)
	resp := serveHTTPRequest(t, router, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.Code, resp.Body.String())
	}
}

func TestRaftMutationRejectsInvalidCASAndBodies(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	api := HttpAPI{URLPrefix: ""}
	api.RegisterRequests(router)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "invalid query expected index",
			method: http.MethodPost,
			path:   "/api/raft/members?expectedIndex=not-a-number",
			body:   `{"id":"n2","address":"127.0.0.1:2","suffrage":"voter"}`,
		},
		{name: "malformed delete body", method: http.MethodDelete, path: "/api/raft/members/n2", body: `{`},
		{name: "unknown CAS field", method: http.MethodDelete, path: "/api/raft/members/n2", body: `{"expectedIndx":2}`},
		{name: "trailing JSON", method: http.MethodPost, path: "/api/raft/leadership/transfer", body: `{} {}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := serveHTTPRequest(t, router, newJSONRequest(t, tc.method, tc.path, tc.body))
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", resp.Code, resp.Body.String())
			}
			var payload struct {
				ErrorClass string
			}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.ErrorClass != string(orcraft.ClassInvalidArgument) {
				t.Fatalf("ErrorClass = %q, want %q", payload.ErrorClass, orcraft.ClassInvalidArgument)
			}
		})
	}
}

func TestExpectedIndexFromRequestRejectsAmbiguity(t *testing.T) {
	req := newJSONRequest(t, http.MethodDelete, "/api/raft/members/n2?expectedIndex=7", "")
	bodyIndex := uint64(7)
	if got, err := expectedIndexFromRequest(req, &bodyIndex); err != nil || got == nil || *got != 7 {
		t.Fatalf("matching body/query index = %v, %v; want pointer to 7, nil", got, err)
	}
	bodyIndex = 8
	if _, err := expectedIndexFromRequest(req, &bodyIndex); err == nil {
		t.Fatal("conflicting body/query expectedIndex succeeded")
	}

	req = newJSONRequest(t, http.MethodDelete, "/api/raft/members/n2?expectedIndex=7&expectedIndex=8", "")
	if _, err := expectedIndexFromRequest(req, nil); err == nil {
		t.Fatal("duplicate query expectedIndex succeeded")
	}
}

func TestExpectedIndexFromRequestPreservesExplicitZero(t *testing.T) {
	req := newJSONRequest(t, http.MethodDelete, "/api/raft/members/n2?expectedIndex=0", "")
	got, err := expectedIndexFromRequest(req, nil)
	if err != nil {
		t.Fatalf("expectedIndexFromRequest: %v", err)
	}
	if got == nil || *got != 0 {
		t.Fatalf("explicit expectedIndex zero = %v, want pointer to zero", got)
	}
}
