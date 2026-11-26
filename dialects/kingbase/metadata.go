package kingbase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jinzhu/gorm"
)

// ErrMetadataTrackingUnavailable means the connection was not opened through
// Open, so bool-only metadata query errors cannot be retrieved.
var ErrMetadataTrackingUnavailable = errors.New("kingbase: metadata error tracking is unavailable; open the connection with kingbase.Open")

// ErrSQLDBUnavailable means the current GORM handle does not contain a
// *sql.DB, for example because it represents an active transaction.
var ErrSQLDBUnavailable = errors.New("kingbase: underlying *sql.DB is unavailable")

type metadataErrorState struct {
	mu  sync.RWMutex
	err error
}

func (s *metadataErrorState) report(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *metadataErrorState) current() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// metadataTrackingDB retains the first metadata error and prevents DDL from
// running after GORM V1's bool-only Has* methods converted the error to false.
type metadataTrackingDB struct {
	gorm.SQLCommon
	state *metadataErrorState
}

func newMetadataTrackingDB(db gorm.SQLCommon) *metadataTrackingDB {
	return &metadataTrackingDB{SQLCommon: db, state: &metadataErrorState{}}
}

func (db *metadataTrackingDB) ReportMetadataError(err error) {
	db.state.report(err)
}

func (db *metadataTrackingDB) MetadataError() error {
	return db.state.current()
}

func (db *metadataTrackingDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	if err := db.MetadataError(); err != nil {
		return nil, err
	}
	return db.SQLCommon.Exec(query, args...)
}

func (db *metadataTrackingDB) Prepare(query string) (*sql.Stmt, error) {
	if err := db.MetadataError(); err != nil {
		return nil, err
	}
	return db.SQLCommon.Prepare(query)
}

func (db *metadataTrackingDB) Begin() (*sql.Tx, error) {
	if err := db.MetadataError(); err != nil {
		return nil, err
	}
	if beginner, ok := db.SQLCommon.(interface{ Begin() (*sql.Tx, error) }); ok {
		return beginner.Begin()
	}
	return nil, gorm.ErrCantStartTransaction
}

func (db *metadataTrackingDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if err := db.MetadataError(); err != nil {
		return nil, err
	}
	if beginner, ok := db.SQLCommon.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return nil, gorm.ErrCantStartTransaction
}

func (db *metadataTrackingDB) Close() error {
	if closer, ok := db.SQLCommon.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return errors.New("can't close current db")
}

// MetadataError returns the first metadata query error reported by db. A nil
// result means no metadata query has failed. Connections opened directly with
// gorm.Open return ErrMetadataTrackingUnavailable.
func MetadataError(db *gorm.DB) error {
	if db == nil {
		return ErrMetadataTrackingUnavailable
	}
	tracker, ok := db.CommonDB().(interface{ MetadataError() error })
	if !ok {
		return ErrMetadataTrackingUnavailable
	}
	return tracker.MetadataError()
}

// SQLDB returns the underlying *sql.DB used by a connection opened through
// Open. Use it instead of gorm.DB.DB when configuring the connection pool,
// because Open installs a SQLCommon wrapper for metadata error tracking.
func SQLDB(db *gorm.DB) (*sql.DB, error) {
	if db == nil {
		return nil, ErrSQLDBUnavailable
	}
	connection := db.CommonDB()
	if tracker, ok := connection.(*metadataTrackingDB); ok {
		connection = tracker.SQLCommon
	}
	if sqlDB, ok := connection.(*sql.DB); ok {
		return sqlDB, nil
	}
	return nil, ErrSQLDBUnavailable
}

func openWithMetadataTracking(args ...interface{}) (*gorm.DB, error) {
	if len(args) == 0 {
		return nil, errors.New("invalid database source")
	}

	var (
		connection gorm.SQLCommon
		own        bool
	)
	switch value := args[0].(type) {
	case string:
		driver := DialectKingbase
		source := value
		if len(args) >= 2 {
			driver = value
			var ok bool
			source, ok = args[1].(string)
			if !ok {
				return nil, fmt.Errorf("invalid database source: %v is not a valid type", args[1])
			}
		}
		sqlDB, err := sql.Open(driver, source)
		if err != nil {
			return nil, err
		}
		connection = sqlDB
		own = true
	case gorm.SQLCommon:
		connection = value
	default:
		return nil, fmt.Errorf("invalid database source: %v is not a valid type", value)
	}

	tracker, ok := connection.(*metadataTrackingDB)
	if !ok {
		tracker = newMetadataTrackingDB(connection)
	}
	db, err := gorm.Open(DialectKingbase, tracker)
	if err != nil && own {
		_ = tracker.Close()
	}
	return db, err
}
