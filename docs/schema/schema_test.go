package schema

import (
	_ "embed"
	"regexp"
	"sort"
	"strings"
	"testing"
)

//go:embed query-index-matrix.md
var queryIndexMatrix string

func TestMySQLSchemaContract(t *testing.T) {
	tablePattern := regexp.MustCompile("(?m)^CREATE TABLE IF NOT EXISTS `([a-z0-9_]+)`")
	tableMatches := tablePattern.FindAllStringSubmatch(mysqlSchema, -1)
	if len(tableMatches) != len(managedTables) {
		t.Fatalf("CREATE TABLE count = %d; want %d", len(tableMatches), len(managedTables))
	}

	actualTables := make([]string, 0, len(tableMatches))
	for _, match := range tableMatches {
		actualTables = append(actualTables, match[1])
	}
	sort.Strings(actualTables)
	wantTables := append([]string(nil), managedTables...)
	sort.Strings(wantTables)
	if strings.Join(actualTables, "\n") != strings.Join(wantTables, "\n") {
		t.Fatalf("managed tables do not match schema\ngot:\n%s\nwant:\n%s", strings.Join(actualTables, "\n"), strings.Join(wantTables, "\n"))
	}

	tableDefinitions := regexp.MustCompile("(?s)CREATE TABLE IF NOT EXISTS `([a-z0-9_]+)` \\((.*?)\\) ENGINE").FindAllStringSubmatch(mysqlSchema, -1)
	for _, definition := range tableDefinitions {
		if !strings.Contains(definition[2], "`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT") || !strings.Contains(definition[2], "PRIMARY KEY (`id`)") || strings.Count(definition[2], "AUTO_INCREMENT") != 1 {
			t.Errorf("%s must have exactly one auto-increment id primary key", definition[1])
		}
	}

	upperSchema := strings.ToUpper(mysqlSchema)
	tableOptions := ") ENGINE = INNODB DEFAULT CHARSET = UTF8MB4 COLLATE = UTF8MB4_GENERAL_CI COMMENT ="
	if count := strings.Count(upperSchema, tableOptions); count != len(managedTables) {
		t.Fatalf("normalized table option count = %d; want %d", count, len(managedTables))
	}
	if legacyCharset := regexp.MustCompile(`(?i)\b(?:character\s+set|charset|collate)\s*=?\s*(?:ascii|latin1|utf8)\b`); legacyCharset.MatchString(mysqlSchema) {
		t.Errorf("schema uses a non-utf8mb4 character set or collation: %q", legacyCharset.FindString(mysqlSchema))
	}
	if utf8mb4Columns, utf8mb4BinaryColumns := strings.Count(upperSchema, "CHARACTER SET UTF8MB4"), strings.Count(upperSchema, "CHARACTER SET UTF8MB4 COLLATE UTF8MB4_BIN"); utf8mb4Columns == 0 || utf8mb4Columns != utf8mb4BinaryColumns {
		t.Errorf("explicit utf8mb4 column/binary-collation count = %d/%d; every explicit protocol column must use utf8mb4_bin", utf8mb4Columns, utf8mb4BinaryColumns)
	}

	for lineNumber, line := range strings.Split(mysqlSchema, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "`") && !strings.Contains(line, " COMMENT '") {
			t.Errorf("column definition on line %d has no comment: %s", lineNumber+1, line)
		}
	}

	indexPattern := regexp.MustCompile("(?m)^CREATE (?:UNIQUE )?INDEX `([a-z0-9_]+)`")
	indexMatches := indexPattern.FindAllStringSubmatch(mysqlSchema, -1)
	if len(indexMatches) != 108 {
		t.Fatalf("secondary index count = %d; want 108", len(indexMatches))
	}
	seenIndexes := make(map[string]struct{}, len(indexMatches))
	for _, match := range indexMatches {
		name := match[1]
		if _, exists := seenIndexes[name]; exists {
			t.Errorf("index %q is defined more than once", name)
		}
		seenIndexes[name] = struct{}{}
		if !strings.HasPrefix(name, "idx_") && !strings.HasPrefix(name, "unq_") {
			t.Errorf("index %q does not use idx_ or unq_ prefix", name)
		}
		if len(name) > 64 {
			t.Errorf("index %q exceeds MySQL's 64-byte identifier limit", name)
		}
		if !strings.Contains(queryIndexMatrix, "`"+name+"`") {
			t.Errorf("index %q has no query-matrix entry", name)
		}
	}
	matrixIndexPattern := regexp.MustCompile(`\x60((?:idx|unq)_[a-z0-9_]+)\x60`)
	for _, match := range matrixIndexPattern.FindAllStringSubmatch(queryIndexMatrix, -1) {
		if _, exists := seenIndexes[match[1]]; !exists {
			t.Errorf("query matrix names index %q that is absent from the schema", match[1])
		}
	}

	for marker, want := range map[string]int{
		CanonicalMigrationPending: 2,
		CanonicalMigration:        1,
	} {
		if count := strings.Count(mysqlSchema, "'"+marker+"'"); count != want {
			t.Errorf("migration marker %q occurrence count = %d; want %d", marker, count, want)
		}
	}

	banned := map[string]*regexp.Regexp{
		"MySQL 8.0 collation": regexp.MustCompile(`(?i)utf8mb4_0900`),
		"display width":       regexp.MustCompile(`(?i)\b(?:tinyint|smallint|mediumint|int|bigint)\s*\([0-9]+\)`),
		"enum":                regexp.MustCompile(`(?i)\benum\s*\(`),
		"generated column":    regexp.MustCompile(`(?i)\bgenerated\s+always\b`),
		"functional index":    regexp.MustCompile(`(?i)create\s+(?:unique\s+)?index[^\n]+\(\s*\(`),
		"descending index":    regexp.MustCompile(`(?i)create\s+(?:unique\s+)?index[^;]+\sdesc\b`),
	}
	for feature, pattern := range banned {
		if pattern.MatchString(mysqlSchema) {
			t.Errorf("schema contains unsupported %s syntax", feature)
		}
	}
}

func TestStatementsAreIndependentCopies(t *testing.T) {
	first := Statements()
	second := Statements()
	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("statement counts = %d/%d", len(first), len(second))
	}
	if len(first) != 161 {
		t.Fatalf("statement count = %d; want 161", len(first))
	}
	first[0] = "changed"
	if second[0] == first[0] {
		t.Fatal("Statements returned shared mutable storage")
	}
}

func TestManagedTablesReturnsCopy(t *testing.T) {
	first := ManagedTables()
	second := ManagedTables()
	first[0] = "changed"
	if second[0] == first[0] {
		t.Fatal("ManagedTables returned shared mutable storage")
	}
}
