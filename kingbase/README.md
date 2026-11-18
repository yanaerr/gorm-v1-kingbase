# GORM V1 KingbaseES 适配说明

## 1. 适配边界

本项目为 GORM V1 提供 KingbaseES 方言：

- 应用统一使用 `kingbase` 方言。方言在连接初始化时执行 `SHOW database_mode`，并根据结果加载 MySQL 或 Oracle 模式实现。
- 原有 `kingbase-mysql` 和 `kingbase-oracle` 名称继续保留，供已有应用兼容使用。
- 模式查询失败或返回未知值时不加载任一模式方言，不根据连接失败情况猜测或切换模式。
- `gokb` 必须使用目标数据库版本安装介质中配套的驱动。
- 模式识别由统一方言在连接初始化阶段完成，应用不需要再次查询或手工选择模式。

当前测试基线：

| 模式 | 数据库版本 | 测试 modfile |
| --- | --- | --- |
| MySQL | KingbaseES `V009R003C018` | `go.mysql.mod` |
| Oracle | KingbaseES `V009R002C016` | `go.oracle.mod` |

两个 modfile 分别引用对应版本的本地 `gokb`，不得把某一个驱动永久写成两个模式共用。根 `go.mod/go.sum` 不用于切换测试驱动。

## 2. 使用方式

```go
import (
    "github.com/jinzhu/gorm"
    kb "github.com/jinzhu/gorm/kingbase/dialects"
    _ "kingbase.com/gokb"
)

db, err := gorm.Open(
    kb.DialectKingbase,
    "host=127.0.0.1 port=54331 user=system password=*** dbname=test sslmode=disable get_last_insert_id=yes",
)
```

MySQL 模式必须设置 `get_last_insert_id=yes`。普通 `Create` 沿用 GORM V1 原始回调；如果配套驱动因自增主键不是表的第一列而未返回 ID，Kingbase 回调会在同一事务中执行 `SELECT last_insert_id()` 完成回填。

`Set("gorm:insert_modifier", "IGNORE").Create(...)` 使用单独的 Kingbase MySQL Create 路径，因为当前配套 `gokb` 的 `INSERT IGNORE` 不能通过 `LastInsertId` 获取生成值。该路径保留 GORM 的事务、Hook、关联、默认值及 `RowsAffected` 语义。

## 3. MySQL R3 已适配内容

- 实现 GORM V1 `Dialect` 接口的全部方法。
- 反引号和内部反引号转义、`?` 参数绑定、schema 限定名称。
- 直接使用 R3 原生 `TINYINT/SMALLINT/MEDIUMINT/INT/BIGINT UNSIGNED` 与 `AUTO_INCREMENT`。
- 不再将无符号类型扩大为有符号类型，不再生成适配层 CHECK，也不再使用 `serial/bigserial` 代替 R3 原生类型。
- 通过 `information_schema` 和 `pg_indexes` 检查表、列、索引和外键。
- `RemoveIndex` 生成 R3 支持的 `DROP INDEX 索引名`，不生成 MySQL 的 `ON 表名` 后缀。
- `ModifyColumn` 生成原生 `ALTER TABLE ... MODIFY COLUMN ...`。
- 支持 `LIMIT`、`OFFSET` 及单独 `OFFSET`。
- 根据实例 `max_identifier_length` 缩短自动生成的索引和约束名。
- 支持索引前缀、特殊索引名和 schema 限定索引。
- 修复 `Group/Having/Count` 与软删除组合产生的错误计数 SQL。
- 修复普通 `Create` 下非第一列自增主键不回填。
- 修复 `INSERT IGNORE` 与配套 `gokb` 自动追加 `RETURNING` 的冲突。

## 4. 已知限制

以下内容不属于当前 GORM V1 方言适配范围：

- `db.Exec` 提交的原始 SQL不会经过方言改写。原始 `DROP INDEX ... ON 表`、`DROP CHECK`、`DROP FOREIGN KEY` 应按 R3 实际语法修改。
- GORM V1 没有 CHECK 约束迁移接口，因此不增加私有 `DropCheck` 扩展。
- `LastInsertId` 的 Go 返回类型是 `int64`；超过 `MaxInt64` 的完整 `uint64` 自增回填不在当前适配范围内。非自增 `BIGINT UNSIGNED` 可按 R3 原生范围读写。
- Oracle R2 的原生 Identity 使用有符号序列，`uint64` 自增主键只保证到 `MaxInt64`；不增加偏移序列、范围约束等私有完整范围扩展。非自增 `NUMBER(20)` 可手动读写完整 `uint64` 范围。
- `AutoMigrate` 按 GORM V1 原生行为逐条执行 DDL，不保证整组迁移原子提交；失败时此前成功的 DDL 可能保留，生产迁移应使用独立迁移脚本或专用迁移工具。
- Oracle R2 索引迁移沿用 GORM V1 原生语义：`HasIndex` 判断后执行创建或删除，不增加创建后的索引定义反查和严格校验。
- R3 的 ENUM 成员大小写规则可能不接受仅大小写不同的成员，应在需求文档中记录。
- GORM V1 原生 MySQL 对“非主键 AUTO_INCREMENT 并依赖建表后再创建索引”的模型存在建表顺序边界，不在 Kingbase 方言中增加私有迁移扩展。
- GORM V1 原生复杂回调排序等框架行为不通过修改 GORM 主干处理。

