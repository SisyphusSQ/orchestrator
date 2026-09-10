package http

import (
	"bytes"
	"expvar"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/http/pprof"

	"github.com/openark/orchestrator/internal/config"
	webassets "github.com/openark/orchestrator/web"
)

// HttpWeb serves the React control plane while preserving the existing Web URLs.
type HttpWeb struct {
	URLPrefix string
	assets    fs.FS
}

var Web HttpWeb

// webConfig contains only public UI capabilities, never server credentials.
type webConfig struct {
	URLPrefix                     string `json:"urlPrefix"`
	UserID                        string `json:"userId"`
	AuthorizedForAction           bool   `json:"authorizedForAction"`
	AuthorizedForConfiguration    bool   `json:"authorizedForConfiguration"`
	AgentsEnabled                 bool   `json:"agentsEnabled"`
	PseudoGTIDEnabled             bool   `json:"pseudoGTIDEnabled"`
	RemoveTextFromHostnameDisplay string `json:"removeTextFromHostnameDisplay"`
	WebMessage                    string `json:"webMessage"`
	AuditPageSize                 int    `json:"auditPageSize"`
	AuditEnabled                  bool   `json:"auditEnabled"`
}

func (web *HttpWeb) AccessToken(_ Params, r Responder, req *http.Request, resp http.ResponseWriter, _ Principal) {
	if err := authenticateToken(req.URL.Query().Get("publicToken"), resp); err != nil {
		writeHTTPJSON(r, http.StatusBadRequest, &APIResponse{Code: ERROR, Message: err.Error()})
		return
	}
	r.Redirect(web.URLPrefix + "/")
}

func (web *HttpWeb) Index(_ Params, r Responder) {
	r.Redirect(web.URLPrefix + "/web/clusters")
}

// Bootstrap is refreshed with the data so leader readiness and permissions do not
// remain frozen at the time the page was opened. API handlers remain authoritative.
func (web *HttpWeb) Bootstrap(_ Params, r Responder, req *http.Request, resp http.ResponseWriter, user Principal) {
	resp.Header().Set("Cache-Control", "no-store")
	writeHTTPJSON(r, http.StatusOK, webConfig{
		URLPrefix:                     web.URLPrefix,
		UserID:                        getUserId(req, user),
		AuthorizedForAction:           isAuthorizedForAction(req, user),
		AuthorizedForConfiguration:    isAuthorizedForConfiguration(req, user),
		AgentsEnabled:                 config.Config.Agents.ServeHTTP,
		PseudoGTIDEnabled:             config.Config.PseudoGTID.Pattern != "",
		RemoveTextFromHostnameDisplay: config.Config.Server.Web.RemoveTextFromHostname,
		WebMessage:                    config.Config.Server.Web.Message,
		AuditPageSize:                 config.AuditPageSize,
		AuditEnabled:                  config.Config.Audit.ToBackend,
	})
}

// Page serves only explicitly registered page routes. Unknown API and asset paths
// continue to return 404 instead of accidentally returning an HTML document.
func (web *HttpWeb) Page(resp http.ResponseWriter, req *http.Request) {
	assets := web.assets
	if assets == nil {
		assets = webassets.Files()
	}
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(resp, "embedded web assets unavailable; rebuild with make binary", http.StatusServiceUnavailable)
		return
	}
	base := []byte(`<base href="` + html.EscapeString(web.URLPrefix+"/web/") + `">`)
	page = bytes.Replace(page, []byte("<!--ORCHESTRATOR_BASE-->"), base, 1)
	resp.Header().Set("Content-Type", "text/html; charset=UTF-8")
	resp.Header().Set("Cache-Control", "no-store")
	resp.Header().Set("X-Content-Type-Options", "nosniff")
	resp.WriteHeader(http.StatusOK)
	if req.Method != http.MethodHead {
		_, _ = resp.Write(page)
	}
}

func (this *HttpWeb) registerWebRequest(m *Router, path string, handler Handler) {
	fullPath := fmt.Sprintf("%s/web/%s", this.URLPrefix, path)
	if path == "/" {
		fullPath = fmt.Sprintf("%s/", this.URLPrefix)
	}

	m.Get(fullPath, raftReverseProxy, handler)

}

