package kingbase

import (
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
)

// kingbaseOracleDialect is intentionally self-contained. Oracle-mode SQL must
// not inherit behavior from another GORM dialect.
type kingbaseOracleDialect struct {
	db gorm.SQLCommon
}

var _ gorm.Dialect = (*kingbaseOracleDialect)(nil)

func (*kingbaseOracleDialect) GetName() string { return DialectKingbaseOracle }

func (d *kingbaseOracleDialect) SetDB(db gorm.SQLCommon) { d.db = db }

// gokb normalizes question-mark placeholders to the wire-level placeholders
// expected by KingbaseES in every database mode.
func (*kingbaseOracleDialect) BindVar(int) string { return "?" }

func (*kingbaseOracleDialect) Quote(key string) string {
	return quoteOracleIdentifier(key)
}

func (d *kingbaseOracleDialect) DataTypeOf(field *gorm.StructField) string {
	return d.rewriteOracleType(field)
}

func (d *kingbaseOracleDialect) HasIndex(tableName, indexName string) bool {
	return d.hasIndexOracleCompat(tableName, indexName)
}

func (d *kingbaseOracleDialect) HasForeignKey(tableName, foreignKeyName string) bool {
	return d.hasForeignKeyOracleCompat(tableName, foreignKeyName)
}

func (d *kingbaseOracleDialect) RemoveIndex(tableName, indexName string) error {
	if d.db == nil {
		return sql.ErrConnDone
	}

	schema, _ := splitSchemaTable(tableName)
	indexSchema, index := splitSchemaTable(indexName)
	if indexSchema == "" {
		indexSchema = schema
	}
	if indexSchema != "" {
		index = indexSchema + "." + index
	}
	_, err := d.db.Exec("DROP INDEX " + index)
	return err
}

func (d *kingbaseOracleDialect) HasTable(tableName string) bool {
	return d.hasTableOracleCompat(tableName)
}

func (d *kingbaseOracleDialect) HasColumn(tableName, columnName string) bool {
	return d.hasColumnOracleCompat(tableName, columnName)
}

func (d *kingbaseOracleDialect) ModifyColumn(tableName, columnName, typ string) error {
	return d.modifyColumnOracleCompat(tableName, columnName, typ)
}

func (*kingbaseOracleDialect) LimitAndOffsetSQL(limit, offset interface{}) (string, error) {
	return limitAndOffsetOracleCompat(limit, offset)
}

func (*kingbaseOracleDialect) SelectFromDummyTable() string { return "FROM DUAL" }

func (*kingbaseOracleDialect) LastInsertIDOutputInterstitial(string, string, []string) string {
	return ""
}

func (*kingbaseOracleDialect) LastInsertIDReturningSuffix(tableName, columnName string) string {
	return fmt.Sprintf("RETURNING %s.%s", tableName, columnName)
}

func (*kingbaseOracleDialect) DefaultValueStr() string { return "DEFAULT VALUES" }

func (*kingbaseOracleDialect) BuildKeyName(kind, tableName string, fields ...string) string {
	return (gorm.DefaultForeignKeyNamer{}).BuildKeyName(kind, tableName, fields...)
}

func (*kingbaseOracleDialect) NormalizeIndexAndColumn(indexName, columnName string) (string, string) {
	return indexName, columnName
}

func (d *kingbaseOracleDialect) CurrentDatabase() string { return d.currentSchema() }

