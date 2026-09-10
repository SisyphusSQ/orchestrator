package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	metadataschema "github.com/openark/orchestrator/docs/schema"
	"github.com/openark/orchestrator/internal/config"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	repositoryschema "github.com/openark/orchestrator/internal/repository/schema"
)

func seedMetadataRow(t *testing.T, database *sql.DB, table string, number int, explicitID bool) {
	t.Helper()
	columns, err := repositoryschema.Columns(context.Background(), database, table)
	if err != nil {
		t.Fatal(err)
	}
	var names, marks []string
	var values []any
	for _, column := range columns {
		if column.Auto && column.PrimaryPosition > 0 && !explicitID {
			continue
		}
		names = append(names, repositoryschema.QuoteIdentifier(column.Name))
		marks = append(marks, "?")
		var value any = fmt.Sprintf("fixture-%d", number)
		switch {
		case strings.Contains(strings.ToLower(column.Kind), "int"):
			value = number
		case strings.Contains(strings.ToLower(column.Kind), "timestamp"):
			value = "2026-09-10 00:00:00"
		case column.Name == "promotion_rule":
			value = "neutral"
		}
		values = append(values, value)
	}
	if _, err := database.Exec("INSERT INTO "+repositoryschema.QuoteIdentifier(table)+" ("+strings.Join(names, ",")+") VALUES ("+strings.Join(marks, ",")+")", values...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func snapshotRowMaps(t *testing.T, database *sql.DB, table string) []map[string]modeldomain.CellData {
	t.Helper()
	data, err := ScanTableContext(context.Background(), database, table)
	if err != nil {
		t.Fatal(err)
	}
	result := []map[string]modeldomain.CellData{}
	for _, row := range data.Data {
		record := map[string]modeldomain.CellData{}
		for i, column := range data.Columns {
			record[column] = row[i]
		}
		result = append(result, record)
	}
	return result
}

func TestMetadataIDMigrationPreservesEveryTable(t *testing.T) {
	useSQLiteMetadataBackend(t)
	for _, source := range []string{"canonical-v1", "legacy-v1"} {
		t.Run(source, func(t *testing.T) {
			database := openMetadataSchemaSQLite(t)
			ctx := context.Background()
			if source == "canonical-v1" {
				if err := deployCanonicalStatementsContext(ctx, database, metadataschema.StatementsV1()); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := deployStatementsContext(ctx, database, generateSQLBase); err != nil {
					t.Fatal(err)
				}
				if err := deployStatementsContext(ctx, database, generateSQLPatches); err != nil {
					t.Fatal(err)
				}
			}
			before := map[string][]map[string]modeldomain.CellData{}
			for _, table := range metadataschema.ManagedTables() {
				if table == "orchestrator_schema_migrations" {
					continue
				}
				seedMetadataRow(t, database, table, 41, true)
				before[table] = snapshotRowMaps(t, database, table)
			}
			// 保留业务方添加的二级索引，不能用空表重建代替数据迁移。
			if _, err := database.Exec("CREATE INDEX idx_fixture_audit_message ON audit(message)"); err != nil {
				t.Fatal(err)
			}
			if err := migrateMetadataIDs(ctx, database); err != nil {
				t.Fatal(err)
			}
			if err := migrateMetadataIDs(ctx, database); err != nil {
				t.Fatalf("repeat migration: %v", err)
			}
			if err := validateMetadataIDs(ctx, database); err != nil {
				t.Fatal(err)
			}
			for table, want := range before {
				if got := snapshotRowMaps(t, database, table); !reflect.DeepEqual(got, want) {
					t.Errorf("%s data changed: got %v want %v", table, got, want)
				}
				seedMetadataRow(t, database, table, 42, false)
				var maximum int64
				if err := database.QueryRow("SELECT MAX(id) FROM " + repositoryschema.QuoteIdentifier(table)).Scan(&maximum); err != nil {
					t.Fatal(err)
				}
				if metadataschema.LegacyAutoID(table) != "" && maximum <= 41 {
					t.Errorf("%s allocator regressed to %d", table, maximum)
				}
			}
			var indexCount int
			if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='idx_fixture_audit_message'").Scan(&indexCount); err != nil || indexCount != 1 {
				t.Fatalf("custom index lost: count=%d err=%v", indexCount, err)
			}
		})
	}
}

func TestMetadataIDMigrationResumesAfterTableFailure(t *testing.T) {
	useSQLiteMetadataBackend(t)
	database := openMetadataSchemaSQLite(t)
	ctx := context.Background()
	if err := deployCanonicalStatementsContext(ctx, database, metadataschema.StatementsV1()); err != nil {
		t.Fatal(err)
	}
	seedMetadataRow(t, database, "audit", 41, true)
	if _, err := database.Exec("CREATE TABLE _auto_id_v2_hostname_ips (sentinel INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateMetadataIDs(ctx, database); err == nil {
		t.Fatal("expected table collision to fail migration")
	}
	var done, pending int
	if err := database.QueryRow(`SELECT SUM(migration_id=?), SUM(migration_id=?) FROM orchestrator_schema_migrations`, metadataschema.CanonicalMigration, metadataschema.UpgradeMigrationPending).Scan(&done, &pending); err != nil {
		t.Fatal(err)
	}
	if done != 0 || pending != 1 {
		t.Fatalf("done=%d pending=%d", done, pending)
	}
	layout, err := detectMetadataSchemaLayoutContext(ctx, database)
	if err != nil || layout != metadataSchemaUpgrade {
		t.Fatalf("layout=%d err=%v", layout, err)
	}
	if _, err := database.Exec("DROP TABLE _auto_id_v2_hostname_ips"); err != nil {
		t.Fatal(err)
	}
	if err := prepareMetadataIDMigration(ctx, database, layout); err != nil {
		t.Fatal(err)
	}
	if err := migrateMetadataIDs(ctx, database); err != nil {
		t.Fatal(err)
	}
	var id, count int
	if err := database.QueryRow("SELECT MIN(id), COUNT(*) FROM audit").Scan(&id, &count); err != nil || id != 41 || count != 1 {
		t.Fatalf("audit id=%d count=%d err=%v", id, count, err)
	}
}

func TestMetadataIDMigrationRejectsUnexpectedShapeBeforeWrites(t *testing.T) {
	useSQLiteMetadataBackend(t)
	database := openMetadataSchemaSQLite(t)
	ctx := context.Background()
	if err := deployCanonicalStatementsContext(ctx, database, metadataschema.StatementsV1()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("ALTER TABLE hostname_ips ADD COLUMN id INTEGER"); err != nil {
		t.Fatal(err)
	}
	if err := migrateMetadataIDs(ctx, database); err == nil {
		t.Fatal("unexpected id accepted")
	}
	columns, err := repositoryschema.Columns(ctx, database, "audit")
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		if column.Name == "id" {
			t.Fatal("migration changed audit before full preflight")
		}
	}
}

func TestOldCanonicalBootstrapRequiresExplicitMigration(t *testing.T) {
	useSQLiteMetadataBackend(t)
	previous := config.RuntimeCLIFlags
	previousPanic := config.Config.Metadata.Schema.PanicOnDifferentDeployment
	config.RuntimeCLIFlags.ConfiguredVersion = ""
	config.Config.Metadata.Schema.PanicOnDifferentDeployment = false
	t.Cleanup(func() {
		config.RuntimeCLIFlags = previous
		config.Config.Metadata.Schema.PanicOnDifferentDeployment = previousPanic
	})
	for _, count := range []int{1, 12, len(metadataschema.StatementsV1())} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			database := openMetadataSchemaSQLite(t)
			ctx := context.Background()
			if err := deployCanonicalStatementsContext(ctx, database, metadataschema.StatementsV1()[:count]); err != nil {
				t.Fatal(err)
			}
			config.RuntimeCLIFlags.MigrateMetadataIDs = false
			if err := initOrchestratorDBContext(ctx, database); err == nil {
				t.Fatal("old canonical was upgraded automatically")
			}
			config.RuntimeCLIFlags.MigrateMetadataIDs = true
			if err := initOrchestratorDBContext(ctx, database); err != nil {
				t.Fatal(err)
			}
			if err := validateMetadataIDs(ctx, database); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSnapshotLocalIDsCannotOverwriteOtherBusinessRows(t *testing.T) {
	useSQLiteMetadataBackend(t)
	ctx := context.Background()
	source := openMetadataSchemaSQLite(t)
	target := openMetadataSchemaSQLite(t)
	for _, database := range []*sql.DB{source, target} {
		if err := deployCanonicalStatementsContext(ctx, database, metadataschema.Statements()); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct {
		database   *sql.DB
		key, value string
	}{{source, "incoming", "new"}, {target, "unrelated", "keep"}, {target, "incoming", "old"}} {
		if _, err := entry.database.Exec("INSERT INTO kv_store (store_key,store_value,last_updated) VALUES (?,?,CURRENT_TIMESTAMP)", entry.key, entry.value); err != nil {
			t.Fatal(err)
		}
	}
	data, err := ScanTableContext(ctx, source, "kv_store")
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range data.Columns {
		if column == "id" {
			t.Fatal("node-local id leaked into snapshot")
		}
	}
	// 模拟带节点本地 id 的快照：导入时也必须去掉它。
	data.Columns = append(data.Columns, "id")
	data.Data[0] = append(data.Data[0], modeldomain.CellData{String: "1", Valid: true})
	if err := WriteTableContext(ctx, target, "kv_store", data); err != nil {
		t.Fatal(err)
	}
	var retained, updated string
	if err := target.QueryRow("SELECT store_value FROM kv_store WHERE store_key='unrelated'").Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow("SELECT store_value FROM kv_store WHERE store_key='incoming'").Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if retained != "keep" || updated != "new" {
		t.Fatalf("unrelated=%s incoming=%s", retained, updated)
	}
}

func TestSnapshotRetainsLegacyRecoveryIdentifiers(t *testing.T) {
	useSQLiteMetadataBackend(t)
	ctx := context.Background()
	source := openMetadataSchemaSQLite(t)
	target := openMetadataSchemaSQLite(t)
	for _, database := range []*sql.DB{source, target} {
		if err := deployCanonicalStatementsContext(ctx, database, metadataschema.Statements()); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"topology_failure_detection", "topology_recovery", "topology_recovery_steps"} {
		seedMetadataRow(t, source, table, 41, true)
		data, err := ScanTableContext(ctx, source, table)
		if err != nil {
			t.Fatal(err)
		}
		if data.Columns[0] != metadataschema.LegacyAutoID(table) {
			t.Fatalf("%s wire identity=%s", table, data.Columns[0])
		}
		if err := WriteTableContext(ctx, target, table, data); err != nil {
			t.Fatal(err)
		}
		var id int
		if err := target.QueryRow("SELECT id FROM " + table).Scan(&id); err != nil || id != 41 {
			t.Fatalf("id=%d err=%v", id, err)
		}
	}
	var linked int
	if err := target.QueryRow("SELECT d.id FROM topology_recovery r JOIN topology_failure_detection d ON r.last_detection_id=d.id").Scan(&linked); err != nil || linked != 41 {
		t.Fatalf("detection link=%d err=%v", linked, err)
	}
}

func TestSnapshotRejectsMissingOrAmbiguousIdentity(t *testing.T) {
	for _, test := range []struct {
		table   string
		columns []string
	}{
		{"kv_store", []string{"id"}},
		{"topology_recovery", []string{"uid"}},
		{"topology_recovery", []string{"id", "recovery_id"}},
	} {
		row := make(modeldomain.RowData, len(test.columns))
		if _, err := snapshotIdentityColumns(test.table, modeldomain.NamedResultData{Columns: test.columns, Data: modeldomain.ResultData{row}}, false); err == nil {
			t.Errorf("accepted %s identity columns %v", test.table, test.columns)
		}
	}
}
