package kingbase

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"

	"github.com/jinzhu/gorm"
)

var mysqlCallbacks sync.Once

func registerMySQLCallbacks() {
	mysqlCallbacks.Do(func() {
		create := gorm.DefaultCallback.Create().Get("gorm:create")
		gorm.DefaultCallback.Create().Replace("gorm:create", func(scope *gorm.Scope) {
			modifier, _ := scope.Get("gorm:insert_modifier")
			if _, mysql := scope.Dialect().(*kingbaseMySQLDialect); mysql && strings.EqualFold(strings.TrimSpace(fmt.Sprint(modifier)), "IGNORE") {
				createMySQLIgnore(scope)
				return
			}
			if mysqlHasUint64Sequence(scope) {
				createMySQLAdapted(scope, false)
				return
			}
			create(scope)
		})
		gorm.DefaultCallback.Create().Before("gorm:create").Register("kingbase:mysql_auto_increment", normalizeMySQLAutoIncrement)
		gorm.DefaultCallback.Create().After("gorm:create").Register("kingbase:mysql_auto_increment_sync", synchronizeMySQLAutoIncrement)
	})
}

// INSERT IGNORE cannot be combined with the RETURNING appended by gokb.
// A leading comment suppresses that driver rewrite without changing the
// server's native INSERT IGNORE semantics. Keep GORM's surrounding callbacks
// (hooks, associations, defaults and transaction handling) in place.
func createMySQLIgnore(scope *gorm.Scope) {
	createMySQLAdapted(scope, true)
}

func mysqlHasUint64Sequence(scope *gorm.Scope) bool {
	if _, ok := scope.Dialect().(*kingbaseMySQLDialect); !ok {
		return false
	}
	if value, ok := scope.InstanceGet("kingbase:mysql_auto_sequences"); ok {
		for _, sequence := range value.([]mysqlCheckedSequence) {
			if sequence.fullUint64 {
				return true
			}
		}
	}
	return false
}

// Keep the surrounding GORM callbacks. A comment suppresses gokb's signed
// LastInsertId/automatic RETURNING path; read the owned allocator in this Tx.
func createMySQLAdapted(scope *gorm.Scope, ignore bool) {
	if scope.HasError() {
		return
	}
	if _, ok := scope.SQLDB().(interface {
		Commit() error
		Rollback() error
	}); !ok {
		scope.Err(fmt.Errorf("kingbase-mysql: adapted INSERT requires a Create transaction"))
		return
	}
	if mysqlHasUint64Sequence(scope) {
		modifier, _ := scope.Get("gorm:insert_modifier")
		if text := strings.ToUpper(strings.TrimSpace(fmt.Sprint(modifier))); modifier != nil && text != "" && text != "INTO" && text != "IGNORE" {
			scope.Err(fmt.Errorf("kingbase-mysql: uint64 allocator does not support insert modifier %q", text))
			return
		}
		if option, ok := scope.Get("gorm:insert_option"); ok && strings.TrimSpace(fmt.Sprint(option)) != "" {
			scope.Err(fmt.Errorf("kingbase-mysql: uint64 allocator does not support insert_option"))
			return
		}
	}
	var columns, values, defaults []string
	for _, field := range scope.Fields() {
		if !mysqlCreateFieldSelected(scope, field) {
			continue
		}
		if field.IsNormal && !field.IsIgnored {
			if field.IsBlank && field.HasDefaultValue {
				defaults = append(defaults, scope.Quote(field.DBName))
			} else if !field.IsPrimaryKey || !field.IsBlank {
				columns = append(columns, scope.Quote(field.DBName))
				values = append(values, scope.AddToVars(field.Field.Interface()))
			}
		} else if field.Relationship != nil && field.Relationship.Kind == "belongs_to" {
			for _, key := range field.Relationship.ForeignDBNames {
				if foreign, ok := scope.FieldByName(key); ok && !mysqlCreateFieldSelected(scope, foreign) {
					columns = append(columns, scope.Quote(foreign.DBName))
					values = append(values, scope.AddToVars(foreign.Field.Interface()))
				}
			}
		}
	}
	if len(defaults) != 0 {
		scope.InstanceSet("gorm:blank_columns_with_default_value", defaults)
	}
	query := "/* kingbase mysql insert ignore */ INSERT IGNORE INTO " + scope.QuotedTableName()
	if !ignore {
		query = "/* kingbase mysql uint64 */ INSERT INTO " + scope.QuotedTableName()
	}
	if len(columns) == 0 {
		query += " " + scope.Dialect().DefaultValueStr()
	} else {
		query += " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(values, ",") + ")"
	}
	if option, ok := scope.Get("gorm:insert_option"); ok {
		query += " " + fmt.Sprint(option)
	}
	scope.Raw(query)
	result, err := scope.SQLDB().Exec(scope.SQL, scope.SQLVars...)
	if scope.Err(err) != nil {
		return
	}
	affected, err := result.RowsAffected()
	if scope.Err(err) != nil {
		return
	}
	scope.DB().RowsAffected = affected
	if affected == 0 {
		return
	}
	if primary := scope.PrimaryField(); primary != nil && primary.IsBlank {
		var id uint64
		query := "SELECT last_insert_id()"
		var args []interface{}
		// The native MySQL ID accessor is not the generated value for checked
		// serial columns on this instance. Their owned sequence is authoritative.
		if value, ok := scope.InstanceGet("kingbase:mysql_auto_sequences"); ok {
			for _, sequence := range value.([]mysqlCheckedSequence) {
				if sequence.column == primary.DBName {
					query = "SELECT currval(?::regclass)"
					if sequence.fullUint64 {
						query = "SELECT " + mysqlUint64ValueSQL("currval")
					}
					args = []interface{}{sequence.name}
					break
				}
			}
		}
		if scope.Err(scope.SQLDB().QueryRow(query, args...).Scan(&id)) == nil {
			if !mysqlUint64ValueFits(primary.Field.Type(), id) {
				scope.Err(fmt.Errorf("kingbase-mysql: generated uint64 value %d does not fit primary field %s", id, primary.DBName))
				return
			}
			scope.Err(primary.Set(id))
		}
	}
}

