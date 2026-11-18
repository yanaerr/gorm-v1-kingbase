package nonfunctional_test

import (
	"fmt"
	"testing"

	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type benchmarkRow struct {
	ID    uint   `gorm:"primary_key"`
	Value string `gorm:"size:128;index"`
}

func TestKingbaseBatchWriteAndPagination(t *testing.T) {
	for _, mode := range []kb.KbMode{kb.KbModeMySQL, kb.KbModeOracle} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			db := testutil.Open(t, mode)
			defer db.Close()

			table := testutil.UniqueTable("kb_performance_rows")
			defer db.DropTable(table)
			if err := db.Table(table).AutoMigrate(&benchmarkRow{}).Error; err != nil {
				t.Fatalf("automigrate: %v", err)
			}

			const totalRows = 200
			for row := 0; row < totalRows; row++ {
				if err := db.Table(table).Create(&benchmarkRow{Value: "batch"}).Error; err != nil {
					t.Fatalf("create row %d: %v", row, err)
				}
			}

			var page []benchmarkRow
			if err := db.Table(table).Order("id asc").Limit(25).Offset(150).Find(&page).Error; err != nil {
				t.Fatalf("paged query: %v", err)
			}
			if len(page) != 25 {
				t.Fatalf("page length = %d, want 25", len(page))
			}
		})
	}
}

func BenchmarkKingbaseCreate(b *testing.B) {
	for _, mode := range []kb.KbMode{kb.KbModeMySQL, kb.KbModeOracle} {
		mode := mode
		b.Run(string(mode), func(b *testing.B) {
			db := testutil.Open(b, mode)
			table := testutil.UniqueTable("kb_benchmark_rows")
			if err := db.Table(table).AutoMigrate(&benchmarkRow{}).Error; err != nil {
				b.Fatalf("automigrate: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := db.New().Table(table).Create(&benchmarkRow{Value: "benchmark"}).Error; err != nil {
					b.Fatalf("create: %v", err)
				}
			}
			b.StopTimer()
			_ = db.DropTable(table).Error
			_ = db.Close()
		})
	}
}

// BenchmarkGorm mirrors the official GORM V1 ORM benchmark as a Kingbase-owned
// two-mode benchmark. Unlike the upstream benchmark it keeps b.N under the Go
// benchmark driver's control.
func BenchmarkGorm(b *testing.B) {
	for _, mode := range []kb.KbMode{kb.KbModeMySQL, kb.KbModeOracle} {
		mode := mode
		b.Run(string(mode), func(b *testing.B) {
			db := testutil.Open(b, mode)
			table := testutil.UniqueTable("kb_benchmark_gorm")
			if err := db.Table(table).AutoMigrate(&benchmarkRow{}).Error; err != nil {
				b.Fatalf("automigrate: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				row := benchmarkRow{Value: fmt.Sprintf("gorm-%d", i)}
				if err := db.Table(table).Create(&row).Error; err != nil {
					b.Fatalf("create: %v", err)
				}
				if err := db.Table(table).Where("id = ?", row.ID).First(&benchmarkRow{}).Error; err != nil {
					b.Fatalf("query: %v", err)
				}
				if err := db.Table(table).Where("id = ?", row.ID).UpdateColumn("value", fmt.Sprintf("updated-%d", i)).Error; err != nil {
					b.Fatalf("update: %v", err)
				}
				if err := db.Table(table).Delete(&row).Error; err != nil {
					b.Fatalf("delete: %v", err)
				}
			}
			b.StopTimer()
			_ = db.DropTable(table).Error
			_ = db.Close()
		})
	}
}

// BenchmarkRawSql retains the official raw-SQL comparison while using GORM's
// active Kingbase connection and dialect quoting instead of a PostgreSQL-only
// connection string and dollar placeholders.
func BenchmarkRawSql(b *testing.B) {
	for _, mode := range []kb.KbMode{kb.KbModeMySQL, kb.KbModeOracle} {
		mode := mode
		b.Run(string(mode), func(b *testing.B) {
			db := testutil.Open(b, mode)
			table := testutil.UniqueTable("kb_benchmark_raw")
			if err := db.Table(table).AutoMigrate(&benchmarkRow{}).Error; err != nil {
				b.Fatalf("automigrate: %v", err)
			}
			quotedTable := db.NewScope(&benchmarkRow{}).Quote(table)
			quotedValue := db.NewScope(&benchmarkRow{}).Quote("value")
			insertSQL := "INSERT INTO " + quotedTable + " (" + quotedValue + ") VALUES (?)"
			querySQL := "SELECT " + quotedValue + " FROM " + quotedTable + " WHERE " + quotedValue + " = ?"
			updateSQL := "UPDATE " + quotedTable + " SET " + quotedValue + " = ? WHERE " + quotedValue + " = ?"
			deleteSQL := "DELETE FROM " + quotedTable + " WHERE " + quotedValue + " = ?"

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				value := fmt.Sprintf("raw-%d", i)
				updated := fmt.Sprintf("raw-updated-%d", i)
				if _, err := db.DB().Exec(insertSQL, value); err != nil {
					b.Fatalf("insert: %v", err)
				}
				var selected string
				if err := db.DB().QueryRow(querySQL, value).Scan(&selected); err != nil || selected != value {
					b.Fatalf("query selected=%q err=%v", selected, err)
				}
				if _, err := db.DB().Exec(updateSQL, updated, value); err != nil {
					b.Fatalf("update: %v", err)
				}
				if _, err := db.DB().Exec(deleteSQL, updated); err != nil {
					b.Fatalf("delete: %v", err)
				}
			}
			b.StopTimer()
			_ = db.DropTable(table).Error
			_ = db.Close()
		})
	}
}
