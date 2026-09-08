package http

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustRouter(t *testing.T, options RouterOptions) *Router {
	t.Helper()
	router, err := NewRouter(options)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func serveRequest(t *testing.T, handler nethttp.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestRouterPreservesParamsTrailingSlashAndHead(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	router.Get("/api/value/:id", func(params Params, responder Responder, _ *nethttp.Request, principal Principal) {
		responder.JSON(nethttp.StatusOK, struct {
			ID   string `json:"id"`
			User string `json:"user"`
		}{ID: params["id"], User: string(principal)})
	})

	for _, target := range []string{"/api/value/node-1", "/api/value/node-1/"} {
		resp := serveRequest(t, router, nethttp.MethodGet, target, nil)
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", target, resp.Code)
		}
		if got, want := resp.Header().Get("Content-Type"), "application/json; charset=UTF-8"; got != want {
			t.Fatalf("GET %s Content-Type = %q, want %q", target, got, want)
		}
		if got, want := resp.Body.String(), `{"id":"node-1","user":""}`; got != want {
			t.Fatalf("GET %s body = %q, want %q", target, got, want)
		}

		head := serveRequest(t, router, nethttp.MethodHead, target, nil)
		if head.Code != nethttp.StatusOK {
			t.Fatalf("HEAD %s status = %d, want 200", target, head.Code)
		}
		if got, want := head.Header().Get("Content-Type"), "application/json; charset=UTF-8"; got != want {
			t.Fatalf("HEAD %s Content-Type = %q, want %q", target, got, want)
		}
	}
}

func TestRouterMapsDifferentParameterNamesUnderSharedPrefix(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	router.Get("/api/topology/:clusterHint", func(params Params, responder Responder) {
		responder.JSON(nethttp.StatusOK, params["clusterHint"])
	})
	router.Get("/api/topology/:host/:port", func(params Params, responder Responder) {
		responder.JSON(nethttp.StatusOK, []string{params["host"], params["port"]})
	})

	cluster := serveRequest(t, router, nethttp.MethodGet, "/api/topology/prod", nil)
	if got, want := cluster.Body.String(), `"prod"`; got != want {
		t.Fatalf("cluster route body = %q, want %q", got, want)
	}
	instance := serveRequest(t, router, nethttp.MethodGet, "/api/topology/mysql-1/3306", nil)
	if got, want := instance.Body.String(), `["mysql-1","3306"]`; got != want {
		t.Fatalf("instance route body = %q, want %q", got, want)
	}
}

func TestRouterDoesNotRedirectOrReturnMethodNotAllowed(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	router.Get("/api/value", func(_ Params, responder Responder) {
		responder.JSON(nethttp.StatusOK, true)
	})

	for _, tc := range []struct {
		method string
		target string
	}{
		{method: nethttp.MethodGet, target: "/API/value"},
		{method: nethttp.MethodPost, target: "/api/value"},
	} {
		resp := serveRequest(t, router, tc.method, tc.target, nil)
		if resp.Code != nethttp.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", tc.method, tc.target, resp.Code)
		}
		if location := resp.Header().Get("Location"); location != "" {
			t.Fatalf("%s %s unexpectedly redirected to %q", tc.method, tc.target, location)
		}
		if got, want := resp.Body.String(), "404 page not found\n"; got != want {
			t.Fatalf("%s %s body = %q, want %q", tc.method, tc.target, got, want)
		}
	}
}

func TestRouterStopsRouteChainAfterProxyResponseIsCommitted(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	secondHandlerCalled := false
	router.Get(
		"/proxy",
		func(writer nethttp.ResponseWriter, _ *nethttp.Request) {
			writer.WriteHeader(nethttp.StatusNoContent)
		},
		func(_ Params, responder Responder) {
			secondHandlerCalled = true
			responder.JSON(nethttp.StatusOK, true)
		},
	)

	resp := serveRequest(t, router, nethttp.MethodGet, "/proxy", nil)
	if resp.Code != nethttp.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.Code)
	}
	if secondHandlerCalled {
		t.Fatal("second route handler ran after the first committed a response")
	}
}

