package metadata

import (
	"context"
	"fmt"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// RegisterCandidateInstance upserts a failover candidate.
func RegisterCandidateInstance(ctx context.Context, candidate modeldomain.CandidateInstance) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into candidate_database_instance (
			hostname, port, promotion_rule, last_suggested
		) values (?, ?, ?, ?)
		on duplicate key update
			last_suggested = values(last_suggested),
			promotion_rule = values(promotion_rule)
	`, candidate.Hostname, candidate.Port, candidate.PromotionRule, candidate.LastSuggested)
	return err
}

// ExpireCandidateInstances removes stale failover candidates.
func ExpireCandidateInstances(ctx context.Context, expiryMinutes uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from candidate_database_instance
		where last_suggested < now() - interval ? minute
	`, expiryMinutes)
	return err
}

// ReadCandidateInstances reads all failover candidates.
func ReadCandidateInstances(ctx context.Context, expiryMinutes uint) ([]modeldomain.CandidateInstance, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.CandidateDatabaseInstance](ctx, `
		select hostname, port, promotion_rule, last_suggested,
			last_suggested + interval ? minute as promotion_rule_expiry
		from candidate_database_instance
	`, expiryMinutes)
	result := make([]modeldomain.CandidateInstance, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.CandidateInstance{
			Hostname:            row.Hostname,
			Port:                row.Port,
			PromotionRule:       row.PromotionRule,
			LastSuggested:       row.LastSuggested,
			PromotionRuleExpiry: row.PromotionRuleExpiry,
		})
	}
	return result, err
}

// WriteClusterDomainName upserts a cluster domain name.
func WriteClusterDomainName(ctx context.Context, clusterName, domainName string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into cluster_domain_name (cluster_name, domain_name, last_registered)
		values (?, ?, now())
		on duplicate key update
			domain_name = values(domain_name),
			last_registered = values(last_registered)
	`, clusterName, domainName)
	return err
}

// ExpireClusterDomainNames removes stale cluster domain names.
func ExpireClusterDomainNames(ctx context.Context, expiryMinutes int) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from cluster_domain_name
		where last_registered < now() - interval ? minute
	`, expiryMinutes)
	return err
}

// WriteMasterPositionEquivalence upserts a bidirectional master coordinate equivalence.
func WriteMasterPositionEquivalence(ctx context.Context, first, second modeldomain.EquivalentCoordinates) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into master_position_equivalence (
			master1_hostname, master1_port, master1_binary_log_file, master1_binary_log_pos,
			master2_hostname, master2_port, master2_binary_log_file, master2_binary_log_pos,
			last_suggested
		) values (?, ?, ?, ?, ?, ?, ?, ?, now())
		on duplicate key update last_suggested = values(last_suggested)
	`, first.Hostname, first.Port, first.BinlogFile, first.BinlogPos,
		second.Hostname, second.Port, second.BinlogFile, second.BinlogPos)
	return err
}

// ReadEquivalentMasterCoordinates reads both directions of a coordinate equivalence.
func ReadEquivalentMasterCoordinates(ctx context.Context, coordinate modeldomain.EquivalentCoordinates) ([]modeldomain.EquivalentCoordinates, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.EquivalentCoordinates](ctx, `
		select master1_hostname as hostname, master1_port as port,
			master1_binary_log_file as binlog_file, master1_binary_log_pos as binlog_pos
		from master_position_equivalence
		where master2_hostname = ? and master2_port = ?
			and master2_binary_log_file = ? and master2_binary_log_pos = ?
		union
		select master2_hostname as hostname, master2_port as port,
			master2_binary_log_file as binlog_file, master2_binary_log_pos as binlog_pos
		from master_position_equivalence
		where master1_hostname = ? and master1_port = ?
			and master1_binary_log_file = ? and master1_binary_log_pos = ?
	`, coordinate.Hostname, coordinate.Port, coordinate.BinlogFile, coordinate.BinlogPos,
		coordinate.Hostname, coordinate.Port, coordinate.BinlogFile, coordinate.BinlogPos)
	result := make([]modeldomain.EquivalentCoordinates, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.EquivalentCoordinates{
			Hostname: row.Hostname, Port: row.Port, BinlogFile: row.BinlogFile, BinlogPos: row.BinlogPos,
		})
	}
	return result, err
}

// ExpireMasterPositionEquivalence removes stale coordinate equivalences.
func ExpireMasterPositionEquivalence(ctx context.Context, expiryHours uint) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		delete from master_position_equivalence
		where last_suggested < now() - interval ? hour
	`, expiryHours)
	return err
}

