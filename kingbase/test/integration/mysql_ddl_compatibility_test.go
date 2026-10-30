package kingbase_test

import (
	"testing"

	"github.com/jinzhu/gorm/kingbase/test/testutil"
)

// Raw MySQL input stays unchanged. A rejection is a compatibility failure,
// not an expected error, and must never trigger a Kingbase-SQL retry.
func TestKingbaseMySQLMode_MySQLDDLCompatibility(t *testing.T) {
	runMySQLDDLCases(t, "原始 MySQL SQL 兼容性：SQL 原样执行，不计 ORM 适配通过", []mysqlDDLTestCase{
		{"01_DropIndexOnTable", "原始 MySQL：DROP INDEX ON 表", mysqlDDLMySQLDropIndex},
		{"06_RenameTable", "原始 MySQL：RENAME TABLE", mysqlDDLRenameTable},
		{"07_ChangeColumn", "原始 MySQL：CHANGE COLUMN", mysqlDDLRenameColumn},
		{"08_DropCheckConstraint", "原始 MySQL：DROP CHECK", mysqlDDLCheckConstraint},
		{"10_DropForeignKey", "原始 MySQL：DROP FOREIGN KEY", mysqlDDLMySQLDropForeignKey},
		{"11_Truncate", "原始 MySQL：TRUNCATE 后自增重置", mysqlDDLMySQLTruncate},
	})
}

func mysqlDDLMySQLDropIndex(c *mysqlDDLCase) {
	table := c.table("id INTEGER PRIMARY KEY, name VARCHAR(32)")
	index := "idx_" + table
	c.prepare("CREATE INDEX " + c.quote(index) + " ON " + c.quote(table) + " (name)")
	c.indexCount("删除前索引存在", table, index, 1)
	c.statement("原始 MySQL 删除索引", "DROP INDEX "+c.quote(index)+" ON "+c.quote(table))
	c.indexCount("删除后索引不存在", table, index, 0)
}

func mysqlDDLRenameTable(c *mysqlDDLCase) {
	table := c.table("id INTEGER PRIMARY KEY, name VARCHAR(32)")
	c.prepare("INSERT INTO " + c.quote(table) + " VALUES (1, 'KeepCase')")
	renamed := table + "_new"
	c.cleanup = append(c.cleanup, renamed)
	if !c.statement("表重命名", "RENAME TABLE "+c.quote(table)+" TO "+c.quote(renamed)) {
		return
	}
	query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?"
	c.count("旧表名不存在", query, 0, c.schema, table)
	c.count("新表名存在", query, 1, c.schema, renamed)
	c.text("重命名后数据保留", "SELECT name FROM "+c.quote(renamed)+" WHERE id = 1", "KeepCase")
}

func mysqlDDLRenameColumn(c *mysqlDDLCase) {
	table := c.table("id INTEGER PRIMARY KEY, old_name VARCHAR(32)")
	c.prepare("INSERT INTO " + c.quote(table) + " VALUES (1, 'KeepCase')")
	if !c.statement("列重命名", "ALTER TABLE "+c.quote(table)+" CHANGE COLUMN old_name new_name VARCHAR(32)") {
		return
	}
	c.columnCount("旧列名不存在", table, "old_name", 0)
	c.columnCount("新列名存在", table, "new_name", 1)
	c.text("列重命名后数据保留", "SELECT new_name FROM "+c.quote(table)+" WHERE id = 1", "KeepCase")
}

func mysqlDDLCheckConstraint(c *mysqlDDLCase) {
	table := c.table("id INTEGER PRIMARY KEY, amount INTEGER")
	constraint := "ck_" + table
	if !c.statement("增加 CHECK 约束", "ALTER TABLE "+c.quote(table)+" ADD CONSTRAINT "+c.quote(constraint)+" CHECK (amount >= 0)") {
		return
	}
	c.constraintCount("CHECK 约束存在", table, constraint, 1)
	c.prepare("INSERT INTO " + c.quote(table) + " VALUES (1, 7)")
	rejected := c.reject("CHECK 拒绝负数", "23514", table, constraint, "", "INSERT INTO "+c.quote(table)+" VALUES (2, -1)")
	c.count("CHECK 拒绝后没有写入记录", "SELECT COUNT(*) FROM "+c.quote(table)+" WHERE id = 2", 0)
	if !rejected {
		c.t.Error("[未通过] CHECK 探测没有返回要求的约束冲突错误")
		return
	}
	if !c.statement("删除 CHECK 约束", "ALTER TABLE "+c.quote(table)+" DROP CHECK "+c.quote(constraint)) {
		return
	}
	c.constraintCount("CHECK 约束已删除", table, constraint, 0)
	c.prepare("INSERT INTO " + c.quote(table) + " VALUES (2, -1)")
	c.count("删除约束后负数可写入", "SELECT COUNT(*) FROM "+c.quote(table)+" WHERE id = 2 AND amount = -1", 1)
}

func mysqlDDLMySQLDropForeignKey(c *mysqlDDLCase) {
	parent := c.table("id INTEGER PRIMARY KEY")
	child := c.table("id INTEGER PRIMARY KEY, parent_id INTEGER")
	destination := c.quote(parent) + "(id)"
	constraint := c.db.Dialect().BuildKeyName(child, "parent_id", destination, "foreign")
	if err := c.db.Table(child).AddForeignKey("parent_id", destination, "RESTRICT", "RESTRICT").Error; err != nil {
		c.t.Fatalf("[准备失败] 创建待删除的外键: %v", err)
	}
	c.constraintCount("删除前外键存在", child, constraint, 1)
	c.statement("原始 MySQL 删除外键", "ALTER TABLE "+c.quote(child)+" DROP FOREIGN KEY "+c.quote(constraint))
	c.constraintCount("删除后外键不存在", child, constraint, 0)
}

type mysqlDDLTruncateRow struct {
	ID   uint   `gorm:"primary_key"`
	Name string `gorm:"type:varchar(32)"`
}

func mysqlDDLMySQLTruncate(c *mysqlDDLCase) {
	table := testutil.UniqueTable("kb_ddl")
	c.cleanup = append(c.cleanup, table)
	if err := c.db.Table(table).AutoMigrate(&mysqlDDLTruncateRow{}).Error; err != nil {
		c.t.Fatalf("[准备失败] ORM 创建自增表: %v", err)
	}
	c.prepare("INSERT INTO " + c.quote(table) + " (name) VALUES ('first'), ('second')")
	c.count("清空前两条记录", "SELECT COUNT(*) FROM "+c.quote(table), 2)
	// Keep the MySQL statement as supplied. Do not add RESTART IDENTITY here.
	if !c.statement("原始 MySQL 清空表", "TRUNCATE TABLE "+c.quote(table)) {
		return
	}
	c.count("清空后零条记录", "SELECT COUNT(*) FROM "+c.quote(table), 0)
	c.prepare("INSERT INTO " + c.quote(table) + " (name) VALUES ('after')")
	var after int64
	if err := c.db.CommonDB().QueryRow("SELECT id FROM " + c.quote(table) + " WHERE name = 'after'").Scan(&after); err != nil {
		c.t.Fatalf("[未通过] 读取 TRUNCATE 后自增值: %v", err)
	}
	if after != 1 {
		c.t.Errorf("[未通过] 原始 MySQL TRUNCATE 的自增重置语义：实际=%d；预期=1；SQL 未追加金仓选项", after)
	} else {
		c.t.Log("[状态通过] 原始 TRUNCATE 后自增值重新从 1 开始")
	}
}
