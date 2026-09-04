package http

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	nethttp "net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openark/golib/log"
)

const basicAuthenticationRealm = "Authorization Required"

// Params contains named path parameters for an HTTP route. It keeps router
// implementation details out of application handlers.
type Params map[string]string

// Principal is the authenticated username made available to HTTP handlers.
type Principal string

// Responder is the transport response surface used by existing API and Web
// handlers. Its implementation preserves the legacy JSON, HTML and redirect
// wire format without exposing Gin outside this adapter.
type Responder interface {
	JSON(status int, value interface{})
	HTML(status int, name string, value interface{})
	Redirect(location string, status ...int)
}

// Handler is one of the supported project HTTP handler signatures.
type Handler interface{}

// AuthenticationOptions configures the legacy Basic and multi-user
// authentication contracts.
type AuthenticationOptions struct {
	Method   string
	Username string
	Password string
}

// RouterOptions configures the explicit middleware owned by the Gin transport.
type RouterOptions struct {
	Authentication AuthenticationOptions
	EnableGzip     bool
	Templates      *TemplateOptions
	VerifyRequest  func(*nethttp.Request) error
}

type routeContract struct {
	Method string
	Path   string
}

// Router adapts the project's handler contracts to a Gin engine.
type Router struct {
	engine        *gin.Engine
	renderer      *templateRenderer
	registered    map[string]struct{}
	logicalRoutes []routeContract
	staticMounts  []string
}

// NewRouter creates a Gin engine with all behavior-affecting defaults set
// explicitly. In particular, path correction and method handling stay aligned
// with the previous router rather than inheriting Gin defaults.
func NewRouter(options RouterOptions) (*Router, error) {
	if err := validateAuthentication(options.Authentication); err != nil {
		return nil, err
	}

	renderer, err := newTemplateRenderer(options.Templates)
	if err != nil {
		return nil, err
	}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.HandleMethodNotAllowed = false
	engine.RemoveExtraSlash = false
	engine.Use(requestLogger())
	engine.Use(recovery())
	if middleware := authenticationMiddleware(options.Authentication); middleware != nil {
		engine.Use(middleware)
	}
	if options.EnableGzip {
		engine.Use(gzipMiddleware())
	}
	if options.VerifyRequest != nil {
		engine.Use(verificationMiddleware(options.VerifyRequest))
	}
	engine.NoRoute(func(ctx *gin.Context) {
		nethttp.NotFound(ctx.Writer, ctx.Request)
		ctx.Abort()
	})

	return &Router{
		engine:        engine,
		renderer:      renderer,
		registered:    make(map[string]struct{}),
		logicalRoutes: make([]routeContract, 0),
		staticMounts:  make([]string, 0, 4),
	}, nil
}

// ServeHTTP implements net/http.Handler.
func (router *Router) ServeHTTP(writer nethttp.ResponseWriter, request *nethttp.Request) {
	router.engine.ServeHTTP(writer, request)
}

// Get registers the requested path, its optional-trailing-slash counterpart,
// and explicit HEAD equivalents.
func (router *Router) Get(path string, handlers ...Handler) {
	router.registerLogicalRoute(nethttp.MethodGet, path, handlers...)
}

// Post registers the requested path and its optional-trailing-slash counterpart.
func (router *Router) Post(path string, handlers ...Handler) {
	router.registerLogicalRoute(nethttp.MethodPost, path, handlers...)
}

func (router *Router) registerLogicalRoute(method, path string, handlers ...Handler) {
	validateHandlers(handlers)
	router.logicalRoutes = append(router.logicalRoutes, routeContract{Method: method, Path: path})
	for _, compatiblePath := range optionalTrailingSlashPaths(path) {
		router.registerExactRoute(method, compatiblePath, handlers...)
		if method == nethttp.MethodGet {
			router.registerExactRoute(nethttp.MethodHead, compatiblePath, handlers...)
		}
	}
}

func optionalTrailingSlashPaths(path string) []string {
	if path == "" || path[0] != '/' {
		panic(fmt.Sprintf("HTTP route must begin with '/': %q", path))
	}
	if path == "/" {
		return []string{path}
	}
	if strings.HasSuffix(path, "/") {
		return []string{path, strings.TrimSuffix(path, "/")}
	}
	return []string{path, path + "/"}
}

func (router *Router) registerExactRoute(method, path string, handlers ...Handler) {
	enginePath, parameterNames := normalizeEnginePath(path)
	key := method + " " + enginePath
	if _, found := router.registered[key]; found {
		panic(fmt.Sprintf("duplicate HTTP route %s", key))
	}
	router.registered[key] = struct{}{}
	router.engine.Handle(method, enginePath, router.dispatch(handlers, parameterNames))
}

func normalizeEnginePath(path string) (string, map[string]string) {
	segments := strings.Split(path, "/")
	parameterNames := make(map[string]string)
	for index, segment := range segments {
		if len(segment) < 2 || (segment[0] != ':' && segment[0] != '*') {
			continue
		}
		canonicalName := fmt.Sprintf("param%d", index)
		parameterNames[canonicalName] = segment[1:]
		segments[index] = segment[:1] + canonicalName
	}
	return strings.Join(segments, "/"), parameterNames
}