func TestRaftProxyFallsThroughWhenRaftRuntimeIsDisabled(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	handlerCalled := false
	router.Get("/proxy", raftReverseProxy, func(_ Params, responder Responder) {
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

func TestRouterAuthenticationContracts(t *testing.T) {
	if _, err := NewRouter(RouterOptions{Authentication: AuthenticationOptions{Method: "multi"}}); err == nil {
		t.Fatal("NewRouter() accepted multi authentication without a username")
	}

	tests := []struct {
		name          string
		auth          AuthenticationOptions
		header        string
		wantStatus    int
		wantPrincipal string
	}{
		{
			name:          "basic valid",
			auth:          AuthenticationOptions{Method: "basic", Username: "writer", Password: "secret"},
			header:        basicAuthorization("writer", "secret"),
			wantStatus:    nethttp.StatusOK,
			wantPrincipal: "writer",
		},
		{
			name:       "basic invalid",
			auth:       AuthenticationOptions{Method: "basic", Username: "writer", Password: "secret"},
			header:     basicAuthorization("writer", "wrong"),
			wantStatus: nethttp.StatusUnauthorized,
		},
		{
			name:          "multi writer",
			auth:          AuthenticationOptions{Method: "multi", Username: "writer", Password: "secret"},
			header:        basicAuthorization("writer", "secret"),
			wantStatus:    nethttp.StatusOK,
			wantPrincipal: "writer",
		},
		{
			name:          "multi readonly accepts legacy arbitrary password",
			auth:          AuthenticationOptions{Method: "multi", Username: "writer", Password: "secret"},
			header:        basicAuthorization("readonly", "anything"),
			wantStatus:    nethttp.StatusOK,
			wantPrincipal: "readonly",
		},
		{
			name:       "multi invalid",
			auth:       AuthenticationOptions{Method: "multi", Username: "writer", Password: "secret"},
			header:     basicAuthorization("other", "secret"),
			wantStatus: nethttp.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := mustRouter(t, RouterOptions{Authentication: tc.auth})
			router.Get("/who", func(_ Params, responder Responder, _ *nethttp.Request, principal Principal) {
				responder.JSON(nethttp.StatusOK, string(principal))
			})

			resp := serveRequest(t, router, nethttp.MethodGet, "/who", map[string]string{"Authorization": tc.header})
			if resp.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %q", resp.Code, tc.wantStatus, resp.Body.String())
			}
			if tc.wantStatus == nethttp.StatusUnauthorized {
				if got, want := resp.Header().Get("WWW-Authenticate"), `Basic realm="Authorization Required"`; got != want {
					t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
				}
				if got, want := resp.Body.String(), "Not Authorized\n"; got != want {
					t.Fatalf("body = %q, want %q", got, want)
				}
				return
			}
			if got, want := resp.Body.String(), `"`+tc.wantPrincipal+`"`; got != want {
				t.Fatalf("body = %q, want %q", got, want)
			}
		})
	}
}

func basicAuthorization(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestRouterGzipWrapsMutualTLSFailureAndAbortsHandler(t *testing.T) {
	handlerCalled := false
	router := mustRouter(t, RouterOptions{
		EnableGzip: true,
		VerifyRequest: func(_ *nethttp.Request) error {
			return errors.New("Invalid OU")
		},
	})
	router.Get("/protected", func(_ Params, responder Responder) {
		handlerCalled = true
		responder.JSON(nethttp.StatusOK, true)
	})

	resp := serveRequest(t, router, nethttp.MethodGet, "/protected", map[string]string{"Accept-Encoding": "gzip"})
	if resp.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.Code)
	}
	if handlerCalled {
		t.Fatal("handler ran after mutual TLS verification failed")
	}
	if got, want := resp.Header().Get("Content-Encoding"), "gzip"; got != want {
		t.Fatalf("Content-Encoding = %q, want %q", got, want)
	}
	gzipReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	body, err := io.ReadAll(gzipReader)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatalf("close gzip reader: %v", err)
	}
	if got, want := string(body), "Invalid OU\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestRouterAuthenticationFailurePrecedesGzip(t *testing.T) {
	router := mustRouter(t, RouterOptions{
		Authentication: AuthenticationOptions{Method: "basic", Username: "writer", Password: "secret"},
		EnableGzip:     true,
	})
	router.Get("/protected", func(_ Params, responder Responder) {
		responder.JSON(nethttp.StatusOK, true)
	})

	resp := serveRequest(t, router, nethttp.MethodGet, "/protected", map[string]string{"Accept-Encoding": "gzip"})
	if resp.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.Code)
	}
	if encoding := resp.Header().Get("Content-Encoding"); encoding != "" {
		t.Fatalf("Content-Encoding = %q, want empty for authentication failure", encoding)
	}
}

