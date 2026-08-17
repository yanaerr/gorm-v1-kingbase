package kingbase

import (
	"strings"

	"github.com/jinzhu/gorm"
)

// rewriteMysqlType keeps common GORM/MySQL tags usable on Kingbase MySQL mode.
func (k *KingbaseDialect) rewriteMysqlType(f *gorm.StructField) string {
	k.ensureRaw()
	rawType := strings.TrimSpace(k.raw.DataTypeOf(f))
	typ := strings.ToLower(rawType)

	// Kingbase MySQL mode does not accept MySQL's "unsigned auto_increment" style.
	// Convert common auto-increment patterns to serial/bigserial.
	if strings.Contains(typ, "auto_increment") {
		if strings.Contains(typ, "bigint") {
			return "bigserial"
		}
		return "serial"
	}

	replacements := []struct {
		old string
		new string
	}{
		{"bigint unsigned", "numeric(20,0)"},
		{"int unsigned", "bigint"},
		{"integer unsigned", "bigint"},
		{"mediumint unsigned", "integer"},
		{"smallint unsigned", "integer"},
		{"tinyint unsigned", "smallint"},
		{"longtext", "text"},
		{"mediumtext", "text"},
		{"tinytext", "text"},
		{"longblob", "blob"},
		{"mediumblob", "blob"},
		{"tinyblob", "blob"},
	}

	for _, repl := range replacements {
		typ = strings.ReplaceAll(typ, repl.old, repl.new)
	}

	return typ
}

// MySQL-compatible metadata queries for Kingbase MySQL mode.
func (k *KingbaseDialect) hasTableMySQLCompat(tableName string) bool {
	schema, name := splitSchemaTable(tableName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
			schema, name,
		)
	} else {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?",
			name,
		)
	}
	return err == nil && count > 0
}

func (k *KingbaseDialect) hasColumnMySQLCompat(tableName, columnName string) bool {
	schema, name := splitSchemaTable(tableName)
	columnName = cleanIdentifier(columnName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?",
			schema, name, columnName,
		)
	} else {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?",
			name, columnName,
		)
	}
	return err == nil && count > 0
}

func (k *KingbaseDialect) hasIndexMySQLCompat(tableName, indexName string) bool {
	schema, name := splitSchemaTable(tableName)
	indexName = cleanIdentifier(indexName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = ? AND tablename = ? AND indexname = ?",
			schema, name, indexName,
		)
	} else {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = ?",
			name, indexName,
		)
	}
	return err == nil && count > 0
}

func (k *KingbaseDialect) hasForeignKeyMySQLCompat(tableName, foreignKeyName string) bool {
	schema, name := splitSchemaTable(tableName)
	foreignKeyName = cleanIdentifier(foreignKeyName)
	var (
		count int
		err   error
	)
	if schema != "" {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = ? AND table_name = ? AND constraint_name = ? AND constraint_type = 'FOREIGN KEY'",
			schema, name, foreignKeyName,
		)
	} else {
		count, err = k.queryCount(
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = current_schema() AND table_name = ? AND constraint_name = ? AND constraint_type = 'FOREIGN KEY'",
			name, foreignKeyName,
		)
	}
	return err == nil && count > 0
}
