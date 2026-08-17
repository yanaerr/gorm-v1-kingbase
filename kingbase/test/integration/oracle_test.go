package kingbase_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type KBOracleUser struct {
	gorm.Model
	Username string `gorm:"type:varchar2(64);unique_index;not null"`
	Email    string `gorm:"type:varchar2(128)"`
	Age      int    `gorm:"not null"`
	Score    float64
	Status   string `gorm:"type:varchar2(16);not null"`
	Note     string `gorm:"type:varchar2(512)"`
	Active   bool   `gorm:"not null;default:true"`
}

type KBOracleMigrationExtension struct {
	ID       uint   `gorm:"primary_key"`
	Category string `gorm:"type:varchar2(32)"`
}

type KBOracleParent struct {
	ID   uint   `gorm:"primary_key"`
	Name string `gorm:"type:varchar2(64);not null"`
}

type KBOracleChild struct {
	ID       uint `gorm:"primary_key"`
	ParentID uint `gorm:"not null"`
	Parent   KBOracleParent
}

type KBOracleLargeValue struct {
	ID         uint      `gorm:"primary_key"`
	Content    string    `gorm:"type:clob;not null"`
	Payload    []byte    `gorm:"type:blob;not null"`
	Optional   *string   `gorm:"type:varchar2(64)"`
	RecordedAt time.Time `gorm:"type:timestamp with time zone;not null"`
}

const oracleTestDivider = "============================================================"

func logOracleTestPhase(t *testing.T, status, phase, detail string) {
	t.Helper()
	t.Logf("\n%s\n【Kingbase Oracle 模式】【%s】%s\n%s\n%s", oracleTestDivider, status, phase, detail, oracleTestDivider)
}

func startOracleTestPhase(t *testing.T, phase, detail string) {
	t.Helper()
	logOracleTestPhase(t, "开始", phase, detail)
}

func passOracleTestPhase(t *testing.T, phase, detail string) {
	t.Helper()
	logOracleTestPhase(t, "通过", phase, detail)
}

func skipOracleTestPhase(t *testing.T, phase, format string, args ...interface{}) {
	t.Helper()
	detail := fmt.Sprintf(format, args...)
	logOracleTestPhase(t, "跳过", phase, detail)
	t.Skipf("【跳过】%s：%s", phase, detail)
}

func failOracleTestPhase(t *testing.T, phase, format string, args ...interface{}) {
	t.Helper()
	detail := fmt.Sprintf(format, args...)
	logOracleTestPhase(t, "失败", phase, detail)
	t.Fatalf("【失败】%s：%s", phase, detail)
}

func openKBOracle(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.Open(t, kb.KbModeOracle)
	passOracleTestPhase(t, "连接与模式校验", "数据库连接、Ping 与模式检查完成，database_mode=oracle")
	db.LogMode(true)
	return db
}

func TestKingbaseOracleMode_ConnectOnly(t *testing.T) {
	startOracleTestPhase(t, "仅连接验证", "验证连接、Ping 和 database_mode=oracle")
	db := openKBOracle(t)
	if db == nil {
		return
	}
	defer db.Close()
	passOracleTestPhase(t, "仅连接验证", "Oracle 模式连接与 Ping 验证完成")
}

