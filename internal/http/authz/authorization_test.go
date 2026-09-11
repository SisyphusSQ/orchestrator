package authz

import (
	"github.com/openark/orchestrator/internal/http/transport"
	"net/http/httptest"
	"testing"

	"github.com/openark/orchestrator/internal/config"
)

func TestAuthorizationModesPreservePrincipalContracts(t *testing.T) {
	previousReadOnly := config.Config.Server.ReadOnly
	previousMethod := config.Config.Authentication.Method
	previousHeader := config.Config.Authentication.Proxy.UserHeader
	previousPowerUsers := config.Config.Authentication.Power.Users
	previousPowerGroups := config.Config.Authentication.Power.Groups
	t.Cleanup(func() {
		config.Config.Server.ReadOnly = previousReadOnly
		config.Config.Authentication.Method = previousMethod
		config.Config.Authentication.Proxy.UserHeader = previousHeader
		config.Config.Authentication.Power.Users = previousPowerUsers
		config.Config.Authentication.Power.Groups = previousPowerGroups
	})

	request := httptest.NewRequest("GET", "/", nil)
	config.Config.Server.ReadOnly = false
	config.Config.Authentication.Power.Groups = nil

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
				config.Config.Authentication.Proxy.UserHeader = "X-Auth-User"
				config.Config.Authentication.Power.Users = []string{"admin"}
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
			config.Config.Authentication.Proxy.UserHeader = ""
			config.Config.Authentication.Power.Users = nil
			config.Config.Authentication.Method = tc.method
			if tc.prepare != nil {
				tc.prepare()
			}
			if got := ForWrite(request, tc.principal); got != tc.want {
				t.Fatalf("ForWrite() = %t, want %t", got, tc.want)
			}
		})
	}

	config.Config.Server.ReadOnly = true
	config.Config.Authentication.Method = "basic"
	if ForWrite(request, "writer") {
		t.Fatal("read-only configuration allowed a mutating action")
	}
}

func TestUninitializedRaftNeverAuthorizesBusinessWrites(t *testing.T) {
	previous := config.Config.Server.ReadOnly
	config.Config.Server.ReadOnly = false
	t.Cleanup(func() { config.Config.Server.ReadOnly = previous })
	if ForAction(httptest.NewRequest("POST", "/api/discover/db/3306", nil), "writer") {
		t.Fatal("uninitialized Raft authorized a topology write")
	}
}
