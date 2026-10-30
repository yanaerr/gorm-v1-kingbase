package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"kingbase.com/gokb"
)

var tableSequence uint64

// Open connects to a Kingbase instance and verifies the requested database mode.
// Tests skip when the configured instance is unavailable so unit-only CI remains usable.
func Open(tb testing.TB, mode kb.KbMode) *gorm.DB {
	tb.Helper()
	return open(tb, mode, nil)
}

// OpenLogged observes real driver errors, including transaction failures.
// Successful calls stay quiet. DSN and mode validation are the same as Open.
func OpenLogged(tb testing.TB, mode kb.KbMode) *gorm.DB {
	tb.Helper()
	return open(tb, mode, &DriverLog{tb: tb})
}

func open(tb testing.TB, mode kb.KbMode, log *DriverLog) *gorm.DB {
	tb.Helper()

	dialect, dsn := connectionConfig(mode)
	var db *gorm.DB
	var err error
	if log == nil {
		db, err = gorm.Open(dialect, "kingbase", dsn)
	} else {
		sqlDB := sql.OpenDB(&loggedConnector{base: &gokb.Driver{}, dsn: dsn, log: log})
		db, err = gorm.Open(dialect, sqlDB)
		if err != nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				tb.Errorf("close Kingbase database after failed connection: %v", closeErr)
			}
		}
	}
	if err != nil {
		if requireKingbaseDatabase() {
			tb.Fatalf("Kingbase %s integration test: connect failed: %v", mode, err)
		}
		tb.Skipf("skip Kingbase %s integration test: connect failed: %v", mode, err)
		return nil
	}

	if err := db.DB().Ping(); err != nil {
		Close(tb, db)
		if requireKingbaseDatabase() {
			tb.Fatalf("Kingbase %s integration test: ping failed: %v", mode, err)
		}
		tb.Skipf("skip Kingbase %s integration test: ping failed: %v", mode, err)
		return nil
	}

	var actualMode string
	if err := db.DB().QueryRow("SHOW database_mode").Scan(&actualMode); err != nil {
		Close(tb, db)
		tb.Fatalf("read database_mode for %s: %v", mode, err)
	}
	if !strings.EqualFold(actualMode, string(mode)) {
		Close(tb, db)
		tb.Fatalf("database mode mismatch: got %q, want %q", actualMode, mode)
	}
	// Acceptance runs own one isolated schema. Refuse a connection which did not
	// honor its search_path before any test can create or delete objects.
	if expected := os.Getenv("KINGBASE_TEST_SCHEMA"); expected != "" {
		var actual string
		if err := db.DB().QueryRow("SELECT current_schema()").Scan(&actual); err != nil || actual != expected {
			Close(tb, db)
			tb.Fatalf("Kingbase test schema mismatch: got %q, want %q, err=%v", actual, expected, err)
		}
	}

	db.LogMode(strings.EqualFold(strings.TrimSpace(os.Getenv("DEBUG")), "true"))
	if log != nil {
		db = db.Set(driverLogKey, log)
	}
	return db
}

// Close reports connection cleanup errors instead of silently discarding them.
func Close(tb testing.TB, db *gorm.DB) {
	tb.Helper()
	if err := db.Close(); err != nil {
		tb.Errorf("close Kingbase database: %v", err)
	}
}

// requireKingbaseDatabase turns the normal no-database skip behavior into a
// hard failure for release/acceptance runs.  Local unit-only runs can leave it
// unset and continue to skip integration tests when no instance is available.
func requireKingbaseDatabase() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REQUIRE_KINGBASE_DB"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// KeepTestTables allows an operator to inspect integration-test tables in
// KStudio after a focused test finishes. It is intentionally opt-in: normal
// CI and full-suite runs keep the existing cleanup behavior.
func KeepTestTables() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KEEP_KINGBASE_TEST_TABLES"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
			dsn = "host=127.0.0.1 port=54321 user=system password=123456 dbname=test sslmode=disable get_last_insert_id=yes"
		} else if !hasDSNOption(dsn, "get_last_insert_id") {
			// gokb exposes MySQL-mode generated IDs through LastInsertId only
			// when this driver option is enabled.
			dsn += " get_last_insert_id=yes"
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

func hasDSNOption(dsn, key string) bool {
	prefix := key + "="
	options := strings.Fields(dsn)
	for _, option := range options {
		if option == key || strings.HasPrefix(option, prefix) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
