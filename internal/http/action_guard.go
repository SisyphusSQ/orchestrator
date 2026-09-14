package http

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/openark/orchestrator/internal/http/authz"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/transport"
)

// webActionNames identifies operations that receive POST aliases for the Web console.
// Existing GET clients and the underlying business/authorization handlers stay intact.
var webActionNames = contract.WebActions()

// guardWebAction prevents cross-site browser writes and intermediary caching.
// Requests without Origin remain available to non-browser clients.
func guardWebAction(_ transport.Params, r transport.Responder, req *http.Request, resp http.ResponseWriter, user transport.Principal) {
	resp.Header().Set("Cache-Control", "no-store")
	// Check user permissions at ingress; leader readiness is checked by the
	// business handler after follower requests have been proxied to the leader.
	if !authz.ForWrite(req, user) {
		presenter.WriteJSON(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "Unauthorized"})
		return
	}
	if req.Header.Get("Sec-Fetch-Site") == "cross-site" {
		presenter.WriteJSON(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "cross-site action rejected"})
		return
	}
	if origin := req.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != req.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			presenter.WriteJSON(r, http.StatusForbidden, &contract.Response{Code: contract.ERROR, Message: "cross-origin action rejected"})
		}
	}
}

func isWebAction(path string) bool {
	name, _, _ := strings.Cut(path, "/")
	return webActionNames[name]
}
