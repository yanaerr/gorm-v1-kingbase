package kingbase

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/jinzhu/gorm"
)

const oracleUint64Marker = "kingbase:gorm-v1:oracle:uint64:v1"
const oracleUint64Offset = "9223372036854775808"
const oracleUint64Max = "18446744073709551615"

// A signed BIGINT sequence has enough distinct states for all positive uint64
// values. Add 2^63 in NUMERIC arithmetic: -2^63+1 maps to 1 and 2^63-1 maps to
// MaxUint64. Native nextval retains concurrency, rollback gaps and exhaustion.
// It is a NUMERIC default backed by an owned sequence, not a native identity.
func oracleUint64Column(db *gorm.DB, schema, table, column string) error {
	qualified := oracleQualified(schema, table)
	col := quoteOracleIdentifier(column)
	if err := db.Exec("LOCK TABLE " + qualified + " IN ACCESS EXCLUSIVE MODE").Error; err != nil {
		return err
	}
	var typ, identity string
	var sequence, defaultValue sql.NullString
	err := db.Raw(`SELECT format_type(a.atttypid,a.atttypmod),a.attidentity,
pg_get_expr(d.adbin,d.adrelid),pg_get_serial_sequence(?,?)
FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
WHERE a.attrelid=to_regclass(?) AND a.attname=? AND NOT a.attisdropped`, qualified, column, qualified, column).
		Row().Scan(&typ, &identity, &defaultValue, &sequence)
	if err != nil {
		return err
	}
	defaultExpr := defaultValue.String
	if identity != "" && identity != "d" {
		return fmt.Errorf("kingbase-oracle: uint64 migration preserves BY DEFAULT semantics and cannot replace ALWAYS identity")
	}
	digest := sha256.Sum256([]byte(schema + "\x00" + table + "\x00" + column))
	suffix := hex.EncodeToString(digest[:12])
	sequenceName := "kb_u64_" + suffix
	qualifiedSequence := oracleQualified(schema, sequenceName)
	constraint := "kb_u64_range_" + suffix
	var marker sql.NullString
	if err := db.Raw(`SELECT obj_description(to_regclass(?),'pg_class')`, qualifiedSequence).Row().Scan(&marker); err != nil {
		return err
	}
	if marker.Valid && marker.String == oracleUint64Marker {
		// Never reset an existing allocator during repeated AutoMigrate.
		var valid bool
		err := db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_attrdef d
JOIN pg_depend dep ON dep.classid='pg_attrdef'::regclass AND dep.objid=d.oid
JOIN pg_sequence s ON s.seqrelid=dep.refobjid
WHERE d.adrelid=to_regclass(?) AND d.adnum=(SELECT attnum FROM pg_attribute WHERE attrelid=d.adrelid AND attname=?)
AND dep.refobjid=to_regclass(?) AND s.seqincrement=1 AND s.seqmin=-9223372036854775807
AND s.seqmax=9223372036854775807 AND s.seqcache=1 AND NOT s.seqcycle
AND EXISTS(SELECT 1 FROM pg_depend owned WHERE owned.classid='pg_class'::regclass
AND owned.objid=s.seqrelid AND owned.refobjid=d.adrelid AND owned.refobjsubid=d.adnum AND owned.deptype='a'))`, qualified, column, qualifiedSequence).Row().Scan(&valid)
		if err != nil {
			return err
		}
		if !valid || typ != "numeric(20,0)" || identity != "" || !strings.Contains(defaultExpr, oracleUint64Offset) {
			return fmt.Errorf("kingbase-oracle: modified uint64 allocator definition for %s.%s", qualified, col)
		}
		var check int
		if err := db.Raw("SELECT COUNT(*) FROM pg_constraint WHERE conrelid=to_regclass(?) AND conname=? AND contype='c' AND convalidated AND obj_description(oid,'pg_constraint')=?", qualified, constraint, oracleUint64Marker).Row().Scan(&check); err != nil {
			return err
		}
		if check != 1 {
			return fmt.Errorf("kingbase-oracle: missing uint64 range constraint for %s.%s", qualified, col)
		}
		return nil
	}
	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", qualifiedSequence).Row().Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("kingbase-oracle: uint64 sequence name already belongs to another object")
	}
	if identity == "" && defaultExpr != "" {
		return fmt.Errorf("kingbase-oracle: refusing to replace an existing non-identity default on %s.%s", qualified, col)
	}
	var minimum, maximum string
	if err := db.Raw("SELECT COALESCE(MIN("+col+"),0)::text,COALESCE(MAX("+col+"),0)::text FROM "+qualified).Row().Scan(&minimum, &maximum); err != nil {
		return err
	}
	low, lowOK := new(big.Int).SetString(minimum, 10)
	high, highOK := new(big.Int).SetString(maximum, 10)
	max, _ := new(big.Int).SetString(oracleUint64Max, 10)
	if !lowOK || !highOK || low.Sign() < 0 || high.Sign() < 0 || high.Cmp(max) > 0 {
		return fmt.Errorf("kingbase-oracle: existing data is outside uint64 range")
	}
	if identity != "" && sequence.Valid {
		var seqSchema, seqName string
		var increment int64
		var cycle bool
		if err := db.Raw(`SELECT n.nspname,c.relname,s.seqincrement,s.seqcycle FROM pg_class c
JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_sequence s ON s.seqrelid=c.oid
WHERE c.oid=to_regclass(?)`, sequence.String).Row().Scan(&seqSchema, &seqName, &increment, &cycle); err != nil {
			return err
		}
		if increment != 1 || cycle {
			return fmt.Errorf("kingbase-oracle: uint64 migration requires ascending non-cycling identity")
		}
		var allocated int64
		var called bool
		if err := db.Raw("SELECT last_value,is_called FROM "+oracleQualified(seqSchema, seqName)).Row().Scan(&allocated, &called); err != nil {
			return err
		}
		if allocated < 1 {
			return fmt.Errorf("kingbase-oracle: negative identity start cannot migrate to uint64")
		}
		if !called {
			allocated--
		}
		if previous := big.NewInt(allocated); previous.Cmp(high) > 0 {
			high.Set(previous)
		}
		if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " DROP IDENTITY").Error; err != nil {
			return err
		}
	}
	if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " TYPE numeric(20) USING " + col + "::numeric(20)").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE SEQUENCE " + qualifiedSequence + " MINVALUE -9223372036854775807 MAXVALUE 9223372036854775807 START WITH -9223372036854775807 CACHE 1 NO CYCLE OWNED BY " + qualified + "." + col).Error; err != nil {
		return err
	}
	if err := db.Exec("COMMENT ON SEQUENCE " + qualifiedSequence + " IS " + oracleSQLString(oracleUint64Marker)).Error; err != nil {
		return err
	}
	if err := db.Exec("ALTER TABLE " + qualified + " ADD CONSTRAINT " + quoteOracleIdentifier(constraint) + " CHECK (" + col + ">=0 AND " + col + "<=" + oracleUint64Max + ")").Error; err != nil {
		return err
	}
	if err := db.Exec("COMMENT ON CONSTRAINT " + quoteOracleIdentifier(constraint) + " ON " + qualified + " IS " + oracleSQLString(oracleUint64Marker)).Error; err != nil {
		return err
	}
	if err := db.Exec("ALTER TABLE " + qualified + " ALTER COLUMN " + col + " SET DEFAULT (nextval(" + oracleSQLString(qualifiedSequence) + "::regclass)::numeric + " + oracleUint64Offset + ")").Error; err != nil {
		return err
	}
	value, called := int64(math.MinInt64+1), false
	if high.Sign() > 0 {
		value, called = int64(high.Uint64()^(uint64(1)<<63)), true
	}
	return db.Exec("SELECT setval(?::regclass,?,?)", qualifiedSequence, value, called).Error
}
