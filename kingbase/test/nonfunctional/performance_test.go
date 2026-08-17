package nonfunctional_test

import (
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
