package kingbase_test

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
)

func TestOracleMigrationPreservesOriginalErrors(t *testing.T) {
	t.Run("remove_exec_error", func(t *testing.T) {
		db, mock, cleanup := openMockGorm(t, kb.DialectKingbaseOracle)
		defer cleanup()
		original := errors.New("original server error")
		mock.ExpectQuery("SELECT n.nspname,c.relname").WithArgs(`"orders"`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table"}).AddRow("public", "orders"))
		mock.ExpectQuery("SELECT t.relname,x.relname FROM pg_index").WithArgs(`"public"."Idx-Mixed"`).WillReturnRows(sqlmock.NewRows([]string{"table", "index"}))
		mock.ExpectExec(regexp.QuoteMeta(`DROP INDEX "public"."Idx-Mixed"`)).WillReturnError(original)
		if got := kb.NewOracleMigrator(db.Table("orders")).RemoveIndex("Idx-Mixed").Error; got != original {
			t.Fatalf("error replaced: got=%v want original error", got)
		}
	})
	t.Run("add_metadata_error", func(t *testing.T) {
		db, mock, cleanup := openMockGorm(t, kb.DialectKingbaseOracle)
		defer cleanup()
		original := errors.New("catalog permission denied")
		mock.ExpectQuery("SELECT n.nspname,c.relname").WithArgs(`"orders"`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table"}).AddRow("public", "orders"))
		mock.ExpectQuery("SELECT i.indisunique").WithArgs("public", "orders", "Idx-Mixed").WillReturnError(original)
		if got := kb.NewOracleMigrator(db.Table("orders")).AddIndex("Idx-Mixed", "name").Error; got != original {
			t.Fatalf("metadata error replaced: %v", got)
		}
		// No CREATE statement is expected after the failed catalog read.
	})
	t.Run("wrong_table_protected", func(t *testing.T) {
		db, mock, cleanup := openMockGorm(t, kb.DialectKingbaseOracle)
		defer cleanup()
		mock.ExpectQuery("SELECT n.nspname,c.relname").WithArgs(`"orders"`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table"}).AddRow("public", "orders"))
		mock.ExpectQuery("SELECT t.relname,x.relname FROM pg_index").WithArgs(`"public"."Idx-Mixed"`).WillReturnRows(sqlmock.NewRows([]string{"table", "index"}).AddRow("another_table", "Idx-Mixed"))
		if err := kb.NewOracleMigrator(db.Table("orders")).RemoveIndex("Idx-Mixed").Error; err == nil {
			t.Fatal("an index on another table was allowed to be dropped")
		}
	})
}
