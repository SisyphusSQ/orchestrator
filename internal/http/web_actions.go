package http

import (
	"net/http"
	"net/url"
	"strings"
)

// webActionNames adds POST aliases only to operations used by the Web console.
// Existing GET clients and the underlying business/authorization handlers stay intact.
var webActionNames = map[string]bool{
	"discover": true, "refresh": true, "forget": true,
	"start-replica": true, "stop-replica": true, "restart-replica": true,
	"reset-replica": true, "skip-query": true, "detach-replica": true,
	"reattach-replica": true, "reattach-replica-master-host": true,
	"set-read-only": true, "set-writeable": true, "enable-gtid": true, "disable-gtid": true,
	"gtid-errant-reset-master": true, "gtid-errant-inject-empty": true,
	"begin-maintenance": true, "end-maintenance": true, "begin-downtime": true, "end-downtime": true,
	"relocate": true, "relocate-replicas": true, "move-up": true, "move-up-replicas": true,
	"move-below": true, "move-below-gtid": true, "move-replicas-gtid": true,
	"move-equivalent": true, "match-below": true, "match-replicas": true,
	"repoint": true, "repoint-replicas": true, "regroup-replicas": true,
	"make-co-master": true, "make-master": true, "make-local-master": true,
	"take-master": true, "take-siblings": true,
	"recover": true, "recover-lite": true, "force-master-failover": true,
	"graceful-master-takeover": true, "register-candidate": true,
	"ack-recovery": true, "set-cluster-alias": true, "submit-pool-instances": true,
	"enable-global-recoveries": true, "disable-global-recoveries": true,
	"reload-configuration": true, "reset-hostname-resolve-cache": true,
	"agent-umount": true, "agent-mount": true, "agent-removelv": true,
	"agent-create-snapshot": true, "agent-mysql-start": true, "agent-mysql-stop": true,
	"agent-seed": true, "agent-abort-seed": true,
}

// guardWebAction prevents cross-site browser writes and intermediary caching.
// Requests without Origin remain available to non-browser clients.
func guardWebAction(_ Params, r Responder, req *http.Request, resp http.ResponseWriter, user Principal) {
	resp.Header().Set("Cache-Control", "no-store")
	// Check user permissions at ingress; leader readiness is checked by the
	// business handler after follower requests have been proxied to the leader.
	if !isAuthorizedForWrite(req, user) {
		writeHTTPJSON(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "Unauthorized"})
		return
	}
	if req.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeHTTPJSON(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "cross-site action rejected"})
		return
	}
	if origin := req.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != req.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeHTTPJSON(r, http.StatusForbidden, &APIResponse{Code: ERROR, Message: "cross-origin action rejected"})
		}
	}
}

func isWebAction(path string) bool {
	name, _, _ := strings.Cut(path, "/")
	return webActionNames[name]
}
