package raft

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/openark/orchestrator/internal/golib/log"

	raftcore "github.com/openark/orchestrator/internal/raft"

	"github.com/openark/orchestrator/internal/observability"
)

func ReverseProxy(w http.ResponseWriter, r *http.Request) {
	if !raftcore.IsInitialized() {
		// Local reads may report startup status; action handlers still require a verified leader.
		return
	}
	if raftcore.IsLeader() {
		// I am the leader. I will handle the request directly.
		return
	}
	if raftcore.GetLeader() == "" {
		return
	}
	if raftcore.LeaderURI.IsThisLeaderURI() {
		// Although I'm not the leader, the value I see for LeaderURI is my own.
		// I'm probably not up-to-date with my raft transaction log and don't have the latest information.
		// But anyway, obviously not going to redirect to myself.
		// Gonna return: this isn't ideal, because I'm not really the leader. If the user tries to
		// run an operation they'll fail.
		return
	}
	url, err := url.Parse(raftcore.LeaderURI.Get())
	if err != nil {
		log.Errore(err)
		return
	}
	r.Header.Del("Accept-Encoding")
	proxy := httputil.NewSingleHostReverseProxy(url)
	proxy.Transport, err = raftcore.GetRaftHttpTransport()
	if err != nil {
		log.Errore(err)
		return
	}
	observability.InjectTrace(r)
	proxy.ServeHTTP(w, r)
}
