package regression_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type officialQueryOptionRow struct {
	ID   int64 `gorm:"primary_key"`
	Name string
}

type officialIgnoreReloadRow struct {
	ID     int64  `gorm:"primary_key"`
	Number int64  `gorm:"unique_index"`
	Name   string `gorm:"default:'panda'"`
}

// These three upstream scenarios exist in this workspace in addition to the
// historical v1.9.16 inventory. Keep construction and real result assertions.
func TestOfficialQueryOptionAndIgnoreCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := testutil.OpenLogged(t, mode)
		defer testutil.Close(t, db)

		table := testutil.UniqueTable("kb_query_option")
		if err := db.Table(table).AutoMigrate(&officialQueryOptionRow{}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.DropTable(table)
		for _, name := range []string{"user1", "user2"} {
			if err := db.Table(table).Create(&officialQueryOptionRow{Name: name}).Error; err != nil {
				t.Fatal(err)
			}
		}
		quoted := db.Dialect().Quote(table)
		option := "WHERE " + quoted + ".name='user2'"
		t.Run("TestSubQueryWithQueryOption", func(t *testing.T) {
			expr := db.Table(table).Select("id").Set("gorm:query_option", option).SubQuery()
			if !strings.Contains(fmt.Sprint(expr), option) {
				t.Fatalf("SubQuery lost query_option: %v", expr)
			}
			var rows []officialQueryOptionRow
			if err := db.Table(table).Where("id IN ?", expr).Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].Name != "user2" {
				t.Fatalf("SubQuery rows=%v err=%v", rows, err)
			}
		})
		t.Run("TestQueryExprWithQueryOption", func(t *testing.T) {
			expr := db.Table(table).Select("id").Set("gorm:query_option", option).QueryExpr()
			if !strings.Contains(fmt.Sprint(expr), option) {
				t.Fatalf("QueryExpr lost query_option: %v", expr)
			}
			var rows []officialQueryOptionRow
			if err := db.Table(table).Where("id IN (?)", expr).Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].Name != "user2" {
				t.Fatalf("QueryExpr rows=%v err=%v", rows, err)
			}
		})
		t.Run("TestFixFullTableScanWhenInsertIgnore", func(t *testing.T) {
			if mode != kb.KbModeMySQL {
				t.Skip("INSERT IGNORE is a MySQL-mode contract")
			}
			ignoreTable := testutil.UniqueTable("kb_ignore_reload")
			if err := db.Table(ignoreTable).AutoMigrate(&officialIgnoreReloadRow{}).Error; err != nil {
				t.Fatal(err)
			}
			defer db.DropTable(ignoreTable)
			row := officialIgnoreReloadRow{Number: 200408301001}
			if result := db.Table(ignoreTable).Create(&row); result.Error != nil || result.RowsAffected != 1 || row.Name != "panda" {
				t.Fatalf("initial insert: row=%v affected=%d err=%v", row, result.RowsAffected, result.Error)
			}
			callbacks := db.Callback()
			callbacks.Query().After("gorm:query").Register("kb:reject_unfiltered_ignore_reload", func(scope *gorm.Scope) {
				if strings.Contains(scope.SQL, ignoreTable) && len(scope.SQLVars) == 0 {
					t.Errorf("unfiltered reload: %s", scope.SQL)
				}
			})
			defer callbacks.Query().Remove("kb:reject_unfiltered_ignore_reload")
			// A fresh zero-primary-key model exercises the dangerous default-value reload.
			duplicate := officialIgnoreReloadRow{Number: row.Number}
			result := db.Table(ignoreTable).Set("gorm:insert_modifier", "IGNORE").Create(&duplicate)
			if result.Error != nil || result.RowsAffected != 0 || duplicate.ID != 0 {
				t.Fatalf("ignored insert: row=%v affected=%d err=%v", duplicate, result.RowsAffected, result.Error)
			}
			var count int
			if err := db.Table(ignoreTable).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("stored count=%d err=%v", count, err)
			}
		})
	})
}
