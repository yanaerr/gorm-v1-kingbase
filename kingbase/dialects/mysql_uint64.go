package kingbase

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strings"

	"github.com/jinzhu/gorm"
)

const mysqlUint64Marker = "kingbase:gorm-v1:mysql:uint64:v1"
const mysqlUint64Offset = "9223372036854775808"
const mysqlUint64Maximum = "18446744073709551615"
const mysqlUint64SequencePrefix = "kb_mysql_u64_"

// AutoMigrateMySQLUint64 installs full-range uint64 allocators after ordinary
// GORM migration. GORM v1 has no post-DDL dialect hook, so this is an explicit
// migration entry, not a transparent change to db.AutoMigrate. DML keeps using
// db.Create. Explicit TYPE/DEFAULT and auto_increment:false are respected.
func AutoMigrateMySQLUint64(db *gorm.DB, models ...interface{}) *gorm.DB {
	if db == nil {
		return &gorm.DB{Error: fmt.Errorf("kingbase-mysql: nil database")}
	}
	r := db.NewScope(db.Value).DB()
	if r.Error != nil {
		return r
	}
	if _, ok := r.Dialect().(*kingbaseMySQLDialect); !ok {
		r.AddError(fmt.Errorf("kingbase-mysql: uint64 migration requires MySQL dialect"))
		return r
	}
	r.AddError(kingbaseAtomicMigration(r, func(tx *gorm.DB) error {
		for _, model := range models {
			scope := tx.NewScope(model)
			var columns []string
			for _, field := range scope.GetModelStruct().StructFields {
				typ := field.Struct.Type
				for typ.Kind() == reflect.Ptr {
					typ = typ.Elem()
				}
				_, explicitType := field.TagSettingsGet("TYPE")
				_, explicitDefault := field.TagSettingsGet("DEFAULT")
				if field.IsNormal && !field.IsIgnored && typ.Kind() == reflect.Uint64 && kingbaseFieldCanAutoIncrement(field) && !explicitType && !explicitDefault {
					if _, indexed := field.TagSettingsGet("INDEX"); !field.IsPrimaryKey && !indexed {
						return fmt.Errorf("kingbase-mysql: non-primary uint64 AUTO_INCREMENT needs an INDEX tag")
					}
					columns = append(columns, field.DBName)
				}
			}
			if err := tx.AutoMigrate(model).Error; err != nil {
				return err
			}
			var schema, table string
			if err := tx.Raw(`SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=to_regclass(?)`, scope.QuotedTableName()).Row().Scan(&schema, &table); err != nil {
				return err
			}
			for _, column := range columns {
				if err := migrateMySQLUint64Column(tx, schema, table, column); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	return r
}

func mysqlQualified(schema, table string) string {
	return quoteMySQLIdentifier(schema) + "." + quoteMySQLIdentifier(table)
}

func mysqlUint64ValueSQL(operation string) string {
	// MySQL-mode NUMERIC arithmetic needs an explicit, wide precision on BOTH
	// operands. An untyped 2^63 literal lost low bits in the original prototype.
	return "CAST((CAST(" + operation + "(?::regclass) AS numeric(40,0)) + CAST('" + mysqlUint64Offset + "' AS numeric(40,0))) AS text)"
}

func migrateMySQLUint64Column(db *gorm.DB, schema, table, column string) error {
	qualified := mysqlQualified(schema, table)
	col := quoteMySQLIdentifier(column)
	if err := db.Exec("LOCK TABLE " + qualified + " IN ACCESS EXCLUSIVE MODE").Error; err != nil {
		return err
	}
	var typ, identity string
	var integerStorage bool
	var oldSequence, defaultValue sql.NullString
	if err := db.Raw(`SELECT format_type(a.atttypid,a.atttypmod),a.attidentity,
pg_get_expr(d.adbin,d.adrelid),pg_get_serial_sequence(?,?)
,a.atttypid IN ('int2'::regtype,'int4'::regtype,'int8'::regtype,'uint8'::regtype)
FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
WHERE a.attrelid=to_regclass(?) AND a.attname=? AND NOT a.attisdropped`, qualified, column, qualified, column).Row().Scan(&typ, &identity, &defaultValue, &oldSequence, &integerStorage); err != nil {
		return err
	}
	if !integerStorage {
		return fmt.Errorf("kingbase-mysql: refusing non-integer uint64 source column (%s)", typ)
	}
	digest := sha256.Sum256([]byte(schema + "\x00" + table + "\x00" + column))
	sequence := mysqlQualified(schema, mysqlUint64SequencePrefix+hex.EncodeToString(digest[:12]))
	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", sequence).Row().Scan(&exists); err != nil {
		return err
	}
	if exists {
		return validateMySQLUint64Sequence(db.CommonDB(), qualified, column, sequence)
	}
	if identity != "" && identity != "d" {
		return fmt.Errorf("kingbase-mysql: cannot replace ALWAYS identity for uint64")
	}
	if defaultValue.Valid && (!oldSequence.Valid || !strings.HasPrefix(defaultValue.String, "nextval(")) {
		return fmt.Errorf("kingbase-mysql: refusing to replace a custom uint64 default")
	}
	var minimum, maximum string
	if err := db.Raw("SELECT COALESCE(CAST(MIN("+col+") AS text),'0'::text),COALESCE(CAST(MAX("+col+") AS text),'0'::text) FROM "+qualified).Row().Scan(&minimum, &maximum); err != nil {
		return err
	}
	low, lowOK := new(big.Int).SetString(minimum, 10)
	high, highOK := new(big.Int).SetString(maximum, 10)
	max, _ := new(big.Int).SetString(mysqlUint64Maximum, 10)
	if !lowOK || !highOK || low.Sign() < 0 || high.Sign() < 0 || high.Cmp(max) > 0 {
		return fmt.Errorf("kingbase-mysql: existing data is outside uint64 range")
	}
	if oldSequence.Valid {
		var seqSchema, seqName string
		var increment int64
		var cycle bool
		if err := db.Raw(`SELECT n.nspname,c.relname,s.seqincrement,s.seqcycle FROM pg_class c
JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_sequence s ON s.seqrelid=c.oid
WHERE c.oid=to_regclass(?)`, oldSequence.String).Row().Scan(&seqSchema, &seqName, &increment, &cycle); err != nil {
			return err
		}
		if increment != 1 || cycle {
			return fmt.Errorf("kingbase-mysql: uint64 migration requires ascending non-cycling sequence")
		}
		var allocated int64
		var called bool
		if err := db.Raw("SELECT last_value,is_called FROM "+mysqlQualified(seqSchema, seqName)).Row().Scan(&allocated, &called); err != nil {
			return err
		}
		if allocated < 1 {
			return fmt.Errorf("kingbase-mysql: legacy sequence must start at a positive value")
		}
		if !called {
			allocated--
		}
		if previous := big.NewInt(allocated); previous.Cmp(high) > 0 {
			high.Set(previous)
		}
		if identity != "" {
			if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " DROP IDENTITY").Error; err != nil {
				return err
			}
		} else {
			if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " DROP DEFAULT").Error; err != nil {
				return err
			}
			// RESTRICT deliberately preserves the original server error when a
			// second object depends on the old sequence. Never cascade it away.
			if err := db.Exec("DROP SEQUENCE " + mysqlQualified(seqSchema, seqName)).Error; err != nil {
				return err
			}
		}
	}
	if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " TYPE bigint unsigned USING CAST(" + col + " AS uint8)").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE SEQUENCE " + sequence + " MINVALUE -9223372036854775807 MAXVALUE 9223372036854775807 START WITH -9223372036854775807 CACHE 1 NO CYCLE OWNED BY " + qualified + "." + col).Error; err != nil {
		return err
	}
	constraint := mysqlUnsignedConstraintName(column)
	var count int
	if err := db.Raw("SELECT COUNT(*) FROM pg_constraint WHERE conrelid=to_regclass(?) AND conname=? AND contype='c' AND convalidated", qualified, constraint).Row().Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if err := db.Exec("ALTER TABLE " + qualified + " ADD CONSTRAINT " + quoteMySQLIdentifier(constraint) + " CHECK (" + col + " BETWEEN 0 AND " + mysqlUint64Maximum + ")").Error; err != nil {
			return err
		}
	}
	if err := db.Exec("COMMENT ON CONSTRAINT " + quoteMySQLIdentifier(constraint) + " ON " + qualified + " IS '" + mysqlUint64Marker + "'").Error; err != nil {
		return err
	}
	var literalSequence string
	if err := db.Raw("SELECT quote_literal(?)", sequence).Row().Scan(&literalSequence); err != nil {
		return err
	}
	defaultExpr := "((CAST(nextval(" + literalSequence + "::regclass) AS numeric(40,0)) + CAST('" + mysqlUint64Offset + "' AS numeric(40,0)))::uint8)"
	if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " SET DEFAULT " + defaultExpr).Error; err != nil {
		return err
	}
	var storedDefault string
	if err := db.Raw(mysqlUint64DefaultQuery, qualified, column).Row().Scan(&storedDefault); err != nil {
		return err
	}
	fingerprint, err := mysqlUint64DefaultFingerprint(storedDefault)
	if err != nil {
		return err
	}
	if err := db.Exec("COMMENT ON SEQUENCE " + sequence + " IS '" + mysqlUint64Marker + ":" + fingerprint + "'").Error; err != nil {
		return err
	}
	value, called := int64(math.MinInt64+1), false
	if high.Sign() > 0 {
		value, called = int64(high.Uint64()^(uint64(1)<<63)), true
	}
	if err := db.Exec("SELECT setval(?::regclass,?,?)", sequence, value, called).Error; err != nil {
		return err
	}
	return validateMySQLUint64Sequence(db.CommonDB(), qualified, column, sequence)
}

