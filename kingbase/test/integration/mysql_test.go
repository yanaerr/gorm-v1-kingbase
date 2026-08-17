package kingbase_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	kb "github.com/jinzhu/gorm/kingbase/dialects"
	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

type KBMySQLUser struct {
	gorm.Model
	Username string `gorm:"type:varchar(64);unique_index;not null"`
	Email    string `gorm:"type:varchar(128);index"`
	Age      int    `gorm:"index"`
	Score    int
	Status   string `gorm:"type:varchar(16);index"`
}

type KBMySQLSpecialType struct {
	ID        uint           `gorm:"primary_key"`
	Name      string         `gorm:"type:varchar(64);not null;unique_index"`
	Price     float64        `gorm:"type:decimal(10,2);not null"`
	Flag      string         `gorm:"type:char(1);not null"`
	Meta      string         `gorm:"type:json"`
	RawData   []byte         `gorm:"type:blob"`
	Note      sql.NullString `gorm:"type:text"`
	CreatedAt time.Time
}

type KBMySQLConstraintRow struct {
	ID         uint      `gorm:"primary_key"`
	TenantID   int       `gorm:"not null;unique_index:uix_tenant_code"`
	Code       string    `gorm:"type:varchar(32);not null;unique_index:uix_tenant_code;index"`
	Name       string    `gorm:"type:varchar(128);not null"`
	Amount     float64   `gorm:"type:decimal(12,4);not null"`
	Active     bool      `gorm:"not null;default:true"`
	OccurredAt time.Time `gorm:"type:datetime"`
}

func openKBMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.Open(t, kb.KbModeMySQL)
	fmt.Println("MySQL模式连接成功")
	db.LogMode(true)
	return db
}

