package domain

import (
	"database/sql"
	"encoding/json"
	"testing"
)

func TestNullableScalarsPreserveSQLAndJSONSemantics(t *testing.T) {
	var integer NullInt64
	if err := integer.Scan([]byte("42")); err != nil {
		t.Fatalf("scan integer: %v", err)
	}
	if integer.Int64 != 42 || !integer.Valid {
		t.Fatalf("integer = %#v; want 42, valid", integer)
	}
	if got, err := integer.Value(); err != nil || got != int64(42) {
		t.Fatalf("integer database value = %#v, %v; want 42, nil", got, err)
	}

	var boolean NullBool
	if err := boolean.Scan(int64(1)); err != nil {
		t.Fatalf("scan boolean: %v", err)
	}
	if !boolean.Bool || !boolean.Valid {
		t.Fatalf("boolean = %#v; want true, valid", boolean)
	}

	var text NullString
	if err := text.Scan(nil); err != nil {
		t.Fatalf("scan null string: %v", err)
	}
	if text.Valid {
		t.Fatalf("text = %#v; want invalid", text)
	}

	want, err := json.Marshal(sql.NullInt64{Int64: 42, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(integer)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("nullable integer JSON = %s; want %s", got, want)
	}
}
