package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, endpoints ...string) *Client {
	t.Helper()
	c, err := New(Config{Endpoints: endpoints, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func TestLeaderSelectionAndSingleWrite(t *testing.T) {
	var writes atomic.Int32
	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "not leader", 404) }))
	defer follower.Close()
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "leader-check") {
			_, _ = w.Write([]byte(`"OK"`))
			return
		}
		writes.Add(1)
		_, _ = w.Write([]byte(`{"Code":"OK","Details":"done"}`))
	}))
	defer leader.Close()
	c := testClient(t, follower.URL, leader.URL)
	if _, err := c.Do(t.Context(), Request{Method: "GET", Path: "recover/db/3306", Mutating: true}); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 1 {
		t.Fatalf("writes = %d", writes.Load())
	}
}
func TestMutationResponseLossNeverReplayed(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	c := testClient(t, server.URL)
	for range 2 {
		_, err := c.Do(t.Context(), Request{Method: "GET", Path: "recover/db/3306", Mutating: true})
		e, ok := errors.AsType[*Error](err)
		if !ok || !e.Unknown {
			t.Fatalf("expected unknown, got %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected replay: %d calls", calls.Load())
	}
}
func TestErrorsAndNoRedirectReplay(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		unknown bool
		kind    string
	}{{"invalid-code", 200, `{"Code":123}`, true, "response"}, {"indeterminate", 503, `{"Code":"ERROR","Message":"apply timeout","ErrorClass":"indeterminate"}`, true, "business"}, {"business", 200, `{"Code":"ERROR","Message":"failed"}`, false, "business"}, {"invalid", 200, `invalid`, true, "response"}, {"unavailable", 503, `{}`, true, "http"}, {"unauthorized", 401, `{}`, false, "http"}, {"redirect", 307, `{}`, false, "http"}} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/api/replayed")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			_, err := testClient(t, s.URL).Do(t.Context(), Request{Method: "GET", Path: "operation", Mutating: true})
			e, ok := errors.AsType[*Error](err)
			if !ok || e.Unknown != tc.unknown || e.Kind != tc.kind {
				t.Fatalf("error = %#v", err)
			}
			if calls.Load() != 1 {
				t.Fatal("request replayed")
			}
		})
	}
}
func TestAuthAndPrefix(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "user" || password != "secret" || r.Header.Get("X-User") != "operator" || r.URL.Path != "/prefix/api/search" || r.URL.Query().Get("s") != "a & b" {
			t.Errorf("request contract mismatch")
		}
		_, _ = w.Write([]byte(`{"Code":"ERROR","Message":"secret operator"}`))
	}))
	defer s.Close()
	c, err := New(Config{Endpoints: []string{s.URL + "/prefix"}, Username: "user", Password: "secret", Headers: []string{"X-User: operator"}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Do(t.Context(), Request{Method: "GET", Path: "search", Query: map[string][]string{"s": {"a & b"}}})
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "operator") {
		t.Fatalf("unredacted error: %v", err)
	}
}
func TestTokenUsesServerCookieContract(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("access-token")
		if err != nil || cookie.Value != "public:secret" {
			t.Error("missing token cookie")
		}
		_, _ = w.Write([]byte(`"OK"`))
	}))
	defer s.Close()
	c, err := New(Config{Endpoints: []string{s.URL}, Token: "public:secret", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Do(t.Context(), Request{Method: "GET", Path: "status"}); err != nil {
		t.Fatal(err)
	}
}
func TestTLSVerificationAndCustomCA(t *testing.T) {
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("one-shot client negotiated HTTP/%d", r.ProtoMajor)
		}
		_, _ = w.Write([]byte(`"OK"`))
	}))
	s.EnableHTTP2 = true
	s.StartTLS()
	defer s.Close()
	if _, err := testClient(t, s.URL).Do(t.Context(), Request{Method: "GET", Path: "status", Mutating: true}); err == nil {
		t.Fatal("untrusted certificate accepted")
	} else if e, ok := errors.AsType[*Error](err); !ok || e.Kind != "tls" || e.Unknown {
		t.Fatalf("TLS error classification: %v", err)
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Endpoints: []string{s.URL}, CA: ca, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Do(t.Context(), Request{Method: "GET", Path: "status"}); err != nil {
		t.Fatal(err)
	}
}
func TestCancellationAndNodeLocalSelection(t *testing.T) {
	c := testClient(t, "http://localhost:1", "http://localhost:2")
	if _, err := c.Do(t.Context(), Request{Method: "POST", Path: "raft/snapshot", Local: true, Mutating: true}); err == nil {
		t.Fatal("ambiguous node-local operation accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "clusters"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
func TestInvalidPathsAndBodies(t *testing.T) {
	for _, path := range []string{"../secret", "%2e%2e/secret", "/root", "http://elsewhere", "status?x=y"} {
		if Validate(Request{Method: "GET", Path: path}) == nil {
			t.Errorf("accepted %s", path)
		}
	}
	if Validate(Request{Method: "POST", Path: "raft/bootstrap", Body: json.RawMessage(`{`)}) == nil {
		t.Fatal("invalid body accepted")
	}
}

func TestMutationTimeoutDoesNotReplay(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); <-r.Context().Done() }))
	defer s.Close()
	c, err := New(Config{Endpoints: []string{s.URL}, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Do(t.Context(), Request{Method: "GET", Path: "recover/db/3306", Mutating: true})
	e, ok := errors.AsType[*Error](err)
	if !ok || !e.Unknown || !strings.Contains(e.Message, "timed out") || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
