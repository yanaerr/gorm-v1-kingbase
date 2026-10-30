package kingbase

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/jinzhu/gorm"
)

// OracleMigration supplies the DDL hooks that GORM v1's Dialect interface lacks.
// Pass db.Table(...) or db.Model(...) before calling a migration method. The
// returned *gorm.DB retains the original database error in Error.
type OracleMigration struct{ db *gorm.DB }

// NewOracleMigrator enables Oracle DDL extensions without changing GORM v1.
func NewOracleMigrator(db *gorm.DB) *OracleMigration { return &OracleMigration{db: db} }

func (m *OracleMigration) result() *gorm.DB {
	if m.db == nil {
		return &gorm.DB{Error: fmt.Errorf("kingbase-oracle: nil database")}
	}
	r := m.db.NewScope(m.db.Value).DB()
	if r.Error == nil && r.Dialect().GetName() != DialectKingbaseOracle {
		r.AddError(fmt.Errorf("kingbase-oracle: Oracle migrator requires Oracle dialect"))
	}
	return r
}

func (m *OracleMigration) AddIndex(name string, columns ...string) *gorm.DB {
	return m.addIndex(false, name, columns)
}

func (m *OracleMigration) AddUniqueIndex(name string, columns ...string) *gorm.DB {
	return m.addIndex(true, name, columns)
}

func (m *OracleMigration) addIndex(unique bool, name string, columns []string) *gorm.DB {
	r := m.result()
	if r.Error != nil {
		return r
	}
	schema, table, err := oracleMigrationTable(r, r.NewScope(r.Value).TableName())
	if err != nil {
		r.AddError(err)
		return r
	}
	indexSchema, index := splitOracleIdentifierPath(name)
	if index == "" || len(columns) == 0 || (indexSchema != "" && indexSchema != schema) {
		r.AddError(fmt.Errorf("kingbase-oracle: index needs a name, columns and the table's schema"))
		return r
	}
	quoted := make([]string, len(columns))
	for i, column := range columns {
		if strings.TrimSpace(column) == "" {
			r.AddError(fmt.Errorf("kingbase-oracle: empty index column"))
			return r
		}
		// Columns are identifiers, not arbitrary SQL expressions.
		quoted[i] = quoteOracleIdentifier(column)
	}
	rows, err := r.Raw(`SELECT i.indisunique,i.indisvalid,i.indpred IS NULL,i.indexprs IS NULL,
pg_get_indexdef(i.indexrelid,k,true) FROM pg_index i
JOIN pg_class x ON x.oid=i.indexrelid JOIN pg_namespace n ON n.oid=x.relnamespace
JOIN pg_class t ON t.oid=i.indrelid CROSS JOIN generate_series(1,i.indnatts) k
WHERE n.nspname=? AND t.relname=? AND x.relname=? ORDER BY k`, schema, table, index).Rows()
	if err != nil {
		r.AddError(err)
		return r
	}
	var existing []string
	for rows.Next() {
		var isUnique, valid, plain, noExpression bool
		var definition string
		if err := rows.Scan(&isUnique, &valid, &plain, &noExpression, &definition); err != nil {
			rows.Close()
			r.AddError(err)
			return r
		}
		if isUnique != unique || !valid || !plain || !noExpression {
			rows.Close()
			r.AddError(fmt.Errorf("kingbase-oracle: existing index has a different definition"))
			return r
		}
		existing = append(existing, definition)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		r.AddError(err)
		return r
	}
	if len(existing) != 0 {
		if len(existing) != len(columns) {
			r.AddError(fmt.Errorf("kingbase-oracle: existing index has different columns"))
			return r
		}
		for i := range existing {
			if cleanIdentifier(existing[i]) != cleanIdentifier(columns[i]) {
				r.AddError(fmt.Errorf("kingbase-oracle: existing index has different columns"))
				break
			}
		}
		return r
	}
	kind := "CREATE INDEX "
	if unique {
		kind = "CREATE UNIQUE INDEX "
	}
	err = kingbaseAtomicMigration(r, func(tx *gorm.DB) error {
		if err := tx.Exec(kind + quoteOracleIdentifier(index) + " ON " + oracleQualified(schema, table) + " (" + strings.Join(quoted, ", ") + ")").Error; err != nil {
			return err
		}
		var actual string
		if err := tx.Raw("SELECT relname FROM pg_class WHERE oid=to_regclass(?)", oracleQualified(schema, index)).Row().Scan(&actual); err != nil {
			return err
		}
		if actual != index {
			return fmt.Errorf("kingbase-oracle: database stored index name %q instead of requested %q", actual, index)
		}
		for i, column := range columns {
			var actualColumn string
			if err := tx.Raw("SELECT pg_get_indexdef(to_regclass(?),?,true)", oracleQualified(schema, index), i+1).Row().Scan(&actualColumn); err != nil {
				return err
			}
			if cleanIdentifier(actualColumn) != cleanIdentifier(column) {
				return fmt.Errorf("kingbase-oracle: database changed the requested index column %q", column)
			}
		}
		return nil
	})
	r.AddError(err)
	return r
}

