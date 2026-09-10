package metadata

import (
	"regexp"
	"strings"
	"testing"
)

var instanceWriteWhitespace = regexp.MustCompile(`\s+`)

func normalizeInstanceWriteSQL(statement string) string {
	return strings.TrimSpace(instanceWriteWhitespace.ReplaceAllString(statement, " "))
}

func TestBuildInsertOnDuplicateKeyUpdate(t *testing.T) {
	statement, err := buildInsertOnDuplicateKeyUpdate(
		"database_instance",
		[]string{"hostname", "port", "last_seen"},
		[]string{"?", "?", "NOW()"},
		2,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	got := normalizeInstanceWriteSQL(statement)
	for _, fragment := range []string{
		"INSERT ignore INTO database_instance",
		"(hostname, port, last_seen)",
		"(?, ?, NOW()), (?, ?, NOW())",
		"hostname=VALUES(hostname), port=VALUES(port), last_seen=VALUES(last_seen)",
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("statement %q does not contain %q", got, fragment)
		}
	}
}

func TestBuildInsertOnDuplicateKeyUpdateRejectsInvalidShape(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		columns []string
		values  []string
		rows    int
	}{
		{name: "empty columns", rows: 1},
		{name: "empty rows", columns: []string{"id"}, values: []string{"?"}},
		{name: "shape mismatch", columns: []string{"id"}, values: []string{"?", "?"}, rows: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := buildInsertOnDuplicateKeyUpdate("t", testCase.columns, testCase.values, testCase.rows, false); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