// Static mounts one explicit static directory. The application registers the
// bootstrap, css, images and js roots separately so no root catch-all can mask
// application routes.
func (router *Router) Static(relativePath, root string) {
	prefix := strings.TrimSuffix(relativePath, "/")
	if prefix == "" || prefix == "/" || prefix[0] != '/' {
		panic(fmt.Sprintf("static route must be a non-root absolute path: %q", relativePath))
	}
	router.staticMounts = append(router.staticMounts, prefix)

	redirectDirectory := func(writer nethttp.ResponseWriter, request *nethttp.Request) {
		location := request.URL.Path + "/"
		if request.URL.RawQuery != "" {
			location += "?" + request.URL.RawQuery
		}
		nethttp.Redirect(writer, request, location, nethttp.StatusFound)
	}
	router.registerExactRoute(nethttp.MethodGet, prefix, redirectDirectory)
	router.registerExactRoute(nethttp.MethodHead, prefix, redirectDirectory)

	fileServer := nethttp.StripPrefix(prefix, nethttp.FileServer(gin.Dir(root, false)))
	serveFile := func(writer nethttp.ResponseWriter, request *nethttp.Request) {
		fileServer.ServeHTTP(writer, request)
	}
	pattern := prefix + "/*filepath"
	router.registerExactRoute(nethttp.MethodGet, pattern, serveFile)
	router.registerExactRoute(nethttp.MethodHead, pattern, serveFile)
}

func (router *Router) dispatch(handlers []Handler, parameterNames map[string]string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		params := make(Params, len(ctx.Params))
		for _, param := range ctx.Params {
			params[parameterNames[param.Key]] = param.Value
		}

		tracker := &commitTrackingWriter{ResponseWriter: ctx.Writer}
		ctx.Writer = tracker
		responder := &response{
			writer:   tracker,
			request:  ctx.Request,
			renderer: router.renderer,
		}
		principal := principalFromRequest(ctx.Request)

		for _, untypedHandler := range handlers {
			switch handler := untypedHandler.(type) {
			case func(Params, Responder):
				handler(params, responder)
			case func(Params, Responder, *nethttp.Request):
				handler(params, responder, ctx.Request)
			case func(Params, Responder, *nethttp.Request, Principal):
				handler(params, responder, ctx.Request, principal)
			case func(Params, Responder, *nethttp.Request, nethttp.ResponseWriter, Principal):
				handler(params, responder, ctx.Request, tracker, principal)
			case func(Params, Responder, *nethttp.Request) string:
				if _, err := tracker.WriteString(handler(params, responder, ctx.Request)); err != nil {
					log.Errorf("write HTTP handler response: %+v", err)
				}
			case func(nethttp.ResponseWriter, *nethttp.Request):
				handler(tracker, ctx.Request)
			case nethttp.Handler:
				handler.ServeHTTP(tracker, ctx.Request)
			default:
				panic(fmt.Sprintf("unsupported HTTP handler type %T", untypedHandler))
			}

			if tracker.committed {
				ctx.Abort()
				return
			}
		}
	}
}

func validateHandlers(handlers []Handler) {
	if len(handlers) == 0 {
		panic("HTTP route requires at least one handler")
	}
	for _, untypedHandler := range handlers {
		switch untypedHandler.(type) {
		case func(Params, Responder),
			func(Params, Responder, *nethttp.Request),
			func(Params, Responder, *nethttp.Request, Principal),
			func(Params, Responder, *nethttp.Request, nethttp.ResponseWriter, Principal),
			func(Params, Responder, *nethttp.Request) string,
			func(nethttp.ResponseWriter, *nethttp.Request),
			nethttp.Handler:
			// Supported transport contract.
		default:
			panic(fmt.Sprintf("unsupported HTTP handler type %T", untypedHandler))
		}
	}
}

type principalContextKey struct{}

func principalFromRequest(request *nethttp.Request) Principal {
	principal, _ := request.Context().Value(principalContextKey{}).(Principal)
	return principal
}

func requestWithPrincipal(request *nethttp.Request, principal Principal) *nethttp.Request {
	ctx := context.WithValue(request.Context(), principalContextKey{}, principal)
	return request.WithContext(ctx)
}

func validateAuthentication(options AuthenticationOptions) error {
	if strings.EqualFold(options.Method, "multi") && options.Username == "" {
		return fmt.Errorf("AuthenticationMethod is 'multi' but HTTPAuthUser is undefined")
	}
	return nil
}

