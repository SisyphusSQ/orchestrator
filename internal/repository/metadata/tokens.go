package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	"github.com/openark/orchestrator/internal/repository/database"
)

// CreateAccessToken persists a new public/secret token pair.
func CreateAccessToken(ctx context.Context, publicToken, secretToken, owner string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into access_token (
			public_token, secret_token, generated_at, generated_by, is_acquired, is_reentrant
		) values (
			?, ?, now(), ?, 0, 0
		)
	`, publicToken, secretToken, owner)
	return err
}

// AcquireAccessToken atomically marks a usable token as acquired and returns its secret.
func AcquireAccessToken(ctx context.Context, publicToken string, useExpirySeconds uint) (string, bool, error) {
	result, err := database.ExecOrchestratorContext(ctx, `
		update access_token
		set is_acquired = 1, acquired_at = now()
		where public_token = ?
			and ((is_acquired = 0 and generated_at > now() - interval ? second) or is_reentrant = 1)
	`, publicToken, useExpirySeconds)
	if err != nil {
		return "", false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return "", false, err
	}
	rows, err := database.QueryOrchestratorRows[modeldo.AccessTokenSecret](ctx, `
		select secret_token
		from access_token
		where public_token = ?
	`, publicToken)
	if err != nil || len(rows) == 0 {
		return "", true, err
	}
	return rows[0].SecretToken, true, nil
}

// AccessTokenIsValid checks an acquired token against its expiry policy.
func AccessTokenIsValid(ctx context.Context, publicToken, secretToken string, expiryMinutes uint) (bool, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.AccessTokenValidity](ctx, `
		select count(*) as valid_token
		from access_token
		where public_token = ?
			and secret_token = ?
			and (generated_at >= now() - interval ? minute or is_reentrant = 1)
	`, publicToken, secretToken, expiryMinutes)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	return rows[0].Count > 0, nil
}

// ExpireAccessTokens removes old non-reentrant tokens.
func ExpireAccessTokens(ctx context.Context, expiryMinutes uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from access_token
		where generated_at < now() - interval ? minute
			and is_reentrant = 0
	`, expiryMinutes)
	return err
}
