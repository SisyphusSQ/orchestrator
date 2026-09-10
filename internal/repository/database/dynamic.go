package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	metadataschema "github.com/openark/orchestrator/docs/schema"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/observability"
)

func scanRowsToData(rows *sql.Rows, onRow func(modeldomain.RowData) error) error {
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read result columns: %w", err)
	}
	for rows.Next() {
		data := make(modeldomain.RowData, len(columns))
		destinations := make([]interface{}, len(columns))
		for i := range data {
			destinations[i] = data[i].NullString()
		}
		if err := rows.Scan(destinations...); err != nil {
			return fmt.Errorf("scan dynamic result row: %w", err)
		}
		if err := onRow(data); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate dynamic result rows: %w", err)
	}
	return nil
}

func rowDataToDynamicRow(data modeldomain.RowData, columns []string) modeldomain.DynamicRow {
	row := make(modeldomain.DynamicRow, len(columns))
	for i, column := range columns {
		row[column] = data[i]
	}
	return row
}

func queryNamedResultDataContext(ctx context.Context, database *sql.DB, query string, args ...interface{}) (result modeldomain.NamedResultData, returnErr error) {
	if ctx == nil {
		return result, errors.New("dynamic query context is nil")
	}
	if database == nil {
		return result, errors.New("dynamic query database is nil")
	}
	begin := time.Now()
	defer func() { observability.RecordSQL(ctx, "dynamic", begin, returnErr) }()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return result, fmt.Errorf("query dynamic result: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close dynamic result rows: %w", err))
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		return result, fmt.Errorf("read dynamic result columns: %w", err)
	}
	result.Columns = append([]string(nil), columns...)
	result.Data = make(modeldomain.ResultData, 0)
	if err := scanRowsToData(rows, func(data modeldomain.RowData) error {
		result.Data = append(result.Data, data)
		return nil
	}); err != nil {
		return result, err
	}
	return result, nil
}

func QueryDynamicRowsContext(ctx context.Context, database *sql.DB, query string, onRow func(modeldomain.DynamicRow) error, args ...interface{}) (returnErr error) {
	if ctx == nil {
		return errors.New("dynamic query context is nil")
	}
	if database == nil {
		return errors.New("dynamic query database is nil")
	}
	if onRow == nil {
		return errors.New("dynamic row callback is nil")
	}
	begin := time.Now()
	defer func() { observability.RecordSQL(ctx, "dynamic", begin, returnErr) }()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query dynamic rows: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close dynamic rows: %w", err))
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read dynamic row columns: %w", err)
	}
	return scanRowsToData(rows, func(data modeldomain.RowData) error {
		return onRow(rowDataToDynamicRow(data, columns))
	})
}

func QueryDynamicRows(database *sql.DB, query string, onRow func(modeldomain.DynamicRow) error, args ...interface{}) error {
	return QueryDynamicRowsContext(context.Background(), database, query, onRow, args...)
}

func QueryResultDataContext(ctx context.Context, database *sql.DB, query string, args ...interface{}) (modeldomain.ResultData, error) {
	result, err := queryNamedResultDataContext(ctx, database, query, args...)
	return result.Data, err
}

func QueryResultData(database *sql.DB, query string, args ...interface{}) (modeldomain.ResultData, error) {
	return QueryResultDataContext(context.Background(), database, query, args...)
}

var safeSQLIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateSQLIdentifier(identifier string) error {
	if !safeSQLIdentifier.MatchString(identifier) {
		return fmt.Errorf("invalid SQL identifier %q", identifier)
	}
	return nil
}

func ScanTableContext(ctx context.Context, database *sql.DB, tableName string) (modeldomain.NamedResultData, error) {
	if err := validateSQLIdentifier(tableName); err != nil {
		return modeldomain.NamedResultData{}, err
	}
	result, err := queryNamedResultDataContext(ctx, database, "select * from "+tableName)
	if err != nil {
		return modeldomain.NamedResultData{}, err
	}
	return snapshotIdentityColumns(tableName, result, true)
}

func WriteTableContext(ctx context.Context, database *sql.DB, tableName string, data modeldomain.NamedResultData) (returnErr error) {
	if err := validateSQLIdentifier(tableName); err != nil {
		return err
	}
	var err error
	data, err = snapshotIdentityColumns(tableName, data, false)
	if err != nil {
		return err
	}
	if len(data.Data) == 0 || len(data.Columns) == 0 {
		return nil
	}
	for _, column := range data.Columns {
		if err := validateSQLIdentifier(column); err != nil {
			return err
		}
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(data.Columns)), ",")
	query := fmt.Sprintf("replace into %s (%s) values (%s)", tableName, strings.Join(data.Columns, ","), placeholders)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot table write: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback snapshot table write: %w", err))
		}
	}()
	for rowIndex, row := range data.Data {
		if len(row) != len(data.Columns) {
			return fmt.Errorf("snapshot row %d has %d cells; want %d", rowIndex, len(row), len(data.Columns))
		}
		if _, err := tx.ExecContext(ctx, query, row.Args()...); err != nil {
			return fmt.Errorf("write snapshot row %d: %w", rowIndex, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot table write: %w", err)
	}
	return nil
}

func NilIfZero(value int64) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

// snapshotIdentityColumns 保持快照中的历史编号名称，同时排除节点本地新增代理键。
func snapshotIdentityColumns(table string, data modeldomain.NamedResultData, export bool) (modeldomain.NamedResultData, error) {
	result := modeldomain.NamedResultData{}
	old := metadataschema.LegacyAutoID(table)
	localID := len(metadataschema.BusinessKey(table)) > 0
	var positions []int
	seen := make(map[string]bool)
	for i, column := range data.Columns {
		if localID && column == "id" {
			continue
		}
		if old != "" {
			if export && column == "id" {
				column = old
			}
			if !export && column == old {
				column = "id"
			}
		}
		if seen[column] {
			return modeldomain.NamedResultData{}, fmt.Errorf("duplicate snapshot column %s for %s", column, table)
		}
		seen[column] = true
		result.Columns = append(result.Columns, column)
		positions = append(positions, i)
	}
	if len(data.Data) > 0 {
		required := metadataschema.BusinessKey(table)
		if old != "" {
			identity := "id"
			if export {
				identity = old
			}
			required = []string{identity}
		}
		for _, column := range required {
			if !seen[column] {
				return modeldomain.NamedResultData{}, fmt.Errorf("snapshot for %s is missing identity column %s", table, column)
			}
		}
	}
	for rowIndex, row := range data.Data {
		if len(row) != len(data.Columns) {
			return modeldomain.NamedResultData{}, fmt.Errorf("snapshot row %d has %d cells; want %d", rowIndex, len(row), len(data.Columns))
		}
		projected := modeldomain.RowData{}
		for _, i := range positions {
			projected = append(projected, row[i])
		}
		result.Data = append(result.Data, projected)
	}
	return result, nil
}
