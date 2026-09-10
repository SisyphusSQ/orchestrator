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

func (web *HttpWeb) registerWebRequest(m *Router, path string, handler Handler) {
	fullPath := fmt.Sprintf("%s/web/%s", web.URLPrefix, path)
	if path == "/" {
		fullPath = fmt.Sprintf("%s/", web.URLPrefix)
	}

	m.Get(fullPath, raftReverseProxy, handler)

}

// RegisterRequests makes for the de-facto list of known Web calls
func (web *HttpWeb) RegisterRequests(m *Router) {
	web.registerWebRequest(m, "access-token", web.AccessToken)
	web.registerWebRequest(m, "", web.Index)
	web.registerWebRequest(m, "/", web.Index)
	web.registerWebRequest(m, "home", web.Page)
	web.registerWebRequest(m, "about", web.Page)
	web.registerWebRequest(m, "keep-calm", web.Page)
	web.registerWebRequest(m, "faq", web.Page)
	web.registerWebRequest(m, "status", web.Page)
	web.registerWebRequest(m, "recovery-settings", web.Page)
	web.registerWebRequest(m, "clusters", web.Page)
	web.registerWebRequest(m, "clusters-analysis", web.Page)
	web.registerWebRequest(m, "cluster/:clusterName", web.Page)
	web.registerWebRequest(m, "cluster/alias/:clusterAlias", web.Page)
	web.registerWebRequest(m, "cluster/instance/:host/:port", web.Page)
	web.registerWebRequest(m, "cluster-pools/:clusterName", web.Page)
	web.registerWebRequest(m, "search/:searchString", web.Page)
	web.registerWebRequest(m, "search", web.Page)
	web.registerWebRequest(m, "discover", web.Page)
	web.registerWebRequest(m, "audit", web.Page)
	web.registerWebRequest(m, "audit/:page", web.Page)
	web.registerWebRequest(m, "audit/instance/:host/:port", web.Page)
	web.registerWebRequest(m, "audit/instance/:host/:port/:page", web.Page)
	web.registerWebRequest(m, "audit-recovery", web.Page)
	web.registerWebRequest(m, "audit-recovery/:page", web.Page)
	web.registerWebRequest(m, "audit-recovery/id/:id", web.Page)
	web.registerWebRequest(m, "audit-recovery/uid/:uid", web.Page)
	web.registerWebRequest(m, "audit-recovery/cluster/:clusterName", web.Page)
	web.registerWebRequest(m, "audit-recovery/cluster/:clusterName/:page", web.Page)
	web.registerWebRequest(m, "audit-recovery/alias/:clusterAlias", web.Page)
	web.registerWebRequest(m, "audit-recovery/alias/:clusterAlias/:page", web.Page)
	web.registerWebRequest(m, "audit-failure-detection", web.Page)
	web.registerWebRequest(m, "audit-failure-detection/:page", web.Page)
	web.registerWebRequest(m, "audit-failure-detection/id/:id", web.Page)
	web.registerWebRequest(m, "audit-failure-detection/alias/:clusterAlias", web.Page)
	web.registerWebRequest(m, "audit-failure-detection/alias/:clusterAlias/:page", web.Page)
	web.registerWebRequest(m, "audit-recovery-steps/:uid", web.Page)
	web.registerWebRequest(m, "agents", web.Page)
	web.registerWebRequest(m, "agent/:host", web.Page)
	web.registerWebRequest(m, "seed-details/:seedId", web.Page)
	web.registerWebRequest(m, "seeds", web.Page)

	m.Get(web.URLPrefix+"/api/web-config", raftReverseProxy, web.Bootstrap)

	web.RegisterDebug(m)
}

// RegisterDebug adds handlers for /debug/vars (expvar) and /debug/pprof (net/http/pprof) support
func (web *HttpWeb) RegisterDebug(m *Router) {
	m.Get(web.URLPrefix+"/debug/vars", func(w http.ResponseWriter, r *http.Request) {
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
	m.Get(web.URLPrefix+"/debug/pprof", pprof.Index)
	m.Get(web.URLPrefix+"/debug/pprof/cmdline", pprof.Cmdline)
	m.Get(web.URLPrefix+"/debug/pprof/profile", pprof.Profile)
	m.Get(web.URLPrefix+"/debug/pprof/symbol", pprof.Symbol)
	m.Post(web.URLPrefix+"/debug/pprof/symbol", pprof.Symbol)
	m.Get(web.URLPrefix+"/debug/pprof/block", pprof.Handler("block").ServeHTTP)
	m.Get(web.URLPrefix+"/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	m.Get(web.URLPrefix+"/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	m.Get(web.URLPrefix+"/debug/pprof/threadcreate", pprof.Handler("threadcreate").ServeHTTP)

}
