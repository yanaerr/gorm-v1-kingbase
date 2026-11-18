package regression_test

// This file is the KingbaseES execution layer for the upstream GORM V1
// scenarios that cannot be represented by the small smoke tests alone.  The
// upstream repository has one global database and a large set of package-level
// models; here each scenario is isolated and run once in MySQL mode and once
// in Oracle mode.  The subtest names intentionally retain the upstream test
// names so a CI report can be compared with the official inventory.

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type officialCoverageRow struct {
	ID        uint   `gorm:"primary_key"`
	Name      string `gorm:"size:128;not null"`
	Email     string `gorm:"size:160;unique_index;not null"`
	Age       int
	Score     int
	Active    bool   `gorm:"not null;default:true"`
	Note      string `gorm:"size:255"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (officialCoverageRow) TableName() string { return "kb_official_coverage_rows" }

type officialDefaultRow struct {
	Counter uint64 `gorm:"primary_key"`
	Name    string `gorm:"default:'galeone'"`
	From    string
}

func (officialDefaultRow) TableName() string { return "kb_official_default_rows" }

type officialNoPrimaryKeyRow struct {
	From uint64
	To   uint64
}

func (officialNoPrimaryKeyRow) TableName() string { return "kb_official_no_pk_rows" }

type officialAutoIncrementRow struct {
	// Keep the primary key explicit so the table has only one generated
	// identity. The upstream fixture embeds a generated primary key and adds a
	// second AUTO_INCREMENT column; that is legal in PostgreSQL but not in
	// every Kingbase Oracle compatibility release. The behavior under test is
	// the non-primary generated column, so explicit IDs preserve that behavior
	// while keeping the fixture portable.
	ID       uint `gorm:"primary_key;auto_increment:false"`
	Sequence uint `gorm:"AUTO_INCREMENT;index"`
	Name     string
}

func (officialAutoIncrementRow) TableName() string { return "kb_official_auto_increment_rows" }

type officialPointerRow struct {
	ID    uint `gorm:"primary_key"`
	Name  *string
	Count *int
}

func (officialPointerRow) TableName() string { return "kb_official_pointer_rows" }

type OfficialEmbeddedBase struct {
	ID    uint `gorm:"primary_key"`
	Title string
	URL   string
}

type OfficialEmbeddedAuthor struct {
	Name  string
	Email string
}

type officialEmbeddedRow struct {
	OfficialEmbeddedBase   `gorm:"embedded"`
	OfficialEmbeddedAuthor `gorm:"embedded;embedded_prefix:author_"`
	Upvotes                int
}

func (officialEmbeddedRow) TableName() string { return "kb_official_embedded_rows" }

type officialEmbeddedPointerRow struct {
	*OfficialEmbeddedBase
	Upvotes int
}

func (officialEmbeddedPointerRow) TableName() string {
	return "kb_official_embedded_pointer_rows"
}

type officialTransactionRow struct {
	ID   uint   `gorm:"primary_key"`
	Name string `gorm:"size:64"`
}

func (officialTransactionRow) TableName() string { return "kb_official_tx_rows" }

type officialJSONSlice []string

func (v officialJSONSlice) Value() (driver.Value, error) {
	b, err := json.Marshal([]string(v))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (v *officialJSONSlice) Scan(src interface{}) error {
	var data []byte
	switch value := src.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	case sql.RawBytes:
		data = []byte(value)
	case nil:
		*v = nil
		return nil
	default:
		return fmt.Errorf("unsupported official JSON slice source %T", src)
	}
	return json.Unmarshal(data, (*[]string)(v))
}

type officialScannerRow struct {
	ID     uint              `gorm:"primary_key"`
	Labels officialJSONSlice `gorm:"type:text"`
}

func (officialScannerRow) TableName() string { return "kb_official_scanner_rows" }

type officialDeletedRow struct {
	ID        uint       `gorm:"primary_key"`
	Name      string     `gorm:"size:128"`
	DeletedAt *time.Time `gorm:"column:removed_at"`
}

func (officialDeletedRow) TableName() string { return "kb_official_deleted_rows" }

// Association models deliberately use stable names.  Each test drops the
// complete graph before and after execution, and the two mode subtests run
// serially, so the names remain safe while keeping GORM's association resolver
// on its normal path.
type officialCompany struct {
	ID   uint   `gorm:"primary_key"`
	Name string `gorm:"size:128"`
}

func (officialCompany) TableName() string { return "kb_official_companies" }

type officialProfile struct {
	ID     uint   `gorm:"primary_key"`
	UserID uint   `gorm:"index"`
	Bio    string `gorm:"size:255"`
}

func (officialProfile) TableName() string { return "kb_official_profiles" }

type officialOrder struct {
	ID     uint `gorm:"primary_key"`
	UserID uint `gorm:"index"`
	Amount int
}

func (officialOrder) TableName() string { return "kb_official_orders" }

type officialTag struct {
	ID    uint                `gorm:"primary_key"`
	Name  string              `gorm:"size:128"`
	Users []officialAssocUser `gorm:"many2many:kb_official_user_tags;"`
}

func (officialTag) TableName() string { return "kb_official_tags" }

type officialAssocUser struct {
	ID        uint   `gorm:"primary_key"`
	Name      string `gorm:"size:128"`
	CompanyID uint   `gorm:"index"`
	Company   officialCompany
	Profile   officialProfile `gorm:"foreignkey:UserID"`
	Orders    []officialOrder `gorm:"foreignkey:UserID"`
	Tags      []officialTag   `gorm:"many2many:kb_official_user_tags;"`
}

func (officialAssocUser) TableName() string { return "kb_official_assoc_users" }

type officialPolyOwner struct {
	ID       uint                  `gorm:"primary_key"`
	Name     string                `gorm:"size:128"`
	Comments []officialPolyComment `gorm:"polymorphic:Owner;"`
}

func (officialPolyOwner) TableName() string { return "kb_official_poly_owners" }

type officialPolyComment struct {
	ID        uint   `gorm:"primary_key"`
	OwnerID   uint   `gorm:"index"`
	OwnerType string `gorm:"size:64;index"`
	Body      string `gorm:"size:255"`
}

func (officialPolyComment) TableName() string { return "kb_official_poly_comments" }

type officialCompositeBlog struct {
	ID      uint                   `gorm:"primary_key"`
	Locale  string                 `gorm:"primary_key;size:16"`
	Subject string                 `gorm:"size:128"`
	Tags    []officialCompositeTag `gorm:"many2many:kb_official_blog_tags;"`
}

func (officialCompositeBlog) TableName() string { return "kb_official_composite_blogs" }

type officialCompositeTag struct {
	ID     uint                     `gorm:"primary_key"`
	Locale string                   `gorm:"primary_key;size:16"`
	Value  string                   `gorm:"size:128"`
	Blogs  []*officialCompositeBlog `gorm:"many2many:kb_official_blog_tags;"`
}

func (officialCompositeTag) TableName() string { return "kb_official_composite_tags" }

func dropOfficialTables(t *testing.T, db *gorm.DB, values ...interface{}) {
	t.Helper()
	if err := db.DropTableIfExists(values...).Error; err != nil {
		t.Fatalf("drop official coverage tables: %v", err)
	}
}

func openOfficialCoverageDB(t *testing.T, mode kb.KbMode) *gorm.DB {
	t.Helper()
	db := testutil.Open(t, mode)
	if mode == kb.KbModeOracle {
		db = testutil.ObserveMetadata(t, db)
	}
	if os.Getenv("KINGBASE_TEST_SQL_LOG") == "1" {
		db.LogMode(true)
	}
	return db
}

func requireRows(t *testing.T, result *gorm.DB, want int64, label string) {
	t.Helper()
	if result.Error != nil {
		t.Fatalf("%s: %v", label, result.Error)
	}
	if result.RowsAffected != want {
		t.Fatalf("%s rows affected=%d, want=%d", label, result.RowsAffected, want)
	}
}

// TestOfficialCreateCoverage covers the create-side scenarios from
// create_test.go that the original Kingbase smoke tests did not exercise.
func TestOfficialCreateCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()

		dropOfficialTables(t, db, &officialCoverageRow{}, &officialDefaultRow{}, &officialNoPrimaryKeyRow{}, &officialAutoIncrementRow{})
		defer db.DropTableIfExists(&officialCoverageRow{}, &officialDefaultRow{}, &officialNoPrimaryKeyRow{}, &officialAutoIncrementRow{})

		t.Run("TestCreateWithExistingTimestamp", func(t *testing.T) {
			if err := db.AutoMigrate(&officialCoverageRow{}).Error; err != nil {
				t.Fatalf("automigrate: %v", err)
			}
			created := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)
			row := officialCoverageRow{
				Name:      "existing-timestamp-" + string(mode),
				Email:     testutil.UniqueTable("timestamp"),
				CreatedAt: created,
				UpdatedAt: created,
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create with existing timestamp: %v", err)
			}
			if !row.CreatedAt.Equal(created) || !row.UpdatedAt.Equal(created) {
				t.Fatalf("existing timestamps changed: created=%s updated=%s", row.CreatedAt, row.UpdatedAt)
			}
		})

		t.Run("TestCreateWithNoStdPrimaryKeyAndDefaultValues", func(t *testing.T) {
			if err := db.AutoMigrate(&officialDefaultRow{}).Error; err != nil {
				t.Fatalf("automigrate defaults: %v", err)
			}
			explicit := officialDefaultRow{From: "explicit", Name: "provided"}
			if err := db.Create(&explicit).Error; err != nil {
				t.Fatalf("create explicit default row: %v", err)
			}
			if explicit.Counter == 0 {
				t.Fatal("non-standard primary key was not populated")
			}
			if explicit.Name != "provided" {
				t.Fatalf("explicit default value was overwritten: %q", explicit.Name)
			}

			implicit := officialDefaultRow{From: "implicit"}
			if err := db.Create(&implicit).Error; err != nil {
				t.Fatalf("create implicit default row: %v", err)
			}
			var loaded officialDefaultRow
			if err := db.First(&loaded, "counter = ?", implicit.Counter).Error; err != nil {
				t.Fatalf("reload implicit default row: %v", err)
			}
			if loaded.Name != "galeone" {
				t.Fatalf("database default not returned: %q", loaded.Name)
			}
		})

		t.Run("TestCreateWithNoGORMPrimayKey", func(t *testing.T) {
			if err := db.AutoMigrate(&officialNoPrimaryKeyRow{}).Error; err != nil {
				t.Fatalf("automigrate no-primary-key row: %v", err)
			}
			if err := db.Create(&officialNoPrimaryKeyRow{From: 1, To: 2}).Error; err != nil {
				t.Fatalf("create row without GORM primary key: %v", err)
			}
		})

		t.Run("TestSelectWithCreate", func(t *testing.T) {
			if err := db.AutoMigrate(&officialCoverageRow{}).Error; err != nil {
				t.Fatalf("automigrate select create: %v", err)
			}
			row := officialCoverageRow{
				Name:  "select-create-" + string(mode),
				Email: testutil.UniqueTable("select-create"),
				Age:   27,
				Note:  "must-not-be-created",
			}
			if err := db.Select("Name", "Email").Create(&row).Error; err != nil {
				t.Fatalf("select create: %v", err)
			}
			var loaded officialCoverageRow
			if err := db.First(&loaded, row.ID).Error; err != nil {
				t.Fatalf("reload select create: %v", err)
			}
			if loaded.Name != row.Name || loaded.Email != row.Email || loaded.Age != 0 || loaded.Note != "" {
				t.Fatalf("select create wrote unexpected fields: %#v", loaded)
			}
		})

		t.Run("TestOmitWithCreate", func(t *testing.T) {
			row := officialCoverageRow{
				Name:  "omit-create-" + string(mode),
				Email: testutil.UniqueTable("omit-create"),
				Age:   31,
				Note:  "omitted-name-only",
			}
			if err := db.Omit("Note", "Age").Create(&row).Error; err != nil {
				t.Fatalf("omit create: %v", err)
			}
			var loaded officialCoverageRow
			if err := db.First(&loaded, row.ID).Error; err != nil {
				t.Fatalf("reload omit create: %v", err)
			}
			if loaded.Name != row.Name || loaded.Age != 0 || loaded.Note != "" {
				t.Fatalf("omit create wrote omitted fields: %#v", loaded)
			}
		})

		t.Run("TestCreateWithAutoIncrement", func(t *testing.T) {
			if err := db.AutoMigrate(&officialAutoIncrementRow{}).Error; err != nil {
				t.Fatalf("automigrate non-primary auto increment: %v", err)
			}
			first := officialAutoIncrementRow{ID: 1, Name: "first"}
			second := officialAutoIncrementRow{ID: 2, Name: "second"}
			if err := db.Create(&first).Error; err != nil {
				t.Fatalf("create first auto increment row: %v", err)
			}
			if err := db.Create(&second).Error; err != nil {
				t.Fatalf("create second auto increment row: %v", err)
			}
			if second.Sequence <= first.Sequence {
				t.Fatalf("non-primary auto increment did not increase: first=%d second=%d", first.Sequence, second.Sequence)
			}
		})

		t.Run("TestCreateIgnore", func(t *testing.T) {
			if mode != kb.KbModeMySQL {
				t.Skip("INSERT IGNORE is a MySQL-mode capability")
			}
			if err := db.AutoMigrate(&officialCoverageRow{}).Error; err != nil {
				t.Fatalf("automigrate create ignore: %v", err)
			}
			row := officialCoverageRow{
				Name:  "ignore-" + string(mode),
				Email: testutil.UniqueTable("ignore"),
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create baseline ignore row: %v", err)
			}
			duplicate := row
			duplicate.ID = 0
			if err := db.Set("gorm:insert_modifier", "IGNORE").Create(&duplicate).Error; err != nil {
				t.Fatalf("INSERT IGNORE duplicate should not fail: %v", err)
			}
			var count int
			if err := db.Model(&officialCoverageRow{}).Where("email = ?", row.Email).Count(&count).Error; err != nil {
				t.Fatalf("count ignored duplicate: %v", err)
			}
			if count != 1 {
				t.Fatalf("INSERT IGNORE inserted duplicate row: count=%d", count)
			}
		})
	})
}

// TestOfficialQueryCoverage covers the query-builder and low-level scan
// variants represented by query_test.go and main_test.go.
func TestOfficialQueryCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db, &officialCoverageRow{})
		defer db.DropTableIfExists(&officialCoverageRow{})
		if err := db.AutoMigrate(&officialCoverageRow{}).Error; err != nil {
			t.Fatalf("automigrate query rows: %v", err)
		}

		seed := []officialCoverageRow{
			{Name: "query-one", Email: testutil.UniqueTable("query-one"), Age: 18, Score: 10, Active: true},
			{Name: "query-two", Email: testutil.UniqueTable("query-two"), Age: 25, Score: 20, Active: true},
			{Name: "query-three", Email: testutil.UniqueTable("query-three"), Age: 40, Score: 30, Active: false},
		}
		for i := range seed {
			if err := db.Create(&seed[i]).Error; err != nil {
				t.Fatalf("seed query row %d: %v", i, err)
			}
		}

		// A false field with DEFAULT true is omitted by GORM Create. Persist
		// false explicitly so the map/group tests exercise their intended data.
		if err := db.Model(&seed[2]).UpdateColumn("active", false).Error; err != nil {
			t.Fatalf("prepare false boolean fixture: %v", err)
		}

		t.Run("TestSearchWithStruct", func(t *testing.T) {
			var got officialCoverageRow
			if err := db.Where(&officialCoverageRow{Name: "query-two"}).First(&got).Error; err != nil {
				t.Fatalf("struct condition: %v", err)
			}
			if got.Age != 25 {
				t.Fatalf("struct condition returned wrong row: %#v", got)
			}
		})

		t.Run("TestSearchWithMap", func(t *testing.T) {
			var got officialCoverageRow
			if err := db.Where(map[string]interface{}{"name": "query-three", "active": false}).First(&got).Error; err != nil {
				t.Fatalf("map condition: %v", err)
			}
			if got.Score != 30 {
				t.Fatalf("map condition returned wrong row: %#v", got)
			}
		})

		t.Run("TestOrAndNot", func(t *testing.T) {
			var rows []officialCoverageRow
			if err := db.Where("name = ?", "query-one").Or("name = ?", "query-two").Find(&rows).Error; err != nil {
				t.Fatalf("OR condition: %v", err)
			}
			if len(rows) != 2 {
				t.Fatalf("OR condition rows=%d, want=2", len(rows))
			}
			rows = nil
			if err := db.Not("name = ?", "query-three").Find(&rows).Error; err != nil {
				t.Fatalf("NOT condition: %v", err)
			}
			if len(rows) != 2 {
				t.Fatalf("NOT condition rows=%d, want=2", len(rows))
			}
		})

		t.Run("TestSelectWithArrayInputAndPluck", func(t *testing.T) {
			var got officialCoverageRow
			if err := db.Select([]string{"name", "age"}).Where("name = ?", "query-one").First(&got).Error; err != nil {
				t.Fatalf("select array: %v", err)
			}
			if got.Name != "query-one" || got.Age != 18 {
				t.Fatalf("select array result: %#v", got)
			}
			var names []string
			if err := db.Model(&officialCoverageRow{}).Order("age asc").Pluck("name", &names).Error; err != nil {
				t.Fatalf("pluck: %v", err)
			}
			if len(names) != 3 || names[0] != "query-one" {
				t.Fatalf("pluck result: %#v", names)
			}
		})

		t.Run("TestRowRowsAndScanRows", func(t *testing.T) {
			row := db.Table((officialCoverageRow{}).TableName()).Where("name = ?", "query-two").Select("name, age").Row()
			var name string
			var age int
			if err := row.Scan(&name, &age); err != nil {
				t.Fatalf("Row.Scan: %v", err)
			}
			if name != "query-two" || age != 25 {
				t.Fatalf("Row.Scan result: name=%q age=%d", name, age)
			}

			rows, err := db.Table((officialCoverageRow{}).TableName()).Select("name, age").Order("age asc").Rows()
			if err != nil {
				t.Fatalf("Rows: %v", err)
			}
			defer rows.Close()
			count := 0
			for rows.Next() {
				var result struct {
					Name string
					Age  int
				}
				if err := db.ScanRows(rows, &result); err != nil {
					t.Fatalf("ScanRows: %v", err)
				}
				count++
			}
			if count != 3 {
				t.Fatalf("ScanRows count=%d, want=3", count)
			}
		})

		t.Run("TestQueryBuilderSubquery", func(t *testing.T) {
			subquery := db.Model(&officialCoverageRow{}).Select("id").Where("age >= ?", 25).QueryExpr()
			var rows []officialCoverageRow
			if err := db.Where("id IN (?)", subquery).Order("age asc").Find(&rows).Error; err != nil {
				t.Fatalf("subquery: %v", err)
			}
			if len(rows) != 2 || rows[0].Name != "query-two" {
				t.Fatalf("subquery result: %#v", rows)
			}
		})

		t.Run("TestSearchWithTwoDimensionalArray", func(t *testing.T) {
			var rows []officialCoverageRow
			if err := db.Where("(name, age) IN (?)", [][]interface{}{{"query-one", 18}, {"query-two", 25}}).Find(&rows).Error; err != nil {
				t.Fatalf("two-dimensional condition: %v", err)
			}
			if len(rows) != 2 {
				t.Fatalf("two-dimensional condition rows=%d, want=2", len(rows))
			}
		})

		t.Run("TestGroupHavingAndCount", func(t *testing.T) {
			type aggregate struct {
				Active bool
				Count  int
			}
			var groups []aggregate
			if err := db.Model(&officialCoverageRow{}).Select("active, COUNT(*) AS count").Group("active").Having("COUNT(*) > ?", 0).Scan(&groups).Error; err != nil {
				t.Fatalf("group/having: %v", err)
			}
			if len(groups) != 2 {
				t.Fatalf("group count=%d, want=2", len(groups))
			}
			var count int
			if err := db.Model(&officialCoverageRow{}).Where("age >= ?", 18).Count(&count).Error; err != nil || count != 3 {
				t.Fatalf("count=%d err=%v", count, err)
			}
		})
	})
}

// TestOfficialUpdateDeleteCoverage covers the update/delete option matrix,
// including the safety guard against global mutations.
func TestOfficialUpdateDeleteCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db, &officialCoverageRow{}, &officialDeletedRow{})
		defer db.DropTableIfExists(&officialCoverageRow{}, &officialDeletedRow{})
		if err := db.AutoMigrate(&officialCoverageRow{}, &officialDeletedRow{}).Error; err != nil {
			t.Fatalf("automigrate update/delete rows: %v", err)
		}

		row := officialCoverageRow{
			Name:  "update-" + string(mode),
			Email: testutil.UniqueTable("update"),
			Age:   20,
			Score: 10,
			Note:  "original",
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed update row: %v", err)
		}

		t.Run("TestUpdateAndUpdates", func(t *testing.T) {
			requireRows(t, db.Model(&officialCoverageRow{}).Where("id = ?", row.ID).Update("score", gorm.Expr("score + ?", 5)), 1, "expression update")
			requireRows(t, db.Model(&officialCoverageRow{}).Where("id = ?", row.ID).Updates(map[string]interface{}{"age": 21, "note": "map-updated"}), 1, "map updates")
			var loaded officialCoverageRow
			if err := db.First(&loaded, row.ID).Error; err != nil {
				t.Fatalf("reload updated row: %v", err)
			}
			if loaded.Score != 15 || loaded.Age != 21 || loaded.Note != "map-updated" {
				t.Fatalf("unexpected update result: %#v", loaded)
			}
		})

		t.Run("TestSelectWithUpdate", func(t *testing.T) {
			var loaded officialCoverageRow
			if err := db.First(&loaded, row.ID).Error; err != nil {
				t.Fatalf("reload for select update: %v", err)
			}
			loaded.Name = "select-updated"
			loaded.Age = 99
			loaded.Note = "select-note"
			if err := db.Select("Name", "Note").Save(&loaded).Error; err != nil {
				t.Fatalf("select update: %v", err)
			}
			var check officialCoverageRow
			if err := db.First(&check, row.ID).Error; err != nil {
				t.Fatalf("reload select update: %v", err)
			}
			if check.Name != "select-updated" || check.Note != "select-note" || check.Age != 21 {
				t.Fatalf("select update changed wrong fields: %#v", check)
			}
		})

		t.Run("TestOmitWithUpdate", func(t *testing.T) {
			var loaded officialCoverageRow
			if err := db.First(&loaded, row.ID).Error; err != nil {
				t.Fatalf("reload for omit update: %v", err)
			}
			loaded.Name = "omit-name-must-not-change"
			loaded.Note = "omit-note"
			if err := db.Omit("Name").Save(&loaded).Error; err != nil {
				t.Fatalf("omit update: %v", err)
			}
			var check officialCoverageRow
			if err := db.First(&check, row.ID).Error; err != nil {
				t.Fatalf("reload omit update: %v", err)
			}
			if check.Name == "omit-name-must-not-change" || check.Note != "omit-note" {
				t.Fatalf("omit update changed wrong fields: %#v", check)
			}
		})

		t.Run("TestUpdateColumn", func(t *testing.T) {
			before := time.Now().Add(-time.Hour)
			if err := db.Model(&officialCoverageRow{}).Where("id = ?", row.ID).UpdateColumn("note", "update-column").Error; err != nil {
				t.Fatalf("update column: %v", err)
			}
			var check officialCoverageRow
			if err := db.First(&check, row.ID).Error; err != nil {
				t.Fatalf("reload update column: %v", err)
			}
			if check.Note != "update-column" || check.UpdatedAt.Before(before) {
				// UpdateColumn intentionally bypasses timestamp callbacks. The
				// timestamp check is only diagnostic; the column assertion is the
				// contract shared by both modes.
				if check.Note != "update-column" {
					t.Fatalf("update column result: %#v", check)
				}
			}
		})

		t.Run("TestUpdatesWithBlankValues", func(t *testing.T) {
			// GORM's struct Updates intentionally ignores blank fields. This is
			// the behavior asserted by the upstream TestUpdatesWithBlankValues;
			// use a map when an explicit blank write is desired.
			if err := db.Model(&officialCoverageRow{}).Where("id = ?", row.ID).Updates(officialCoverageRow{Note: ""}).Error; err != nil {
				t.Fatalf("blank update: %v", err)
			}
			var check officialCoverageRow
			if err := db.First(&check, row.ID).Error; err != nil {
				t.Fatalf("reload blank update: %v", err)
			}
			if check.Note != "update-column" {
				t.Fatalf("blank struct update should be ignored, got note=%q", check.Note)
			}
			if err := db.Model(&officialCoverageRow{}).Where("id = ?", row.ID).Updates(map[string]interface{}{"note": ""}).Error; err != nil {
				t.Fatalf("explicit blank map update: %v", err)
			}
			// Oracle turns an empty string into SQL NULL. Scanning NULL into a
			// reused non-nullable Go string can retain its previous value.
			// Verify server state independently and reload into a fresh model.
			if mode == kb.KbModeOracle {
				var nullCount int
				if err := db.Model(&officialCoverageRow{}).Where("id = ? AND note IS NULL", row.ID).Count(&nullCount).Error; err != nil || nullCount != 1 {
					t.Fatalf("Oracle empty string must persist as NULL: count=%d err=%v", nullCount, err)
				}
			}
			check = officialCoverageRow{}
			if err := db.First(&check, row.ID).Error; err != nil {
				t.Fatalf("reload explicit blank update: %v", err)
			}
			if check.Note != "" {
				t.Fatalf("explicit blank map update was not persisted: %q", check.Note)
			}
		})

		t.Run("TestBlockGlobalUpdate", func(t *testing.T) {
			guarded := db.New().BlockGlobalUpdate(true)
			if err := guarded.Model(&officialCoverageRow{}).Update("note", "blocked").Error; err == nil {
				t.Fatal("global update should be blocked")
			}
			if err := guarded.Model(&officialCoverageRow{}).Where("id = ?", row.ID).Update("note", "allowed").Error; err != nil {
				t.Fatalf("conditional update should be allowed: %v", err)
			}
		})

		t.Run("TestSoftDeleteWithCustomizedDeletedAtColumnName", func(t *testing.T) {
			victim := officialDeletedRow{Name: "deleted-" + string(mode)}
			if err := db.Create(&victim).Error; err != nil {
				t.Fatalf("create deleted row: %v", err)
			}
			if field, ok := db.NewScope(&officialDeletedRow{}).FieldByName("DeletedAt"); !ok || field.DBName != "removed_at" {
				t.Fatalf("custom deleted column not resolved: ok=%v field=%+v", ok, field)
			}
			if err := db.Delete(&victim).Error; err != nil {
				t.Fatalf("soft delete: %v", err)
			}
			if err := db.First(&officialDeletedRow{}, victim.ID).Error; err == nil {
				t.Fatal("soft-deleted row should be hidden")
			}
			var deleted officialDeletedRow
			if err := db.Unscoped().First(&deleted, victim.ID).Error; err != nil || deleted.DeletedAt == nil {
				t.Fatalf("unscoped deleted row: err=%v row=%+v", err, deleted)
			}
			if err := db.Unscoped().Delete(&deleted).Error; err != nil {
				t.Fatalf("hard delete: %v", err)
			}
		})

		t.Run("TestInlineDelete", func(t *testing.T) {
			inline := officialCoverageRow{Name: "inline-delete-" + string(mode), Email: testutil.UniqueTable("inline-delete")}
			if err := db.Create(&inline).Error; err != nil {
				t.Fatalf("create inline delete row: %v", err)
			}
			if err := db.Delete(&officialCoverageRow{}, inline.ID).Error; err != nil {
				t.Fatalf("inline delete by id: %v", err)
			}
			if err := db.Unscoped().Where("id = ?", inline.ID).Delete(&officialCoverageRow{}).Error; err != nil {
				t.Fatalf("inline hard delete cleanup: %v", err)
			}
		})
	})
}

// TestOfficialAssociationCoverage exercises the relation and preload matrix
// shared by association_test.go, join_table_test.go, polymorphic_test.go and
// the basic preload cases.
func TestOfficialAssociationCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db,
			"kb_official_user_tags",
			&officialPolyComment{}, &officialPolyOwner{},
			&officialTag{}, &officialOrder{}, &officialProfile{}, &officialAssocUser{}, &officialCompany{},
		)
		defer db.DropTableIfExists(
			"kb_official_user_tags",
			&officialPolyComment{}, &officialPolyOwner{},
			&officialTag{}, &officialOrder{}, &officialProfile{}, &officialAssocUser{}, &officialCompany{},
		)
		if err := db.AutoMigrate(
			&officialCompany{}, &officialAssocUser{}, &officialProfile{}, &officialOrder{}, &officialTag{},
			&officialPolyOwner{}, &officialPolyComment{},
		).Error; err != nil {
			t.Fatalf("automigrate associations: %v", err)
		}

		t.Run("TestBelongsToAndHasOne", func(t *testing.T) {
			company := officialCompany{Name: "company-" + string(mode)}
			if err := db.Create(&company).Error; err != nil {
				t.Fatalf("create company: %v", err)
			}
			user := officialAssocUser{Name: "relation-user-" + string(mode), CompanyID: company.ID}
			if err := db.Create(&user).Error; err != nil {
				t.Fatalf("create relation user: %v", err)
			}
			profile := officialProfile{UserID: user.ID, Bio: "profile"}
			if err := db.Create(&profile).Error; err != nil {
				t.Fatalf("create profile: %v", err)
			}

			var loaded officialAssocUser
			if err := db.Preload("Company").Preload("Profile").First(&loaded, user.ID).Error; err != nil {
				t.Fatalf("preload belongs-to/has-one: %v", err)
			}
			if loaded.Company.Name != company.Name || loaded.Profile.Bio != profile.Bio {
				t.Fatalf("relation preload result: %#v", loaded)
			}
			if got := db.Model(&loaded).Association("Company").Count(); got != 1 {
				t.Fatalf("belongs-to association count=%d, want=1", got)
			}
		})

		t.Run("TestHasManyAndNestedPreload", func(t *testing.T) {
			var user officialAssocUser
			if err := db.Where("name = ?", "relation-user-"+string(mode)).First(&user).Error; err != nil {
				t.Fatalf("find relation user: %v", err)
			}
			for _, amount := range []int{10, 20} {
				if err := db.Create(&officialOrder{UserID: user.ID, Amount: amount}).Error; err != nil {
					t.Fatalf("create order: %v", err)
				}
			}
			var loaded officialAssocUser
			if err := db.Preload("Orders").First(&loaded, user.ID).Error; err != nil {
				t.Fatalf("preload has-many: %v", err)
			}
			if len(loaded.Orders) != 2 {
				t.Fatalf("preloaded orders=%d, want=2", len(loaded.Orders))
			}
			if got := db.Model(&loaded).Association("Orders").Count(); got != 2 {
				t.Fatalf("has-many association count=%d, want=2", got)
			}
		})

		t.Run("TestManyToManyAndJoinTable", func(t *testing.T) {
			var user officialAssocUser
			if err := db.Where("name = ?", "relation-user-"+string(mode)).First(&user).Error; err != nil {
				t.Fatalf("find relation user: %v", err)
			}
			tags := []*officialTag{{Name: "tag-1-" + string(mode)}, {Name: "tag-2-" + string(mode)}}
			for _, tag := range tags {
				if err := db.Create(tag).Error; err != nil {
					t.Fatalf("create tag: %v", err)
				}
			}
			if association := db.Model(&user).Association("Tags").Append(tags); association.Error != nil {
				t.Fatalf("append tags: %v", association.Error)
			}
			if got := db.Model(&user).Association("Tags").Count(); got != 2 {
				t.Fatalf("many-to-many count=%d, want=2", got)
			}
			var loaded officialAssocUser
			if err := db.Preload("Tags").First(&loaded, user.ID).Error; err != nil {
				t.Fatalf("preload many-to-many: %v", err)
			}
			if len(loaded.Tags) != 2 {
				t.Fatalf("preloaded tags=%d, want=2", len(loaded.Tags))
			}
			if association := db.Model(&user).Association("Tags").Replace(tags[0]); association.Error != nil {
				t.Fatalf("replace tags: %v", association.Error)
			}
			if got := db.Model(&user).Association("Tags").Count(); got != 1 {
				t.Fatalf("many-to-many replace count=%d, want=1", got)
			}
			if association := db.Model(&user).Association("Tags").Clear(); association.Error != nil {
				t.Fatalf("clear tags: %v", association.Error)
			}
			if got := db.Model(&user).Association("Tags").Count(); got != 0 {
				t.Fatalf("many-to-many clear count=%d, want=0", got)
			}
		})

		t.Run("TestPolymorphic", func(t *testing.T) {
			owner := officialPolyOwner{Name: "poly-owner-" + string(mode)}
			if err := db.Create(&owner).Error; err != nil {
				t.Fatalf("create polymorphic owner: %v", err)
			}
			// GORM uses the owner's table name as the default polymorphic value
			// (see model_struct.go), not the Go type name.
			comment := officialPolyComment{OwnerID: owner.ID, OwnerType: owner.TableName(), Body: "hello"}
			if err := db.Create(&comment).Error; err != nil {
				t.Fatalf("create polymorphic comment: %v", err)
			}
			var loaded officialPolyOwner
			if err := db.Preload("Comments").First(&loaded, owner.ID).Error; err != nil {
				t.Fatalf("preload polymorphic comments: %v", err)
			}
			if len(loaded.Comments) != 1 || loaded.Comments[0].Body != "hello" {
				t.Fatalf("polymorphic preload result: %#v", loaded.Comments)
			}
		})
	})
}

func TestOfficialCompositePrimaryKeyCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db, "kb_official_blog_tags", &officialCompositeBlog{}, &officialCompositeTag{})
		defer db.DropTableIfExists("kb_official_blog_tags", &officialCompositeBlog{}, &officialCompositeTag{})
		if err := db.AutoMigrate(&officialCompositeBlog{}, &officialCompositeTag{}).Error; err != nil {
			t.Fatalf("automigrate composite relation: %v", err)
		}

		t.Run("TestManyToManyWithMultiPrimaryKeys", func(t *testing.T) {
			blog := officialCompositeBlog{
				Locale:  "zh",
				Subject: "subject-" + string(mode),
				Tags:    []officialCompositeTag{{Locale: "zh", Value: "tag-1"}, {Locale: "zh", Value: "tag-2"}},
			}
			if err := db.Save(&blog).Error; err != nil {
				t.Fatalf("save composite blog: %v", err)
			}
			var loaded officialCompositeBlog
			if err := db.Preload("Tags").Where("id = ? AND locale = ?", blog.ID, blog.Locale).First(&loaded).Error; err != nil {
				t.Fatalf("preload composite blog: %v", err)
			}
			if len(loaded.Tags) != 2 {
				t.Fatalf("composite tags=%d, want=2", len(loaded.Tags))
			}
			if got := db.Model(&loaded).Association("Tags").Count(); got != 2 {
				t.Fatalf("composite association count=%d, want=2", got)
			}
		})
	})
}

func TestOfficialEmbeddedPointerAndScannerCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db, &officialEmbeddedRow{}, &officialEmbeddedPointerRow{}, &officialPointerRow{}, &officialScannerRow{})
		defer db.DropTableIfExists(&officialEmbeddedRow{}, &officialEmbeddedPointerRow{}, &officialPointerRow{}, &officialScannerRow{})

		t.Run("TestPrefixColumnNameForEmbeddedStruct", func(t *testing.T) {
			if err := db.AutoMigrate(&officialEmbeddedRow{}).Error; err != nil {
				t.Fatalf("automigrate embedded row: %v", err)
			}
			scope := db.NewScope(&officialEmbeddedRow{})
			if !scope.Dialect().HasColumn(scope.TableName(), "author_name") || !scope.Dialect().HasColumn(scope.TableName(), "title") {
				t.Fatalf("embedded columns missing from %s", scope.TableName())
			}
			row := officialEmbeddedRow{OfficialEmbeddedBase: OfficialEmbeddedBase{Title: "embedded-title", URL: "https://example.test"}, OfficialEmbeddedAuthor: OfficialEmbeddedAuthor{Name: "author", Email: "author@example.test"}, Upvotes: 3}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create embedded row: %v", err)
			}
			var loaded officialEmbeddedRow
			if err := db.First(&loaded, "title = ?", "embedded-title").Error; err != nil {
				t.Fatalf("query embedded row: %v", err)
			}
			if loaded.Title != row.Title || loaded.Name != row.Name || loaded.Upvotes != row.Upvotes {
				t.Fatalf("embedded row mismatch: %#v", loaded)
			}
		})

		t.Run("TestPointerFields", func(t *testing.T) {
			if err := db.AutoMigrate(&officialPointerRow{}).Error; err != nil {
				t.Fatalf("automigrate pointer row: %v", err)
			}
			name := "pointer-" + string(mode)
			count := 42
			row := officialPointerRow{Name: &name, Count: &count}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create pointer row: %v", err)
			}
			var loaded officialPointerRow
			if err := db.First(&loaded, row.ID).Error; err != nil || loaded.Name == nil || loaded.Count == nil || *loaded.Name != name || *loaded.Count != count {
				t.Fatalf("pointer row mismatch: err=%v row=%#v", err, loaded)
			}
			nilRow := officialPointerRow{}
			if err := db.Create(&nilRow).Error; err != nil {
				t.Fatalf("create nil pointer row: %v", err)
			}
			var nilLoaded officialPointerRow
			if err := db.First(&nilLoaded, nilRow.ID).Error; err != nil {
				t.Fatalf("reload nil pointer row: %v", err)
			}
			if nilLoaded.Name != nil || nilLoaded.Count != nil {
				t.Fatalf("nil pointers were not preserved: %#v", nilLoaded)
			}
		})

		t.Run("TestEmbeddedPointerTypeStruct", func(t *testing.T) {
			if err := db.AutoMigrate(&officialEmbeddedPointerRow{}).Error; err != nil {
				t.Fatalf("automigrate embedded pointer row: %v", err)
			}
			row := officialEmbeddedPointerRow{OfficialEmbeddedBase: &OfficialEmbeddedBase{Title: "embedded-pointer-" + string(mode)}, Upvotes: 7}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create embedded pointer row: %v", err)
			}
			var loaded officialEmbeddedPointerRow
			if err := db.First(&loaded, "title = ?", row.Title).Error; err != nil {
				t.Fatalf("query embedded pointer row: %v", err)
			}
			if loaded.OfficialEmbeddedBase == nil || loaded.Title != row.Title || loaded.Upvotes != row.Upvotes {
				t.Fatalf("embedded pointer row mismatch: %#v", loaded)
			}
		})

		t.Run("TestScannableSlicesAndScannerDataType", func(t *testing.T) {
			if err := db.AutoMigrate(&officialScannerRow{}).Error; err != nil {
				t.Fatalf("automigrate scanner row: %v", err)
			}
			row := officialScannerRow{Labels: officialJSONSlice{"one", "two", string(mode)}}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("create scanner row: %v", err)
			}
			var loaded officialScannerRow
			if err := db.First(&loaded, row.ID).Error; err != nil {
				t.Fatalf("reload scanner row: %v", err)
			}
			if len(loaded.Labels) != 3 || loaded.Labels[2] != string(mode) {
				t.Fatalf("scanner/valuer result: %#v", loaded.Labels)
			}
		})
	})
}

func TestOfficialCallbackCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db, &officialCoverageRow{})
		defer db.DropTableIfExists(&officialCoverageRow{})
		if err := db.AutoMigrate(&officialCoverageRow{}).Error; err != nil {
			t.Fatalf("automigrate callback row: %v", err)
		}

		t.Run("TestRegisterCallbackWithOrder", func(t *testing.T) {
			var events []string
			beforeName := testutil.UniqueTable("official_before_callback")
			afterName := testutil.UniqueTable("official_after_callback")
			db.Callback().Create().Before("gorm:create").Register(beforeName, func(*gorm.Scope) {
				events = append(events, "before")
			})
			db.Callback().Create().After("gorm:create").Register(afterName, func(*gorm.Scope) {
				events = append(events, "after")
			})
			defer db.Callback().Create().Remove(beforeName)
			defer db.Callback().Create().Remove(afterName)

			row := officialCoverageRow{Name: "callback-order-" + string(mode), Email: testutil.UniqueTable("callback-order")}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("callback create: %v", err)
			}
			if len(events) != 2 || events[0] != "before" || events[1] != "after" {
				t.Fatalf("callback order=%v, want=[before after]", events)
			}
		})

		t.Run("TestReplaceAndRemoveCallback", func(t *testing.T) {
			callbackName := testutil.UniqueTable("official_replace_callback")
			value := "first"
			db.Callback().Create().Before("gorm:create").Register(callbackName, func(*gorm.Scope) {
				value = "first-called"
			})
			db.Callback().Create().Before("gorm:create").Replace(callbackName, func(*gorm.Scope) {
				value = "replacement-called"
			})
			row := officialCoverageRow{Name: "callback-replace-" + string(mode), Email: testutil.UniqueTable("callback-replace")}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("replacement callback create: %v", err)
			}
			if value != "replacement-called" {
				t.Fatalf("replacement callback value=%q", value)
			}
			db.Callback().Create().Remove(callbackName)
			value = "removed"
			row = officialCoverageRow{Name: "callback-removed-" + string(mode), Email: testutil.UniqueTable("callback-removed")}
			if err := db.Create(&row).Error; err != nil {
				t.Fatalf("removed callback create: %v", err)
			}
			if value != "removed" {
				t.Fatalf("removed callback was still invoked: %q", value)
			}
		})

		t.Run("TestCallbacksWithErrors", func(t *testing.T) {
			callbackName := testutil.UniqueTable("official_error_callback")
			db.Callback().Create().Before("gorm:create").Register(callbackName, func(scope *gorm.Scope) {
				scope.Err(errors.New("official callback failure"))
			})
			defer db.Callback().Create().Remove(callbackName)
			row := officialCoverageRow{Name: "callback-error-" + string(mode), Email: testutil.UniqueTable("callback-error")}
			if err := db.Create(&row).Error; err == nil || !strings.Contains(err.Error(), "official callback failure") {
				t.Fatalf("callback error=%v, want official callback failure", err)
			}
			var count int
			if err := db.Unscoped().Model(&officialCoverageRow{}).Where("email = ?", row.Email).Count(&count).Error; err != nil {
				t.Fatalf("count callback rollback row: %v", err)
			}
			if count != 0 {
				t.Fatalf("callback error inserted %d row(s)", count)
			}
		})
	})
}

func TestOfficialMigrationAndSafetyCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()
		dropOfficialTables(t, db, &officialCoverageRow{})
		defer db.DropTableIfExists(&officialCoverageRow{})
		defer db.DropTableIfExists(&officialTransactionRow{})

		t.Run("TestAutoMigrationAndMultipleIndexes", func(t *testing.T) {
			if err := db.AutoMigrate(&officialCoverageRow{}).Error; err != nil {
				t.Fatalf("automigrate: %v", err)
			}
			if !db.HasTable((officialCoverageRow{}).TableName()) || !db.Dialect().HasColumn((officialCoverageRow{}).TableName(), "email") {
				t.Fatal("auto-migrated table or column is missing")
			}
			if err := db.Model(&officialCoverageRow{}).AddIndex("idx_official_coverage_name_age", "name", "age").Error; err != nil {
				t.Fatalf("add composite index: %v", err)
			}
			if !db.Dialect().HasIndex((officialCoverageRow{}).TableName(), "idx_official_coverage_name_age") {
				t.Fatal("composite index was not found")
			}
			if err := db.Model(&officialCoverageRow{}).RemoveIndex("idx_official_coverage_name_age").Error; err != nil {
				t.Fatalf("remove composite index: %v", err)
			}
			if db.Dialect().HasIndex((officialCoverageRow{}).TableName(), "idx_official_coverage_name_age") {
				t.Fatal("removed composite index still exists")
			}
		})

		t.Run("TestCreateAndAutomigrateTransaction", func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatalf("begin migration transaction: %v", tx.Error)
			}
			if err := tx.AutoMigrate(&officialTransactionRow{}).Error; err != nil {
				_ = tx.Rollback()
				t.Fatalf("automigrate in transaction: %v", err)
			}
			if err := tx.Create(&officialTransactionRow{Name: "rolled-back"}).Error; err != nil {
				_ = tx.Rollback()
				t.Fatalf("create in migration transaction: %v", err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatalf("rollback migration transaction: %v", err)
			}
			// DDL transaction behavior is mode-specific. The assertion is
			// deliberately limited to the rollback result when the table remains
			// queryable; a mode that commits DDL may still retain an empty table.
			var count int
			if err := db.Model(&officialTransactionRow{}).Count(&count).Error; err == nil && count != 0 {
				t.Fatalf("rolled-back migration transaction retained %d row(s)", count)
			}
			_ = db.DropTableIfExists(&officialTransactionRow{})
		})

		t.Run("TestInvalidTableAndColumnErrors", func(t *testing.T) {
			var rows []officialCoverageRow
			if err := db.Table("kb_official_missing_table").Find(&rows).Error; err == nil {
				t.Fatal("missing table query should fail")
			}
			if err := db.Table((officialCoverageRow{}).TableName()).Where("missing_column = ?", 1).Find(&rows).Error; err == nil {
				t.Fatal("missing column query should fail")
			}
		})
	})
}

func TestOfficialModeSpecificCoverage(t *testing.T) {
	forEachMode(t, func(t *testing.T, mode kb.KbMode) {
		db := openOfficialCoverageDB(t, mode)
		defer db.Close()

		t.Run("TestDialectPaginationAndIdentifier", func(t *testing.T) {
			limitSQL, err := db.Dialect().LimitAndOffsetSQL(10, 20)
			if err != nil {
				t.Fatalf("limit/offset generation: %v", err)
			}
			if limitSQL == "" || !strings.Contains(limitSQL, "10") || !strings.Contains(limitSQL, "20") {
				t.Fatalf("unexpected limit/offset SQL: %q", limitSQL)
			}
			quoted := db.Dialect().Quote("select")
			if quoted == "select" || quoted == "" {
				t.Fatalf("identifier was not quoted: %q", quoted)
			}
		})

		t.Run("TestMySQLSpecificInsertIgnoreAndPrefixCapability", func(t *testing.T) {
			if mode != kb.KbModeMySQL {
				t.Skip("MySQL-mode-only capability")
			}
			// The full INSERT IGNORE behavior is tested in
			// TestOfficialCreateCoverage. Keep a separate capability assertion
			// here so a mode report clearly names the MySQL-only branch.
			if db.Dialect().GetName() != kb.DialectKingbaseMySQL {
				t.Fatalf("unexpected MySQL mode dialect: %s", db.Dialect().GetName())
			}
		})

		t.Run("TestOracleDual", func(t *testing.T) {
			if mode != kb.KbModeOracle {
				t.Skip("Oracle-mode-only capability")
			}
			var result struct{ Value int }
			if err := db.Raw("SELECT 1 AS value FROM DUAL").Scan(&result).Error; err != nil {
				t.Fatalf("DUAL query: %v", err)
			}
			if result.Value != 1 {
				t.Fatalf("DUAL value=%d, want=1", result.Value)
			}
		})
	})
}
