package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DateTimeFormat = "2006-01-02 15:04:05.999999"

// CellData preserves the historical snapshot representation: every dynamic
// database value is serialized as a string or JSON null.
type CellData sql.NullString

func (cell CellData) MarshalJSON() ([]byte, error) {
	if !cell.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(cell.String)
}

func (cell *CellData) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		cell.String = ""
		cell.Valid = false
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	cell.String = value
	cell.Valid = true
	return nil
}

func (cell *CellData) nullString() *sql.NullString {
	return (*sql.NullString)(cell)
}

// DynamicRow is intentionally limited to topology and other result sets whose
// columns are not known at compile time. Stable backend reads use typed DTOs.
type DynamicRow map[string]CellData

func (row DynamicRow) GetString(key string) string {
	return row[key].String
}

func (row DynamicRow) GetStringD(key, fallback string) string {
	if cell, ok := row[key]; ok {
		return cell.String
	}
	return fallback
}

func (row DynamicRow) GetInt64(key string) int64 {
	value, _ := strconv.ParseInt(row.GetString(key), 10, 64)
	return value
}

func (row DynamicRow) GetNullInt64(key string) sql.NullInt64 {
	cell, ok := row[key]
	if !ok || !cell.Valid {
		return sql.NullInt64{}
	}
	value, err := strconv.ParseInt(cell.String, 10, 64)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}

func (row DynamicRow) GetInt(key string) int {
	value, _ := strconv.Atoi(row.GetString(key))
	return value
}

func (row DynamicRow) GetIntD(key string, fallback int) int {
	value, err := strconv.Atoi(row.GetString(key))
	if err != nil {
		return fallback
	}
	return value
}

func (row DynamicRow) GetUint(key string) uint {
	value, _ := strconv.ParseUint(row.GetString(key), 10, 0)
	return uint(value)
}

func (row DynamicRow) GetUintD(key string, fallback uint) uint {
	value, err := strconv.ParseUint(row.GetString(key), 10, 0)
	if err != nil {
		return fallback
	}
	return uint(value)
}

func (row DynamicRow) GetUint64(key string) uint64 {
	value, _ := strconv.ParseUint(row.GetString(key), 10, 64)
	return value
}

func (row DynamicRow) GetUint64D(key string, fallback uint64) uint64 {
	value, err := strconv.ParseUint(row.GetString(key), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func (row DynamicRow) GetBool(key string) bool {
	return row.GetInt(key) != 0
}

func (row DynamicRow) GetTime(key string) time.Time {
	value, err := time.Parse(DateTimeFormat, row.GetString(key))
	if err != nil {
		return time.Time{}
	}
	return value
}

type RowData []CellData

func (row RowData) MarshalJSON() ([]byte, error) {
	cells := make([]*CellData, len(row))
	for i := range row {
		cells[i] = &row[i]
	}
	return json.Marshal(cells)
}

func (row RowData) Args() []interface{} {
	args := make([]interface{}, len(row))
	for i := range row {
		args[i] = *row[i].nullString()
	}
	return args
}

type ResultData []RowData

type NamedResultData struct {
	Columns []string
	Data    ResultData
}

func scanRowsToData(rows *sql.Rows, onRow func(RowData) error) error {
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read result columns: %w", err)
	}
	for rows.Next() {
		data := make(RowData, len(columns))
		destinations := make([]interface{}, len(columns))
		for i := range data {
			destinations[i] = data[i].nullString()
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

func rowDataToDynamicRow(data RowData, columns []string) DynamicRow {
	row := make(DynamicRow, len(columns))
	for i, column := range columns {
		row[column] = data[i]
	}
	return row
}

func queryNamedResultDataContext(ctx context.Context, database *sql.DB, query string, args ...interface{}) (result NamedResultData, returnErr error) {
	if ctx == nil {
		return result, errors.New("dynamic query context is nil")
	}
	if database == nil {
		return result, errors.New("dynamic query database is nil")
	}
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
	result.Data = make(ResultData, 0)
	if err := scanRowsToData(rows, func(data RowData) error {
		result.Data = append(result.Data, data)
		return nil
	}); err != nil {
		return result, err
	}
	return result, nil
}

func QueryDynamicRowsContext(ctx context.Context, database *sql.DB, query string, onRow func(DynamicRow) error, args ...interface{}) (returnErr error) {
	if ctx == nil {
		return errors.New("dynamic query context is nil")
	}
	if database == nil {
		return errors.New("dynamic query database is nil")
	}
	if onRow == nil {
		return errors.New("dynamic row callback is nil")
	}
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
	return scanRowsToData(rows, func(data RowData) error {
		return onRow(rowDataToDynamicRow(data, columns))
	})
}

func QueryDynamicRows(database *sql.DB, query string, onRow func(DynamicRow) error, args ...interface{}) error {
	return QueryDynamicRowsContext(context.Background(), database, query, onRow, args...)
}

func QueryResultDataContext(ctx context.Context, database *sql.DB, query string, args ...interface{}) (ResultData, error) {
	result, err := queryNamedResultDataContext(ctx, database, query, args...)
	return result.Data, err
}

func QueryResultData(database *sql.DB, query string, args ...interface{}) (ResultData, error) {
	return QueryResultDataContext(context.Background(), database, query, args...)
}

var safeSQLIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateSQLIdentifier(identifier string) error {
	if !safeSQLIdentifier.MatchString(identifier) {
		return fmt.Errorf("invalid SQL identifier %q", identifier)
	}
	return nil
}

func ScanTableContext(ctx context.Context, database *sql.DB, tableName string) (NamedResultData, error) {
	if err := validateSQLIdentifier(tableName); err != nil {
		return NamedResultData{}, err
	}
	return queryNamedResultDataContext(ctx, database, "select * from "+tableName)
}

func WriteTableContext(ctx context.Context, database *sql.DB, tableName string, data NamedResultData) (returnErr error) {
	if err := validateSQLIdentifier(tableName); err != nil {
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