func mysqlCreateFieldSelected(scope *gorm.Scope, field *gorm.Field) bool {
	if selected := scope.SelectAttrs(); len(selected) > 0 {
		for _, name := range selected {
			if name == field.Name || name == field.DBName {
				return true
			}
		}
		return false
	}
	for _, name := range scope.OmitAttrs() {
		if name == field.Name || name == field.DBName {
			return false
		}
	}
	return true
}

// Checked unsigned serials need MySQL semantics around explicit values and
// NULL/zero inputs. Serialize ORM writers until transaction end so setval
// cannot rewind a concurrent allocation. Raw SQL remains untouched.
func normalizeMySQLAutoIncrement(scope *gorm.Scope) {
	if scope.HasError() {
		return
	}
	if _, ok := scope.Dialect().(*kingbaseMySQLDialect); !ok {
		return
	}
	table := scope.QuotedTableName()
	sequences, err := mysqlCheckedSequences(scope, table)
	if err != nil {
		scope.Err(err)
		return
	}
	if len(sequences) == 0 {
		return
	}
	for _, sequence := range sequences {
		if sequence.fullUint64 {
			var increment, offset string
			if err := scope.SQLDB().QueryRow("SELECT current_setting('auto_increment_increment'), current_setting('auto_increment_offset')").Scan(&increment, &offset); err != nil {
				scope.Err(err)
				return
			}
			if increment != "1" || offset != "1" {
				scope.Err(fmt.Errorf("kingbase-mysql: uint64 allocator requires auto_increment_increment=1 and auto_increment_offset=1"))
				return
			}
			break
		}
	}
	// The transaction lock must span the INSERT and the post-insert sync.
	if _, ok := scope.SQLDB().(interface {
		Commit() error
		Rollback() error
	}); !ok {
		scope.Err(fmt.Errorf("kingbase-mysql: checked AUTO_INCREMENT requires a Create transaction"))
		return
	}
	if _, err := scope.SQLDB().Exec(mysqlAutoIncrementLock, table); err != nil {
		scope.Err(err)
		return
	}
	var flags string
	flagsRead := false
	for i := range sequences {
		sequence := &sequences[i]
		// Include existing rows on the first adapted write; do not use MAX alone,
		// since DELETE/rollback must not cause already allocated values to be reused.
		query := "SELECT setval(?::regclass, m.v, true) FROM " + sequence.name + " s CROSS JOIN (SELECT CAST(MAX(" + quoteMySQLIdentifier(sequence.column) + ") AS bigint) AS v FROM " + table + ") m WHERE m.v > s.last_value OR (m.v = s.last_value AND NOT s.is_called)"
		if sequence.fullUint64 {
			query = "SELECT setval(?::regclass, CAST(m.v AS bigint), true) FROM " + sequence.name + " s CROSS JOIN (SELECT CAST(MAX(" + quoteMySQLIdentifier(sequence.column) + ") AS numeric(40,0))-CAST('" + mysqlUint64Offset + "' AS numeric(40,0)) AS v FROM " + table + ") m WHERE m.v > s.last_value OR (m.v = s.last_value AND NOT s.is_called)"
		}
		if _, err := scope.SQLDB().Exec(query, sequence.name); err != nil {
			scope.Err(err)
			return
		}
		var field *gorm.Field
		for _, candidate := range scope.Fields() {
			if candidate.DBName == sequence.column {
				field = candidate
				break
			}
		}
		// GORM will use the sequence default for omitted/defaulted columns, even
		// when the input struct does not contain the column at all.
		if field == nil || !mysqlCreateSendsField(scope, field) {
			continue
		}
		null, zero, err := mysqlAutoInput(field.Field)
		if err != nil {
			scope.Err(err)
			return
		}
		if !null && !zero {
			sequence.explicit = mysqlPositiveAutoInput(field.Field)
			if sequence.fullUint64 {
				sequence.unsignedExplicit = mysqlUnsignedAutoInput(field.Field)
			}
			continue
		}
		if zero {
			if !flagsRead {
				if err := scope.SQLDB().QueryRow("SHOW sql_mode").Scan(&flags); err != nil {
					scope.Err(err)
					return
				}
				flagsRead = true
			}
			if mysqlModeEnabled(flags, "NO_AUTO_VALUE_ON_ZERO") {
				continue
			}
		}
		if sequence.fullUint64 {
			var value uint64
			if err := scope.SQLDB().QueryRow("SELECT "+mysqlUint64ValueSQL("nextval"), sequence.name).Scan(&value); err != nil {
				scope.Err(err)
				return
			}
			if !mysqlUint64ValueFits(field.Field.Type(), value) {
				scope.Err(fmt.Errorf("kingbase-mysql: generated uint64 value %d does not fit field %s", value, field.DBName))
				return
			}
			if err := field.Set(value); err != nil {
				scope.Err(err)
				return
			}
			continue
		}
		var value int64
		if err := scope.SQLDB().QueryRow("SELECT nextval(?::regclass)", sequence.name).Scan(&value); err != nil {
			scope.Err(err)
			return
		}
		if !mysqlAutoValueFits(field.Field.Type(), value) {
			scope.Err(fmt.Errorf("kingbase-mysql: generated value %d does not fit field %s (%s)", value, field.DBName, field.Field.Type()))
			return
		}
		if err := field.Set(value); err != nil {
			scope.Err(err)
			return
		}
	}
	scope.InstanceSet("kingbase:mysql_auto_sequences", sequences)
}