func authenticationMiddleware(options AuthenticationOptions) gin.HandlerFunc {
	switch strings.ToLower(options.Method) {
	case "basic":
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(options.Username+":"+options.Password))
		return func(ctx *gin.Context) {
			if !secureCompare(ctx.Request.Header.Get("Authorization"), expected) {
				writeUnauthorized(ctx)
				return
			}
			ctx.Request = requestWithPrincipal(ctx.Request, Principal(options.Username))
			ctx.Next()
		}
	case "multi":
		return func(ctx *gin.Context) {
			username, password, ok := parseBasicAuthorization(ctx.Request.Header.Get("Authorization"))
			if !ok || (username != "readonly" && !(secureCompare(username, options.Username) && secureCompare(password, options.Password))) {
				writeUnauthorized(ctx)
				return
			}
			ctx.Request = requestWithPrincipal(ctx.Request, Principal(username))
			ctx.Next()
		}
	default:
		return nil
	}
}

func parseBasicAuthorization(header string) (username, password string, ok bool) {
	if len(header) < len("Basic ") || header[:len("Basic ")] != "Basic " {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(header[len("Basic "):])
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func secureCompare(given, actual string) bool {
	givenSHA := sha256.Sum256([]byte(given))
	actualSHA := sha256.Sum256([]byte(actual))
	return subtle.ConstantTimeCompare(givenSHA[:], actualSHA[:]) == 1
}

func writeUnauthorized(ctx *gin.Context) {
	ctx.Header("WWW-Authenticate", `Basic realm="`+basicAuthenticationRealm+`"`)
	nethttp.Error(ctx.Writer, "Not Authorized", nethttp.StatusUnauthorized)
	ctx.Abort()
}

func requestLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		started := time.Now()
		address := ctx.Request.Header.Get("X-Real-IP")
		if address == "" {
			address = ctx.Request.Header.Get("X-Forwarded-For")
			if address == "" {
				address = ctx.Request.RemoteAddr
			}
		}
		log.Infof("Started %s %s for %s", ctx.Request.Method, ctx.Request.URL.Path, address)
		ctx.Next()
		log.Infof("Completed %v %s in %v", ctx.Writer.Status(), nethttp.StatusText(ctx.Writer.Status()), time.Since(started))
	}
}

func recovery() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Errorf("PANIC: %v\n%s", recovered, debug.Stack())
				ctx.Writer.WriteHeader(nethttp.StatusInternalServerError)
				if _, err := ctx.Writer.Write([]byte("500 Internal Server Error")); err != nil {
					log.Errorf("write panic response: %+v", err)
				}
				ctx.Abort()
			}
		}()
		ctx.Next()
	}
}

func verificationMiddleware(verify func(*nethttp.Request) error) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := verify(ctx.Request); err != nil {
			nethttp.Error(ctx.Writer, err.Error(), nethttp.StatusUnauthorized)
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

func gzipMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !strings.Contains(ctx.Request.Header.Get("Accept-Encoding"), "gzip") {
			ctx.Next()
			return
		}

		ctx.Header("Content-Encoding", "gzip")
		ctx.Header("Vary", "Accept-Encoding")
		gzipWriter := gzip.NewWriter(ctx.Writer)
		writer := &gzipResponseWriter{ResponseWriter: ctx.Writer, writer: gzipWriter}
		ctx.Writer = writer
		defer func() {
			writer.Header().Del("Content-Length")
			if err := gzipWriter.Close(); err != nil {
				log.Errorf("close gzip response: %+v", err)
			}
		}()
		ctx.Next()
	}
}

type gzipResponseWriter struct {
	gin.ResponseWriter
	writer *gzip.Writer
}

var _ gin.ResponseWriter = (*gzipResponseWriter)(nil)

func (writer *gzipResponseWriter) Write(data []byte) (int, error) {
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", nethttp.DetectContentType(data))
	}
	return writer.writer.Write(data)
}

func (writer *gzipResponseWriter) WriteString(value string) (int, error) {
	return writer.Write([]byte(value))
}

func (writer *gzipResponseWriter) Flush() {
	if err := writer.writer.Flush(); err != nil {
		log.Errorf("flush gzip response: %+v", err)
	}
	writer.ResponseWriter.Flush()
}

func (writer *gzipResponseWriter) Unwrap() nethttp.ResponseWriter {
	return writer.ResponseWriter
}

type commitTrackingWriter struct {
	gin.ResponseWriter
	committed bool
}

var _ gin.ResponseWriter = (*commitTrackingWriter)(nil)

func (writer *commitTrackingWriter) WriteHeader(code int) {
	writer.committed = true
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *commitTrackingWriter) Write(data []byte) (int, error) {
	writer.committed = true
	return writer.ResponseWriter.Write(data)
}

func (writer *commitTrackingWriter) WriteString(value string) (int, error) {
	writer.committed = true
	return writer.ResponseWriter.WriteString(value)
}

func (writer *commitTrackingWriter) WriteHeaderNow() {
	writer.committed = true
	writer.ResponseWriter.WriteHeaderNow()
}

func (writer *commitTrackingWriter) Flush() {
	writer.committed = true
	writer.ResponseWriter.Flush()
}

func (writer *commitTrackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.committed = true
	return writer.ResponseWriter.Hijack()
}

func (writer *commitTrackingWriter) Unwrap() nethttp.ResponseWriter {
	return writer.ResponseWriter
}
