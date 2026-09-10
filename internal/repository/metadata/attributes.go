package metadata

import (
	"context"
	"fmt"
	"strings"

	modeldo "github.com/openark/orchestrator/internal/models/do"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

func hostAttributesFromRows(rows []modeldo.HostAttribute) []domain.HostAttributes {
	result := make([]domain.HostAttributes, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.HostAttributes{
			Hostname:        row.Hostname,
			AttributeName:   row.AttributeName,
			AttributeValue:  row.AttributeValue,
			SubmitTimestamp: row.SubmitTimestamp,
			ExpireTimestamp: row.ExpireTimestamp,
		})
	}
	return result
}

// SetHostAttribute persists one host attribute.
func SetHostAttribute(ctx context.Context, hostname, attributeName, attributeValue string) error {
	_, err := database.ExecOrchestratorContext(ctx, `
		replace
			into host_attributes (
				hostname, attribute_name, attribute_value, submit_timestamp, expire_timestamp
			) values (
				?, ?, ?, now(), null
			)
	`, hostname, attributeName, attributeValue)
	return err
}

// ReadHostAttributesByMatch reads attributes using optional regular-expression filters.
func ReadHostAttributesByMatch(ctx context.Context, hostnameMatch, attributeNameMatch, attributeValueMatch string) ([]domain.HostAttributes, error) {
	terms := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if hostnameMatch != "" {
		terms = append(terms, `hostname rlike ?`)
		args = append(args, hostnameMatch)
	}
	if attributeNameMatch != "" {
		terms = append(terms, `attribute_name rlike ?`)
		args = append(args, attributeNameMatch)
	}
	if attributeValueMatch != "" {
		terms = append(terms, `attribute_value rlike ?`)
		args = append(args, attributeValueMatch)
	}
	return readHostAttributes(ctx, terms, args)
}

// ReadHostAttribute reads one exact hostname/name combination.
func ReadHostAttribute(ctx context.Context, hostname, attributeName string) ([]domain.HostAttributes, error) {
	return readHostAttributes(ctx, []string{`hostname = ?`, `attribute_name = ?`}, []any{hostname, attributeName})
}

// ReadHostAttributesByAttribute reads one attribute name and a value pattern.
func ReadHostAttributesByAttribute(ctx context.Context, attributeName, valueMatch string) ([]domain.HostAttributes, error) {
	return readHostAttributes(ctx, []string{`attribute_name = ?`, `attribute_value rlike ?`}, []any{attributeName, valueMatch})
}

func readHostAttributes(ctx context.Context, terms []string, args []any) ([]domain.HostAttributes, error) {
	where := ""
	if len(terms) > 0 {
		where = fmt.Sprintf("where %s", strings.Join(terms, " and "))
	}
	query := fmt.Sprintf(`
		select
			hostname,
			attribute_name,
			attribute_value,
			submit_timestamp,
			ifnull(expire_timestamp, '') as expire_timestamp
		from host_attributes
		%s
		order by hostname, attribute_name
	`, where)
	rows, err := database.QueryOrchestratorRows[modeldo.HostAttribute](ctx, query, args...)
	return hostAttributesFromRows(rows), err
}