func TestKingbaseOracleMode_FullIntegration(t *testing.T) {
	startOracleTestPhase(t, "完整集成测试", "覆盖 DDL、元数据、CRUD、约束、删除、外键、大字段、事务和 DUAL")
	db := openKBOracle(t)
	if db == nil {
		return
	}
	defer db.Close()

	runID := time.Now().Unix()
	table := fmt.Sprintf("kb_oracle_users_%d", runID)
	statusIndexName := fmt.Sprintf("idx_oracle_st_%d", runID)
	_ = db.DropTableIfExists(table).Error
	defer db.DropTableIfExists(table)

	t.Run("DDL_And_Metadata", func(t *testing.T) {
		startOracleTestPhase(t, "DDL 与元数据", "验证建表、字段、索引和字段类型修改")
		if err := db.Table(table).AutoMigrate(&KBOracleUser{}).Error; err != nil {
			failOracleTestPhase(t, "DDL 与元数据", "自动迁移失败：%v", err)
		}
		if !db.HasTable(table) {
			failOracleTestPhase(t, "DDL 与元数据", "表未创建成功：%s", table)
		}
		if !db.Dialect().HasColumn(table, "username") {
			failOracleTestPhase(t, "DDL 与元数据", "字段 username 未创建成功")
		}

		if err := db.Table(table).AddIndex(statusIndexName, "status").Error; err != nil {
			failOracleTestPhase(t, "DDL 与元数据", "新增索引失败：%v", err)
		}
		if !db.Dialect().HasIndex(table, statusIndexName) {
			failOracleTestPhase(t, "DDL 与元数据", "索引 %s 未创建成功", statusIndexName)
		}
		if err := db.Table(table).ModifyColumn("note", "varchar2(1024)").Error; err != nil {
			failOracleTestPhase(t, "DDL 与元数据", "字段类型变更失败：%v", err)
		}
		if err := db.Table(table).AutoMigrate(&KBOracleMigrationExtension{}).Error; err != nil {
			failOracleTestPhase(t, "DDL 与元数据", "已有表自动增列失败：%v", err)
		}
		if !db.Dialect().HasColumn(table, "category") {
			failOracleTestPhase(t, "DDL 与元数据", "已有表未新增 category 字段")
		}

		schema := db.Dialect().CurrentDatabase()
		if schema == "" {
			failOracleTestPhase(t, "DDL 与元数据", "未能读取当前 schema")
		}
		qualifiedTable := schema + "." + table
		if !db.Dialect().HasTable(qualifiedTable) {
			failOracleTestPhase(t, "DDL 与元数据", "schema 限定表查询失败：%s", qualifiedTable)
		}
		if !db.Dialect().HasColumn(qualifiedTable, "username") {
			failOracleTestPhase(t, "DDL 与元数据", "schema 限定字段查询失败：%s.username", qualifiedTable)
		}
		if !db.Dialect().HasIndex(qualifiedTable, statusIndexName) {
			failOracleTestPhase(t, "DDL 与元数据", "schema 限定索引查询失败：%s.%s", qualifiedTable, statusIndexName)
		}
		passOracleTestPhase(t, "DDL 与元数据", "建表、增列、字段修改及 USER_*/ALL_* 元数据查询完成")
	})

	seed := []KBOracleUser{
		{Username: "alice", Email: "a@test.com", Age: 18, Score: 90, Status: "active", Note: strings.Repeat("n", 700), Active: true},
		{Username: "bob", Email: "b@test.com", Age: 22, Score: 75, Status: "active", Note: "second", Active: true},
		{Username: "cindy", Email: "c@test.com", Age: 31, Score: 60, Status: "inactive", Note: "third", Active: false},
	}

	t.Run("CRUD_And_Pagination", func(t *testing.T) {
		startOracleTestPhase(t, "CRUD 与分页", "验证插入回填、分页查询和表达式更新")
		for i := range seed {
			if err := db.Table(table).Create(&seed[i]).Error; err != nil {
				failOracleTestPhase(t, "CRUD 与分页", "插入数据失败：%v", err)
			}
			if seed[i].ID == 0 {
				failOracleTestPhase(t, "CRUD 与分页", "插入后未返回自增主键")
			}
		}

		var users []KBOracleUser
		if err := db.Table(table).
			Where("age >= ?", 18).
			Order("score desc").
			Limit(2).
			Offset(1).
			Find(&users).Error; err != nil {
			failOracleTestPhase(t, "CRUD 与分页", "分页查询失败：%v", err)
		}
		if len(users) != 2 {
			failOracleTestPhase(t, "CRUD 与分页", "分页结果不符合预期：期望=2，实际=%d", len(users))
		}

		if err := db.Table(table).Where("username = ?", "bob").
			Update("score", gorm.Expr("score + ?", 5)).Error; err != nil {
			failOracleTestPhase(t, "CRUD 与分页", "表达式更新失败：%v", err)
		}
		var bob KBOracleUser
		if err := db.Table(table).Where("username = ?", "bob").First(&bob).Error; err != nil {
			failOracleTestPhase(t, "CRUD 与分页", "查询 bob 失败：%v", err)
		}
		if bob.Score != 80 {
			failOracleTestPhase(t, "CRUD 与分页", "更新结果不符合预期：期望分数=80，实际=%v", bob.Score)
		}
		passOracleTestPhase(t, "CRUD 与分页", "3 条记录插入回填成功，分页和表达式更新验证完成")
	})

	t.Run("Constraints_And_Index_Removal", func(t *testing.T) {
		startOracleTestPhase(t, "约束与索引删除", "验证唯一约束冲突、索引创建和 DROP INDEX")

		indexName := fmt.Sprintf("idx_oracle_rm_%d", runID)
		if err := db.Table(table).AddIndex(indexName, "email").Error; err != nil {
			failOracleTestPhase(t, "约束与索引删除", "创建待删除索引失败：%v", err)
		}
		if !db.Dialect().HasIndex(table, indexName) {
			failOracleTestPhase(t, "约束与索引删除", "新建索引未在系统目录中查到：%s", indexName)
		}
		if err := db.Table(table).RemoveIndex(indexName).Error; err != nil {
			failOracleTestPhase(t, "约束与索引删除", "删除索引失败：%v", err)
		}
		if db.Dialect().HasIndex(table, indexName) {
			failOracleTestPhase(t, "约束与索引删除", "索引删除后仍能查到：%s", indexName)
		}

		duplicate := KBOracleUser{
			Username: "alice",
			Email:    "duplicate@test.com",
			Age:      20,
			Status:   "active",
			Active:   true,
		}
		if err := db.New().LogMode(false).Table(table).Create(&duplicate).Error; err == nil {
			failOracleTestPhase(t, "约束与索引删除", "重复 username 插入未触发唯一约束错误")
		}
		passOracleTestPhase(t, "约束与索引删除", "索引创建/删除成功，重复 username 被唯一约束拒绝")
	})

	t.Run("Transaction_Commit_And_Rollback", func(t *testing.T) {
		startOracleTestPhase(t, "事务提交与回滚", "验证提交数据持久化、回滚数据不落库")
		tx := db.Begin()
		if tx.Error != nil {
			failOracleTestPhase(t, "事务提交与回滚", "开启事务失败：%v", tx.Error)
		}
		committed := KBOracleUser{Username: "tx_commit", Age: 20, Score: 66, Status: "active", Active: true}
		if err := tx.Table(table).Create(&committed).Error; err != nil {
			tx.Rollback()
			failOracleTestPhase(t, "事务提交与回滚", "事务内插入失败：%v", err)
		}
		if err := tx.Commit().Error; err != nil {
			failOracleTestPhase(t, "事务提交与回滚", "事务提交失败：%v", err)
		}

		tx = db.Begin()
		if tx.Error != nil {
			failOracleTestPhase(t, "事务提交与回滚", "开启回滚事务失败：%v", tx.Error)
		}
		rolledBack := KBOracleUser{Username: "tx_rollback", Age: 20, Score: 50, Status: "active", Active: true}
		if err := tx.Table(table).Create(&rolledBack).Error; err != nil {
			tx.Rollback()
			failOracleTestPhase(t, "事务提交与回滚", "回滚事务内插入失败：%v", err)
		}
		if err := tx.Rollback().Error; err != nil {
			failOracleTestPhase(t, "事务提交与回滚", "事务回滚失败：%v", err)
		}

		var committedCount, rolledBackCount int
		if err := db.Table(table).Where("username = ?", "tx_commit").Count(&committedCount).Error; err != nil {
			failOracleTestPhase(t, "事务提交与回滚", "查询提交记录失败：%v", err)
		}
		if err := db.Table(table).Where("username = ?", "tx_rollback").Count(&rolledBackCount).Error; err != nil {
			failOracleTestPhase(t, "事务提交与回滚", "查询回滚记录失败：%v", err)
		}
		if committedCount != 1 || rolledBackCount != 0 {
			failOracleTestPhase(t, "事务提交与回滚", "事务结果不符合预期：提交记录=%d，回滚记录=%d", committedCount, rolledBackCount)
		}
		passOracleTestPhase(t, "事务提交与回滚", "提交记录=1，回滚记录=0，事务语义验证完成")
	})

	t.Run("Soft_Delete_Restore_And_Hard_Delete", func(t *testing.T) {
		startOracleTestPhase(t, "软删除、恢复与硬删除", "验证 deleted_at 过滤、恢复和 Unscoped 永久删除")

		victim := KBOracleUser{
			Username: "delete_target",
			Email:    "delete@test.com",
			Age:      25,
			Status:   "active",
			Active:   true,
		}
		if err := db.Table(table).Create(&victim).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "创建待删除记录失败：%v", err)
		}
		if err := db.Table(table).Delete(&victim).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "软删除失败：%v", err)
		}

		var visible KBOracleUser
		err := db.Table(table).Where("id = ?", victim.ID).First(&visible).Error
		if !gorm.IsRecordNotFoundError(err) {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "软删除记录仍可被普通查询命中：%v", err)
		}

		var deleted KBOracleUser
		if err := db.Table(table).Unscoped().Where("id = ?", victim.ID).First(&deleted).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "Unscoped 查询软删除记录失败：%v", err)
		}
		if deleted.DeletedAt == nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "软删除后 deleted_at 仍为空")
		}

		if err := db.Table(table).Unscoped().Model(&KBOracleUser{}).
			Where("id = ?", victim.ID).
			UpdateColumn("deleted_at", nil).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "恢复软删除记录失败：%v", err)
		}
		var restored KBOracleUser
		if err := db.Table(table).Where("id = ?", victim.ID).First(&restored).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "恢复后普通查询未找到记录：%v", err)
		}

		if err := db.Table(table).Unscoped().Delete(&restored).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "永久删除失败：%v", err)
		}
		var remaining int
		if err := db.Table(table).Unscoped().Where("id = ?", victim.ID).Count(&remaining).Error; err != nil {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "查询永久删除结果失败：%v", err)
		}
		if remaining != 0 {
			failOracleTestPhase(t, "软删除、恢复与硬删除", "永久删除后仍存在记录：%d", remaining)
		}
		passOracleTestPhase(t, "软删除、恢复与硬删除", "软删除过滤、Unscoped 查询、恢复和永久删除全部正确")
	})

	t.Run("Foreign_Key_And_Cascade", func(t *testing.T) {
		startOracleTestPhase(t, "外键与级联删除", "验证外键创建、USER_CONSTRAINTS 查询、级联删除和约束删除")

		parentTable := fmt.Sprintf("kb_oracle_parent_%d", runID)
		childTable := fmt.Sprintf("kb_oracle_child_%d", runID)
		constraintName := fmt.Sprintf("fk_oracle_%d", runID)
		_ = db.DropTableIfExists(childTable).Error
		_ = db.DropTableIfExists(parentTable).Error
		defer db.DropTableIfExists(parentTable)
		defer db.DropTableIfExists(childTable)

		if err := db.Table(parentTable).AutoMigrate(&KBOracleParent{}).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "创建父表失败：%v", err)
		}
		if err := db.Table(childTable).AutoMigrate(&KBOracleChild{}).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "创建子表失败：%v", err)
		}

		addForeignKeySQL := fmt.Sprintf(
			`ALTER TABLE "%s" ADD CONSTRAINT "%s" FOREIGN KEY ("parent_id") REFERENCES "%s" ("id") ON DELETE CASCADE`,
			childTable,
			constraintName,
			parentTable,
		)
		if err := db.Exec(addForeignKeySQL).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "创建外键失败：%v", err)
		}
		if !db.Dialect().HasForeignKey(childTable, constraintName) {
			failOracleTestPhase(t, "外键与级联删除", "外键未在 USER_CONSTRAINTS 中查到：%s", constraintName)
		}

		parent := KBOracleParent{Name: "parent"}
		if err := db.Table(parentTable).Create(&parent).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "插入父记录失败：%v", err)
		}
		child := KBOracleChild{ParentID: parent.ID}
		if err := db.Table(childTable).Create(&child).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "插入子记录失败：%v", err)
		}
		if err := db.Table(parentTable).Delete(&parent).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "删除父记录失败：%v", err)
		}
		var childCount int
		if err := db.Table(childTable).Where("id = ?", child.ID).Count(&childCount).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "查询级联删除结果失败：%v", err)
		}
		if childCount != 0 {
			failOracleTestPhase(t, "外键与级联删除", "父记录删除后子记录未被级联删除：%d", childCount)
		}

		dropForeignKeySQL := fmt.Sprintf(`ALTER TABLE "%s" DROP CONSTRAINT "%s"`, childTable, constraintName)
		if err := db.Exec(dropForeignKeySQL).Error; err != nil {
			failOracleTestPhase(t, "外键与级联删除", "删除外键失败：%v", err)
		}
		if db.Dialect().HasForeignKey(childTable, constraintName) {
			failOracleTestPhase(t, "外键与级联删除", "外键删除后仍能在系统目录中查到：%s", constraintName)
		}
		passOracleTestPhase(t, "外键与级联删除", "外键目录查询、ON DELETE CASCADE 和约束删除验证完成")
	})

	t.Run("CLOB_BLOB_NULL_And_Timestamp", func(t *testing.T) {
		startOracleTestPhase(t, "大字段、NULL 与时间", "验证 CLOB、BLOB、NULL 和 timestamp with time zone 的写入读取")

		lobDB := db.New().LogMode(false)
		lobTable := fmt.Sprintf("kb_oracle_lob_%d", runID)
		_ = lobDB.DropTableIfExists(lobTable).Error
		defer lobDB.DropTableIfExists(lobTable)
		if err := lobDB.Table(lobTable).AutoMigrate(&KBOracleLargeValue{}).Error; err != nil {
			failOracleTestPhase(t, "大字段、NULL 与时间", "创建大字段测试表失败：%v", err)
		}

		content := strings.Repeat("Kingbase Oracle CLOB 测试内容。", 300)
		payload := bytes.Repeat([]byte{0x00, 0x01, 0x7f, 0xff}, 2048)
		recordedAt := time.Now().Truncate(time.Microsecond)
		value := KBOracleLargeValue{
			Content:    content,
			Payload:    payload,
			Optional:   nil,
			RecordedAt: recordedAt,
		}
		if err := lobDB.Table(lobTable).Create(&value).Error; err != nil {
			failOracleTestPhase(t, "大字段、NULL 与时间", "写入大字段记录失败：%v", err)
		}
		var loaded KBOracleLargeValue
		if err := lobDB.Table(lobTable).Where("id = ?", value.ID).First(&loaded).Error; err != nil {
			failOracleTestPhase(t, "大字段、NULL 与时间", "读取大字段记录失败：%v", err)
		}
		if loaded.Content != content {
			failOracleTestPhase(t, "大字段、NULL 与时间", "CLOB 内容不一致：期望长度=%d，实际长度=%d", len(content), len(loaded.Content))
		}
		if !bytes.Equal(loaded.Payload, payload) {
			failOracleTestPhase(t, "大字段、NULL 与时间", "BLOB 内容不一致：期望长度=%d，实际长度=%d", len(payload), len(loaded.Payload))
		}
		if loaded.Optional != nil {
			failOracleTestPhase(t, "大字段、NULL 与时间", "NULL 字段读取后不是 nil：%q", *loaded.Optional)
		}
		delta := loaded.RecordedAt.Sub(recordedAt)
		if delta < 0 {
			delta = -delta
		}
		if delta > time.Second {
			failOracleTestPhase(t, "大字段、NULL 与时间", "时间值偏差过大：期望=%s，实际=%s", recordedAt, loaded.RecordedAt)
		}
		passOracleTestPhase(t, "大字段、NULL 与时间", fmt.Sprintf("CLOB=%d 字节、BLOB=%d 字节、NULL 与时区时间验证完成", len(content), len(payload)))
	})

	t.Run("Oracle_Dual", func(t *testing.T) {
		startOracleTestPhase(t, "DUAL 常量查询", "验证 Oracle 模式的 DUAL 伪表")
		var result struct {
			Value int
		}
		if err := db.Raw("SELECT 1 AS value FROM DUAL").Scan(&result).Error; err != nil {
			failOracleTestPhase(t, "DUAL 常量查询", "DUAL 查询失败：%v", err)
		}
		if result.Value != 1 {
			failOracleTestPhase(t, "DUAL 常量查询", "DUAL 查询结果不符合预期：期望=1，实际=%d", result.Value)
		}
		passOracleTestPhase(t, "DUAL 常量查询", "SELECT 1 FROM DUAL 返回结果正确")
	})

	if !t.Failed() {
		passOracleTestPhase(t, "完整集成测试", "DDL、元数据、CRUD、约束、删除、外键、大字段、事务和 DUAL 全部通过")
	}
}
