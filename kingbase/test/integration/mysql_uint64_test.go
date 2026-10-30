package kingbase_test

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type mysqlUint64MigrationRow struct {
	ID   uint64 `gorm:"primary_key"`
	Name string `gorm:"size:64"`
}

type mysqlUint64HookRow struct {
	ID    uint64   `gorm:"primary_key"`
	Name  string   `gorm:"size:64"`
	Hooks []string `sql:"-"`
}

func (r *mysqlUint64HookRow) BeforeCreate() { r.Hooks = append(r.Hooks, "before") }
func (r *mysqlUint64HookRow) AfterCreate()  { r.Hooks = append(r.Hooks, "after") }

func mysqlUint64Table(t *testing.T, db *gorm.DB) string {
	t.Helper()
	table := testutil.UniqueTable("kb_u64_mysql")
	t.Cleanup(func() { mysqlTestCleanup(t, db, table) })
	if err := kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}).Error; err != nil {
		t.Fatal("uint64 migration:", err)
	}
	var valid bool
	if err := db.Raw("SELECT a.atttypid='uint8'::regtype FROM pg_attribute a WHERE a.attrelid=to_regclass(?) AND a.attname='id'", db.Dialect().Quote(table)).Row().Scan(&valid); err != nil || !valid {
		t.Fatalf("native unsigned column=%t err=%v", valid, err)
	}
	return table
}

func mysqlUint64Sequence(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var sequence string
	if err := db.Raw("SELECT pg_get_serial_sequence(?, 'id')", db.Dialect().Quote(table)).Row().Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	return sequence
}

func mysqlUint64Seed(t *testing.T, db *gorm.DB, table string, next int64) {
	t.Helper()
	if err := db.Exec("SELECT setval(?::regclass,?,false)", mysqlUint64Sequence(t, db, table), next).Error; err != nil {
		t.Fatal(err)
	}
}

func mysqlUint64Create(t *testing.T, db *gorm.DB, table string, input, want uint64) {
	t.Helper()
	row := mysqlUint64MigrationRow{ID: input, Name: fmt.Sprint(want)}
	result := db.Table(table).Create(&row)
	if result.Error != nil || result.RowsAffected != 1 || row.ID != want {
		t.Fatalf("Create input=%d ID=%d want=%d affected=%d error=%v", input, row.ID, want, result.RowsAffected, result.Error)
	}
	var stored mysqlUint64MigrationRow
	if err := db.Table(table).Where("id=?", want).First(&stored).Error; err != nil || stored.ID != want || stored.Name != row.Name {
		t.Fatalf("read ID=%d want=%d error=%v", stored.ID, want, err)
	}
}