// RegisterRequests makes for the de-facto list of known Web calls
func (this *HttpWeb) RegisterRequests(m *Router) {
	this.registerWebRequest(m, "access-token", this.AccessToken)
	this.registerWebRequest(m, "", this.Index)
	this.registerWebRequest(m, "/", this.Index)
	this.registerWebRequest(m, "home", this.Page)
	this.registerWebRequest(m, "about", this.Page)
	this.registerWebRequest(m, "keep-calm", this.Page)
	this.registerWebRequest(m, "faq", this.Page)
	this.registerWebRequest(m, "status", this.Page)
	this.registerWebRequest(m, "recovery-settings", this.Page)
	this.registerWebRequest(m, "clusters", this.Page)
	this.registerWebRequest(m, "clusters-analysis", this.Page)
	this.registerWebRequest(m, "cluster/:clusterName", this.Page)
	this.registerWebRequest(m, "cluster/alias/:clusterAlias", this.Page)
	this.registerWebRequest(m, "cluster/instance/:host/:port", this.Page)
	this.registerWebRequest(m, "cluster-pools/:clusterName", this.Page)
	this.registerWebRequest(m, "search/:searchString", this.Page)
	this.registerWebRequest(m, "search", this.Page)
	this.registerWebRequest(m, "discover", this.Page)
	this.registerWebRequest(m, "audit", this.Page)
	this.registerWebRequest(m, "audit/:page", this.Page)
	this.registerWebRequest(m, "audit/instance/:host/:port", this.Page)
	this.registerWebRequest(m, "audit/instance/:host/:port/:page", this.Page)
	this.registerWebRequest(m, "audit-recovery", this.Page)
	this.registerWebRequest(m, "audit-recovery/:page", this.Page)
	this.registerWebRequest(m, "audit-recovery/id/:id", this.Page)
	this.registerWebRequest(m, "audit-recovery/uid/:uid", this.Page)
	this.registerWebRequest(m, "audit-recovery/cluster/:clusterName", this.Page)
	this.registerWebRequest(m, "audit-recovery/cluster/:clusterName/:page", this.Page)
	this.registerWebRequest(m, "audit-recovery/alias/:clusterAlias", this.Page)
	this.registerWebRequest(m, "audit-recovery/alias/:clusterAlias/:page", this.Page)
	this.registerWebRequest(m, "audit-failure-detection", this.Page)
	this.registerWebRequest(m, "audit-failure-detection/:page", this.Page)
	this.registerWebRequest(m, "audit-failure-detection/id/:id", this.Page)
	this.registerWebRequest(m, "audit-failure-detection/alias/:clusterAlias", this.Page)
	this.registerWebRequest(m, "audit-failure-detection/alias/:clusterAlias/:page", this.Page)
	this.registerWebRequest(m, "audit-recovery-steps/:uid", this.Page)
	this.registerWebRequest(m, "agents", this.Page)
	this.registerWebRequest(m, "agent/:host", this.Page)
	this.registerWebRequest(m, "seed-details/:seedId", this.Page)
	this.registerWebRequest(m, "seeds", this.Page)

	handlers := []Handler{this.Bootstrap}

	handlers = []Handler{raftReverseProxy, this.Bootstrap}

	m.Get(this.URLPrefix+"/api/web-config", handlers...)

	this.RegisterDebug(m)
}

// RegisterDebug adds handlers for /debug/vars (expvar) and /debug/pprof (net/http/pprof) support
func (this *HttpWeb) RegisterDebug(m *Router) {
	m.Get(this.URLPrefix+"/debug/vars", func(w http.ResponseWriter, r *http.Request) {
		// from expvar.go, since the expvarHandler isn't exported :(
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprintf(w, "{\n")
		first := true
		expvar.Do(func(kv expvar.KeyValue) {
			if !first {
				fmt.Fprintf(w, ",\n")
			}
			first = false
			fmt.Fprintf(w, "%q: %s", kv.Key, kv.Value)
		})
		fmt.Fprintf(w, "\n}\n")
	})

	// list all the /debug/ endpoints we want
	m.Get(this.URLPrefix+"/debug/pprof", pprof.Index)
	m.Get(this.URLPrefix+"/debug/pprof/cmdline", pprof.Cmdline)
	m.Get(this.URLPrefix+"/debug/pprof/profile", pprof.Profile)
	m.Get(this.URLPrefix+"/debug/pprof/symbol", pprof.Symbol)
	m.Post(this.URLPrefix+"/debug/pprof/symbol", pprof.Symbol)
	m.Get(this.URLPrefix+"/debug/pprof/block", pprof.Handler("block").ServeHTTP)
	m.Get(this.URLPrefix+"/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	m.Get(this.URLPrefix+"/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	m.Get(this.URLPrefix+"/debug/pprof/threadcreate", pprof.Handler("threadcreate").ServeHTTP)

}
