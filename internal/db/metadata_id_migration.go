package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	metadataschema "github.com/openark/orchestrator/docs/schema"
)

// metadataColumn 仅描述主键迁移需要的数据库结构，不参与业务传输。
type metadataColumn struct {
	name            string
	kind            string
	notNull         bool
	auto            bool
	primaryPosition int
}

func quoteMetadataIdentifier(name string) string { return "`" + name + "`" }

func metadataColumns(ctx context.Context, database *sql.DB, table string) ([]metadataColumn, error) {
	query := `SELECT column_name, column_type, is_nullable = 'NO', extra LIKE '%auto_increment%', IF(column_key = 'PRI', ordinal_position, 0) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position`
	args := []any{table}
	if IsSQLite() {
		query = "PRAGMA table_info(" + quoteMetadataIdentifier(table) + ")"
		args = nil
	}
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []metadataColumn
	for rows.Next() {
		var column metadataColumn
		if IsSQLite() {
			var sequence int
			var defaultValue sql.NullString
			if err := rows.Scan(&sequence, &column.name, &column.kind, &column.notNull, &defaultValue, &column.primaryPosition); err != nil {
				return nil, err
			}
			column.auto = strings.EqualFold(column.kind, "integer") && column.primaryPosition == 1
		} else if err := rows.Scan(&column.name, &column.kind, &column.notNull, &column.auto, &column.primaryPosition); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func metadataPrimaryKey(ctx context.Context, database *sql.DB, table string, columns []metadataColumn) ([]string, error) {
	if !IsSQLite() {
		rows, err := database.QueryContext(ctx, `SELECT column_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = 'PRIMARY' ORDER BY seq_in_index`, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		return keys, rows.Err()
	}
	var primary []metadataColumn
	for _, column := range columns {
		if column.primaryPosition > 0 {
			primary = append(primary, column)
		}
	}
	sort.Slice(primary, func(i, j int) bool { return primary[i].primaryPosition < primary[j].primaryPosition })
	var keys []string
	for _, column := range primary {
		keys = append(keys, column.name)
	}
	return keys, nil
}

func metadataHasBusinessKey(ctx context.Context, database *sql.DB, table string, key []string) (bool, error) {
	if len(key) == 0 {
		return true, nil
	}
	indexes := make(map[string][]string)
	if IsSQLite() {
		rows, err := database.QueryContext(ctx, "PRAGMA index_list("+quoteMetadataIdentifier(table)+")")
		if err != nil {
			return false, err
		}
		var names []string
		for rows.Next() {
			var sequence, unique, partial int
			var name, origin string
			if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
				rows.Close()
				return false, err
			}
			if unique == 1 && partial == 0 {
				names = append(names, name)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		for _, name := range names {
			rows, err := database.QueryContext(ctx, "PRAGMA index_info("+quoteMetadataIdentifier(name)+")")
			if err != nil {
				return false, err
			}
			for rows.Next() {
				var sequence, cid int
				var column sql.NullString
				if err := rows.Scan(&sequence, &cid, &column); err != nil {
					rows.Close()
					return false, err
				}
				indexes[name] = append(indexes[name], column.String)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return false, err
			}
		}
	} else {
		rows, err := database.QueryContext(ctx, `SELECT index_name, column_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND non_unique = 0 AND sub_part IS NULL ORDER BY index_name, seq_in_index`, table)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, column string
			if err := rows.Scan(&name, &column); err != nil {
				return false, err
			}
			indexes[name] = append(indexes[name], column)
		}
		if err := rows.Err(); err != nil {
			return false, err
		}
	}
	for _, columns := range indexes {
		if reflect.DeepEqual(columns, key) {
			return true, nil
		}
	}
	return false, nil
}

func validateMetadataTableID(ctx context.Context, database *sql.DB, table string) error {
	columns, err := metadataColumns(ctx, database, table)
	if err != nil {
		return err
	}
	primary, err := metadataPrimaryKey(ctx, database, table, columns)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(primary, []string{"id"}) {
		return fmt.Errorf("%s primary key is %v; expected id", table, primary)
	}
	for _, column := range columns {
		if column.name == "id" && (!column.auto || (!IsSQLite() && (!column.notNull || !strings.HasPrefix(strings.ToLower(column.kind), "bigint") || !strings.Contains(strings.ToLower(column.kind), "unsigned")))) {
			return fmt.Errorf("%s.id is not an auto-increment integer of the required type", table)
		}
	}
	key := metadataschema.BusinessKey(table)
	for _, name := range key {
		for _, column := range columns {
			if column.name == name && !column.notNull {
				return fmt.Errorf("%s.%s business key must remain not null", table, name)
			}
		}
	}
	unique, err := metadataHasBusinessKey(ctx, database, table, key)
	if err != nil {
		return err
	}
	if !unique {
		return fmt.Errorf("%s business key %v is not unique", table, key)
	}
	return nil
}

func validateMetadataIDs(ctx context.Context, database *sql.DB) error {
	for _, table := range metadataschema.ManagedTables() {
		if err := validateMetadataTableID(ctx, database, table); err != nil {
			return fmt.Errorf("validate metadata id contract: %w", err)
		}
	}
	return nil
}

// migrateMetadataIDs 仅由显式管理命令调用，调用方负责停写与备份。
// 每张表独立提交；重启时读取真实结构，已完成的表只校验，不重复回填。
func migrateMetadataIDs(ctx context.Context, database *sql.DB) error {
	tables := metadataschema.ManagedTables()
	// 完整预检，防止在识别出不支持的主键之前修改其他表。
	for _, table := range tables {
		columns, err := metadataColumns(ctx, database, table)
		if err != nil {
			return err
		}
		primary, err := metadataPrimaryKey(ctx, database, table, columns)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(primary, []string{"id"}) {
			if err := validateMetadataTableID(ctx, database, table); err != nil {
				return err
			}
			continue
		}
		want := metadataschema.BusinessKey(table)
		if old := metadataschema.LegacyAutoID(table); old != "" {
			want = []string{old}
		}
		if !reflect.DeepEqual(primary, want) {
			return fmt.Errorf("cannot migrate %s primary key %v; expected %v", table, primary, want)
		}
		for _, column := range columns {
			if column.name == "id" {
				return fmt.Errorf("cannot migrate %s: non-primary id already exists", table)
			}
		}
		for _, name := range metadataschema.BusinessKey(table) {
			for _, column := range columns {
				if column.name == name && !column.notNull {
					return fmt.Errorf("cannot migrate %s: business key %s is nullable", table, name)
				}
			}
		}
	}
	if _, err := execInternalContext(ctx, database, `INSERT IGNORE INTO orchestrator_schema_migrations (migration_id, applied_at) VALUES (?, NOW())`, metadataschema.UpgradeMigrationPending); err != nil {
		return err
	}
	for _, table := range tables {
		columns, err := metadataColumns(ctx, database, table)
		if err != nil {
			return err
		}
		primary, err := metadataPrimaryKey(ctx, database, table, columns)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(primary, []string{"id"}) {
			continue
		}
		if IsSQLite() {
			err = migrateSQLiteTableID(ctx, database, table, columns)
		} else {
			err = migrateMySQLTableID(ctx, database, table)
		}
		if err != nil {
			return fmt.Errorf("migrate %s primary key: %w", table, err)
		}
		if err := validateMetadataTableID(ctx, database, table); err != nil {
			return err
		}
	}
	if err := validateMetadataIDs(ctx, database); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	insert := `INSERT IGNORE INTO orchestrator_schema_migrations (migration_id, applied_at) VALUES (?, CURRENT_TIMESTAMP)`
	if IsSQLite() {
		insert = ToSQLiteDialect(insert)
	}
	if _, err := tx.ExecContext(ctx, insert, metadataschema.CanonicalMigration); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM orchestrator_schema_migrations WHERE migration_id IN (?, ?, ?)`, metadataschema.UpgradeMigrationPending, metadataschema.PreviousCanonicalMigration+"-pending", metadataschema.CanonicalMigrationPending); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateMySQLTableID(ctx context.Context, database *sql.DB, table string) error {
	clause := ""
	if old := metadataschema.LegacyAutoID(table); old != "" {
		clause = "CHANGE COLUMN " + quoteMetadataIdentifier(old) + " `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '记录主键'"
	} else {
		var key []string
		for _, column := range metadataschema.BusinessKey(table) {
			key = append(key, quoteMetadataIdentifier(column))
		}
		clause = "ADD COLUMN `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '记录主键', DROP PRIMARY KEY, ADD PRIMARY KEY (`id`), ADD UNIQUE INDEX " + quoteMetadataIdentifier("unq_"+table+"_identity") + " (" + strings.Join(key, ",") + ")"
	}
	_, err := database.ExecContext(ctx, "ALTER TABLE "+quoteMetadataIdentifier(table)+" "+clause)
	return err
}

var sqlitePrimaryKey = regexp.MustCompile(`(?i)PRIMARY\s+KEY\s*\([^)]*\)`)
var sqliteCreateTableName = regexp.MustCompile("(?i)^CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?(?:`[^`]+`|\"[^\"]+\"|[a-z_][a-z0-9_]*)")

func migrateSQLiteTableID(ctx context.Context, database *sql.DB, table string, columns []metadataColumn) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var definition string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&definition); err != nil {
		return err
	}
	if len(sqlitePrimaryKey.FindAllString(definition, -1)) != 1 {
		return fmt.Errorf("unsupported SQLite primary key definition for %s", table)
	}
	rows, err := tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL`, table)
	if err != nil {
		return err
	}
	var objects []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, statement)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	old := metadataschema.LegacyAutoID(table)
	temp := "_auto_id_v2_" + table
	definition = sqliteCreateTableName.ReplaceAllString(definition, "CREATE TABLE "+quoteMetadataIdentifier(temp))
	if old != "" {
		identifier := regexp.MustCompile(`\b` + regexp.QuoteMeta(old) + `\b`)
		definition = identifier.ReplaceAllString(definition, "id")
		for i, object := range objects {
			objects[i] = identifier.ReplaceAllString(object, "id")
		}
	} else {
		definition = strings.Replace(definition, "(", "(`id` INTEGER,", 1)
		primary := sqlitePrimaryKey.FindString(definition)
		definition = strings.Replace(definition, primary, "UNIQUE"+primary[strings.Index(primary, "("):]+", PRIMARY KEY (`id`)", 1)
	}
	if _, err := tx.ExecContext(ctx, definition); err != nil {
		return err
	}
	var source, target []string
	for _, column := range columns {
		source = append(source, quoteMetadataIdentifier(column.name))
		name := column.name
		if name == old {
			name = "id"
		}
		target = append(target, quoteMetadataIdentifier(name))
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+quoteMetadataIdentifier(temp)+" ("+strings.Join(target, ",")+") SELECT "+strings.Join(source, ",")+" FROM "+quoteMetadataIdentifier(table)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE "+quoteMetadataIdentifier(table)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE "+quoteMetadataIdentifier(temp)+" RENAME TO "+quoteMetadataIdentifier(table)); err != nil {
		return err
	}
	for _, object := range objects {
		if _, err := tx.ExecContext(ctx, object); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func prepareMetadataIDMigration(ctx context.Context, database *sql.DB, layout metadataSchemaLayout) error {
	if layout == metadataSchemaLegacy {
		if err := deployStatementsContext(ctx, database, generateSQLBase); err != nil {
			return err
		}
		return deployStatementsContext(ctx, database, generateSQLPatches)
	}
	var completed, pending, upgrading int
	if err := database.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN migration_id = ? THEN 1 ELSE 0 END),0)
 FROM orchestrator_schema_migrations`, metadataschema.PreviousCanonicalMigration, metadataschema.PreviousCanonicalMigration+"-pending", metadataschema.UpgradeMigrationPending).Scan(&completed, &pending, &upgrading); err != nil {
		return err
	}
	// 迁移已经开始时禁止再执行含旧主键名称的历史 DDL。
	if upgrading > 0 {
		return nil
	}
	if completed == 0 && (pending > 0 || layout == metadataSchemaUpgrade) {
		return deployCanonicalStatementsContext(ctx, database, metadataschema.StatementsV1())
	}
	return nil
}