func TestKingbaseMySQLMode_Uint64Migration(t *testing.T) {
	db := openKBMySQL(t)
	defer finishLoggedTest(t, db)

	runLoggedTest(t, db, "01_Default_Boundary_And_Idempotence", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		mysqlUint64Create(t, db, table, 0, 1)
		mysqlUint64Seed(t, db, table, -1)
		for _, want := range []uint64{math.MaxInt64, uint64(math.MaxInt64) + 1, uint64(math.MaxInt64) + 2} {
			mysqlUint64Create(t, db, table, 0, want)
		}
		if err := kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}).Error; err != nil {
			t.Fatal("repeat migration:", err)
		}
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+3)
	})
	runLoggedTest(t, db, "02_Maximum_And_Exhaustion", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		mysqlUint64Seed(t, db, table, math.MaxInt64-1)
		mysqlUint64Create(t, db, table, 0, math.MaxUint64-1)
		mysqlUint64Create(t, db, table, 0, math.MaxUint64)
		oracleExpectedServerError(t, db, "2200H", "", func() error {
			return db.Table(table).Create(&mysqlUint64MigrationRow{}).Error
		})
		mysqlUint64Create(t, db, table, math.MaxUint64-2, math.MaxUint64-2)
	})
	runLoggedTest(t, db, "03_Explicit_Positive_Advances_Without_Rewind", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		mysqlUint64Create(t, db, table, uint64(math.MaxInt64)+7, uint64(math.MaxInt64)+7)
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+8)
		mysqlUint64Create(t, db, table, 12, 12)
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+9)
	})
	runLoggedTest(t, db, "04_Null_Zero_And_SqlMode", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		mysqlUint64Seed(t, db, table, 0)
		type pointerRow struct {
			ID   *uint64 `gorm:"primary_key"`
			Name string
		}
		for i, value := range []*uint64{nil, new(uint64)} {
			row := pointerRow{ID: value, Name: fmt.Sprint(i)}
			if err := db.Table(table).Create(&row).Error; err != nil || row.ID == nil || *row.ID != uint64(math.MaxInt64)+1+uint64(i) {
				t.Fatalf("NULL/zero ID=%v err=%v", row.ID, err)
			}
		}
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		var flags string
		if err := tx.Raw("SHOW sql_mode").Row().Scan(&flags); err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec("SELECT set_config('sql_mode',?,true)", flags+",NO_AUTO_VALUE_ON_ZERO").Error; err != nil {
			t.Fatal(err)
		}
		zero := pointerRow{ID: new(uint64), Name: "mode-zero"}
		if err := tx.Table(table).Create(&zero).Error; err != nil || zero.ID == nil || *zero.ID != 0 {
			t.Fatalf("mode zero=%v err=%v", zero.ID, err)
		}
		oracleRequireCount(t, tx, "SELECT COUNT(*) FROM "+tx.Dialect().Quote(table)+" WHERE id=0", 1)
		null := pointerRow{Name: "mode-null"}
		if err := tx.Table(table).Create(&null).Error; err != nil || null.ID == nil || *null.ID != uint64(math.MaxInt64)+3 {
			t.Fatalf("mode null=%v err=%v", null.ID, err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+4)
	})
	runLoggedTest(t, db, "05_Raw_Default_And_Omitted_Primary", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		mysqlUint64Seed(t, db, table, 0)
		// A separate raw statement tests the installed DB default. It receives no
		// ORM rewrite and is not substituted for the original raw SQL probes.
		if err := db.Exec("/* raw default fixture */ INSERT INTO " + db.Dialect().Quote(table) + " (name) VALUES ('raw')").Error; err != nil {
			t.Fatal(err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(table)+" WHERE id=?", 1, uint64(math.MaxInt64)+1)
		for i, query := range []*gorm.DB{db.Table(table).Select("name"), db.Table(table).Omit("id")} {
			row := mysqlUint64MigrationRow{Name: fmt.Sprint(i)}
			if err := query.Create(&row).Error; err != nil || row.ID != uint64(math.MaxInt64)+2+uint64(i) {
				t.Fatalf("omitted primary ID=%d err=%v", row.ID, err)
			}
		}
	})
	runLoggedTest(t, db, "06_Concurrent_And_Rollback", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		rolled := uint64(math.MaxInt64) + 20
		mysqlUint64Create(t, tx, table, rolled, rolled)
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		const workers = 12
		ids := make(chan uint64, workers)
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				row := mysqlUint64MigrationRow{Name: "concurrent"}
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
			if seen[id] || id <= rolled {
				t.Errorf("duplicate/reused ID=%d", id)
			}
			seen[id] = true
		}
		if len(seen) != workers {
			t.Fatalf("generated=%d want=%d", len(seen), workers)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(table), workers)
		mysqlUint64Create(t, db, table, 0, rolled+workers+1)
	})
	runLoggedTest(t, db, "07_Insert_Ignore_And_Hooks", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		index := "uix_" + table
		if err := db.Table(table).AddUniqueIndex(index, "name").Error; err != nil {
			t.Fatal(err)
		}
		first := mysqlUint64HookRow{Name: "same"}
		if err := db.Table(table).Create(&first).Error; err != nil || first.ID != 1 || strings.Join(first.Hooks, ",") != "before,after" {
			t.Fatalf("initial ID=%d hooks=%v err=%v", first.ID, first.Hooks, err)
		}
		ignored := mysqlUint64HookRow{Name: "same"}
		result := db.Table(table).Set("gorm:insert_modifier", "IGNORE").Create(&ignored)
		if result.Error != nil || result.RowsAffected != 0 || ignored.ID != 0 {
			t.Fatalf("ignored=%+v affected=%d err=%v", ignored, result.RowsAffected, result.Error)
		}
		mysqlUint64Seed(t, db, table, 0)
		next := mysqlUint64HookRow{Name: "next"}
		result = db.Table(table).Set("gorm:insert_modifier", "IGNORE").Create(&next)
		if result.Error != nil || result.RowsAffected != 1 || next.ID != uint64(math.MaxInt64)+1 || strings.Join(next.Hooks, ",") != "before,after" {
			t.Fatalf("ignored-success=%+v err=%v", next, result.Error)
		}
		large := mysqlUint64HookRow{ID: math.MaxUint64 - 1, Name: "same"}
		result = db.Table(table).Set("gorm:insert_modifier", "IGNORE").Create(&large)
		if result.Error != nil || result.RowsAffected != 0 {
			t.Fatalf("explicit ignored err=%v affected=%d", result.Error, result.RowsAffected)
		}
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+2)
		oracleExpectedServerError(t, db, "23505", index, func() error { return db.Table(table).Create(&mysqlUint64MigrationRow{Name: "same"}).Error })
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+4)
	})
	runLoggedTest(t, db, "08_Legacy_Consumed_Sequence", func(t *testing.T) {
		table := testutil.UniqueTable("kb_u64_legacy")
		defer mysqlTestCleanup(t, db, table)
		if err := db.Table(table).AutoMigrate(&mysqlUint64MigrationRow{}).Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, db, table, 0, 1)
		old := mysqlUint64Sequence(t, db, table)
		if err := db.Exec("SELECT setval(?::regclass,100,true)", old).Error; err != nil {
			t.Fatal(err)
		}
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		mysqlUint64Create(t, tx, table, 0, 101)
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}).Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, db, table, 0, 102)
		var gone bool
		if err := db.Raw("SELECT to_regclass(?) IS NULL", old).Row().Scan(&gone); err != nil || !gone {
			t.Fatalf("old sequence remains=%t err=%v", !gone, err)
		}
	})
	runLoggedTest(t, db, "09_Manual_Unsigned_Existing_Maximum", func(t *testing.T) {
		table := testutil.UniqueTable("kb_u64_manual")
		defer mysqlTestCleanup(t, db, table)
		if err := db.Table(table).AutoMigrate(&mysqlAdapterManual64{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table(table).Create(&mysqlAdapterManual64{ID: math.MaxUint64 - 1, Name: "existing"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}).Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, db, table, 0, math.MaxUint64)
		oracleExpectedServerError(t, db, "2200H", "", func() error { return db.Table(table).Create(&mysqlUint64MigrationRow{}).Error })
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(table), 2)
	})
	runLoggedTest(t, db, "10_Atomic_Failure_And_Outer_Transaction", func(t *testing.T) {
		table := testutil.UniqueTable("kb_u64_atomic")
		defer mysqlTestCleanup(t, db, table)
		if err := db.Table(table).AutoMigrate(&mysqlUint64MigrationRow{}).Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, db, table, 0, 1)
		old := mysqlUint64Sequence(t, db, table)
		oracleExpectedServerError(t, db, "42704", "", func() error {
			return kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}, &oracleMigrationBadType{}).Error
		})
		if got := mysqlUint64Sequence(t, db, table); got != old {
			t.Fatalf("failed migration replaced allocator: %s", got)
		}
		mysqlUint64Create(t, db, table, 0, 2)
		newTable := testutil.UniqueTable("kb_u64_outer")
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := kb.AutoMigrateMySQLUint64(tx.Table(newTable), &mysqlUint64MigrationRow{}).Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, tx, newTable, 0, 1)
		oracleExpectedServerError(t, db, "42704", "", func() error {
			return kb.AutoMigrateMySQLUint64(tx.Table(table), &mysqlUint64MigrationRow{}, &oracleMigrationBadType{}).Error
		})
		var one int
		if err := tx.Raw("SELECT 1").Row().Scan(&one); err != nil || one != 1 {
			t.Fatalf("savepoint did not recover transaction: %v", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?", 0, newTable)
	})
	runLoggedTest(t, db, "11_Modified_Default_Is_Rejected", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		var original string
		if err := db.Raw("SELECT pg_get_expr(d.adbin,d.adrelid) FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid=d.adrelid AND a.attnum=d.adnum WHERE d.adrelid=to_regclass(?) AND a.attname='id'", db.Dialect().Quote(table)).Row().Scan(&original); err != nil {
			t.Fatal(err)
		}
		query := "ALTER TABLE " + db.Dialect().Quote(table) + " ALTER COLUMN id SET DEFAULT "
		if err := db.Exec(query + "((" + original + ")+1)::uint8").Error; err != nil {
			t.Fatal(err)
		}
		for _, err := range []error{db.Table(table).Create(&mysqlUint64MigrationRow{}).Error, kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}).Error} {
			if err == nil || !strings.Contains(err.Error(), "modified uint64 default") {
				t.Fatalf("changed default accepted: %v", err)
			}
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(table), 0)
		if err := db.Exec(query + original).Error; err != nil {
			t.Fatal(err)
		}
		mysqlUint64Create(t, db, table, 0, 1)
	})
	runLoggedTest(t, db, "12_NonPrimary_Null_And_Readback", func(t *testing.T) {
		type row struct {
			ID     int64   `gorm:"primary_key;AUTO_INCREMENT:false"`
			Ticket *uint64 `gorm:"AUTO_INCREMENT;INDEX"`
			Name   string
		}
		table := testutil.UniqueTable("kb_u64_ticket")
		defer mysqlTestCleanup(t, db, table)
		if err := kb.AutoMigrateMySQLUint64(db.Table(table), &row{}).Error; err != nil {
			t.Fatal(err)
		}
		var seq string
		if err := db.Raw("SELECT pg_get_serial_sequence(?, 'ticket')", db.Dialect().Quote(table)).Row().Scan(&seq); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("SELECT setval(?::regclass,0,false)", seq).Error; err != nil {
			t.Fatal(err)
		}
		for i, value := range []*uint64{nil, new(uint64)} {
			r := row{ID: int64(i + 1), Ticket: value, Name: fmt.Sprint(i)}
			if err := db.Table(table).Create(&r).Error; err != nil || r.ID != int64(i+1) || r.Ticket == nil || *r.Ticket != uint64(math.MaxInt64)+1+uint64(i) {
				t.Fatalf("nonprimary=%+v err=%v", r, err)
			}
		}
	})
	runLoggedTest(t, db, "13_Unsupported_Settings_And_Options_Reject", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := tx.Exec("SELECT set_config('auto_increment_increment','2',true)").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Table(table).Create(&mysqlUint64MigrationRow{}).Error; err == nil || !strings.Contains(err.Error(), "auto_increment_increment=1") {
			t.Fatalf("unsupported step accepted: %v", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{"gorm:insert_modifier": "REPLACE", "gorm:insert_option": "ON CONFLICT DO NOTHING"} {
			if err := db.Table(table).Set(key, value).Create(&mysqlUint64MigrationRow{}).Error; err == nil {
				t.Fatalf("unsupported %s accepted", key)
			}
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(table), 0)
		mysqlUint64Create(t, db, table, 0, 1)
	})
	runLoggedTest(t, db, "14_Smaller_Go_Primary_Cannot_Truncate", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		mysqlUint64Seed(t, db, table, 0)
		type narrow struct {
			ID   uint32 `gorm:"primary_key"`
			Name string
		}
		row := narrow{Name: "invalid destination"}
		if err := db.Table(table).Create(&row).Error; err == nil || !strings.Contains(err.Error(), "does not fit primary") || row.ID != 0 {
			t.Fatalf("primary silently truncated: ID=%d err=%v", row.ID, err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(table), 0)
		mysqlUint64Create(t, db, table, 0, uint64(math.MaxInt64)+2)
	})
	runLoggedTest(t, db, "15_Mixed_Explicit_And_Generated_Concurrency", func(t *testing.T) {
		table := mysqlUint64Table(t, db)
		const workers = 8
		results := make(chan error, workers)
		for i := 1; i <= workers; i++ {
			go func(i int) {
				value := uint64(math.MaxInt64) + uint64(i*1000)
				row := mysqlUint64MigrationRow{ID: value, Name: "explicit"}
				err := db.Table(table).Create(&row).Error
				if err == nil {
					generated := mysqlUint64MigrationRow{Name: "generated"}
					err = db.Table(table).Create(&generated).Error
					if err == nil && generated.ID <= value {
						err = fmt.Errorf("generated=%d explicit=%d", generated.ID, value)
					}
				}
				results <- err
			}(i)
		}
		for i := 0; i < workers; i++ {
			if err := <-results; err != nil {
				t.Error(err)
			}
		}
		oracleRequireCount(t, db, "SELECT COUNT(DISTINCT id) FROM "+db.Dialect().Quote(table), workers*2)
	})
	runLoggedTest(t, db, "16_Custom_Default_And_NonInteger_Are_Preserved", func(t *testing.T) {
		table := testutil.UniqueTable("kb_u64_guard")
		defer mysqlTestCleanup(t, db, table)
		quoted := db.Dialect().Quote(table)
		if err := db.Exec("CREATE TABLE " + quoted + " (id NUMERIC(30,1) PRIMARY KEY, name VARCHAR(64))").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO " + quoted + " VALUES (1,'one'),(1.5,'fraction'),(2,'two')").Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.AutoMigrateMySQLUint64(db.Table(table), &mysqlUint64MigrationRow{}).Error; err == nil || !strings.Contains(err.Error(), "non-integer") {
			t.Fatalf("fractional source accepted: %v", err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+quoted+" WHERE id=1.5", 1)
		other := testutil.UniqueTable("kb_u64_default")
		defer mysqlTestCleanup(t, db, other)
		if err := db.Exec("CREATE TABLE " + db.Dialect().Quote(other) + " (id BIGINT UNSIGNED DEFAULT 17 PRIMARY KEY, name VARCHAR(64))").Error; err != nil {
			t.Fatal(err)
		}
		if err := kb.AutoMigrateMySQLUint64(db.Table(other), &mysqlUint64MigrationRow{}).Error; err == nil || !strings.Contains(err.Error(), "custom uint64 default") {
			t.Fatalf("custom default overwritten: %v", err)
		}
		if err := db.Exec("/* custom default fixture */ INSERT INTO " + db.Dialect().Quote(other) + " (name) VALUES ('retained')").Error; err != nil {
			t.Fatal(err)
		}
		oracleRequireCount(t, db, "SELECT COUNT(*) FROM "+db.Dialect().Quote(other)+" WHERE id=17", 1)
	})
}
