package metadata

import (
	"context"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// WriteAudit inserts one backend audit record.
func WriteAudit(ctx context.Context, auditType, hostname string, port int, clusterName, message string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into audit (audit_timestamp, audit_type, hostname, port, cluster_name, message)
		values (now(), ?, ?, ?, ?, ?)
	`, auditType, hostname, port, clusterName, message)
	return err
}

// ReadRecentAudit reads one page of audit records, optionally for one instance.
func ReadRecentAudit(ctx context.Context, hostname string, port int, matchInstance bool, limit, offset int) ([]modeldomain.AuditRecord, error) {
	query := `
		select id, audit_timestamp, audit_type, hostname, port, message
		from audit
	`
	args := make([]any, 0, 4)
	if matchInstance {
		query += ` where hostname = ? and port = ?`
		args = append(args, hostname, port)
	}
	query += ` order by audit_timestamp desc limit ? offset ?`
	args = append(args, limit, offset)
	rows, err := database.QueryOrchestratorRows[modeldo.Audit](ctx, query, args...)
	result := make([]modeldomain.AuditRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.AuditRecord(row))
	}
	return result, err
}

// ExpireAudit removes old audit records.
func ExpireAudit(ctx context.Context, purgeDays uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from audit where audit_timestamp < now() - interval ? day
	`, purgeDays)
	return err
}
