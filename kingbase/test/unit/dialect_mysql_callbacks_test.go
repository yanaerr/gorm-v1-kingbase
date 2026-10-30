package kingbase_test

import (
	"database/sql"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
)

func TestKingbaseMySQLInsertIgnoreRetainsErrorsAndRollsBack(t *testing.T) {
	for _, phase := range []string{"insert", "rows_affected", "id_lookup"} {
		t.Run(phase, func(t *testing.T) {
			db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
			defer close()
			db.LogMode(false)
			type row struct {
				ID   int64 `gorm:"primary_key"`
				Name string
			}
			failure := errors.New("original Kingbase failure")
			mock.ExpectBegin()
			expectNoCheckedSequences(mock)
			insert := mock.ExpectExec("^" + regexp.QuoteMeta("/* kingbase mysql insert ignore */ INSERT IGNORE INTO `events` (`name`) VALUES (?)") + "$").WithArgs("a")
			if phase == "insert" {
				insert.WillReturnError(failure)
			} else if phase == "rows_affected" {
				insert.WillReturnResult(sqlmock.NewErrorResult(failure))
			} else {
				insert.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery("^" + regexp.QuoteMeta("SELECT last_insert_id()") + "$").WillReturnError(failure)
			}
			mock.ExpectRollback()
			if err := db.Table("events").Set("gorm:insert_modifier", "IGNORE").Create(&row{Name: "a"}).Error; err != failure {
				t.Fatalf("original error was lost: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKingbaseMySQLCreateNormalizesCheckedUnsigned(t *testing.T) {
	failure := errors.New("sequence lookup unavailable")
	for _, test := range []struct {
		name, flags string
		input       *uint32
		owned       bool
		lookupError error
		sequenceErr error
		generated   int64
		cache       int64
		want        uint32
		fails       bool
	}{
		{name: "null", owned: true, generated: 3, want: 3},
		{name: "zero", input: new(uint32), owned: true, generated: 4, want: 4},
		{name: "zero_mode", input: new(uint32), owned: true, flags: "ANSI_QUOTES, NO_AUTO_VALUE_ON_ZERO", want: 0},
		{name: "ordinary_nullable_column", owned: false},
		{name: "unsupported_sequence_cache", owned: true, cache: 10, fails: true},
		{name: "metadata_error", lookupError: failure, fails: true},
		{name: "sequence_error", owned: true, sequenceErr: failure, fails: true},
		{name: "no_integer_wrap", owned: true, generated: 4294967296, fails: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
			defer close()
			db.LogMode(false)
			type row struct {
				ID     int `gorm:"primary_key;AUTO_INCREMENT:false"`
				Ticket *uint32
			}
			input := row{ID: 4, Ticket: test.input}
			mock.ExpectBegin()
			lookup := mock.ExpectQuery("^"+regexp.QuoteMeta(expectedCheckedSequencesQuery)+"$").WithArgs("`events`", "`events`")
			if test.lookupError != nil {
				lookup.WillReturnError(test.lookupError)
			} else {
				rows := sqlmock.NewRows([]string{"column", "constraint", "schema", "sequence", "cache"})
				if test.owned {
					cache := int64(1)
					if test.cache != 0 {
						cache = test.cache
					}
					rows.AddRow("ticket", "gorm_kb_uint_ticket", "public", "events_ticket_seq", cache)
				}
				lookup.WillReturnRows(rows)
			}
			if test.owned && test.cache == 0 {
				expectAutoSyncBefore(mock)
			}
			if test.owned && test.cache == 0 && test.input != nil {
				mock.ExpectQuery("^SHOW sql_mode$").WillReturnRows(sqlmock.NewRows([]string{"mode"}).AddRow(test.flags))
			}
			if test.owned && test.cache == 0 && test.flags == "" {
				next := mock.ExpectQuery("^" + regexp.QuoteMeta("SELECT nextval(?::regclass)") + "$").WithArgs("`public`.`events_ticket_seq`")
				if test.sequenceErr != nil {
					next.WillReturnError(test.sequenceErr)
				} else {
					next.WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(test.generated))
				}
			}
			if test.fails {
				mock.ExpectRollback()
			} else {
				var value interface{}
				if test.owned {
					value = test.want
				}
				mock.ExpectExec("^"+regexp.QuoteMeta("INSERT INTO `events` (`id`,`ticket`) VALUES (?,?)")+"$").WithArgs(4, value).WillReturnResult(sqlmock.NewResult(4, 1))
				mock.ExpectCommit()
			}
			err := db.Table("events").Create(&input).Error
			if (err != nil) != test.fails {
				t.Fatalf("Create error=%v, want failure=%t", err, test.fails)
			}
			if test.lookupError != nil && err != test.lookupError {
				t.Fatalf("original metadata error lost: %v", err)
			}
			if test.sequenceErr != nil && err != test.sequenceErr {
				t.Fatalf("original sequence error lost: %v", err)
			}
			if !test.fails && test.owned && (input.Ticket == nil || *input.Ticket != test.want) {
				t.Fatalf("generated field=%v, want %d", input.Ticket, test.want)
			}
			if !test.owned && test.lookupError == nil && input.Ticket != nil {
				t.Fatal("ordinary NULL column was changed")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKingbaseMySQLCreateHonorsOmitAndOtherDialects(t *testing.T) {
	for _, dialect := range []string{kb.DialectKingbaseMySQL, "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			db, mock, close := openMockGorm(t, dialect)
			defer close()
			db.LogMode(false)
			type row struct {
				ID     int `gorm:"primary_key;AUTO_INCREMENT:false"`
				Ticket *uint32
			}
			query := db.Table("events")
			insert := "INSERT INTO `events` (`id`,`ticket`) VALUES (?,?)"
			mock.ExpectBegin()
			if dialect == kb.DialectKingbaseMySQL {
				query = query.Omit("ticket")
				expectNoCheckedSequences(mock)
				insert = "INSERT INTO `events` (`id`) VALUES (?)"
				mock.ExpectExec("^" + regexp.QuoteMeta(insert) + "$").WithArgs(4).WillReturnResult(sqlmock.NewResult(4, 1))
			} else {
				mock.ExpectExec("^"+regexp.QuoteMeta(insert)+"$").WithArgs(4, nil).WillReturnResult(sqlmock.NewResult(4, 1))
			}
			mock.ExpectCommit()
			if err := query.Create(&row{ID: 4}).Error; err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKingbaseMySQLSequenceSyncErrorsRollbackCreate(t *testing.T) {
	failure := errors.New("sequence synchronization unavailable")
	for _, phase := range []string{"lock", "existing_rows", "after_insert", "success"} {
		t.Run(phase, func(t *testing.T) {
			db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
			defer close()
			db.LogMode(false)
			type row struct {
				ID     int `gorm:"primary_key;AUTO_INCREMENT:false"`
				Ticket uint32
			}
			mock.ExpectBegin()
			mock.ExpectQuery("^"+regexp.QuoteMeta(expectedCheckedSequencesQuery)+"$").WithArgs("`events`", "`events`").WillReturnRows(sqlmock.NewRows([]string{"column", "constraint", "schema", "sequence", "cache"}).AddRow("ticket", "gorm_kb_uint_ticket", "public", "events_ticket_seq", 1))
			lock := mock.ExpectExec("^SELECT pg_advisory_xact_lock").WithArgs("`events`")
			if phase == "lock" {
				lock.WillReturnError(failure)
			} else {
				lock.WillReturnResult(sqlmock.NewResult(0, 1))
				sync := mock.ExpectExec("^SELECT setval.*CAST\\(MAX").WithArgs("`public`.`events_ticket_seq`")
				if phase == "existing_rows" {
					sync.WillReturnError(failure)
				} else {
					sync.WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectExec("^INSERT INTO `events`").WithArgs(4, 12).WillReturnResult(sqlmock.NewResult(4, 1))
					after := mock.ExpectExec("^SELECT setval.*CAST\\(\\? AS bigint\\)").WithArgs("`public`.`events_ticket_seq`", 12, 12, 12)
					if phase == "after_insert" {
						after.WillReturnError(failure)
					} else {
						after.WillReturnResult(sqlmock.NewResult(0, 1))
					}
				}
			}
			if phase == "success" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := db.Table("events").Create(&row{ID: 4, Ticket: 12}).Error
			if phase == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err != failure {
				t.Fatalf("同步错误丢失: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// This SQL is the expected driver interaction, independent of implementation constants.
const expectedCheckedSequencesQuery = "SELECT a.attname, c.conname, n.nspname, s.relname, q.seqcache FROM pg_attribute a JOIN pg_constraint c ON c.conrelid=a.attrelid AND a.attnum=ANY(c.conkey) JOIN pg_class s ON s.oid=pg_get_serial_sequence(?, a.attname)::regclass AND s.relkind='S' JOIN pg_namespace n ON n.oid=s.relnamespace JOIN pg_sequence q ON q.seqrelid=s.oid WHERE a.attrelid=to_regclass(?) AND a.attnum>0 AND NOT a.attisdropped AND c.contype='c' ORDER BY a.attnum, c.conname"

func expectNoCheckedSequences(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("^"+regexp.QuoteMeta(expectedCheckedSequencesQuery)+"$").WithArgs("`events`", "`events`").WillReturnRows(sqlmock.NewRows([]string{"column", "constraint", "schema", "sequence", "cache"}))
}
func expectAutoSyncBefore(mock sqlmock.Sqlmock) {
	mock.ExpectExec("^" + regexp.QuoteMeta("SELECT pg_advisory_xact_lock((742617::bigint << 32) + to_regclass(?)::oid::bigint)") + "$").WithArgs("`events`").WillReturnResult(sqlmock.NewResult(0, 1))
	query := "SELECT setval(?::regclass, m.v, true) FROM `public`.`events_ticket_seq` s CROSS JOIN (SELECT CAST(MAX(`ticket`) AS bigint) AS v FROM `events`) m WHERE m.v > s.last_value OR (m.v = s.last_value AND NOT s.is_called)"
	mock.ExpectExec("^" + regexp.QuoteMeta(query) + "$").WithArgs("`public`.`events_ticket_seq`").WillReturnResult(sqlmock.NewResult(0, 0))
}

func newAutoScope(db *gorm.DB, input interface{}) *gorm.Scope {
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "ID", Type: reflect.TypeOf(int(0)), Tag: `gorm:"primary_key;AUTO_INCREMENT:false"`},
		{Name: "Ticket", Type: reflect.TypeOf(input)},
	})
	model := reflect.New(typ)
	model.Elem().FieldByName("ID").SetInt(4)
	model.Elem().FieldByName("Ticket").Set(reflect.ValueOf(input))
	return db.Table("events").NewScope(model.Interface())
}

func invokeAutoCallback(t *testing.T, scope *gorm.Scope) {
	t.Helper()
	callback := scope.DB().Callback().Create().Get("kingbase:mysql_auto_increment")
	if callback == nil {
		t.Fatal("Kingbase Create extension was not registered")
	}
	callback(scope)
}

func expectAutoSequence(mock sqlmock.Sqlmock, zero bool, generated int64) {
	mock.ExpectQuery("^"+regexp.QuoteMeta(expectedCheckedSequencesQuery)+"$").WithArgs("`events`", "`events`").WillReturnRows(sqlmock.NewRows([]string{"column", "constraint", "schema", "sequence", "cache"}).AddRow("ticket", "gorm_kb_uint_ticket", "public", "events_ticket_seq", 1))
	expectAutoSyncBefore(mock)
	if zero {
		mock.ExpectQuery("^SHOW sql_mode$").WillReturnRows(sqlmock.NewRows([]string{"mode"}).AddRow(""))
	}
	mock.ExpectQuery("^" + regexp.QuoteMeta("SELECT nextval(?::regclass)") + "$").WithArgs("`public`.`events_ticket_seq`").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(generated))
}

func TestKingbaseMySQLAutoValueCannotOverflow(t *testing.T) {
	for _, test := range []struct {
		typ   reflect.Type
		value int64
		want  bool
	}{
		{reflect.TypeOf(uint8(0)), 255, true},
		{reflect.TypeOf(uint8(0)), 256, false},
		{reflect.TypeOf((*uint32)(nil)), 4294967295, true},
		{reflect.TypeOf((*uint32)(nil)), 4294967296, false},
		{reflect.TypeOf(uint64(0)), -1, false},
		{reflect.TypeOf(uintptr(0)), -1, false},
		{reflect.TypeOf(int8(0)), 128, false},
	} {
		db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
		db.LogMode(false)
		input := reflect.Zero(test.typ).Interface()
		mock.ExpectBegin()
		tx := db.Begin()
		scope := newAutoScope(tx, input)
		expectAutoSequence(mock, test.typ.Kind() != reflect.Ptr, test.value)
		invokeAutoCallback(t, scope)
		err := scope.DB().Error
		if (err == nil) != test.want {
			t.Errorf("%s generated=%d: error=%v, want success=%t", test.typ, test.value, err, test.want)
		}
		if !test.want && (err == nil || !strings.Contains(err.Error(), "does not fit field ticket")) {
			t.Errorf("expected explicit overflow error, got %v", err)
		}
		field, _ := scope.FieldByName("Ticket")
		if !test.want {
			if !reflect.DeepEqual(field.Field.Interface(), input) {
				t.Errorf("overflow changed original value: %v", field.Field.Interface())
			}
		} else {
			value := field.Field
			for value.Kind() == reflect.Ptr {
				value = value.Elem()
			}
			var got int64
			if value.Kind() >= reflect.Int && value.Kind() <= reflect.Int64 {
				got = value.Int()
			} else {
				got = int64(value.Uint())
			}
			if got != test.value {
				t.Errorf("generated value truncated: got %d, want %d", got, test.value)
			}
		}
		mock.ExpectRollback()
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		close()
	}
}

func TestKingbaseMySQLAutoInputDistinguishesNullZeroAndOrdinaryValues(t *testing.T) {
	var pointer *uintptr
	for _, test := range []struct {
		value      interface{}
		null, zero bool
	}{
		{pointer, true, false}, {uintptr(0), false, true},
		{uint32(0), false, true}, {uint32(12), false, false},
		{sql.NullInt64{}, true, false}, {sql.NullInt64{Valid: true}, false, true},
		{float64(0), false, false}, {"0", false, false},
	} {
		db, mock, close := openMockGorm(t, kb.DialectKingbaseMySQL)
		mock.ExpectBegin()
		tx := db.Begin()
		scope := newAutoScope(tx, test.value)
		if test.null || test.zero {
			expectAutoSequence(mock, test.zero, 7)
		} else {
			expectNoCheckedSequences(mock)
		}
		invokeAutoCallback(t, scope)
		if err := scope.DB().Error; err != nil {
			t.Errorf("input %T: %v", test.value, err)
		}
		field, _ := scope.FieldByName("Ticket")
		if test.null || test.zero {
			value := field.Field
			for value.Kind() == reflect.Ptr {
				value = value.Elem()
			}
			if integer, ok := value.Interface().(sql.NullInt64); ok {
				if !integer.Valid || integer.Int64 != 7 {
					t.Errorf("nullable integer did not receive generated value: %+v", integer)
				}
			} else if value.Uint() != 7 {
				t.Errorf("numeric input %T did not receive 7: %v", test.value, value.Interface())
			}
		} else if !reflect.DeepEqual(field.Field.Interface(), test.value) {
			t.Errorf("ordinary input %T was changed: %v", test.value, field.Field.Interface())
		}
		mock.ExpectRollback()
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		close()
	}
}