func TestRouterRendererPreservesJSONAndYieldLayout(t *testing.T) {
	templateDir := t.TempDir()
	writeTemplate(t, templateDir, "templates/layout.tmpl", `<html>{{current}}|{{yield}}</html>`)
	writeTemplate(t, templateDir, "templates/page.tmpl", `<body>{{.Name}}</body>`)

	router := mustRouter(t, RouterOptions{Templates: &TemplateOptions{
		Directory:       templateDir,
		Layout:          "templates/layout",
		HTMLContentType: "text/html",
	}})
	router.Get("/page", func(_ Params, responder Responder) {
		responder.HTML(nethttp.StatusOK, "templates/page", map[string]string{"Name": "Ada"})
	})
	router.Get("/json", func(_ Params, responder Responder) {
		responder.JSON(nethttp.StatusCreated, map[string]string{"result": "ok"})
	})
	router.Get("/redirect", func(_ Params, responder Responder) {
		responder.Redirect("/page")
	})

	page := serveRequest(t, router, nethttp.MethodGet, "/page", nil)
	if page.Code != nethttp.StatusOK {
		t.Fatalf("HTML status = %d, want 200; body = %q", page.Code, page.Body.String())
	}
	if got, want := page.Header().Get("Content-Type"), "text/html; charset=UTF-8"; got != want {
		t.Fatalf("HTML Content-Type = %q, want %q", got, want)
	}
	if got, want := page.Body.String(), `<html>templates/page|<body>Ada</body></html>`; got != want {
		t.Fatalf("HTML body = %q, want %q", got, want)
	}

	jsonResponse := serveRequest(t, router, nethttp.MethodGet, "/json", nil)
	if jsonResponse.Code != nethttp.StatusCreated {
		t.Fatalf("JSON status = %d, want 201", jsonResponse.Code)
	}
	if got, want := jsonResponse.Header().Get("Content-Type"), "application/json; charset=UTF-8"; got != want {
		t.Fatalf("JSON Content-Type = %q, want %q", got, want)
	}
	if got, want := jsonResponse.Body.String(), `{"result":"ok"}`; got != want {
		t.Fatalf("JSON body = %q, want %q", got, want)
	}

	redirect := serveRequest(t, router, nethttp.MethodGet, "/redirect", nil)
	if got, want := redirect.Code, nethttp.StatusFound; got != want {
		t.Fatalf("redirect status = %d, want %d", got, want)
	}
	if got, want := redirect.Header().Get("Location"), "/page"; got != want {
		t.Fatalf("redirect Location = %q, want %q", got, want)
	}
}

func TestProjectTemplatesLoadAndRender(t *testing.T) {
	resources := filepath.Clean("../../resources")
	router := mustRouter(t, RouterOptions{Templates: &TemplateOptions{
		Directory:       resources,
		Layout:          "templates/layout",
		HTMLContentType: "text/html",
	}})
	router.Get("/about", func(_ Params, responder Responder) {
		responder.HTML(nethttp.StatusOK, "templates/about", map[string]interface{}{
			"title":  "About",
			"prefix": "/orchestrator",
		})
	})

	response := serveRequest(t, router, nethttp.MethodGet, "/about", nil)
	if got, want := response.Code, nethttp.StatusOK; got != want {
		t.Fatalf("project template status = %d, want %d; body = %q", got, want, response.Body.String())
	}
	for _, expected := range []string{"<title>Orchestrator - About</title>", "<strong>Orchestrator</strong>"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("project template body does not contain %q", expected)
		}
	}
}

