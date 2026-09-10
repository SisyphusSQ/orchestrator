package process

import (
	"context"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/metadata"
	"github.com/openark/orchestrator/internal/util"
)

// GenerateAccessToken creates a new token and returns its public portion.
func GenerateAccessToken(owner string) (string, error) {
	publicToken := util.NewToken().Hash
	secretToken := util.NewToken().Hash
	if err := metadata.CreateAccessToken(context.Background(), publicToken, secretToken, owner); err != nil {
		return publicToken, log.Errore(err)
	}
	return publicToken, nil
}

// AcquireAccessToken acquires a usable token and returns its secret portion.
func AcquireAccessToken(publicToken string) (string, error) {
	secretToken, acquired, err := metadata.AcquireAccessToken(
		context.Background(),
		publicToken,
		config.Config.Authentication.AccessToken.UseExpirySeconds,
	)
	if err != nil {
		return "", log.Errore(err)
	}
	if !acquired {
		return "", log.Errorf("Cannot acquire token %s", publicToken)
	}
	return secretToken, nil
}

// TokenIsValid checks whether a token exists and has not expired.
func TokenIsValid(publicToken, secretToken string) (bool, error) {
	valid, err := metadata.AccessTokenIsValid(
		context.Background(),
		publicToken,
		secretToken,
		config.Config.Authentication.AccessToken.ExpiryMinutes,
	)
	return valid, log.Errore(err)
}

// ExpireAccessTokens removes old non-reentrant tokens.
func ExpireAccessTokens() error {
	err := metadata.ExpireAccessTokens(context.Background(), config.Config.Authentication.AccessToken.ExpiryMinutes)
	return log.Errore(err)
}
