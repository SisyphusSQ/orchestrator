package http

import (
	"net/http"

	"github.com/openark/orchestrator/go/observability"
)

// RegisterObservability keeps these routes local and retains router authentication and mTLS checks.
func RegisterObservability(router *Router, prefix string) {
	router.Get(prefix+"/metrics", http.HandlerFunc(observability.ServeMetrics))
	router.Get(prefix+"/health/live", func(_ Params, r Responder) { r.JSON(http.StatusOK, map[string]bool{"live": true}) })
	for _, kind := range []string{"ready", "leader-ready"} {
		router.Get(prefix+"/health/"+kind, func(_ Params, r Responder) {
			h := observability.CurrentHealth()
			ready := h.Ready
			if kind == "leader-ready" {
				ready = h.LeaderReady
			}
			status := http.StatusOK
			if !ready {
				status = http.StatusServiceUnavailable
			}
			r.JSON(status, h)
		})
	}
}
