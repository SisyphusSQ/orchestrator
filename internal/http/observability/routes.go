package observability

import (
	"net/http"

	"github.com/openark/orchestrator/internal/http/presenter"
	"github.com/openark/orchestrator/internal/http/transport"
	observabilityruntime "github.com/openark/orchestrator/internal/observability"
)

// RegisterObservability keeps these routes local and retains router authentication and mTLS checks.
func Register(router *transport.Router, prefix string) {
	router.Get(prefix+"/metrics", http.HandlerFunc(observabilityruntime.ServeMetrics))
	router.Get(prefix+"/health/live", func(_ transport.Params, r transport.Responder) {
		presenter.WriteJSON(r, http.StatusOK, map[string]bool{"live": true})
	})
	for _, kind := range []string{"ready", "leader-ready"} {
		router.Get(prefix+"/health/"+kind, func(_ transport.Params, r transport.Responder) {
			h := observabilityruntime.CurrentHealth()
			ready := h.Ready
			if kind == "leader-ready" {
				ready = h.LeaderReady
			}
			status := http.StatusOK
			if !ready {
				status = http.StatusServiceUnavailable
			}
			presenter.WriteJSON(r, status, h)
		})
	}
}
