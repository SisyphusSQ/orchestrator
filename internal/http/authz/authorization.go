package authz

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/http/transport"
	"github.com/openark/orchestrator/internal/os"
	"github.com/openark/orchestrator/internal/process"
	orcraft "github.com/openark/orchestrator/internal/raft"
)

func proxyUser(req *http.Request) string {
	for _, user := range req.Header[config.Config.Authentication.Proxy.UserHeader] {
		return user
	}
	return ""
}

// ForWrite reports whether the authenticated principal may mutate local state.
func ForWrite(req *http.Request, user transport.Principal) bool {
	if config.Config.Server.ReadOnly {
		return false
	}

	switch strings.ToLower(config.Config.Authentication.Method) {
	case "basic":
		return true
	case "multi":
		return string(user) != "readonly"
	case "proxy":
		authUser := proxyUser(req)
		for _, allowed := range config.Config.Authentication.Power.Users {
			if allowed == "*" || allowed == authUser {
				return true
			}
		}
		return len(config.Config.Authentication.Power.Groups) > 0 && os.UserInGroups(authUser, config.Config.Authentication.Power.Groups)
	case "token":
		cookie, err := req.Cookie("access-token")
		if err != nil {
			return false
		}
		publicToken, secretToken, ok := strings.Cut(cookie.Value, ":")
		if !ok || publicToken == "" || secretToken == "" {
			return false
		}
		valid, _ := process.TokenIsValid(publicToken, secretToken)
		return valid
	case "oauth":
		return false
	default:
		return true
	}
}

// ForAction additionally requires verified Raft leader readiness.
func ForAction(req *http.Request, user transport.Principal) bool {
	return ForWrite(req, user) && orcraft.IsLeaderReady()
}

// ForConfiguration is narrower than topology write access because hook
// commands execute with the orchestrator process identity.
func ForConfiguration(req *http.Request, user transport.Principal) bool {
	if !ForAction(req, user) {
		return false
	}
	if strings.TrimSpace(config.Config.Authentication.Method) == "" {
		return true
	}
	userID := UserID(req, user)
	for _, allowed := range config.Config.Authentication.ConfigurationAdmins.Users {
		if allowed == "*" || allowed == userID {
			return true
		}
	}
	return userID != "" && len(config.Config.Authentication.ConfigurationAdmins.Groups) > 0 && os.UserInGroups(userID, config.Config.Authentication.ConfigurationAdmins.Groups)
}

// AuthenticateToken exchanges a public token for the cookie used by token auth.
func AuthenticateToken(publicToken string, resp http.ResponseWriter) error {
	secretToken, err := process.AcquireAccessToken(publicToken)
	if err != nil {
		return err
	}
	cookieValue := fmt.Sprintf("%s:%s", publicToken, secretToken)
	http.SetCookie(resp, &http.Cookie{Name: "access-token", Value: cookieValue, Path: "/"})
	return nil
}

// UserID returns the authenticated user identifier when the configured method
// exposes one.
func UserID(req *http.Request, user transport.Principal) string {
	if config.Config.Server.ReadOnly {
		return ""
	}
	switch strings.ToLower(config.Config.Authentication.Method) {
	case "basic", "multi":
		return string(user)
	case "proxy":
		return proxyUser(req)
	default:
		return ""
	}
}