func TestKingbaseMySQLMode_FullIntegration(t *testing.T) {
	t.Log("开始测试：MySQL模式完整集成测试（DDL、CRUD、事务、软删除、原生语法）")
	db := openKBMySQL(t)
	if db == nil {
		return
	}
	defer db.Close()

	table := "kb_mysql_users_full"
	_ = db.DropTable(table).Error
	defer db.DropTable(table)

	t.Run("DDL_AutoMigrate_Indexes", func(t *testing.T) {
		t.Log("当前测试：DDL 自动迁移与索引创建")
		if err := db.Table(table).AutoMigrate(&KBMySQLUser{}).Error; err != nil {
			t.Fatalf("自动迁移失败: %v", err)
		}
		if !db.HasTable(table) {
			t.Fatalf("表未创建成功: %s", table)
		}
		if !db.Dialect().HasColumn(table, "username") {
			t.Fatal("字段 username 未创建成功")
		}
		t.Log("成功：建表、基础字段与索引创建检查通过")
	})

	seed := []KBMySQLUser{
		{Username: "alice", Email: "a@test.com", Age: 18, Score: 90, Status: "active"},
		{Username: "bob", Email: "b@test.com", Age: 22, Score: 75, Status: "active"},
		{Username: "cindy", Email: "c@test.com", Age: 31, Score: 60, Status: "inactive"},
		{Username: "david", Email: "d@test.com", Age: 27, Score: 88, Status: "active"},
	}

	t.Run("CRUD_Create_Batch_Query", func(t *testing.T) {
		t.Log("当前测试：基础 CRUD（插入、统计、首条查询）")
		for i := range seed {
			if err := db.Table(table).Create(&seed[i]).Error; err != nil {
				t.Fatalf("插入数据失败: %v", err)
			}
		}

		var cnt int
		if err := db.Table(table).Where("status = ?", "active").Count(&cnt).Error; err != nil {
			t.Fatalf("统计数据失败: %v", err)
		}
		if cnt != 3 {
			t.Fatalf("期望 active 用户为 3，实际为 %d", cnt)
		}

		var one KBMySQLUser
		if err := db.Table(table).Where("username = ?", "alice").First(&one).Error; err != nil {
			t.Fatalf("查询首条数据失败: %v", err)
		}
		if one.Email != "a@test.com" {
			t.Fatalf("邮箱字段不符合预期: %s", one.Email)
		}
		t.Log("成功：基础 CRUD（插入、统计、首条查询）通过")
	})

	t.Run("Pagination_Order_Group_Having", func(t *testing.T) {
		t.Log("当前测试：分页、排序、分组与聚合查询")
		var list []KBMySQLUser
		if err := db.Table(table).
			Where("age >= ?", 18).
			Order("score desc").
			Limit(2).
			Offset(1).
			Find(&list).Error; err != nil {
			t.Fatalf("分页查询失败: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("期望分页结果 2 条，实际为 %d 条", len(list))
		}

		type Stat struct {
			Status string
			Cnt    int
			MaxAge int
		}
		var stats []Stat
		if err := db.Table(table).
			Select("status, COUNT(*) AS cnt, MAX(age) AS max_age").
			Group("status").
			Having("COUNT(*) >= ?", 1).
			Scan(&stats).Error; err != nil {
			t.Fatalf("分组聚合查询失败: %v", err)
		}
		if len(stats) == 0 {
			t.Fatal("分组聚合结果为空")
		}
		t.Log("成功：分页、排序、分组聚合查询通过")
	})

	t.Run("Update_Expr_And_Save", func(t *testing.T) {
		t.Log("当前测试：表达式更新与保存更新")
		if err := db.Table(table).Where("username = ?", "bob").
			Update("score", gorm.Expr("score + ?", 5)).Error; err != nil {
			t.Fatalf("表达式更新失败: %v", err)
		}

		var bob KBMySQLUser
		if err := db.Table(table).Where("username = ?", "bob").First(&bob).Error; err != nil {
			t.Fatalf("查询 bob 失败: %v", err)
		}
		if bob.Score != 80 {
			t.Fatalf("期望 bob 分数为 80，实际为 %d", bob.Score)
		}

		bob.Status = "inactive"
		if err := db.Table(table).Save(&bob).Error; err != nil {
			t.Fatalf("保存数据失败: %v", err)
		}
		t.Log("成功：表达式更新与保存更新通过")
	})

	t.Run("Transaction_Commit_And_Rollback", func(t *testing.T) {
		t.Log("当前测试：事务提交与回滚")
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatalf("开启事务失败: %v", tx.Error)
		}
		if err := tx.Table(table).Create(&KBMySQLUser{
			Username: "tx_commit", Email: "tx_commit@test.com", Age: 20, Score: 66, Status: "active",
		}).Error; err != nil {
			tx.Rollback()
			t.Fatalf("事务内插入数据失败: %v", err)
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatalf("事务提交失败: %v", err)
		}

		var c1 int
		_ = db.Table(table).Where("username = ?", "tx_commit").Count(&c1).Error
		if c1 != 1 {
			t.Fatalf("提交后数据不存在，count=%d", c1)
		}

		tx2 := db.Begin()
		if tx2.Error != nil {
			t.Fatalf("开启回滚事务失败: %v", tx2.Error)
		}
		if err := tx2.Table(table).Create(&KBMySQLUser{
			Username: "tx_rollback", Email: "tx_rollback@test.com", Age: 19, Score: 50, Status: "active",
		}).Error; err != nil {
			tx2.Rollback()
			t.Fatalf("回滚事务内插入数据失败: %v", err)
		}
		_ = tx2.Rollback().Error

		var c2 int
		_ = db.Table(table).Where("username = ?", "tx_rollback").Count(&c2).Error
		if c2 != 0 {
			t.Fatalf("回滚后数据不应存在，count=%d", c2)
		}
		t.Log("成功：事务提交与回滚校验通过")
	})

	t.Run("SoftDelete_And_Unscoped", func(t *testing.T) {
		t.Log("当前测试：软删除与 Unscoped 查询")
		var user KBMySQLUser
		if err := db.Table(table).Where("username = ?", "cindy").First(&user).Error; err != nil {
			t.Fatalf("查询 cindy 失败: %v", err)
		}
		if err := db.Table(table).Delete(&user).Error; err != nil {
			t.Fatalf("软删除失败: %v", err)
		}

		var normalCnt int
		_ = db.Table(table).Where("username = ? AND deleted_at IS NULL", "cindy").Count(&normalCnt).Error
		if normalCnt != 0 {
			t.Fatalf("软删除后记录应被隐藏，count=%d", normalCnt)
		}

		var unscopedCnt int
		_ = db.Table(table).Unscoped().Where("username = ?", "cindy").Count(&unscopedCnt).Error
		if unscopedCnt == 0 {
			t.Fatal("Unscoped 查询应包含软删除记录")
		}
		t.Log("成功：软删除与 Unscoped 查询校验通过")
	})

	t.Run("RawSQL_MySQLStyle_Normal_And_SpecialSyntax", func(t *testing.T) {
		t.Log("当前测试：原生 MySQL 常规与特殊语法 SQL")
		var rows []KBMySQLUser
		sql1 := fmt.Sprintf("SELECT `id`,`username`,`email`,`age`,`score`,`status`,`created_at`,`updated_at`,`deleted_at` FROM `%s` WHERE `age` >= ? ORDER BY `id` ASC LIMIT 2", table)
		if err := db.Raw(sql1, 18).Scan(&rows).Error; err != nil {
			t.Fatalf("原生 MySQL 常规语法 SQL 执行失败: %v", err)
		}
		if len(rows) == 0 {
			t.Fatal("原生 MySQL 常规语法 SQL 返回 0 行")
		}

		type FuncRow struct {
			Username string
			Nick     string
			DayStr   string
		}
		var out []FuncRow
		sql2 := fmt.Sprintf(
			"SELECT `username`, IFNULL(`email`, 'none') AS `nick`, DATE_FORMAT(`created_at`, '%%Y-%%m-%%d') AS `day_str` FROM `%s` WHERE `created_at` <= ? ORDER BY `id` ASC LIMIT 3",
			table,
		)
		if err := db.Raw(sql2, time.Now()).Scan(&out).Error; err != nil {
			t.Fatalf("原生 MySQL 特殊语法 SQL 执行失败: %v", err)
		}
		if len(out) == 0 {
			t.Fatal("原生 MySQL 特殊语法 SQL 返回 0 行")
		}
		t.Log("成功：原生 MySQL 常规与特殊语法 SQL 校验通过")
	})

	t.Log("成功：MySQL 模式完整集成测试全部通过")
}

