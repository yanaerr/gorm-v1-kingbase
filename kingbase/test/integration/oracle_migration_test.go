package kingbase_test

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type oracleMigrationIndex struct {
	ID   int64  `gorm:"primary_key"`
	Name string `gorm:"column:Display-Name;size:64;comment:'金仓 O''Reilly'"`
}

type oracleMigrationExtension struct {
	ID   int64  `gorm:"primary_key"`
	Name string `gorm:"column:Display-Name;size:64;comment:'金仓 O''Reilly'"`
	Note string `gorm:"size:64;comment:新增字段"`
}

type oracleMigrationUint64 struct {
	ID   uint64 `gorm:"primary_key"`
	Name string `gorm:"size:64"`
}

type oracleMigrationBadType struct {
	ID  int64  `gorm:"primary_key"`
	Bad string `gorm:"type:kb_deliberately_missing_type"`
}

func oracleUint64Sequence(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var sequence string
	if err := db.Raw("SELECT pg_get_serial_sequence(?, 'id')", table).Row().Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	return sequence
}

func oracleRestartUint64(t *testing.T, db *gorm.DB, table string, signedStart int64) {
	t.Helper()
	// Independent fixture positions the native allocator; INSERT still enters
	// through the unchanged GORM Create and the installed database default.
	sequence := oracleUint64Sequence(t, db, table)
	if err := db.Exec(fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH %d", sequence, signedStart)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestKingbaseOracleMode_MigrationExtensions(t *testing.T) {
	db := openKBOracle(t)
	defer finishLoggedTest(t, db)

	runLoggedTest(t, db, "01_Quoted_Index_And_Original_Error", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_index")
		defer oracleCleanupTable(t, db, table)
		if err := kb.NewOracleMigrator(db.Table(table)).CreateTable(&oracleMigrationIndex{}).Error; err != nil {
			t.Fatal(err)
		}
		index := "Idx-Mixed.Dot_" + table
		quotedIndex := `"` + index + `"`
		if err := kb.NewOracleMigrator(db.Table(table)).AddUniqueIndex(quotedIndex, "Display-Name").Error; err != nil {
			t.Fatal(err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename=? AND indexname=?", 1, table, index)
		if err := kb.NewOracleMigrator(db.Table(table)).AddUniqueIndex(quotedIndex, "Display-Name").Error; err != nil {
			t.Fatal("idempotent AddUniqueIndex:", err)
		}
		if err := kb.NewOracleMigrator(db.Table(table)).AddIndex(quotedIndex, "Display-Name").Error; err == nil {
			t.Fatal("a unique index was incorrectly accepted as the requested non-unique index")
		}
		first := oracleMigrationIndex{Name: "same"}
		if err := db.Table(table).Create(&first).Error; err != nil {
			t.Fatal(err)
		}
		oracleExpectedServerError(t, db, "23505", index, func() error {
			return db.Table(table).Create(&oracleMigrationIndex{Name: "same"}).Error
		})
		if err := kb.NewOracleMigrator(db.Table(table)).RemoveIndex(quotedIndex).Error; err != nil {
			t.Fatal(err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename=? AND indexname=?", 0, table, index)
		oracleExpectedServerError(t, db, "42704", "", func() error {
			return kb.NewOracleMigrator(db.Table(table)).RemoveIndex(quotedIndex).Error
		})
		if err := db.Table(table).Create(&oracleMigrationIndex{Name: "same"}).Error; err != nil {
			t.Fatal("removed unique index still rejects duplicate:", err)
		}
		if err := kb.NewOracleMigrator(db.Table(table)).AddIndex(quotedIndex, "Display-Name").Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.NewOracleMigrator(db.Table(table)).RemoveIndex(quotedIndex).Error; err != nil {
			t.Fatal(err)
		}
		for _, changedName := range []string{"KB_FULL_UPPER", "Idx-" + strings.Repeat("x", 80)} {
			if err := kb.NewOracleMigrator(db.Table(table)).AddIndex(changedName, "Display-Name").Error; err == nil {
				t.Fatalf("database-changed name incorrectly accepted: %s", changedName)
			}
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename=? AND indexname<>?", 0, table, table+"_pkey")
	})

	runLoggedTest(t, db, "02_Comments_New_And_Added_Columns", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_comment")
		defer oracleCleanupTable(t, db, table)
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationIndex{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationExtension{}).Error; err != nil {
			t.Fatal(err)
		}
		for column, want := range map[string]string{"Display-Name": "金仓 O'Reilly", "note": "新增字段"} {
			var got string
			if err := db.Raw("SELECT col_description(a.attrelid,a.attnum) FROM pg_attribute a WHERE a.attrelid=to_regclass(?) AND a.attname=?", table, column).Row().Scan(&got); err != nil || got != want {
				t.Fatalf("column %s comment=%q want=%q error=%v", column, got, want, err)
			}
		}
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationExtension{}).Error; err != nil {
			t.Fatal("repeat migration:", err)
		}
	})

	runLoggedTest(t, db, "03_Uint64_SignedBoundary_And_Idempotence", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_u64")
		defer oracleCleanupTable(t, db, table)
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		if got := oracleStoredType(t, db, table, "id"); got != "numeric(20,0)" {
			t.Fatal("uint64 type:", got)
		}
		oracleRestartUint64(t, db, table, -1)
		for _, want := range []uint64{math.MaxInt64, uint64(math.MaxInt64) + 1} {
			row := oracleMigrationUint64{Name: "boundary"}
			if err := db.Table(table).Create(&row).Error; err != nil || row.ID != want {
				t.Fatalf("Create ID=%d want=%d error=%v", row.ID, want, err)
			}
			var got oracleMigrationUint64
			if err := db.Table(table).Where("id=?", row.ID).First(&got).Error; err != nil || got.ID != want {
				t.Fatalf("read ID=%d want=%d error=%v", got.ID, want, err)
			}
		}
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal("repeat uint64 migration:", err)
		}
		row := oracleMigrationUint64{Name: "after repeat"}
		if err := db.Table(table).Create(&row).Error; err != nil || row.ID != uint64(math.MaxInt64)+2 {
			t.Fatalf("allocator reset: ID=%d error=%v", row.ID, err)
		}
	})

	runLoggedTest(t, db, "04_Uint64_Maximum_Exhaustion_And_RawDefault", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_u64max")
		defer oracleCleanupTable(t, db, table)
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		oracleRestartUint64(t, db, table, math.MaxInt64-1)
		var raw uint64
		if err := db.Raw(fmt.Sprintf(`INSERT INTO "%s" DEFAULT VALUES RETURNING id`, table)).Row().Scan(&raw); err != nil || raw != math.MaxUint64-1 {
			t.Fatalf("Raw default ID=%d error=%v", raw, err)
		}
		row := oracleMigrationUint64{Name: "last"}
		if err := db.Table(table).Create(&row).Error; err != nil || row.ID != math.MaxUint64 {
			t.Fatalf("maximum ID=%d error=%v", row.ID, err)
		}
		var maximum oracleMigrationUint64
		if err := db.Table(table).Where("id=?", uint64(math.MaxUint64)).First(&maximum).Error; err != nil || maximum.ID != math.MaxUint64 {
			t.Fatalf("read maximum ID=%d error=%v", maximum.ID, err)
		}
		oracleExpectedServerError(t, db, "2200H", "", func() error {
			return db.Table(table).Create(&oracleMigrationUint64{}).Error
		})
		oracleRequireCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table), 2)
		explicit := oracleMigrationUint64{ID: math.MaxUint64 - 2, Name: "explicit"}
		if err := db.Table(table).Create(&explicit).Error; err != nil || explicit.ID != math.MaxUint64-2 {
			t.Fatalf("explicit uint64 ID=%d error=%v", explicit.ID, err)
		}
		if err := db.Exec(fmt.Sprintf(`INSERT INTO "%s" (id) VALUES (0)`, table)).Error; err != nil {
			t.Fatal("explicit zero is a valid uint64 value:", err)
		}
		oracleExpectedServerError(t, db, "23514", "", func() error {
			return db.Exec(fmt.Sprintf(`INSERT INTO "%s" (id) VALUES (-1)`, table)).Error
		})
		oracleExpectedServerError(t, db, "23514", "", func() error {
			return db.Exec(fmt.Sprintf(`INSERT INTO "%s" (id) VALUES (18446744073709551616)`, table)).Error
		})
		oracleRequireCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table), 4)
	})

	runLoggedTest(t, db, "05_Uint64_Rollback_And_Concurrent_Allocation", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_u64con")
		defer oracleCleanupTable(t, db, table)
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		first := oracleMigrationUint64{}
		if err := tx.Table(table).Create(&first).Error; err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Rollback().Error; err != nil || first.ID != 1 {
			t.Fatalf("rollback ID=%d error=%v", first.ID, err)
		}
		const workers = 12
		ids := make(chan uint64, workers)
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				row := oracleMigrationUint64{Name: "concurrent"}
				if err := db.Table(table).Create(&row).Error; err != nil {
					errs <- err
					return
				}
				ids <- row.ID
			}()
		}
		wg.Wait()
		close(ids)
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		seen := map[uint64]bool{}
		for id := range ids {
			if id <= 1 || seen[id] {
				t.Fatalf("reused allocation: %d", id)
			}
			seen[id] = true
		}
		if len(seen) != workers {
			t.Fatalf("allocated=%d want=%d", len(seen), workers)
		}
		oracleRequireCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table), workers)
	})

	runLoggedTest(t, db, "06_Existing_Identity_And_Atomic_Failure", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_existing")
		defer oracleCleanupTable(t, db, table)
		if err := db.Table(table).CreateTable(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		allocated := oracleMigrationUint64{}
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		if err := tx.Table(table).Create(&allocated).Error; err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		row := oracleMigrationUint64{}
		if err := db.Table(table).Create(&row).Error; err != nil || row.ID != allocated.ID+1 {
			t.Fatalf("existing identity rewound: ID=%d previous=%d error=%v", row.ID, allocated.ID, err)
		}
		bad := testutil.UniqueTable("kb_ora_mig_negative")
		defer oracleCleanupTable(t, db, bad)
		if err := db.Table(bad).CreateTable(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(fmt.Sprintf(`INSERT INTO "%s" (id) VALUES(-1)`, bad)).Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.NewOracleMigrator(db.Table(bad)).AutoMigrate(&oracleMigrationUint64{}).Error; err == nil || !strings.Contains(err.Error(), "outside uint64 range") {
			t.Fatalf("invalid old data accepted: %v", err)
		}
		if got := oracleStoredType(t, db, bad, "id"); got != "bigint" {
			t.Fatal("failed migration changed type:", got)
		}
	})

	runLoggedTest(t, db, "07_Caller_Transaction_Rollback", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_tx")
		defer oracleCleanupTable(t, db, table)
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		if err := kb.NewOracleMigrator(tx.Table(table)).AutoMigrate(&oracleMigrationIndex{}).Error; err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		bad := testutil.UniqueTable("kb_ora_mig_tx_bad")
		oracleExpectedServerError(t, db, "42704", "", func() error {
			return kb.NewOracleMigrator(tx.Table(bad)).AutoMigrate(&oracleMigrationBadType{}).Error
		})
		var alive int
		if err := tx.Raw("SELECT 1").Row().Scan(&alive); err != nil || alive != 1 {
			tx.Rollback()
			t.Fatalf("migration failure poisoned the caller transaction: %v", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?", 0, table)
	})

	runLoggedTest(t, db, "08_ConstraintConflict_Restores_Identity", func(t *testing.T) {
		table := testutil.UniqueTable("kb_ora_mig_dependency")
		defer oracleCleanupTable(t, db, table)
		if err := db.Table(table).CreateTable(&oracleMigrationUint64{}).Error; err != nil {
			t.Fatal(err)
		}
		var schema string
		if err := db.Raw("SELECT current_schema()").Row().Scan(&schema); err != nil {
			t.Fatal(err)
		}
		// Reserve the adapter's documented constraint name with an unrelated
		// definition, forcing a server error after DROP IDENTITY and TYPE.
		digest := sha256.Sum256([]byte(schema + "\x00" + table + "\x00id"))
		constraint := fmt.Sprintf("kb_u64_range_%x", digest[:12])
		if err := db.Exec(fmt.Sprintf(`ALTER TABLE "%s" ADD CONSTRAINT "%s" CHECK(id>=0)`, table, constraint)).Error; err != nil {
			t.Fatal(err)
		}
		oracleExpectedServerError(t, db, "42710", "", func() error {
			return kb.NewOracleMigrator(db.Table(table)).AutoMigrate(&oracleMigrationUint64{}).Error
		})
		if got := oracleStoredType(t, db, table, "id"); got != "bigint" {
			t.Fatal("failed type change changed original column:", got)
		}
		var identity string
		if err := db.Raw("SELECT attidentity FROM pg_attribute WHERE attrelid=to_regclass(?) AND attname='id'", table).Row().Scan(&identity); err != nil || identity != "d" {
			t.Fatalf("original identity was lost: %q error=%v", identity, err)
		}
		row := oracleMigrationUint64{}
		if err := db.Table(table).Create(&row).Error; err != nil || row.ID != 1 {
			t.Fatalf("original allocator stopped working: ID=%d error=%v", row.ID, err)
		}
	})
}
