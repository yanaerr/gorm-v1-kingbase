package kingbase_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
)

func TestMySQLUint64MigrationRejectsInvalidEntryAndKeepsBeginError(t *testing.T) {
	if result := kb.AutoMigrateMySQLUint64(nil); result.Error == nil {
		t.Fatal("nil DB accepted")
	}
	db, mock, cleanup := openMockGorm(t, kb.DialectKingbaseOracle)
	defer cleanup()
	if err := kb.AutoMigrateMySQLUint64(db).Error; err == nil {
		t.Fatal("Oracle DB accepted by MySQL migration")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	mysql, mysqlMock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
	defer close()
	original := errors.New("original Begin failure")
	mysqlMock.ExpectBegin().WillReturnError(original)
	if err := kb.AutoMigrateMySQLUint64(mysql).Error; err != original {
		t.Fatalf("Begin error replaced: %v", err)
	}
	if err := mysqlMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLUint64AllocatorMetadataFailurePreventsInsert(t *testing.T) {
	for _, phase := range []string{"original_error", "invalid_definition"} {
		t.Run(phase, func(t *testing.T) {
			db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
			defer close()
			db.LogMode(false)
			type row struct {
				ID   uint64 `gorm:"primary_key"`
				Name string
			}
			original := errors.New("allocator catalog permission denied")
			mock.ExpectBegin()
			mock.ExpectQuery("^"+regexp.QuoteMeta(expectedCheckedSequencesQuery)+"$").WithArgs("`events`", "`events`").WillReturnRows(sqlmock.NewRows([]string{"column", "constraint", "schema", "sequence", "cache"}).AddRow("id", "gorm_kb_uint_id", "public", "kb_mysql_u64_fixture", 1))
			query := mock.ExpectQuery("SELECT EXISTS").WithArgs("`events`", "id", "`public`.`kb_mysql_u64_fixture`", "kingbase:gorm-v1:mysql:uint64:v1:%", "gorm_kb_uint_id", "kingbase:gorm-v1:mysql:uint64:v1")
			if phase == "original_error" {
				query.WillReturnError(original)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(false))
			}
			// No INSERT, allocator reset, or retry may follow either failure.
			mock.ExpectRollback()
			err := db.Table("events").Create(&row{Name: "blocked"}).Error
			if phase == "original_error" && err != original {
				t.Fatalf("catalog error replaced: %v", err)
			}
			if phase == "invalid_definition" && (err == nil || !strings.Contains(err.Error(), "modified uint64 allocator")) {
				t.Fatalf("invalid allocator accepted: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