func writeTemplate(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create template directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
}

func TestRouterStaticMountUsesExplicitPrefix(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	staticRoot := t.TempDir()
	for _, directory := range []string{"bootstrap", "css", "images", "js"} {
		directoryRoot := filepath.Join(staticRoot, directory)
		if err := os.MkdirAll(directoryRoot, 0o755); err != nil {
			t.Fatalf("create %s static fixture directory: %v", directory, err)
		}
		if err := os.WriteFile(filepath.Join(directoryRoot, "asset.txt"), []byte(directory), 0o644); err != nil {
			t.Fatalf("write %s static fixture: %v", directory, err)
		}
		router.Static("/orchestrator/"+directory, directoryRoot)

		file := serveRequest(t, router, nethttp.MethodGet, "/orchestrator/"+directory+"/asset.txt", nil)
		if file.Code != nethttp.StatusOK {
			t.Fatalf("%s static status = %d, want 200; body = %q", directory, file.Code, file.Body.String())
		}
		if got, want := file.Body.String(), directory; got != want {
			t.Fatalf("%s static body = %q, want %q", directory, got, want)
		}
	}

	directory := serveRequest(t, router, nethttp.MethodGet, "/orchestrator/js", nil)
	if directory.Code != nethttp.StatusFound {
		t.Fatalf("static directory status = %d, want 302", directory.Code)
	}
	if got, want := directory.Header().Get("Location"), "/orchestrator/js/"; got != want {
		t.Fatalf("static directory Location = %q, want %q", got, want)
	}

	missing := serveRequest(t, router, nethttp.MethodGet, "/orchestrator/js/missing.js", nil)
	if missing.Code != nethttp.StatusNotFound {
		t.Fatalf("missing static status = %d, want 404", missing.Code)
	}
}

func TestRouterServesHTTPHTTPSAndUnixSocketWithURLPrefix(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	router.Get("/orchestrator/ping", func(_ Params, responder Responder) {
		responder.JSON(nethttp.StatusOK, "pong")
	})

	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	assertRemoteResponse(t, httpServer.Client(), httpServer.URL+"/orchestrator/ping")

	tlsServer := httptest.NewTLSServer(router)
	t.Cleanup(tlsServer.Close)
	assertRemoteResponse(t, tlsServer.Client(), tlsServer.URL+"/orchestrator/ping")

	socketFile, err := os.CreateTemp("", "orchestrator-*.sock")
	if err != nil {
		t.Fatalf("reserve unix socket path: %v", err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		t.Fatalf("close unix socket path fixture: %v", err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("prepare unix socket path: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	unixServer := &nethttp.Server{Handler: router}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- unixServer.Serve(listener)
	}()
	t.Cleanup(func() {
		if err := unixServer.Close(); err != nil {
			t.Errorf("close unix HTTP server: %v", err)
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			t.Errorf("serve unix HTTP: %v", err)
		}
	})

	transport := &nethttp.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	assertRemoteResponse(t, &nethttp.Client{Transport: transport}, "http://unix/orchestrator/ping")
}

func assertRemoteResponse(t *testing.T, client *nethttp.Client, target string) {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read GET %s response: %v", target, err)
	}
	if got, want := response.StatusCode, nethttp.StatusOK; got != want {
		t.Fatalf("GET %s status = %d, want %d", target, got, want)
	}
	if got, want := string(body), `"pong"`; got != want {
		t.Fatalf("GET %s body = %q, want %q", target, got, want)
	}
}

func TestRouterRecoveryPreservesProductionResponse(t *testing.T) {
	router := mustRouter(t, RouterOptions{})
	router.Get("/panic", func(_ Params, _ Responder) {
		panic("boom")
	})

	resp := serveRequest(t, router, nethttp.MethodGet, "/panic", nil)
	if resp.Code != nethttp.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.Code)
	}
	if got, want := resp.Body.String(), "500 Internal Server Error"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
