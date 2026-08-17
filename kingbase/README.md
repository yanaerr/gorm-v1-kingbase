# GORM V1 KingbaseES 适配说明

## 1. 方案边界

本项目采用“一个 GORM V1 主干 + 一个 Kingbase 方言包 + 显式模式选择”的方式适配 KingbaseES，不复制 GORM 主干，也不在运行期自动切换 ORM 方言。

应用只依赖 GORM V1 接口；`kingbase/dialects` 负责类型映射、标识符、分页、DDL 和系统表查询；`gokb` 负责连接协议、参数绑定和结果解码。

模式必须与数据库实例的 `SHOW database_mode` 结果一致：

| 数据库模式 | GORM 方言名 | 适配基线 |
| --- | --- | --- |
| MySQL | `kingbase-mysql` | GORM MySQL 方言 + Kingbase 覆盖 |
| Oracle | `kingbase-oracle` | 独立的 Kingbase Oracle 方言 |

本期的验收重点是 MySQL 和 Oracle 两种模式。

## 2. 已确认的环境基线

| 项目 | 当前基线 | 说明 |
| --- | --- | --- |
| Go | 1.22.5 | 当前适配与测试使用的 Go 环境；`go.mod` 仍保留 `go 1.12` 兼容声明。 |
| GORM | GORM V1 `v1.9.16` + 本项目补丁 | 模块为 `github.com/jinzhu/gorm`；主干复用官方 V1，另包含连接回调隔离、查询选项等本项目已有改动。 |
| KingbaseES | V9R1C10 | 最终验收应使用相同版本、补丁级别、操作系统和 `gokb` 驱动版本。 |

## 3. 使用方式

```go
import (
    "github.com/jinzhu/gorm"
    kb "github.com/jinzhu/gorm/kingbase/dialects"
    _ "kingbase.com/gokb"
)

db, err := gorm.Open(
    kb.DialectKingbaseOracle, // MySQL 模式改为 DialectKingbaseMySQL
    "kingbase",
    "host=127.0.0.1 port=54321 user=system password=*** dbname=test sslmode=disable",
)
```

不要根据连接成功与否自动回退到另一种方言。启动检查应执行 `SHOW database_mode`，发现模式不一致时直接终止启动。

## 4. 已覆盖的兼容点

### MySQL 模式

- 反引号、`?` 参数和 MySQL 分页沿用 GORM MySQL 方言。
- 将 `auto_increment` 转换为 Kingbase 可执行的 `serial/bigserial`。
- 转换 unsigned、text/blob 等常用类型。
- 通过 `information_schema` 和 `pg_indexes` 实现表、列、索引、外键检查。

### Oracle 模式

- 完整实现 GORM V1 `Dialect` 接口，不代理其他 GORM 方言行为。
- `?` 参数由 `gokb` 转换为 KingbaseES 的线级绑定占位符。
- 使用双引号并转义标识符，支持 schema 限定表名。
- 映射 `varchar2/clob/blob/number/timestamp/identity` 等常用类型。
- 分页生成 `FETCH FIRST` 或 `OFFSET ... FETCH NEXT`。
- 字段修改生成 `ALTER TABLE ... MODIFY ...`。
- 无 schema 时优先查询 `USER_*` 视图；显式 schema 时查询 `ALL_*` 视图；视图不可用时回退到 Kingbase 原生目录。
- `SELECT` 常量场景使用 `DUAL`；插入主键回填沿用 Kingbase 支持的 `RETURNING` 链路。

Oracle 兼容能力受 KingbaseES 版本和实例配置影响。分页、identity、布尔类型、`MODIFY` 和 `RETURNING` 必须在甲方目标版本执行集成测试，不能只依据 SQL Mock 验收。

## 5. 测试与验收

单元和竞态测试：

```bash
go test ./kingbase/dialects
go test -race ./kingbase/dialects
go vet ./kingbase/dialects ./kingbase/test/...
```

真实库测试通过环境变量连接；Oracle 优先读取 `KINGBASE_ORACLE_DSN`，MySQL 使用 `KINGBASE_DSN`：

```bash
KINGBASE_ORACLE_DSN='host=... port=... user=... password=... dbname=... sslmode=disable' \
go test -v ./kingbase/test/integration -run Oracle

KINGBASE_DSN='host=... port=... user=... password=... dbname=... sslmode=disable' \
go test -v ./kingbase/test/integration -run MySQL
```

真实库用例覆盖连接与模式校验、AutoMigrate 建表/增列、USER_*/ALL_* 元数据、索引创建/删除、唯一约束、字段修改、CRUD、分页、软删除/恢复/硬删除、事务提交/回滚、外键级联、CLOB/BLOB/NULL/时区时间和 Oracle `DUAL`。

### 5.1 各模式测试覆盖

| 模式 | 测试文件 | 已覆盖场景 | 验收状态 |
| --- | --- | --- | --- |
| MySQL | `kingbase/test/integration/mysql_test.go` | `SHOW database_mode` 校验、AutoMigrate 建表/增列、字段和索引检查、CRUD、分页/聚合、表达式更新、事务、软删除、MySQL 风格原生 SQL、类型映射、自增、唯一约束、索引生命周期、字段修改、常用函数。 | 本期重点；须在 V9R1C10 MySQL 模式实库执行。 |
| Oracle | `kingbase/test/integration/oracle_test.go` | `SHOW database_mode` 校验、AutoMigrate 建表/增列、USER_*/ALL_* 与 schema 元数据、CRUD/主键回填、分页、索引生命周期、字段修改、事务、软删除/恢复/硬删除、外键级联、CLOB/BLOB/NULL/时区时间、`DUAL`。 | 本期重点；须在 V9R1C10 Oracle 模式实库执行。 |

方言单元测试位于 `kingbase/dialects/dialect_test.go`，使用 SQL Mock 覆盖 MySQL/Oracle 类型映射、Oracle 标识符、分页、`RETURNING` 主键回填、`ModifyColumn`、元数据 SQL 及目录回退逻辑，以及方言连接隔离。

真实库测试依赖可用 DSN；连接失败时部分用例会跳过。因此只有在目标 V9R1C10 实例上执行并保存结果后，才能认定为完成该模式验收。

完整的目录、回归、非功能和异常测试方案见 [GORM V1 KingbaseES 测试文档](GORM_V1_KingbaseES测试文档.docx)。

## 6. 发布门禁

- 将 `go.mod` 中指向 `/home/wang/go/src/kingbase.com/gokb` 的本机 `replace` 改为甲方可访问的正式模块版本或内部制品地址。
- 固定 GORM、`gokb` 和 KingbaseES 小版本，保存兼容矩阵及失败 SQL。
- 分别在 MySQL、Oracle 模式实例运行集成测试。
- 对业务实际使用的原生 SQL、函数、默认值、序列和批量写入另建回归清单。

系统上下文图位于 `kingbase/diagram/gorm-v1-kingbase-system-context.vsdx`。
