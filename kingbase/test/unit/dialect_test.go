package kingbase_test

import (
	"database/sql"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
)

func openMockGorm(t *testing.T, dialect string) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	db, err := gorm.Open(dialect, sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("open gorm: %v", err)
	}
	return db, mock, func() {
		_ = db.Close()
	}
}

func fieldType(t *testing.T, db *gorm.DB, model interface{}, name string) string {
	t.Helper()
	for _, field := range db.NewScope(model).GetModelStruct().StructFields {
		if field.Name == name {
			return strings.ToLower(db.Dialect().DataTypeOf(field))
		}
	}
	t.Fatalf("field %s not found", name)
	return ""
}

func TestKingbaseIdentifierBoundaryHelpers(t *testing.T) {
	dialect := kb.NewKingbaseDialect(kb.KbModeOracle)
	for _, test := range []struct{ input, want string }{
		{"  `APP`  ", "APP"}, {"[Order]", "Order"},
		{`"order""detail"`, `order"detail`}, {"`order``detail`", "order`detail"},
		{"]unbalanced[", "]unbalanced["},
	} {
		want := `"` + strings.ReplaceAll(test.want, `"`, `""`) + `"`
		if got := dialect.Quote(test.input); got != want {
			t.Errorf("Quote(%q)=%q, want %q", test.input, got, want)
		}
	}
	db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
	defer close()
	for _, test := range []struct{ input, schema, name string }{
		{" APP.Order ", "APP", "Order"}, {"`APP`.`Order`", "APP", "Order"}, {"users", "", "users"},
	} {
		if test.schema == "" {
			mock.ExpectQuery("^" + regexp.QuoteMeta("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?") + "$").WithArgs(test.name).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		} else {
			mock.ExpectQuery("^"+regexp.QuoteMeta("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?")+"$").WithArgs(test.schema, test.name).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		}
		if !db.Dialect().HasTable(test.input) {
			t.Errorf("HasTable(%q) did not preserve schema/name", test.input)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if got, want := dialect.Quote("APP.Order"), `"APP.Order"`; got != want {
		t.Fatalf("single identifier: got %q, want %q", got, want)
	}
	oracle, omock, oclose := openMockGorm(t, kb.DialectKingbaseOracle)
	defer oclose()
	omock.ExpectExec("^" + regexp.QuoteMeta(`ALTER TABLE "APP"."Order" MODIFY "name" varchar2(32)`) + "$").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := oracle.Table(`"APP"."Order"`).ModifyColumn("name", "varchar2(32)").Error; err != nil {
		t.Fatal(err)
	}
	if err := omock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestKingbaseDialectPaginationBoundaryValues(t *testing.T) {
	tests := []struct {
		name    string
		dialect gorm.Dialect
		limit   interface{}
		offset  interface{}
		want    string
	}{
		{name: "mysql zero limit", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), limit: 0, want: " LIMIT 0"},
		{name: "mysql zero limit and offset", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), limit: 0, offset: 0, want: " LIMIT 0 OFFSET 0"},
		{name: "mysql offset without limit", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), offset: 10, want: " OFFSET 10"},
		{name: "mysql negative limit retains offset", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), limit: -1, offset: 10, want: " OFFSET 10"},
		{name: "oracle zero limit", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), limit: 0, want: " FETCH FIRST 0 ROWS ONLY"},
		{name: "oracle zero offset", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), offset: 0, want: " OFFSET 0 ROWS"},
		{name: "oracle zero limit and offset", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), limit: 0, offset: 0, want: " OFFSET 0 ROWS FETCH NEXT 0 ROWS ONLY"},
		{name: "oracle negative limit with offset", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), limit: -1, offset: 0, want: " OFFSET 0 ROWS"},
		{name: "oracle limit with negative offset", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), limit: 0, offset: -1, want: " FETCH FIRST 0 ROWS ONLY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.dialect.LimitAndOffsetSQL(tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("LimitAndOffsetSQL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("pagination mismatch: got %q, want %q", got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name    string
		dialect gorm.Dialect
		limit   interface{}
		offset  interface{}
	}{
		{name: "mysql fractional limit", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), limit: "1.5"},
		{name: "mysql bool offset", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), limit: 1, offset: true},
		{name: "mysql invalid offset without limit", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), offset: "invalid"},
		{name: "mysql overflow offset", dialect: kb.NewKingbaseDialect(kb.KbModeMySQL), offset: "9223372036854775808"},
		{name: "oracle fractional limit", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), limit: "1.5"},
		{name: "oracle overflow offset", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), offset: "9223372036854775808"},
		{name: "oracle underflow limit", dialect: kb.NewKingbaseDialect(kb.KbModeOracle), limit: "-9223372036854775809"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.dialect.LimitAndOffsetSQL(tt.limit, tt.offset); err == nil {
				t.Fatal("expected invalid pagination value to return an error")
			}
		})
	}
}

func TestKingbaseMetadataNoConnectionIsSafeAtBoundary(t *testing.T) {
	for _, dialect := range []gorm.Dialect{
		kb.NewKingbaseDialect(kb.KbModeMySQL),
		kb.NewKingbaseDialect(kb.KbModeOracle),
	} {
		if dialect.HasTable("APP.users") || dialect.HasColumn("APP.users", "id") ||
			dialect.HasIndex("APP.users", "idx_users") || dialect.HasForeignKey("APP.users", "fk_users") {
			t.Fatalf("metadata lookup without a DB unexpectedly succeeded for %s", dialect.GetName())
		}
		if got := dialect.CurrentDatabase(); got != "" {
			t.Fatalf("CurrentDatabase without a DB = %q, want empty", got)
		}
		if err := dialect.RemoveIndex("APP.users", "idx_users"); err != sql.ErrConnDone {
			t.Fatalf("RemoveIndex without a DB = %v, want sql.ErrConnDone", err)
		}
		if err := dialect.ModifyColumn("APP.users", "name", "varchar(32)"); err != sql.ErrConnDone {
			t.Fatalf("ModifyColumn without a DB = %v, want sql.ErrConnDone", err)
		}
	}
}

func TestNewKingbaseDialectInitializesMode(t *testing.T) {
	tests := []struct {
		mode kb.KbMode
		want string
	}{
		{mode: kb.KbModeMySQL, want: kb.DialectKingbaseMySQL},
		{mode: kb.KbModeOracle, want: kb.DialectKingbaseOracle},
	}
	for _, tt := range tests {
		if got := kb.NewKingbaseDialect(tt.mode).GetName(); got != tt.want {
			t.Errorf("mode %q returned dialect %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestNewKingbaseDialectRejectsUnsupportedMode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported mode should panic")
		}
	}()

	kb.NewKingbaseDialect(kb.KbMode("unsupported"))
}

// definitionType sends the original definition through the public Dialect API.
func definitionType(dialect gorm.Dialect, definition, column string) string {
	return dialect.DataTypeOf(&gorm.StructField{
		Name: "Value", DBName: column, IsNormal: true,
		Struct:      reflect.StructField{Name: "Value", Type: reflect.TypeOf(int64(0))},
		TagSettings: map[string]string{"TYPE": definition},
	})
}
