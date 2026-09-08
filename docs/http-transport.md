# HTTP transport

`orchestrator` uses Gin v1.12.0 as its HTTP routing engine. Gin is isolated
behind the transport adapter in `internal/http`: application handlers depend on the
project-owned `Params`, `Responder`, `Principal`, `Handler`, and `Router`
contracts rather than on `gin.Context` or other Gin types.

## Route contract

The standard and agent listeners register 391 logical method-and-path routes
(385 standard and 6 agent). Legacy API registration retains 258 paths including
synonyms. Web operations add 73 POST aliases and `/api/web-config` exposes
public UI capabilities. All 39 explicit Web routes are preserved.

静态资源不计入上述逻辑路由数量。标准监听器在 `URLPrefix + /web/assets`
提供 Go 二进制内嵌的 React 资源，不再挂载旧静态目录或读取磁盘模板。
Only explicit
Web page routes return the SPA entry; unknown APIs and assets remain 404.
Go injects an escaped base URL for prefixed deep links. HTML and UI capability
responses use `Cache-Control: no-store`.

The adapter disables Gin's automatic trailing-slash redirect, fixed-path
redirect, extra-slash cleanup, and automatic 405 response. Each existing route
is instead registered with both trailing-slash forms. GET routes also receive
an explicit HEAD route. Unknown paths and unsupported methods retain the
existing 404 behavior.

Gin requires parameter names to agree where route trees share a wildcard
segment. The adapter therefore uses position-based names internally and maps
them back to the names declared by each application route. Application code
continues to receive keys such as `host`, `port`, and `clusterHint`.

## Middleware and responses

The transport constructs `gin.New()` and registers logging, panic recovery,
authentication, gzip, and optional mutual-TLS OU verification explicitly.
Basic and multi-user authentication preserve the existing credential and
`readonly` principal rules. Proxy, token, OAuth, and unauthenticated
authorization decisions remain in the application authorization layer.

Authentication or mutual-TLS failure aborts the request before an application
handler runs. A Raft follower proxy that commits a response also terminates the
route chain, so the same request cannot continue into the local mutating
handler.
Web POST aliases check user write privileges and browser origin at ingress,
then use the existing leader proxy and business handler. Leader readiness is
checked after proxying, allowing follower clients to reach the leader. Legacy
GET action routes remain available.

The project responder preserves the existing wire contract:

- JSON uses `application/json; charset=UTF-8` without an added newline;
- HTML uses the configured content type and `UTF-8` charset;
- the `templates/layout` template retains `{{yield}}` and `{{current}}`; and
- redirects retain the existing default 302 status.

## Listener ownership

`internal/app` remains responsible for selecting the HTTP, HTTPS, or Unix socket
listener, loading TLS key pairs, starting the optional agent listener, and
starting continuous discovery. The Gin adapter is a standard
`net/http.Handler`, so it does not own sockets or TLS configuration.

## Validation boundary

Unit and fixture tests cover route registration, path parameters, optional
trailing slashes, HEAD, authentication, gzip, mutual-TLS failure, Raft-style
proxy termination, response rendering, project templates, in-memory static
assets without working-directory resources, debug endpoints, URL prefixes, and isolated HTTP, TLS, and Unix socket
listeners.

Live listener, real certificate/OU, Raft-cluster, browser, and business-flow
validation is intentionally not part of the transport migration. It is tracked
by Linear issue TOO-415 together with the MySQL and GORM migration E2E matrix.