func TestKingbaseMySQLMode_SpecialTypesAndFunctions(t *testing.T) {
	t.Log("开始测试：MySQL特性专项测试（特有类型、主键约束、函数兼容）")
	db := openKBMySQL(t)
	if db == nil {
		return
	}
	defer db.Close()

	table := "kb_mysql_special_types"
	_ = db.DropTable(table).Error
	defer db.DropTable(table)

	t.Run("MySQL特有数据类型测试", func(t *testing.T) {
		t.Log("当前测试：MySQL特有数据类型映射与读写")
		if err := db.Table(table).AutoMigrate(&KBMySQLSpecialType{}).Error; err != nil {
			t.Fatalf("特殊类型表自动迁移失败: %v", err)
		}

		row := KBMySQLSpecialType{
			Name:    fmt.Sprintf("row_%d", time.Now().UnixNano()),
			Price:   123.45,
			Flag:    "Y",
			Meta:    `{"k":"v","n":1}`,
			RawData: []byte{0x01, 0x02, 0x03},
			Note:    sql.NullString{String: "hello", Valid: true},
		}
		if err := db.Table(table).Create(&row).Error; err != nil {
			t.Fatalf("插入特殊类型数据失败: %v", err)
		}

		var got KBMySQLSpecialType
		if err := db.Table(table).Where("name = ?", row.Name).First(&got).Error; err != nil {
			t.Fatalf("查询特殊类型数据失败: %v", err)
		}
		if got.Name == "" || got.Price == 0 {
			t.Fatal("特殊类型数据读取异常")
		}
		t.Log("成功：MySQL特有数据类型测试通过")
	})

	t.Run("自增主键与特殊约束测试", func(t *testing.T) {
		t.Log("当前测试：自增主键与唯一约束")
		name := fmt.Sprintf("unique_%d", time.Now().UnixNano())
		first := KBMySQLSpecialType{Name: name, Price: 1.00, Flag: "Y", Meta: "{}"}
		if err := db.Table(table).Create(&first).Error; err != nil {
			t.Fatalf("首条插入失败: %v", err)
		}

		var inserted KBMySQLSpecialType
		if err := db.Table(table).Where("name = ?", name).First(&inserted).Error; err != nil {
			t.Fatalf("查询首条记录失败: %v", err)
		}
		if inserted.ID == 0 {
			t.Fatal("自增主键未生效，数据库中的 ID 仍为 0")
		}

		dup := KBMySQLSpecialType{Name: name, Price: 2.00, Flag: "N", Meta: "{}"}
		if err := db.Table(table).Create(&dup).Error; err == nil {
			t.Fatal("期望唯一约束生效，但重复数据插入未报错")
		}
		t.Log("成功：自增主键与唯一约束测试通过")
	})

	t.Run("MySQL函数兼容性测试", func(t *testing.T) {
		t.Log("当前测试：MySQL函数兼容性（CONCAT/LENGTH/SUBSTRING/IFNULL/DATE_FORMAT）")
		type FuncOut struct {
			FullName string
			LenName  int
			Prefix   string
			MetaFix  string
			DayStr   string
		}
		var out []FuncOut
		sqlFn := fmt.Sprintf(
			"SELECT CONCAT(`name`, '_x') AS `full_name`, LENGTH(`name`) AS `len_name`, SUBSTRING(`name`, 1, 3) AS `prefix`, IFNULL(`meta`, '{}') AS `meta_fix`, DATE_FORMAT(`created_at`, '%%Y-%%m-%%d') AS `day_str` FROM `%s` ORDER BY `id` ASC LIMIT 5",
			table,
		)
		if err := db.Raw(sqlFn).Scan(&out).Error; err != nil {
			t.Fatalf("MySQL函数兼容性SQL执行失败: %v", err)
		}
		if len(out) == 0 {
			t.Fatal("MySQL函数兼容性SQL返回 0 行")
		}
		t.Log("成功：MySQL函数兼容性测试通过")
	})
}