func (d *kingbaseOracleDialect) queryCount(query string, args ...interface{}) (int, error) {
	if d.db == nil {
		return 0, sql.ErrConnDone
	}
	var count int
	err := d.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (d *kingbaseOracleDialect) currentSchema() (name string) {
	if d.db == nil {
		return ""
	}
	_ = d.db.QueryRow("SELECT current_schema()").Scan(&name)
	return
}

func quoteOracleIdentifier(identifier string) string {
	identifier = cleanIdentifier(identifier)
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func quoteOracleIdentifierPath(identifier string) string {
	schema, name := splitSchemaTable(identifier)
	if schema == "" {
		return quoteOracleIdentifier(name)
	}
	return quoteOracleIdentifier(schema) + "." + quoteOracleIdentifier(name)
}

func (d *kingbaseOracleDialect) modifyColumnOracleCompat(tableName, columnName, typ string) error {
	if d.db == nil {
		return sql.ErrConnDone
	}
	_, err := d.db.Exec(fmt.Sprintf(
		"ALTER TABLE %s MODIFY %s %s",
		quoteOracleIdentifierPath(tableName),
		quoteOracleIdentifier(columnName),
		typ,
	))
	return err
}

func limitAndOffsetOracleCompat(limit, offset interface{}) (string, error) {
	var (
		parsedLimit  int64 = -1
		parsedOffset int64 = -1
	)
	if limit != nil {
		value, err := strconv.ParseInt(fmt.Sprint(limit), 0, 0)
		if err != nil {
			return "", err
		}
		parsedLimit = value
	}
	if offset != nil {
		value, err := strconv.ParseInt(fmt.Sprint(offset), 0, 0)
		if err != nil {
			return "", err
		}
		parsedOffset = value
	}

	if parsedOffset >= 0 {
		if parsedLimit >= 0 {
			return fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", parsedOffset, parsedLimit), nil
		}
		return fmt.Sprintf(" OFFSET %d ROWS", parsedOffset), nil
	}
	if parsedLimit >= 0 {
		return fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", parsedLimit), nil
	}
	return "", nil
}

// rewriteOracleType maps common GORM field definitions to Kingbase Oracle-mode
// types while preserving tags such as not null, unique and default.
func (d *kingbaseOracleDialect) rewriteOracleType(f *gorm.StructField) string {
	dataValue, sqlType, size, additionalType := gorm.ParseFieldStructForDialect(f, d)

	if sqlType != "" {
		sqlType = rewriteOracleExplicitType(sqlType)
	} else {
		switch dataValue.Kind() {
		case reflect.Bool:
			sqlType = "boolean"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uintptr:
			if kingbaseFieldCanAutoIncrement(f) {
				f.TagSettingsSet("AUTO_INCREMENT", "AUTO_INCREMENT")
				sqlType = "integer generated by default as identity"
			} else if f.IsForeignKey {
				// Foreign keys must use the same base type as uint/int identity keys.
				sqlType = "integer"
			} else {
				sqlType = "number(10)"
			}
		case reflect.Int64, reflect.Uint32, reflect.Uint64:
			if kingbaseFieldCanAutoIncrement(f) {
				f.TagSettingsSet("AUTO_INCREMENT", "AUTO_INCREMENT")
				sqlType = "bigint generated by default as identity"
			} else if f.IsForeignKey {
				sqlType = "bigint"
			} else if dataValue.Kind() == reflect.Uint64 {
				sqlType = "number(20)"
			} else {
				sqlType = "number(19)"
			}
		case reflect.Float32:
			sqlType = "binary_float"
		case reflect.Float64:
			sqlType = "binary_double"
		case reflect.String:
			if _, ok := f.TagSettingsGet("SIZE"); !ok {
				size = 0
			}
			if size > 0 && size <= 4000 {
				sqlType = fmt.Sprintf("varchar2(%d)", size)
			} else {
				sqlType = "clob"
			}
		case reflect.Struct:
			if _, ok := dataValue.Interface().(time.Time); ok {
				sqlType = "timestamp with time zone"
			}
		default:
			if gorm.IsByteArrayOrSlice(dataValue) {
				sqlType = "blob"
			}
		}
	}

	if sqlType == "" {
		panic(fmt.Sprintf("invalid sql type %s (%s) for kingbase oracle", dataValue.Type().Name(), dataValue.Kind().String()))
	}

	additionalType = stripOracleInlineComment(additionalType)
	additionalType = strings.Join(strings.Fields(additionalType), " ")
	if strings.TrimSpace(additionalType) == "" {
		return sqlType
	}
	return fmt.Sprintf("%v %v", sqlType, additionalType)
}

func rewriteOracleExplicitType(sqlType string) string {
	typ := strings.ToLower(strings.TrimSpace(sqlType))

	for _, repl := range []struct {
		from string
		to   string
	}{
		{"bigserial", "bigint generated by default as identity"},
		{"serial", "integer generated by default as identity"},
		{"varchar2", "varchar2"},
		{"nvarchar2", "nvarchar2"},
		{"nvarchar", "nvarchar2"},
		{"varchar", "varchar2"},
		{"character varying", "varchar2"},
		{"longtext", "clob"},
		{"mediumtext", "clob"},
		{"tinytext", "clob"},
		{"text", "clob"},
		{"jsonb", "clob"},
		{"json", "clob"},
		{"longblob", "blob"},
		{"mediumblob", "blob"},
		{"tinyblob", "blob"},
		{"bytea", "blob"},
		{"varbinary", "raw"},
		{"binary", "raw"},
		{"numeric", "number"},
		{"decimal", "number"},
		{"datetime", "timestamp"},
		{"timestamptz", "timestamp with time zone"},
		{"timestamp with time zone", "timestamp with time zone"},
		{"double precision", "binary_double"},
		{"double", "binary_double"},
		{"float", "binary_double"},
		{"real", "binary_float"},
		{"bigint unsigned", "number(20)"},
		{"bigint", "number(19)"},
		{"integer unsigned", "number(10)"},
		{"int unsigned", "number(10)"},
		{"integer", "number(10)"},
		{"int", "number(10)"},
		{"smallint", "number(5)"},
		{"tinyint", "number(3)"},
		{"bool", "boolean"},
	} {
		if typ == repl.from {
			return repl.to
		}
		if strings.HasPrefix(typ, repl.from+"(") {
			return repl.to + typ[len(repl.from):]
		}
		if strings.HasPrefix(typ, repl.from+" ") {
			return repl.to + typ[len(repl.from):]
		}
	}

	return typ
}

func stripOracleInlineComment(additionalType string) string {
	upper := strings.ToUpper(additionalType)
	if idx := strings.Index(upper, " COMMENT "); idx >= 0 {
		return strings.TrimSpace(additionalType[:idx])
	}
	if strings.HasPrefix(upper, "COMMENT ") {
		return ""
	}
	return additionalType
}

func kingbaseFieldCanAutoIncrement(field *gorm.StructField) bool {
	if value, ok := field.TagSettingsGet("AUTO_INCREMENT"); ok {
		return strings.ToLower(value) != "false"
	}
	return field.IsPrimaryKey
}

func (d *kingbaseOracleDialect) hasTableOracleCompat(tableName string) bool {
	schema, name := splitSchemaTable(tableName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM all_tables WHERE LOWER(owner) = LOWER(?) AND LOWER(table_name) = LOWER(?)",
			schema, name,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM user_tables WHERE LOWER(table_name) = LOWER(?)",
			name,
		)
	}
	if err == nil && count > 0 {
		return true
	}
	return d.hasTableInformationSchema(tableName)
}

func (d *kingbaseOracleDialect) hasColumnOracleCompat(tableName, columnName string) bool {
	schema, name := splitSchemaTable(tableName)
	columnName = cleanIdentifier(columnName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM all_tab_columns WHERE LOWER(owner) = LOWER(?) AND LOWER(table_name) = LOWER(?) AND LOWER(column_name) = LOWER(?)",
			schema, name, columnName,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM user_tab_columns WHERE LOWER(table_name) = LOWER(?) AND LOWER(column_name) = LOWER(?)",
			name, columnName,
		)
	}
	if err == nil && count > 0 {
		return true
	}
	return d.hasColumnInformationSchema(tableName, columnName)
}