// Only successful INSERTs advance to an explicitly supplied positive value.
// Failed CHECK/unique inserts must keep their original error and cannot poison
// a narrow unsigned sequence with an out-of-range value.
func synchronizeMySQLAutoIncrement(scope *gorm.Scope) {
	if scope.HasError() || scope.DB().RowsAffected == 0 {
		return
	}
	value, ok := scope.InstanceGet("kingbase:mysql_auto_sequences")
	if !ok {
		return
	}
	for _, sequence := range value.([]mysqlCheckedSequence) {
		if sequence.fullUint64 {
			if sequence.unsignedExplicit != 0 {
				mapped := int64(sequence.unsignedExplicit ^ (uint64(1) << 63))
				query := "SELECT setval(?::regclass, CAST(? AS bigint), true) FROM " + sequence.name + " WHERE last_value < CAST(? AS bigint) OR (last_value = CAST(? AS bigint) AND NOT is_called)"
				if _, err := scope.SQLDB().Exec(query, sequence.name, mapped, mapped, mapped); err != nil {
					scope.Err(err)
					return
				}
			}
			continue
		}
		if sequence.explicit <= 0 {
			continue
		}
		query := "SELECT setval(?::regclass, CAST(? AS bigint), true) FROM " + sequence.name + " WHERE last_value < CAST(? AS bigint) OR (last_value = CAST(? AS bigint) AND NOT is_called)"
		if _, err := scope.SQLDB().Exec(query, sequence.name, sequence.explicit, sequence.explicit, sequence.explicit); err != nil {
			scope.Err(err)
			return
		}
	}
}

type mysqlCheckedSequence struct {
	column, name     string
	explicit         int64
	fullUint64       bool
	unsignedExplicit uint64
}

// Match both the reserved CHECK name and the actual owned serial sequence.
// Read database identifiers separately and quote them in Go, including dots
// and embedded backticks. No table metadata is cached across schema changes.
const mysqlCheckedSequencesQuery = "SELECT a.attname, c.conname, n.nspname, s.relname, q.seqcache FROM pg_attribute a JOIN pg_constraint c ON c.conrelid=a.attrelid AND a.attnum=ANY(c.conkey) JOIN pg_class s ON s.oid=pg_get_serial_sequence(?, a.attname)::regclass AND s.relkind='S' JOIN pg_namespace n ON n.oid=s.relnamespace JOIN pg_sequence q ON q.seqrelid=s.oid WHERE a.attrelid=to_regclass(?) AND a.attnum>0 AND NOT a.attisdropped AND c.contype='c' ORDER BY a.attnum, c.conname"

