// Package client 实现独立的 orchestrator HTTP 客户端，不加载服务端配置。
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Error 区分请求失败和已发送变更的未知结果；不得对 Unknown 自动重放。
type Error struct {
	Kind    string
	Message string
	Unknown bool
}

func (e *Error) Error() string {
	if e.Unknown {
		return "result unknown: " + e.Message
	}
	return e.Message
}

// Config 只包含客户端连接参数。Headers 由可信部署方配置。
type Config struct {
	Endpoints                                []string
	Username, Password, Token, CA, Cert, Key string
	Headers                                  []string
	Timeout                                  time.Duration
}

// Client 不重试业务请求，且不跟随可能重放变更的 HTTP 重定向。
type Client struct {
	endpoints []*url.URL
	http      *http.Client
	config    Config
	headers   http.Header
}

// Request 的 Mutating 表示业务副作用，不由 HTTP 方法推导。
type Request struct {
	Method, Path string
	Query        url.Values
	Body         json.RawMessage
	Mutating     bool
	Local        bool
}

func New(config Config) (*Client, error) {
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}
	if len(config.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one endpoint is required")
	}
	c := &Client{config: config, headers: make(http.Header)}
	for _, endpoint := range config.Endpoints {
		u, err := url.Parse(endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("endpoint must be an HTTP(S) base URL without credentials, query or fragment")
		}
		u.Path = strings.TrimRight(u.Path, "/")
		if !strings.HasSuffix(u.Path, "/api") {
			u.Path += "/api"
		}
		c.endpoints = append(c.endpoints, u)
	}
	if config.Token != "" && (config.Username != "" || config.Password != "") {
		return nil, fmt.Errorf("token and basic authentication are mutually exclusive")
	}
	if config.Token != "" {
		parts := strings.Split(config.Token, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(config.Token, "; \r\n") {
			return nil, fmt.Errorf("token must be public:secret")
		}
	}
	for _, header := range config.Headers {
		name, value, ok := strings.Cut(header, ":")
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(header, "\r\n") {
			return nil, fmt.Errorf("invalid authentication header")
		}
		c.headers.Add(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.CA != "" {
		pem, err := os.ReadFile(config.CA)
		if err != nil {
			return nil, fmt.Errorf("read CA certificate: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("invalid CA certificate")
		}
		tlsConfig.RootCAs = roots
	}
	if (config.Cert == "") != (config.Key == "") {
		return nil, fmt.Errorf("client certificate and key must be provided together")
	}
	if config.Cert != "" {
		cert, err := tls.LoadX509KeyPair(config.Cert, config.Key)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	// 禁用连接复用：Go Transport 会在陈旧复用连接上重试 GET，即使该 GET 有业务副作用。
	transport.DisableKeepAlives = true
	// Keep HTTP/2 stream retries out of the one-shot mutation contract as well.
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	c.http = &http.Client{Transport: transport, Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return c, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }

// Endpoint 对单地址直接使用；多地址只探测 leader，不执行任何业务操作。
func (c *Client) Endpoint(ctx context.Context, local bool) (*url.URL, error) {
	if local && len(c.endpoints) != 1 {
		return nil, fmt.Errorf("node-local operation requires exactly one endpoint")
	}
	if len(c.endpoints) == 1 {
		return c.endpoints[0], nil
	}
	for _, check := range []string{"leader-check", "routed-leader-check"} {
		for _, u := range c.endpoints {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if _, err := c.send(ctx, u, Request{Method: "GET", Path: check}); err == nil {
				return u, nil
			}
		}
	}
	return nil, &Error{Kind: "leader", Message: "no available leader among configured endpoints"}
}
func (c *Client) Do(ctx context.Context, request Request) (json.RawMessage, error) {
	if err := Validate(request); err != nil {
		return nil, err
	}
	u, err := c.Endpoint(ctx, request.Local)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, u, request)
}

// Validate 拒绝越过 API 基础路径的通用请求和无效 JSON。
func Validate(r Request) error {
	if _, err := http.NewRequest(r.Method, "http://localhost/", nil); err != nil {
		return fmt.Errorf("invalid HTTP method")
	}
	if r.Method == "" {
		return fmt.Errorf("HTTP method is required")
	}
	if r.Path == "" || strings.HasPrefix(r.Path, "/") || strings.ContainsAny(r.Path, "?#\\") || strings.Contains(r.Path, "://") {
		return fmt.Errorf("path must be relative to the API base URL")
	}
	for part := range strings.SplitSeq(r.Path, "/") {
		p, err := url.PathUnescape(part)
		if err != nil || p == "." || p == ".." {
			return fmt.Errorf("invalid API path segment")
		}
	}
	if len(r.Body) > 0 && !json.Valid(r.Body) {
		return fmt.Errorf("request body must be valid JSON")
	}
	return nil
}
func (c *Client) send(ctx context.Context, base *url.URL, r Request) (json.RawMessage, error) {
	endpoint := strings.TrimRight(base.String(), "/") + "/" + r.Path
	if len(r.Query) > 0 {
		endpoint += "?" + r.Query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, endpoint, bytes.NewReader(r.Body))
	if err != nil {
		return nil, fmt.Errorf("invalid HTTP request")
	}
	req.Header = c.headers.Clone()
	req.Header.Set("Accept", "application/json")
	if len(r.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.config.Username != "" || c.config.Password != "" {
		req.SetBasicAuth(c.config.Username, c.config.Password)
	}
	if c.config.Token != "" {
		req.AddCookie(&http.Cookie{Name: "access-token", Value: c.config.Token})
	}
	response, err := c.http.Do(req)
	if err != nil {
		if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
			return nil, &Error{Kind: "tls", Message: "TLS certificate verification failed"}
		}
		if networkError, ok := errors.AsType[*net.OpError](err); ok && networkError.Op == "dial" {
			return nil, &Error{Kind: "connect", Message: "HTTP connection failed"}
		}
		message := "HTTP transport failed"
		if errors.Is(err, context.DeadlineExceeded) {
			message = "HTTP request timed out"
		}
		if errors.Is(err, context.Canceled) {
			message = "HTTP request canceled"
		}
		return nil, &Error{Kind: "transport", Message: message, Unknown: r.Mutating}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil || len(body) > 32<<20 {
		return nil, &Error{Kind: "response", Message: "incomplete or oversized HTTP response", Unknown: r.Mutating}
	}
	if !json.Valid(body) {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, &Error{Kind: "http", Message: fmt.Sprintf("HTTP status %d", response.StatusCode), Unknown: r.Mutating && response.StatusCode >= 500}
		}
		return nil, &Error{Kind: "response", Message: "invalid JSON response", Unknown: r.Mutating}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) == nil && fields["Code"] != nil {
		var code string
		if json.Unmarshal(fields["Code"], &code) != nil || (code != "OK" && code != "ERROR") {
			return nil, &Error{Kind: "response", Message: "invalid API response code", Unknown: r.Mutating}
		}
	}
	var envelope struct{ Code, Message, ErrorClass string }
	if json.Unmarshal(body, &envelope) == nil && envelope.Code == "ERROR" {
		return body, &Error{Kind: "business", Message: c.redact(envelope.Message), Unknown: envelope.ErrorClass == "indeterminate"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &Error{Kind: "http", Message: fmt.Sprintf("HTTP status %d", response.StatusCode), Unknown: r.Mutating && response.StatusCode >= 500}
	}

	return body, nil
}
func (c *Client) redact(message string) string {
	for _, secret := range []string{c.config.Password, c.config.Token} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	for _, values := range c.headers {
		for _, v := range values {
			if v != "" {
				message = strings.ReplaceAll(message, v, "[redacted]")
			}
		}
	}
	return message
}