// PutInstanceTag upserts one instance tag.
func PutInstanceTag(ctx context.Context, hostname string, port int, name, value string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		insert into database_instance_tags (hostname, port, tag_name, tag_value, last_updated)
		values (?, ?, ?, ?, now())
		on duplicate key update
			tag_value = values(tag_value),
			last_updated = values(last_updated)
	`, hostname, port, name, value)
	return err
}

// DeleteInstanceTags returns the affected instance keys and deletes matching tags.
func DeleteInstanceTags(ctx context.Context, hostname string, port int, matchInstance bool, name, value string, hasValue bool) ([]modeldomain.InstanceIdentity, error) {
	clause := `tag_name = ?`
	args := []any{name}
	if hasValue {
		clause += ` and tag_value = ?`
		args = append(args, value)
	}
	if matchInstance {
		clause += ` and hostname = ? and port = ?`
		args = append(args, hostname, port)
	}
	rows, err := readTaggedInstanceKeys(ctx, clause, args...)
	if err != nil {
		return nil, err
	}
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	query := fmt.Sprintf(`delete from database_instance_tags where %s`, clause)
	if _, err := database.ExecOrchestratorContext(ctx, query, args...); err != nil {
		return result, err
	}
	return result, nil
}

// ReadInstanceTagValue reads one tag value for an instance.
func ReadInstanceTagValue(ctx context.Context, hostname string, port int, name string) ([]string, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.TagValue](ctx, `
		select tag_value
		from database_instance_tags
		where hostname = ? and port = ? and tag_name = ?
	`, hostname, port, name)
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Value)
	}
	return result, err
}

// ReadInstanceTags reads all tags for an instance.
func ReadInstanceTags(ctx context.Context, hostname string, port int) ([]modeldomain.InstanceTag, error) {
	rows, err := database.QueryOrchestratorRows[modeldo.InstanceTag](ctx, `
		select tag_name, tag_value
		from database_instance_tags
		where hostname = ? and port = ?
		order by tag_name
	`, hostname, port)
	result := make([]modeldomain.InstanceTag, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceTag(row))
	}
	return result, err
}

// ReadInstanceKeysByTag reads instance keys matching tag presence/value semantics.
func ReadInstanceKeysByTag(ctx context.Context, name, value string, hasValue, negate bool) ([]modeldomain.InstanceIdentity, error) {
	clause := `tag_name = ?`
	args := []any{name}
	switch {
	case hasValue && !negate:
		clause += ` and tag_value = ?`
		args = append(args, value)
	case !hasValue && !negate:
	case hasValue && negate:
		clause += ` and tag_value != ?`
		args = append(args, value)
	case !hasValue && negate:
		clause = `1 = 1 group by hostname, port having sum(tag_name = ?) = 0`
	}
	rows, err := readTaggedInstanceKeys(ctx, clause, args...)
	result := make([]modeldomain.InstanceIdentity, 0, len(rows))
	for _, row := range rows {
		result = append(result, modeldomain.InstanceIdentity(row))
	}
	return result, err
}

func readTaggedInstanceKeys(ctx context.Context, clause string, args ...any) ([]modeldo.InstanceKey, error) {
	query := fmt.Sprintf(`
		select hostname, port
		from database_instance_tags
		where %s
		order by hostname, port
	`, clause)
	return database.QueryOrchestratorRows[modeldo.InstanceKey](ctx, query, args...)
}