## 5. Oracle R2 适配

Oracle 模式使用独立方言，主要覆盖：

- 双引号标识符和 schema 限定名称。
- `varchar2/clob/blob/number/timestamp/identity` 等类型。
- `FETCH FIRST`、`OFFSET ... FETCH NEXT` 分页。
- `ALTER TABLE ... MODIFY ...`。
- `information_schema/pg_indexes` 元数据查询。
- `DUAL` 和 `RETURNING` 主键回填。

Oracle R2 的 `uint64` 边界与 GORM V1 原生方言保持一致：非自增字段映射为 `NUMBER(20)`，可手动写入完整范围；自增主键使用原生 `BIGINT IDENTITY`，只保证到 `MaxInt64`。不增加偏移序列、范围约束等完整 `uint64` 自动分配扩展。

字段注释迁移使用 `NewOracleMigrator`；该扩展只属于 Oracle 模式：

```go
kb.NewOracleMigrator(db).AutoMigrate(&Account{}).Error
```

## 6. 测试

离线单元测试：

```bash
go test -modfile=go.mysql.mod -mod=readonly ./kingbase/test/unit ./kingbase/test/testutil
go test -modfile=go.mysql.mod -mod=readonly -race ./kingbase/test/unit
go vet -modfile=go.mysql.mod ./kingbase/dialects ./kingbase/test/unit ./kingbase/test/integration
```

MySQL R3 真实库测试：

```bash
export KINGBASE_MYSQL_DSN='host=127.0.0.1 port=54331 user=system password=*** dbname=test sslmode=disable get_last_insert_id=yes'
export REQUIRE_KINGBASE_DB=1

go test -modfile=go.mysql.mod -mod=readonly -v -count=1 \
  ./kingbase/test/integration \
  -run '^TestKingbaseMySQLMode_FullIntegration$'

go test -modfile=go.mysql.mod -mod=readonly -v -count=1 \
  ./kingbase/test/integration \
  -run '^TestKingbaseMySQLMode_NormalCreateNonFirstPrimaryKey$'
```

`testutil.Open` 在未设置 `REQUIRE_KINGBASE_DB=1` 时允许连接失败后跳过，因此正式验收必须设置该变量。MySQL DSN 未显式设置 `get_last_insert_id` 时，测试入口会自动补上 `yes`。

原始 MySQL SQL 能力探针和纯服务器参考探针不属于 ORM 适配验收，已从默认测试目录移除。语法差异应在需求或限制文档中记录，不用故意失败的测试阻断正常回归。

## 7. 目录职责

```text
kingbase/
├── dialects/              # KingbaseES 方言实现
└── test/
    ├── unit/              # SQL Mock 与公开接口测试
    ├── integration/       # 真实数据库功能与适配验收
    ├── regression/        # GORM V1 行为回归
    ├── negative/          # 错误传播与异常场景
    ├── nonfunctional/     # 并发、性能和稳定性
    └── testutil/          # 测试连接、日志和公共辅助
```

生产适配文件：

| 文件 | 职责 |
| --- | --- |
| `dialect.go` | 模式常量、方言注册、构造入口和名称解析 |
| `dialect_mysql.go` | MySQL `Dialect` 方法、分页、DDL 和自动名称 |
| `dialect_mysql_types.go` | 复用 GORM MySQL 类型并保留 R3 原生定义 |
| `dialect_mysql_metadata.go` | MySQL 模式元数据查询 |
| `dialect_mysql_callbacks.go` | 普通 Create 主键兜底及 `INSERT IGNORE` |
| `dialect_callbacks.go` | 两个模式共用的分组计数修补 |
| `dialect_oracle.go` | Oracle 方言接口、类型、分页和 DDL |
| `dialect_oracle_metadata.go` | Oracle 模式元数据查询 |
| `oracle_migration.go` | Oracle 专项迁移入口 |
