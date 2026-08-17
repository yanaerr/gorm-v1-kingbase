package testutil

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	_ "kingbase.com/gokb"
)

var tableSequence uint64

// Open connects to a Kingbase instance and verifies the requested database mode.
// Tests skip when the configured instance is unavailable so unit-only CI remains usable.
func Open(tb testing.TB, mode kb.KbMode) *gorm.DB {
	tb.Helper()

	dialect, dsn := connectionConfig(mode)
	db, err := gorm.Open(dialect, "kingbase", dsn)
	if err != nil {
		tb.Skipf("skip Kingbase %s integration test: connect failed: %v", mode, err)
		return nil
	}

	if err := db.DB().Ping(); err != nil {
		_ = db.Close()
		tb.Skipf("skip Kingbase %s integration test: ping failed: %v", mode, err)
		return nil
	}

	var actualMode string
	if err := db.DB().QueryRow("SHOW database_mode").Scan(&actualMode); err != nil {
		_ = db.Close()
		tb.Fatalf("read database_mode for %s: %v", mode, err)
	}
	if !strings.EqualFold(actualMode, string(mode)) {
		_ = db.Close()
		tb.Fatalf("database mode mismatch: got %q, want %q", actualMode, mode)
	}

	db.LogMode(false)
	return db
}

// UniqueTable returns a short, isolated table name suitable for an integration test.
func UniqueTable(prefix string) string {
	sequence := atomic.AddUint64(&tableSequence, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), sequence)
}

func connectionConfig(mode kb.KbMode) (dialect, dsn string) {
	switch mode {
	case kb.KbModeMySQL:
		dialect = kb.DialectKingbaseMySQL
		dsn = firstNonEmpty(os.Getenv("KINGBASE_MYSQL_DSN"), os.Getenv("KINGBASE_DSN"))
		if dsn == "" {
			dsn = "host=127.0.0.1 port=54321 user=system password=123456 dbname=test sslmode=disable"
		}
	case kb.KbModeOracle:
		dialect = kb.DialectKingbaseOracle
		dsn = firstNonEmpty(os.Getenv("KINGBASE_ORACLE_DSN"), os.Getenv("KINGBASE_DSN"))
		if dsn == "" {
			dsn = "host=127.0.0.1 port=54324 user=system password=123456 dbname=test sslmode=disable"
		}
	default:
		panic(fmt.Sprintf("unsupported Kingbase test mode: %s", mode))
	}
	return dialect, dsn
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
