package authz

import (
	"net/http/httptest"
	"testing"

	"github.com/openark/orchestrator/internal/http/transport"

	"github.com/openark/orchestrator/internal/config"
)

func TestAuthorizationModesPreservePrincipalContracts(t *testing.T) {
	previousReadOnly := config.Current().Server.ReadOnly
	previousMethod := config.Current().Authentication.Method
	previousHeader := config.Current().Authentication.Proxy.UserHeader
	previousPowerUsers := config.Current().Authentication.Power.Users
	previousPowerGroups := config.Current().Authentication.Power.Groups
	t.Cleanup(func() {
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = previousReadOnly })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = previousMethod })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Proxy.UserHeader = previousHeader })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Power.Users = previousPowerUsers })
		config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Power.Groups = previousPowerGroups })
	})

	request := httptest.NewRequest("GET", "/", nil)
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = false })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Power.Groups = nil })

	tests := []struct {
		name      string
		method    string
		principal transport.Principal
		prepare   func()
		want      bool
	}{
		{name: "default", method: "", want: true},
		{name: "basic", method: "basic", principal: "writer", want: true},
		{name: "multi writer", method: "multi", principal: "writer", want: true},
		{name: "multi readonly", method: "multi", principal: "readonly", want: false},
		{
			name:   "proxy power user",
			method: "proxy",
			prepare: func() {
				config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Proxy.UserHeader = "X-Auth-User" })
				config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Power.Users = []string{"admin"} })
				request.Header.Set("X-Auth-User", "admin")
			},
			want: true,
		},
		{name: "token without cookie", method: "token", want: false},
		{name: "malformed token", method: "token", prepare: func() { request.Header.Set("Cookie", "access-token=missing-secret") }, want: false},
		{name: "oauth", method: "oauth", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request.Header = make(map[string][]string)
			config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Proxy.UserHeader = "" })
			config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Power.Users = nil })
			config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = tc.method })
			if tc.prepare != nil {
				tc.prepare()
			}
			if got := ForWrite(request, tc.principal); got != tc.want {
				t.Fatalf("ForWrite() = %t, want %t", got, tc.want)
			}
		})
	}

	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = true })
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Authentication.Method = "basic" })
	if ForWrite(request, "writer") {
		t.Fatal("read-only configuration allowed a mutating action")
	}
}

func TestUninitializedRaftNeverAuthorizesBusinessWrites(t *testing.T) {
	previous := config.Current().Server.ReadOnly
	config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = false })
	t.Cleanup(func() { config.TestUpdate(func(cfg *config.Configuration) { cfg.Server.ReadOnly = previous }) })
	if ForAction(httptest.NewRequest("POST", "/api/discover/db/3306", nil), "writer") {
		t.Fatal("uninitialized Raft authorized a topology write")
	}
}