func (m *OracleMigration) RemoveIndex(name string) *gorm.DB {
	r := m.result()
	if r.Error != nil {
		return r
	}
	schema, table, err := oracleMigrationTable(r, r.NewScope(r.Value).TableName())
	if err != nil {
		r.AddError(err)
		return r
	}
	indexSchema, index := splitOracleIdentifierPath(name)
	if indexSchema != "" && indexSchema != schema {
		r.AddError(fmt.Errorf("kingbase-oracle: index schema differs from table schema"))
		return r
	}
	var owner, actual string
	err = r.Raw(`SELECT t.relname,x.relname FROM pg_index i JOIN pg_class x ON x.oid=i.indexrelid
JOIN pg_class t ON t.oid=i.indrelid WHERE x.oid=to_regclass(?)`, oracleQualified(schema, index)).Row().Scan(&owner, &actual)
	if err != nil && err != sql.ErrNoRows {
		r.AddError(err)
		return r
	}
	if err == nil && owner != table {
		r.AddError(fmt.Errorf("kingbase-oracle: index belongs to another table"))
		return r
	}
	if err == nil && actual != index {
		r.AddError(fmt.Errorf("kingbase-oracle: resolved index name differs from requested name"))
		return r
	}
	// A missing index deliberately reaches the server and returns its original
	// error, rather than being hidden by IF EXISTS or GORM's RemoveIndex hook.
	return r.Exec("DROP INDEX " + oracleQualified(schema, index))
}

func (m *OracleMigration) AutoMigrate(models ...interface{}) *gorm.DB {
	return m.migrate(false, models)
}

func (m *OracleMigration) CreateTable(models ...interface{}) *gorm.DB {
	return m.migrate(true, models)
}

func (m *OracleMigration) migrate(create bool, models []interface{}) *gorm.DB {
	r := m.result()
	if r.Error != nil {
		return r
	}
	err := kingbaseAtomicMigration(r, func(tx *gorm.DB) error {
		for _, model := range models {
			var migrated *gorm.DB
			if create {
				migrated = tx.CreateTable(model)
			} else {
				migrated = tx.AutoMigrate(model)
			}
			if migrated.Error != nil {
				return migrated.Error
			}
			scope := tx.NewScope(model)
			schema, table, err := oracleMigrationTable(tx, scope.TableName())
			if err != nil {
				return err
			}
			for _, field := range scope.GetModelStruct().StructFields {
				if !field.IsNormal || field.IsIgnored {
					continue
				}
				typ := field.Struct.Type
				for typ.Kind() == reflect.Ptr {
					typ = typ.Elem()
				}
				_, explicitType := field.TagSettingsGet("TYPE")
				_, explicitDefault := field.TagSettingsGet("DEFAULT")
				if typ.Kind() == reflect.Uint64 && kingbaseFieldCanAutoIncrement(field) && !explicitType && !explicitDefault {
					if err := oracleUint64Column(tx, schema, table, field.DBName); err != nil {
						return err
					}
				}
				if comment, ok := field.TagSettingsGet("COMMENT"); ok {
					if len(comment) >= 2 && comment[0] == '\'' && comment[len(comment)-1] == '\'' {
						comment = strings.ReplaceAll(comment[1:len(comment)-1], "''", "'")
					}
					query := "COMMENT ON COLUMN " + oracleQualified(schema, table) + "." + quoteOracleIdentifier(field.DBName) + " IS " + oracleSQLString(comment)
					if err := tx.Exec(query).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	r.AddError(err)
	return r
}

func oracleQualified(schema, name string) string {
	return quoteOracleIdentifier(schema) + "." + quoteOracleIdentifier(name)
}

func oracleSQLString(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func oracleMigrationTable(db *gorm.DB, table string) (schema, name string, err error) {
	err = db.Raw(`SELECT n.nspname,c.relname FROM pg_class c
JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=to_regclass(?)`, quoteOracleIdentifierPath(table)).Row().Scan(&schema, &name)
	return
}