// A private advisory-lock namespace plus the table OID identifies one table
// consistently across schemas, model shapes and adapter processes.
const mysqlAutoIncrementLock = "SELECT pg_advisory_xact_lock((742617::bigint << 32) + to_regclass(?)::oid::bigint)"

func mysqlCheckedSequences(scope *gorm.Scope, table string) ([]mysqlCheckedSequence, error) {
	rows, err := scope.SQLDB().Query(mysqlCheckedSequencesQuery, table, table)
	if err != nil {
		return nil, err
	}
	var sequences []mysqlCheckedSequence
	for rows.Next() {
		var column, constraint, schema, name string
		var cache int64
		if err := rows.Scan(&column, &constraint, &schema, &name, &cache); err != nil {
			rows.Close()
			return nil, err
		}
		if constraint == mysqlUnsignedConstraintName(column) {
			if cache != 1 {
				rows.Close()
				return nil, fmt.Errorf("kingbase-mysql: checked AUTO_INCREMENT column %s requires sequence CACHE 1 (got %d)", column, cache)
			}
			sequences = append(sequences, mysqlCheckedSequence{column: column, name: quoteMySQLIdentifier(schema) + "." + quoteMySQLIdentifier(name)})
		}
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	// Read additional catalogs only for explicitly installed full-range
	// allocators, after closing rows on the same transaction connection.
	for i := range sequences {
		_, name := splitSchemaTable(sequences[i].name)
		if strings.HasPrefix(name, mysqlUint64SequencePrefix) {
			if err := validateMySQLUint64Sequence(scope.SQLDB(), table, sequences[i].column, sequences[i].name); err != nil {
				return nil, err
			}
			sequences[i].fullUint64 = true
		}
	}
	return sequences, nil
}

func mysqlUnsignedAutoInput(value reflect.Value) uint64 {
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return 0
		}
		value = value.Elem()
	}
	if value.Kind() >= reflect.Uint && value.Kind() <= reflect.Uintptr {
		return value.Uint()
	}
	if value.Kind() >= reflect.Int && value.Kind() <= reflect.Int64 && value.Int() > 0 {
		return uint64(value.Int())
	}
	return uint64(mysqlPositiveAutoInput(value))
}

func mysqlUint64ValueFits(typ reflect.Type, value uint64) bool {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	zero := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value <= math.MaxInt64 && !zero.OverflowInt(int64(value))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return !zero.OverflowUint(value)
	}
	return value <= math.MaxInt64 // Nullable signed SQL integers must not wrap.
}

func mysqlPositiveAutoInput(value reflect.Value) int64 {
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return 0
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n := value.Int(); n > 0 {
			return n
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if n := value.Uint(); n > 0 && n <= math.MaxInt64 {
			return int64(n)
		}
	default:
		if value.CanInterface() {
			if v, ok := value.Interface().(driver.Valuer); ok {
				if raw, err := v.Value(); err == nil {
					if n, ok := raw.(int64); ok && n > 0 {
						return n
					}
				}
			}
		}
	}
	return 0
}

func mysqlCreateSendsField(scope *gorm.Scope, field *gorm.Field) bool {
	if !field.IsNormal || field.IsIgnored || !field.Field.IsValid() || (field.IsBlank && (field.HasDefaultValue || field.IsPrimaryKey)) {
		return false
	}
	return mysqlCreateFieldSelected(scope, field)
}

func mysqlAutoInput(value reflect.Value) (null, zero bool, err error) {
	typ := value.Type()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Uintptr {
		for value.Kind() == reflect.Ptr {
			if value.IsNil() {
				return true, false, nil
			}
			value = value.Elem()
		}
		if value.Kind() >= reflect.Int && value.Kind() <= reflect.Int64 {
			return false, value.Int() == 0, nil
		}
		return false, value.Uint() == 0, nil
	}
	// Nullable SQL integers expose a driver.Value and implement sql.Scanner
	// for the generated result. Leave other application types to the driver.
	if value.CanAddr() {
		if _, ok := value.Addr().Interface().(sql.Scanner); ok {
			if valuer, ok := value.Interface().(driver.Valuer); ok {
				v, err := valuer.Value()
				if err != nil {
					return false, false, err
				}
				if v == nil {
					return true, false, nil
				}
				if number, ok := v.(int64); ok {
					return false, number == 0, nil
				}
			}
		}
	}
	return false, false, nil
}

func mysqlAutoValueFits(typ reflect.Type, value int64) bool {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	zero := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return !zero.OverflowInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value >= 0 && !zero.OverflowUint(uint64(value))
	}
	return true // sql.Scanner validates its own destination.
}

func mysqlModeEnabled(flags, flag string) bool {
	for _, mode := range strings.Split(flags, ",") {
		if strings.EqualFold(strings.TrimSpace(mode), flag) {
			return true
		}
	}
	return false
}
