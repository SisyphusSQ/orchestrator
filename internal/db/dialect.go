package db

import (
	"regexp"
	"strings"
)

type regexpMap struct {
	pattern     *regexp.Regexp
	replacement string
}

func newRegexpMap(expression, replacement string) regexpMap {
	return regexpMap{
		pattern:     regexp.MustCompile(strings.ReplaceAll(expression, " ", `\s+`)),
		replacement: replacement,
	}
}

func applyConversions(statement string, conversions []regexpMap) string {
	for _, conversion := range conversions {
		statement = conversion.pattern.ReplaceAllString(statement, conversion.replacement)
	}
	return statement
}

var sqliteCreateTableConversions = []regexpMap{
	newRegexpMap(`(?i) (character set|charset) [\S]+`, ``),
	newRegexpMap(`(?i)int unsigned`, `int`),
	newRegexpMap(`(?i)int[\s]*[(][\s]*([0-9]+)[\s]*[)] unsigned`, `int`),
	newRegexpMap(`(?i)engine[\s]*=[\s]*(innodb|myisam|ndb|memory|tokudb)`, ``),
	newRegexpMap(`(?i)DEFAULT CHARSET[\s]*=[\s]*[\S]+`, ``),
	newRegexpMap(`(?i)[\S]*int( not null|) auto_increment`, `integer`),
	newRegexpMap(`(?i)comment '[^']*'`, ``),
	newRegexpMap(`(?i)after [\S]+`, ``),
	newRegexpMap(`(?i)alter table ([\S]+) add (index|key) ([\S]+) (.+)`, `create index ${3}_${1} on $1 $4`),
	newRegexpMap(`(?i)alter table ([\S]+) add unique (index|key) ([\S]+) (.+)`, `create unique index ${3}_${1} on $1 $4`),
	newRegexpMap(`(?i)([\S]+) enum[\s]*([(].*?[)])`, `$1 text check($1 in $2)`),
	newRegexpMap(`(?i)([\s\S]+[/][*] sqlite3-skip [*][/][\s\S]+)`, ``),
	newRegexpMap(`(?i)timestamp default current_timestamp`, `timestamp default ('')`),
	newRegexpMap(`(?i)timestamp not null default current_timestamp`, `timestamp not null default ('')`),
	newRegexpMap(`(?i)add column (.*int) not null[\s]*$`, `add column $1 not null default 0`),
	newRegexpMap(`(?i)add column (.* text) not null[\s]*$`, `add column $1 not null default ''`),
	newRegexpMap(`(?i)add column (.* varchar.*) not null[\s]*$`, `add column $1 not null default ''`),
}

var sqliteInsertConversions = []regexpMap{
	newRegexpMap(`(?i)insert ignore ([\s\S]+) on duplicate key update [\s\S]+`, `insert or ignore $1`),
	newRegexpMap(`(?i)insert ignore`, `insert or ignore`),
	newRegexpMap(`(?i)now[(][)]`, `datetime('now')`),
	newRegexpMap(`(?i)insert into ([\s\S]+) on duplicate key update [\s\S]+`, `replace into $1`),
}

var sqliteGeneralConversions = []regexpMap{
	newRegexpMap(`(?i)now[(][)][\s]*[-][\s]*interval [?] ([\w]+)`, `datetime('now', printf('-%d $1', ?))`),
	newRegexpMap(`(?i)now[(][)][\s]*[+][\s]*interval [?] ([\w]+)`, `datetime('now', printf('+%d $1', ?))`),
	newRegexpMap(`(?i)now[(][)][\s]*[-][\s]*interval ([0-9.]+) ([\w]+)`, `datetime('now', '-${1} $2')`),
	newRegexpMap(`(?i)now[(][)][\s]*[+][\s]*interval ([0-9.]+) ([\w]+)`, `datetime('now', '+${1} $2')`),
	newRegexpMap(`(?i)[=<>\s]([\S]+[.][\S]+)[\s]*[-][\s]*interval [?] ([\w]+)`, ` datetime($1, printf('-%d $2', ?))`),
	newRegexpMap(`(?i)[=<>\s]([\S]+[.][\S]+)[\s]*[+][\s]*interval [?] ([\w]+)`, ` datetime($1, printf('+%d $2', ?))`),
	newRegexpMap(`(?i)unix_timestamp[(][)]`, `strftime('%s', 'now')`),
	newRegexpMap(`(?i)unix_timestamp[(]([^)]+)[)]`, `strftime('%s', $1)`),
	newRegexpMap(`(?i)now[(][)]`, `datetime('now')`),
	newRegexpMap(`(?i)cast[(][\s]*([\S]+) as signed[\s]*[)]`, `cast($1 as integer)`),
	newRegexpMap(`(?i)\bconcat[(][\s]*([^,)]+)[\s]*,[\s]*([^,)]+)[\s]*[)]`, `($1 || $2)`),
	newRegexpMap(`(?i)\bconcat[(][\s]*([^,)]+)[\s]*,[\s]*([^,)]+)[\s]*,[\s]*([^,)]+)[\s]*[)]`, `($1 || $2 || $3)`),
	newRegexpMap(`(?i) rlike `, ` like `),
	newRegexpMap(`(?i)create index([\s\S]+)[(][\s]*[0-9]+[\s]*[)]([\s\S]+)`, `create index ${1}${2}`),
	newRegexpMap(`(?i)drop index ([\S]+) on ([\S]+)`, `drop index if exists $1`),
}

var (
	identifyCreateTable = regexp.MustCompile(`(?i)^\s*create\s+table`)
	identifyCreateIndex = regexp.MustCompile(`(?i)^\s*create\s+(unique\s+)?index`)
	identifyDropIndex   = regexp.MustCompile(`(?i)^\s*drop\s+index`)
	identifyAlterTable  = regexp.MustCompile(`(?i)^\s*alter\s+table`)
	identifyInsert      = regexp.MustCompile(`(?i)^\s*(insert|replace)`)
)

func IsCreateTable(statement string) bool { return identifyCreateTable.MatchString(statement) }
func IsCreateIndex(statement string) bool { return identifyCreateIndex.MatchString(statement) }
func IsDropIndex(statement string) bool   { return identifyDropIndex.MatchString(statement) }
func IsAlterTable(statement string) bool  { return identifyAlterTable.MatchString(statement) }
func IsInsert(statement string) bool      { return identifyInsert.MatchString(statement) }

func ToSQLiteDialect(statement string) string {
	if IsCreateTable(statement) || IsAlterTable(statement) {
		return applyConversions(statement, sqliteCreateTableConversions)
	}
	statement = applyConversions(statement, sqliteGeneralConversions)
	if IsInsert(statement) {
		return applyConversions(statement, sqliteInsertConversions)
	}
	return statement
}