func (d *kingbaseOracleDialect) hasIndexOracleCompat(tableName, indexName string) bool {
	schema, name := splitSchemaTable(tableName)
	indexName = cleanIdentifier(indexName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM all_indexes WHERE LOWER(owner) = LOWER(?) AND LOWER(table_name) = LOWER(?) AND LOWER(index_name) = LOWER(?)",
			schema, name, indexName,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM user_indexes WHERE LOWER(table_name) = LOWER(?) AND LOWER(index_name) = LOWER(?)",
			name, indexName,
		)
	}
	if err == nil && count > 0 {
		return true
	}
	return d.hasIndexNativeCatalog(tableName, indexName)
}

func (d *kingbaseOracleDialect) hasForeignKeyOracleCompat(tableName, foreignKeyName string) bool {
	schema, name := splitSchemaTable(tableName)
	foreignKeyName = cleanIdentifier(foreignKeyName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM all_constraints WHERE LOWER(owner) = LOWER(?) AND LOWER(table_name) = LOWER(?) AND LOWER(constraint_name) = LOWER(?) AND constraint_type = 'R'",
			schema, name, foreignKeyName,
		)
	} else {
		count, err = d.queryCount(
			"SELECT COUNT(*) FROM user_constraints WHERE LOWER(table_name) = LOWER(?) AND LOWER(constraint_name) = LOWER(?) AND constraint_type = 'R'",
			name, foreignKeyName,
		)
	}
	if err == nil && count > 0 {
		return true
	}
	return d.hasForeignKeyInformationSchema(tableName, foreignKeyName)
}