func TestKingbaseMySQLMode_AdvancedCompatibility(t *testing.T) {
	t.Log("开始测试：MySQL模式高级兼容测试（DDL变更、索引生命周期、约束冲突、函数语法）")
	db := openKBMySQL(t)
	if db == nil {
		return
	}
	defer db.Close()

	table := fmt.Sprintf("kb_mysql_adv_%d", time.Now().Unix())
	_ = db.DropTable(table).Error
	defer db.DropTable(table)

	t.Run("DDL变更与索引生命周期", func(t *testing.T) {
		t.Log("当前测试：建表、加索引、删索引、字段变更")
		if err := db.Table(table).AutoMigrate(&KBMySQLConstraintRow{}).Error; err != nil {
			t.Fatalf("高级表自动迁移失败: %v", err)
		}
		if !db.HasTable(table) {
			t.Fatalf("高级表未创建成功: %s", table)
		}

		if err := db.Table(table).AddIndex("idx_adv_name", "name").Error; err != nil {
			t.Fatalf("新增索引失败: %v", err)
		}
		if !db.Dialect().HasIndex(table, "idx_adv_name") {
			t.Fatal("索引 idx_adv_name 未生效")
		}
		if err := db.Table(table).RemoveIndex("idx_adv_name").Error; err != nil {
			t.Fatalf("删除索引失败: %v", err)
		}

		// 仅做语法回归：字段类型变更链路是否可执行
		if err := db.Table(table).ModifyColumn("name", "varchar(256)").Error; err != nil {
			t.Fatalf("字段变更失败: %v", err)
		}
		t.Log("成功：DDL变更与索引生命周期测试通过")
	})

	t.Run("批量数据与统计校验", func(t *testing.T) {
		t.Log("当前测试：批量写入、计数、聚合统计")
		rows := []KBMySQLConstraintRow{
			{TenantID: 1, Code: "A001", Name: "订单A", Amount: 12.3456, Active: true, OccurredAt: time.Now()},
			{TenantID: 1, Code: "A002", Name: "订单B", Amount: 88.8800, Active: true, OccurredAt: time.Now()},
			{TenantID: 2, Code: "A001", Name: "订单C", Amount: 30.5000, Active: false, OccurredAt: time.Now()},
		}
		for i := range rows {
			if err := db.Table(table).Create(&rows[i]).Error; err != nil {
				t.Fatalf("批量写入失败: %v", err)
			}
		}

		var total int
		if err := db.Table(table).Count(&total).Error; err != nil {
			t.Fatalf("总数统计失败: %v", err)
		}
		if total < 3 {
			t.Fatalf("期望至少 3 条记录，实际 %d", total)
		}

		type Agg struct {
			TenantID int
			Cnt      int
			SumAmt   float64
		}
		var aggs []Agg
		if err := db.Table(table).
			Select("tenant_id, COUNT(*) AS cnt, SUM(amount) AS sum_amt").
			Group("tenant_id").
			Order("tenant_id asc").
			Scan(&aggs).Error; err != nil {
			t.Fatalf("聚合统计失败: %v", err)
		}
		if len(aggs) == 0 {
			t.Fatal("聚合统计结果为空")
		}
		t.Log("成功：批量数据与统计校验通过")
	})

	t.Run("复合唯一约束冲突测试", func(t *testing.T) {
		t.Log("当前测试：复合唯一键（tenant_id + code）冲突")
		okRow := KBMySQLConstraintRow{
			TenantID: 9, Code: "DUP1", Name: "基准记录", Amount: 1.23, Active: true, OccurredAt: time.Now(),
		}
		if err := db.Table(table).Create(&okRow).Error; err != nil {
			t.Fatalf("插入基准记录失败: %v", err)
		}

		dup := KBMySQLConstraintRow{
			TenantID: 9, Code: "DUP1", Name: "重复记录", Amount: 9.99, Active: true, OccurredAt: time.Now(),
		}
		if err := db.Table(table).Create(&dup).Error; err == nil {
			t.Fatal("期望复合唯一键冲突报错，但插入成功")
		}
		t.Log("成功：复合唯一约束冲突测试通过")
	})

	t.Run("更多MySQL函数兼容性", func(t *testing.T) {
		t.Log("当前测试：CONCAT_WS/ROUND/CAST/IFNULL/DATE_FORMAT")
		type FnRow struct {
			KeyText string
			Amt2    float64
			FlagNum int
			DayStr  string
		}
		var out []FnRow
		sqlFn := fmt.Sprintf(
			"SELECT CONCAT_WS('-', `tenant_id`, `code`) AS `key_text`, ROUND(`amount`, 2) AS `amt2`, CAST(IFNULL(`active`, false) AS int) AS `flag_num`, DATE_FORMAT(`occurred_at`, '%%Y-%%m-%%d') AS `day_str` FROM `%s` ORDER BY `id` ASC LIMIT 10",
			table,
		)
		if err := db.Raw(sqlFn).Scan(&out).Error; err != nil {
			t.Fatalf("更多函数兼容性 SQL 执行失败: %v", err)
		}
		if len(out) == 0 {
			t.Fatal("更多函数兼容性 SQL 返回 0 行")
		}
		t.Log("成功：更多MySQL函数兼容性测试通过")
	})

	t.Log("成功：MySQL模式高级兼容测试全部通过")
}
