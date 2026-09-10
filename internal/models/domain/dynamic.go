package domain

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

const DateTimeFormat = "2006-01-02 15:04:05.999999"

// CellData preserves the historical snapshot representation: each dynamic
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

// NullString returns the scan/write representation used by database/sql.
func (cell *CellData) NullString() *sql.NullString {
	return (*sql.NullString)(cell)
}

// DynamicRow represents a result whose columns are not known at compile time.
type DynamicRow map[string]CellData

func (row DynamicRow) GetString(key string) string { return row[key].String }

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

func (row DynamicRow) GetNullInt64(key string) NullInt64 {
	cell, ok := row[key]
	if !ok || !cell.Valid {
		return NullInt64{}
	}
	value, err := strconv.ParseInt(cell.String, 10, 64)
	if err != nil {
		return NullInt64{}
	}
	return NullInt64{Int64: value, Valid: true}
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

func (row DynamicRow) GetBool(key string) bool { return row.GetInt(key) != 0 }

func (row DynamicRow) GetTime(key string) time.Time {
	value, err := time.Parse(DateTimeFormat, row.GetString(key))
	if err != nil {
		return time.Time{}
	}
	return value
}

// RowData is one positional dynamic result row.
type RowData []CellData

func (row RowData) MarshalJSON() ([]byte, error) {
	cells := make([]*CellData, len(row))
	for i := range row {
		cells[i] = &row[i]
	}
	return json.Marshal(cells)
}

func (row RowData) Args() []any {
	args := make([]any, len(row))
	for i := range row {
		args[i] = *row[i].NullString()
	}
	return args
}

type ResultData []RowData

// NamedResultData preserves column names alongside dynamic snapshot rows.
type NamedResultData struct {
	Columns []string
	Data    ResultData
}
