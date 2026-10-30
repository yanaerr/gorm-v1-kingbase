package kingbase

import (
	"database/sql"
	"fmt"
	"sync/atomic"

	"github.com/jinzhu/gorm"
)

var migrationSavepoint uint64

func kingbaseAtomicMigration(db *gorm.DB, run func(*gorm.DB) error) error {
	if _, ok := db.CommonDB().(*sql.Tx); ok {
		name := fmt.Sprintf("kb_migration_%d", atomic.AddUint64(&migrationSavepoint, 1))
		if err := db.Exec("SAVEPOINT " + name).Error; err != nil {
			return err
		}
		defer func() {
			if value := recover(); value != nil {
				db.Exec("ROLLBACK TO SAVEPOINT " + name)
				db.Exec("RELEASE SAVEPOINT " + name)
				panic(value)
			}
		}()
		if err := run(db); err != nil {
			if rollback := db.Exec("ROLLBACK TO SAVEPOINT " + name).Error; rollback != nil {
				return gorm.Errors{err, rollback}
			}
			if release := db.Exec("RELEASE SAVEPOINT " + name).Error; release != nil {
				return gorm.Errors{err, release}
			}
			return err
		}
		return db.Exec("RELEASE SAVEPOINT " + name).Error
	}
	tx := db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	finished := false
	defer func() {
		if !finished {
			tx.Rollback()
		}
	}()
	if err := run(tx); err != nil {
		finished = true
		if rollback := tx.Rollback().Error; rollback != nil {
			return gorm.Errors{err, rollback}
		}
		return err
	}
	finished = true
	return tx.Commit().Error
}
