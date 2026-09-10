package domain

import (
	"database/sql"
	"database/sql/driver"
)

// NullBool is a nullable boolean that preserves database/sql scan and value semantics.
type NullBool sql.NullBool

func (value *NullBool) Scan(src any) error { return (*sql.NullBool)(value).Scan(src) }
func (value NullBool) Value() (driver.Value, error) {
	return sql.NullBool(value).Value()
}

// NullInt64 is a nullable integer that preserves database/sql scan and value semantics.
type NullInt64 sql.NullInt64

func (value *NullInt64) Scan(src any) error { return (*sql.NullInt64)(value).Scan(src) }
func (value NullInt64) Value() (driver.Value, error) {
	return sql.NullInt64(value).Value()
}
func (value NullInt64) SQLNullInt64() sql.NullInt64 { return sql.NullInt64(value) }

// NullString is a nullable string that preserves database/sql scan and value semantics.
type NullString sql.NullString

func (value *NullString) Scan(src any) error { return (*sql.NullString)(value).Scan(src) }
func (value NullString) Value() (driver.Value, error) {
	return sql.NullString(value).Value()
}
