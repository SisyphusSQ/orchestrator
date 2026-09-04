package http

import (
	"net/http/httptest"
	"testing"

	"github.com/openark/orchestrator/go/config"
)

func TestAuthorizationModesPreservePrincipalContracts(t *testing.T) {
	previousReadOnly := config.Config.ReadOnly
	previousMethod := config.Config.AuthenticationMethod
	previousHeader := config.Config.AuthUserHeader
	previousPowerUsers := config.Config.PowerAuthUsers
	previousPowerGroups := config.Config.PowerAuthGroups
	t.Cleanup(func() {
		config.Config.ReadOnly = previousReadOnly
		config.Config.AuthenticationMethod = previousMethod
		config.Config.AuthUserHeader = previousHeader
		config.Config.PowerAuthUsers = previousPowerUsers
		config.Config.PowerAuthGroups = previousPowerGroups
	})

	request := httptest.NewRequest("GET", "/", nil)
	config.Config.ReadOnly = false
	config.Config.PowerAuthGroups = nil

	tests := []struct {
		name      string
		method    string
		principal Principal
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
				config.Config.AuthUserHeader = "X-Auth-User"
				config.Config.PowerAuthUsers = []string{"admin"}
				request.Header.Set("X-Auth-User", "admin")
			},
			want: true,
		},
		{name: "token without cookie", method: "token", want: false},
		{name: "oauth", method: "oauth", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request.Header = make(map[string][]string)
			config.Config.AuthUserHeader = ""
			config.Config.PowerAuthUsers = nil
			config.Config.AuthenticationMethod = tc.method
			if tc.prepare != nil {
				tc.prepare()
			}
			if got := isAuthorizedForAction(request, tc.principal); got != tc.want {
				t.Fatalf("isAuthorizedForAction() = %t, want %t", got, tc.want)
			}
		})
	}

	config.Config.ReadOnly = true
	config.Config.AuthenticationMethod = "basic"
	if isAuthorizedForAction(request, "writer") {
		t.Fatal("read-only configuration allowed a mutating action")
	}
}
