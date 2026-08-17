package kingbase

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/jinzhu/gorm"
)

// KbMode defines compatibility mode for Kingbase.
type KbMode string

const (
	KbModeMySQL  KbMode = "mysql"
	KbModeOracle KbMode = "oracle"
)

const (
	DialectKingbaseMySQL  = "kingbase-mysql"
	DialectKingbaseOracle = "kingbase-oracle"
)

// KingbaseDialect proxies a real GORM dialect and can override behavior by mode.
type KingbaseDialect struct {
	mode KbMode
	db   gorm.SQLCommon
	raw  gorm.Dialect
}

func NewKingbaseDialect(mode KbMode) gorm.Dialect {
	base := KingbaseDialect{mode: mode}
	switch mode {
	case KbModeMySQL:
		return &kingbaseMySQLDialect{KingbaseDialect: base}
	case KbModeOracle:
		return &kingbaseOracleDialect{}
	default:
		panic(fmt.Sprintf("unsupported Kingbase mode: %s", mode))
	}
}

func Register() {
	gorm.RegisterDialect(DialectKingbaseMySQL, &kingbaseMySQLDialect{})
	gorm.RegisterDialect(DialectKingbaseOracle, &kingbaseOracleDialect{})
}

func init() {
	Register()
}

func (k *KingbaseDialect) ensureRaw() {
	if k.raw != nil {
		return
	}
	rawName := "mysql"
	prototype, ok := gorm.GetDialect(rawName)
	if !ok {
		panic(fmt.Sprintf("raw dialect not registered: %s", rawName))
	}

	// Registered GORM dialects are prototypes. Clone the prototype so SetDB on
	// one Kingbase connection cannot overwrite another connection's DB handle.
	typ := reflect.TypeOf(prototype)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	raw, ok := reflect.New(typ).Interface().(gorm.Dialect)
	if !ok {
		panic(fmt.Sprintf("raw dialect does not implement gorm.Dialect: %s", rawName))
	}
	k.raw = raw
}

func (*KingbaseDialect) GetName() string { return DialectKingbaseMySQL }
func (k *KingbaseDialect) SetDB(db gorm.SQLCommon) {
	k.db = db
	k.ensureRaw()
	k.raw.SetDB(db)
}
func (k *KingbaseDialect) BindVar(i int) string { k.ensureRaw(); return k.raw.BindVar(i) }
func (k *KingbaseDialect) Quote(key string) string {
	k.ensureRaw()
	return k.raw.Quote(key)
}
func (k *KingbaseDialect) DataTypeOf(field *gorm.StructField) string {
	return k.rewriteMysqlType(field)
}
func (k *KingbaseDialect) HasIndex(tableName string, indexName string) bool {
	return k.hasIndexMySQLCompat(tableName, indexName)
}
func (k *KingbaseDialect) HasForeignKey(tableName string, foreignKeyName string) bool {
	return k.hasForeignKeyMySQLCompat(tableName, foreignKeyName)
}
func (k *KingbaseDialect) RemoveIndex(tableName string, indexName string) error {
	k.ensureRaw()
	return k.raw.RemoveIndex(tableName, indexName)
}
func (k *KingbaseDialect) HasTable(tableName string) bool {
	return k.hasTableMySQLCompat(tableName)
}
func (k *KingbaseDialect) HasColumn(tableName string, columnName string) bool {
	return k.hasColumnMySQLCompat(tableName, columnName)
}
func (k *KingbaseDialect) ModifyColumn(tableName string, columnName string, typ string) error {
	k.ensureRaw()
	return k.raw.ModifyColumn(tableName, columnName, typ)
}
func (k *KingbaseDialect) LimitAndOffsetSQL(limit, offset interface{}) (string, error) {
	k.ensureRaw()
	return k.raw.LimitAndOffsetSQL(limit, offset)
}
func (k *KingbaseDialect) SelectFromDummyTable() string {
	k.ensureRaw()
	return k.raw.SelectFromDummyTable()
}
func (k *KingbaseDialect) LastInsertIDOutputInterstitial(tableName, columnName string, columns []string) string {
	k.ensureRaw()
	return k.raw.LastInsertIDOutputInterstitial(tableName, columnName, columns)
}
func (k *KingbaseDialect) LastInsertIDReturningSuffix(tableName, columnName string) string {
	k.ensureRaw()
	return k.raw.LastInsertIDReturningSuffix(tableName, columnName)
}
func (k *KingbaseDialect) DefaultValueStr() string {
	k.ensureRaw()
	return k.raw.DefaultValueStr()
}
func (k *KingbaseDialect) BuildKeyName(kind, tableName string, fields ...string) string {
	k.ensureRaw()
	return k.raw.BuildKeyName(kind, tableName, fields...)
}
func (k *KingbaseDialect) NormalizeIndexAndColumn(indexName, columnName string) (string, string) {
	k.ensureRaw()
	return k.raw.NormalizeIndexAndColumn(indexName, columnName)
}
func (k *KingbaseDialect) CurrentDatabase() string {
	if name := k.currentSchema(); name != "" {
		return name
	}
	k.ensureRaw()
	return k.raw.CurrentDatabase()
}

func (k *KingbaseDialect) queryCount(query string, args ...interface{}) (int, error) {
	if k.db == nil {
		return 0, sql.ErrConnDone
	}
	var count int
	err := k.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (k *KingbaseDialect) currentSchema() (name string) {
	if k.db == nil {
		return ""
	}
	_ = k.db.QueryRow("SELECT current_schema()").Scan(&name)
	return
}

func cleanIdentifier(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 {
		switch {
		case name[0] == '"' && name[len(name)-1] == '"':
			return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
		case name[0] == '`' && name[len(name)-1] == '`':
			return strings.ReplaceAll(name[1:len(name)-1], "``", "`")
		case name[0] == '[' && name[len(name)-1] == ']':
			return strings.ReplaceAll(name[1:len(name)-1], "]]", "]")
		}
	}
	return name
}

func splitSchemaTable(table string) (schema, name string) {
	table = strings.TrimSpace(table)
	for i := 0; i < len(table); i++ {
		if table[i] == '.' {
			return cleanIdentifier(table[:i]), cleanIdentifier(table[i+1:])
		}
	}
	return "", cleanIdentifier(table)
}

type kingbaseMySQLDialect struct{ KingbaseDialect }

func (d *kingbaseMySQLDialect) SetDB(db gorm.SQLCommon) {
	d.mode = KbModeMySQL
	d.KingbaseDialect.SetDB(db)
}
