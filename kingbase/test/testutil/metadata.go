package testutil

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/jinzhu/gorm"
)

// ObserveMetadata keeps a shared observer beneath GORM's per-operation dialect
// clones. Metadata failures fail the test even when GORM only returns false.
// The returned DB takes ownership of the existing connection.
func ObserveMetadata(tb testing.TB, db *gorm.DB) *gorm.DB {
	tb.Helper()
	observer := &metadataObserver{
		SQLCommon: db.CommonDB(),
		report: func(err error) {
			if DriverLogFor(db) != nil {
				tb.Error("[未通过] 元数据查询，详见金仓错误日志")
			} else {
				tb.Errorf("[未通过] 元数据查询: %v", err)
			}
		},
	}
	observed, err := gorm.Open(db.Dialect().GetName(), observer)
	if err != nil {
		Close(tb, db)
		tb.Fatalf("初始化金仓元数据观测失败: %v", err)
	}
	if log := DriverLogFor(db); log != nil {
		observed = observed.Set(driverLogKey, log)
	}
	return observed
}

type metadataObserver struct {
	gorm.SQLCommon
	mu     sync.Mutex
	err    error
	report func(error)
}

func (o *metadataObserver) ReportMetadataError(err error) {
	if err == nil {
		return
	}
	o.mu.Lock()
	if o.err == nil {
		o.err = err
	}
	o.mu.Unlock()
	o.report(err)
}

func (o *metadataObserver) Exec(query string, args ...interface{}) (sql.Result, error) {
	if err := o.metadataError(); err != nil {
		// Do not execute DDL based on metadata that could not be read.
		return nil, err
	}
	return o.SQLCommon.Exec(query, args...)
}

// GORM V1 uses both methods to recognize a connection that can start
// transactions, including the automatic transactions around writes.
func (o *metadataObserver) Begin() (*sql.Tx, error) {
	if err := o.metadataError(); err != nil {
		return nil, err
	}
	if connection, ok := o.SQLCommon.(interface{ Begin() (*sql.Tx, error) }); ok {
		return connection.Begin()
	}
	return nil, gorm.ErrCantStartTransaction
}

func (o *metadataObserver) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if err := o.metadataError(); err != nil {
		return nil, err
	}
	if connection, ok := o.SQLCommon.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}); ok {
		return connection.BeginTx(ctx, opts)
	}
	return nil, gorm.ErrCantStartTransaction
}

func (o *metadataObserver) metadataError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

func (o *metadataObserver) Close() error {
	if connection, ok := o.SQLCommon.(interface{ Close() error }); ok {
		return connection.Close()
	}
	return sql.ErrConnDone
}
