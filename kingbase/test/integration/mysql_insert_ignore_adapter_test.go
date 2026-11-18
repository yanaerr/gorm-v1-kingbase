package kingbase_test

import (
	"fmt"
	"testing"
	"time"

	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type mysqlIgnoreAdapterRow struct {
	ID          int64 `gorm:"primary_key"`
	Email       string
	Note        string `gorm:"default:'READY'"`
	Omitted     string
	BeforeCalls int `gorm:"-"`
	AfterCalls  int `gorm:"-"`
}

func (r *mysqlIgnoreAdapterRow) BeforeCreate() error { r.BeforeCalls++; return nil }
func (r *mysqlIgnoreAdapterRow) AfterCreate() error  { r.AfterCalls++; return nil }

// The transaction owns every object in a fresh schema and rolls it all back.
// It checks both ignored duplicates and successful generated-ID returning.
func TestKingbaseMySQLMode_InsertIgnoreAdapter(t *testing.T) {
	db := testutil.OpenLogged(t, kb.KbModeMySQL)
	defer testutil.Close(t, db)

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	schema := fmt.Sprintf("kb_ignore_%d", time.Now().UnixNano())
	if err := tx.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("SET LOCAL search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	const table = "kb_ignore_items"
	if err := tx.Exec("CREATE TABLE " + table + " (id BIGINT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(80) NOT NULL UNIQUE, note VARCHAR(32) DEFAULT 'READY', omitted VARCHAR(32) DEFAULT 'OMITTED')").Error; err != nil {
		t.Fatal(err)
	}

	first := mysqlIgnoreAdapterRow{Email: "first"}
	if err := tx.Table(table).Create(&first).Error; err != nil || first.ID <= 0 {
		t.Fatalf("normal Create: id=%d error=%v", first.ID, err)
	}

	duplicate := mysqlIgnoreAdapterRow{Email: first.Email}
	ignored := tx.Table(table).Set("gorm:insert_modifier", "IGNORE").Create(&duplicate)
	if ignored.Error != nil || ignored.RowsAffected != 0 || duplicate.ID != 0 {
		t.Fatalf("ignored duplicate: id=%d rows=%d error=%v", duplicate.ID, ignored.RowsAffected, ignored.Error)
	}
	var count int
	if err := tx.Table(table).Where("email = ?", first.Email).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("ignored duplicate changed data: count=%d error=%v", count, err)
	}

	created := mysqlIgnoreAdapterRow{Email: "second", Omitted: "must not be written"}
	inserted := tx.Table(table).Omit("Omitted").Set("gorm:insert_modifier", "IGNORE").Create(&created)
	if inserted.Error != nil || inserted.RowsAffected != 1 || created.ID <= first.ID {
		t.Fatalf("successful INSERT IGNORE: id=%d first=%d rows=%d error=%v", created.ID, first.ID, inserted.RowsAffected, inserted.Error)
	}
	var actual mysqlIgnoreAdapterRow
	if err := tx.Table(table).Where("email = ?", created.Email).First(&actual).Error; err != nil || actual.ID != created.ID {
		t.Fatalf("generated ID mismatch: model=%d stored=%d error=%v", created.ID, actual.ID, err)
	}
	if created.BeforeCalls != 1 || created.AfterCalls != 1 || created.Note != "READY" || actual.Omitted != "OMITTED" {
		t.Fatalf("Create semantics changed: before=%d after=%d default=%q omitted=%q", created.BeforeCalls, created.AfterCalls, created.Note, actual.Omitted)
	}

	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", schema).Row().Scan(&count); err != nil || count != 0 {
		t.Fatalf("isolated schema still exists: count=%d error=%v", count, err)
	}
	if log := testutil.DriverLogFor(db); log != nil {
		// Success is only meaningful if the driver itself returned no error.
		for _, result := range log.SQLResults(0) {
			if result.Err != nil {
				t.Fatalf("unexpected Kingbase driver error: %v; SQL=%s", result.Err, result.SQL)
			}
		}
	}
}
