package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// WriteResolvedHostname upserts current resolution and best-effort history.
func WriteResolvedHostname(ctx context.Context, hostname, resolvedHostname string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into hostname_resolve (hostname, resolved_hostname, resolved_timestamp)
		values (?, ?, now())
		on duplicate key update
			resolved_hostname = values(resolved_hostname),
			resolved_timestamp = values(resolved_timestamp)
	`, hostname, resolvedHostname)
	if err != nil {
		return err
	}
	if hostname != resolvedHostname {
		// Preserve the legacy best-effort history behavior.
		_, _ = database.ExecOrchestratorContext(ctx, `
			insert into hostname_resolve_history (hostname, resolved_hostname, resolved_timestamp)
			values (?, ?, now())
			on duplicate key update
			hostname = values(hostname), resolved_timestamp = values(resolved_timestamp)
		`, hostname, resolvedHostname)
	}
	return nil
}

func ReadResolvedHostname(ctx context.Context, hostname string) ([]modeldomain.HostnameResolve, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HostnameResolve](ctx, `
		select hostname, resolved_hostname from hostname_resolve where hostname = ?
	`, hostname)
	result := make([]modeldomain.HostnameResolve, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.HostnameResolve(row))
	}
	return result, err
}

func ReadAllHostnameResolves(ctx context.Context) ([]modeldomain.HostnameResolve, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HostnameResolve](ctx, `
		select hostname, resolved_hostname from hostname_resolve
	`)
	result := make([]modeldomain.HostnameResolve, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.HostnameResolve(row))
	}
	return result, err
}

func ReadAllHostnameUnresolves(ctx context.Context) ([]modeldomain.HostnameUnresolve, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HostnameUnresolve](ctx, `
		select hostname, unresolved_hostname from hostname_unresolve
	`)
	result := make([]modeldomain.HostnameUnresolve, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.HostnameUnresolve(row))
	}
	return result, err
}

func ReadUnresolvedHostname(ctx context.Context, hostname string) ([]modeldomain.HostnameUnresolve, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HostnameUnresolve](ctx, `
		select hostname, unresolved_hostname from hostname_unresolve where hostname = ?
	`, hostname)
	result := make([]modeldomain.HostnameUnresolve, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.HostnameUnresolve(row))
	}
	return result, err
}

func ReadMissingHostnameResolves(ctx context.Context) ([]modeldomain.MissingHostnameResolve, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.MissingHostnameResolve](ctx, `
		select hostname_unresolve.unresolved_hostname, database_instance.port
		from database_instance
		join hostname_unresolve on database_instance.hostname = hostname_unresolve.hostname
		left join hostname_resolve on database_instance.hostname = hostname_resolve.resolved_hostname
		where hostname_resolve.hostname is null
	`)
	result := make([]modeldomain.MissingHostnameResolve, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.MissingHostnameResolve(row))
	}
	return result, err
}

// WriteHostnameUnresolve upserts current reverse resolution and best-effort history.
func WriteHostnameUnresolve(ctx context.Context, hostname, unresolvedHostname string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into hostname_unresolve (hostname, unresolved_hostname, last_registered)
		values (?, ?, now())
		on duplicate key update
			unresolved_hostname = values(unresolved_hostname), last_registered = now()
	`, hostname, unresolvedHostname)
	if err != nil {
		return err
	}
	// Preserve the legacy best-effort history behavior.
	_, _ = database.ExecOrchestratorContext(ctx, `
		replace into hostname_unresolve_history (hostname, unresolved_hostname, last_registered)
		values (?, ?, now())
	`, hostname, unresolvedHostname)
	return nil
}

func DeleteHostnameUnresolve(ctx context.Context, hostname string) error {
	_, err := database.ExecOrchestratorContext(ctx, `delete from hostname_unresolve where hostname = ?`, hostname)
	return err
}

func ExpireHostnameUnresolve(ctx context.Context, expiryMinutes int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from hostname_unresolve where last_registered < now() - interval ? minute
	`, expiryMinutes)
	return err
}

func ForgetExpiredHostnameResolves(ctx context.Context, expiryMinutes int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from hostname_resolve where resolved_timestamp < now() - interval ? minute
	`, expiryMinutes)
	return err
}

func ReadInvalidHostnameResolves(ctx context.Context) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.Hostname](ctx, `
		select early.hostname
		from hostname_resolve as latest
		join hostname_resolve early
			on latest.resolved_hostname = early.hostname and latest.hostname = early.resolved_hostname
		where latest.hostname != latest.resolved_hostname
			and latest.resolved_timestamp > early.resolved_timestamp
	`)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Hostname)
	}
	return result, err
}

func DeleteResolvedHostname(ctx context.Context, hostname string) error {
	_, err := database.ExecOrchestratorContext(ctx, `delete from hostname_resolve where hostname = ?`, hostname)
	return err
}

func DeleteAllHostnameResolves(ctx context.Context) error {
	_, err := database.ExecOrchestratorContext(ctx, `delete from hostname_resolve`)
	return err
}

func WriteHostnameIPs(ctx context.Context, hostname, ipv4, ipv6 string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into hostname_ips (hostname, ipv4, ipv6, last_updated)
		values (?, ?, ?, now())
		on duplicate key update
			ipv4 = values(ipv4), ipv6 = values(ipv6), last_updated = values(last_updated)
	`, hostname, ipv4, ipv6)
	return err
}

func ReadHostnameIPs(ctx context.Context, hostname string) ([]modeldomain.HostnameIP, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.HostnameIP](ctx, `
		select ipv4, ipv6 from hostname_ips where hostname = ?
	`, hostname)
	result := make([]modeldomain.HostnameIP, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.HostnameIP(row))
	}
	return result, err
}