func validateMySQLUint64Sequence(db gorm.SQLCommon, table, column, sequence string) error {
	var valid bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_attribute a
JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
JOIN pg_depend dep ON dep.classid='pg_attrdef'::regclass AND dep.objid=d.oid
JOIN pg_sequence s ON s.seqrelid=dep.refobjid
WHERE a.attrelid=to_regclass(?) AND a.attname=? AND a.atttypid='uint8'::regtype AND a.attidentity=''
AND dep.refobjid=to_regclass(?) AND s.seqincrement=1 AND s.seqmin=-9223372036854775807
AND s.seqmax=9223372036854775807 AND s.seqcache=1 AND NOT s.seqcycle
AND obj_description(s.seqrelid,'pg_class') LIKE ?
AND POSITION('9223372036854775808' IN pg_get_expr(d.adbin,d.adrelid))>0
AND EXISTS(SELECT 1 FROM pg_depend owned WHERE owned.classid='pg_class'::regclass
AND owned.objid=s.seqrelid AND owned.refobjid=a.attrelid AND owned.refobjsubid=a.attnum AND owned.deptype='a')
AND EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid=a.attrelid AND c.conname=?
AND c.contype='c' AND c.convalidated AND a.attnum=ANY(c.conkey) AND obj_description(c.oid,'pg_constraint')=?))`, table, column, sequence, mysqlUint64Marker+":%", mysqlUnsignedConstraintName(column), mysqlUint64Marker).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("kingbase-mysql: missing or modified uint64 allocator for %s.%s", table, column)
	}
	var definition, marker string
	if err := db.QueryRow(mysqlUint64DefaultQuery, table, column).Scan(&definition); err != nil {
		return err
	}
	if err := db.QueryRow("SELECT obj_description(to_regclass(?),'pg_class')", sequence).Scan(&marker); err != nil {
		return err
	}
	fingerprint, err := mysqlUint64DefaultFingerprint(definition)
	if err != nil {
		return err
	}
	if marker != mysqlUint64Marker+":"+fingerprint {
		return fmt.Errorf("kingbase-mysql: modified uint64 default for %s.%s", table, column)
	}
	return nil
}

const mysqlUint64DefaultQuery = `SELECT pg_get_expr(d.adbin,d.adrelid) FROM pg_attribute a
JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
WHERE a.attrelid=to_regclass(?) AND a.attname=?`

var mysqlUint64Nextval = regexp.MustCompile(`nextval\((?:E)?'(?:[^'\\]|\\.|'')*'::regclass\)`)

func mysqlUint64DefaultFingerprint(definition string) (string, error) {
	if len(mysqlUint64Nextval.FindAllStringIndex(definition, -1)) != 1 {
		return "", fmt.Errorf("kingbase-mysql: uint64 default must reference one owned sequence")
	}
	// regclass's display qualification depends on search_path and changes on
	// dump/restore. Ownership/dependency is checked separately; normalize only
	// that reference, retaining every arithmetic operation and cast in the hash.
	normalized := mysqlUint64Nextval.ReplaceAllString(definition, "nextval(__owned_sequence__)")
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:]), nil
}
