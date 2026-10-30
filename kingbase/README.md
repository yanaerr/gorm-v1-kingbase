# GORM V1 KingbaseES 适配说明

## 1. 方案边界

本项目采用“一个 GORM V1 主干 + 一个 Kingbase 方言包 + 显式模式选择”的方式适配 KingbaseES，不复制 GORM 主干，也不在运行期自动切换 ORM 方言。

常规 CRUD 使用 GORM V1 接口；Oracle 专项迁移使用 `NewOracleMigrator`。`kingbase/dialects` 负责类型映射、标识符、分页、DDL、MySQL Create 回调和系统表查询；`gokb` 负责连接协议、参数绑定和结果解码。

模式必须与数据库实例的 `SHOW database_mode` 结果一致：

| 数据库模式 | GORM 方言名 | 适配基线 |
| --- | --- | --- |
| MySQL | `kingbase-mysql` | GORM MySQL 方言 + Kingbase 覆盖 |
| Oracle | `kingbase-oracle` | 独立的 Kingbase Oracle 方言 |

本期的验收重点是 MySQL 和 Oracle 两种模式。

2026-10-27 的同版本双模式回归、剩余失败及交付条件集中见[双模式验收记录](docs/dual-mode-acceptance-2026-10-27.md)。下文带日期的旧统计保留为历史记录。

后续 [MySQL 完整 UINT64 显式迁移扩展](docs/mysql-uint64-migration-2026-10-27.md)已补齐全范围生成与回填；使用 `kb.AutoMigrateMySQLUint64(db, &Model{})`，迁移后沿用原 Create/Find。标准 `db.AutoMigrate` 的全范围限制继续保留，扩展结果不覆盖旧入口。最新同版本回归及使用条件见该扩展报告。

## 2. 已确认的环境基线

| 项目 | 当前基线 | 说明 |
| --- | --- | --- |
| Go | 1.22.5 | 当前适配与测试使用的 Go 环境；`go.mod` 仍保留 `go 1.12` 兼容声明。 |
| GORM | 官方 GORM V1 源码 | 模块为 `github.com/jinzhu/gorm`；以下载源码的具体分支/提交为准。官方 `master` 与 `v1.9.16` 标签存在差异，不能据此认定修改过官方源码。 |
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
    "host=127.0.0.1 port=54324 user=system password=*** dbname=test sslmode=disable",
)
```

MySQL 兼容模式需要在 DSN 中设置 `get_last_insert_id=yes`，以便 gokb 通过 `LastInsertId` 回填自增主键；Oracle 模式使用 `RETURNING` 链路。

MySQL 模式的 `Set("gorm:insert_modifier", "IGNORE").Create(...)` 由方言包现有的 Create 回调处理，连接仍使用 `gorm.Open(kb.DialectKingbaseMySQL, "kingbase", dsn)`。回调保留原生 `INSERT IGNORE`，避免驱动追加当前实例不接受的 `RETURNING`；实际插入成功时从当前事务读取主键，重复行返回 `RowsAffected=0`。该修补要求保留 GORM 的正常 Create 事务；`Raw/Exec` 不经过 Create 回调，原始 SQL 能力探测仍按数据库实际结果判断。

编写需求、设计或验收文档时，应明确此处的驱动是 `kingbase.com/gokb`，冲突来自当前驱动自动追加的 `RETURNING *` 与当前实例的 `INSERT IGNORE` 组合。[驱动行为专项说明与可直接引用的文案](docs/mysql-followup-adaptation-2026-10-26.md#gokb-returning-驱动行为专项说明)列出了触发条件、实际 SQL、回调修补、版本依赖与验收证据。

两个模式均通过共享 RowQuery 回调修补 `Group/Having/Count` 与软删除组合：沿用 GORM 已构造的内层查询及参数，避免再次拼接外层模型过滤条件。普通查询和其他方言继续走原回调。

不要根据连接成功与否自动回退到另一种方言。启动检查应执行 `SHOW database_mode`，发现模式不一致时直接终止启动。

## 4. 已覆盖的兼容点

### MySQL 模式

- 反引号、`?` 参数和 MySQL 分页沿用 GORM MySQL 方言。
- 有符号整数保留原生 `AUTO_INCREMENT`；无符号回退列保留 `serial/bigserial` 与范围 CHECK，并通过 Kingbase Create 回调补齐数值 NULL/0 自增。模式、并发、回滚及原失败项均已验收；Raw/Exec 不转换，标准迁移入口的 UINT64 自增全范围仍有限制。见 [自增适配与限制](docs/mysql-autoincrement-adaptation-2026-10-25.md)。
- 完整 UINT64 自增使用 `kb.AutoMigrateMySQLUint64(db, &Model{})` 显式迁移：原生 uint8 存储、拥有序列的精确默认值及同事务回填。16 项专项通过；需要正常 Create 事务、步长/偏移均为 1，支持 INSERT/IGNORE。原 AutoMigrate、Raw/Exec、非空 insert_option 等边界见[扩展报告](docs/mysql-uint64-migration-2026-10-27.md)。
- 转换 unsigned、text/blob 等常用类型。
- 通过 `information_schema` 和 `pg_indexes` 实现表、列、索引、外键检查。
- `RemoveIndex` 在 Kingbase 适配层生成不带 `ON 表名` 的删除语句，支持按 schema 限定索引名；离线与真实 ORM 删除后状态验收均通过。

### Oracle 模式

- 完整实现 GORM V1 `Dialect` 接口，不代理其他 GORM 方言行为。
- `?` 参数由 `gokb` 转换为 KingbaseES 的线级绑定占位符。
- 使用双引号并转义标识符，支持 schema 限定表名。
- 映射 `varchar2/clob/blob/number/timestamp/identity` 等常用类型；普通整数和 identity 使用一致的 `integer/bigint` 基类型，保留原生 `FLOAT(p)`、`JSON/JSONB` 及字面量内容。
- 分页生成 `FETCH FIRST` 或 `OFFSET ... FETCH NEXT`。
- 字段修改生成 `ALTER TABLE ... MODIFY ...`。
- 元数据直接查询 Kingbase 支持的 `information_schema/pg_indexes`，无 schema 时限定 `current_schema()`；查询错误向观测器报告，不回退重试来掩盖错误。
- 索引删除正确引用 schema 与索引名；超长自动约束名按实例字节上限缩短并保留完整名称的散列。
- `SELECT` 常量场景使用 `DUAL`；插入主键回填沿用 Kingbase 支持的 `RETURNING` 链路。

特殊索引名、可靠的删除索引错误、列注释和完整 `uint64` 主键自增使用 Oracle 迁移扩展：

```go
kb.NewOracleMigrator(db).AutoMigrate(&Account{}).Error
kb.NewOracleMigrator(db.Model(&Account{})).AddIndex("Idx-Account.Name", "name").Error
kb.NewOracleMigrator(db.Model(&Account{})).RemoveIndex("Idx-Account.Name").Error
```

应检查每次调用的 `.Error`。迁移完成后的 `Create/Find` 等 DML 沿用 GORM。扩展使用 NUMBER(20) 和数据库序列默认值覆盖 `uint64` 的正整数自增范围；原始 GORM v1 的迁移方法不会自动获得这些能力。原理、8 项专项和 29 个叶子实库 race 结果见[Oracle 迁移扩展说明](docs/oracle-migration-extensions-2026-10-26.md)。

Oracle 兼容能力受 KingbaseES 版本和实例配置影响。分页、identity、布尔类型、`MODIFY` 和 `RETURNING` 必须在甲方目标版本执行集成测试，不能只依据 SQL Mock 验收。

## 5. 测试与验收

MySQL 本轮工作、当前适配范围、逐项实测结果、金仓差异及需求补充条款，集中见 [2026-10-24—25 MySQL 适配总结](docs/mysql-adaptation-summary-2026-10-24-25.md)。 UINT64 自增全范围与 ENUM 大小写成员冲突的状态、影响及证据见[两项已知限制](docs/mysql-known-limitations-uint64-enum-2026-10-25.md)。

后续[主要集成与公共回归验收](docs/mysql-acceptance-results-2026-10-25.md)已完成：主要集成 14 项、事务 15 个 MySQL 主分支通过；公共回归 219 PASS、4 FAIL、1 SKIP，新增失败分别涉及 FLOAT(M,D)、INSERT IGNORE/RETURNING、分组计数 SQL 和复杂回调。不能仅凭原 UINT64/ENUM 两项限制判断其余均已适配。

2026-10-26 [MySQL 后续修补](docs/mysql-followup-adaptation-2026-10-26.md)通过方言包 Create 回调修复并单项复测了 `TestCreateIgnore`；上句 219/4/1 是 2026-10-25 整批历史数据，并非修补后的整批重跑统计。FLOAT(M,D)、完整 UINT64 自增、ENUM 大小写成员及两项 GORM 核心回归仍未改判。

2026-10-27 [MySQL 四项修补及剩余限制](docs/mysql-repair-results-2026-10-27.md)进一步修复分组计数与软删除、无符号 IGNORE 主键回填、引用索引名中的点、AutoMigrate 特殊索引标签。隔离实例验收的 62 个集成叶子为 60 PASS、2 FAIL，失败仍为 UINT64 自增和 ENUM；原公共回归 TestCountWithHaving、TestCreateIgnore 单项通过，FLOAT 与复杂回调仍失败。未重跑全部公共回归；原始 AddIndex/RemoveIndex 和元数据 bool 接口的边界另见该报告。

Oracle 本轮按官方 V9 SQL 参考修补并实测，见[Oracle 适配修补与验收记录](docs/oracle-adaptation-results-2026-10-25.md)：集成 21 个叶子用例通过（含新增适配边界 7 项），事务 16 个叶子用例通过；公共回归 218 PASS、2 FAIL、4 SKIP。两项 FAIL 为当前 GORM v1 的分组计数 SQL 和复杂回调问题；跳过项和未覆盖项不计入兼容通过。[Oracle 适配总结与需求补充项](docs/oracle-adaptation-summary-2026-10-25.md)汇总了全部已测范围、金仓特性和限制，包括当前实例双引号纯大写表名、列名落库变小写的问题。

2026-10-26 对照[金仓 V9R1C10 Oracle 官方文档的核对记录](docs/oracle-official-alignment-2026-10-26.md)修正了 `SIZE:4001` 被降成 CLOB 的问题，并用实库目录类型断言复测；该记录也列出仍需金仓确认和未覆盖的项目。

单元和竞态测试：

```bash
go test ./kingbase/test/unit
go test -race ./kingbase/test/unit
go vet ./kingbase/dialects ./kingbase/test/...
```

真实库测试通过环境变量连接。一键跑两个模式时应分别设置两个 DSN；`KINGBASE_DSN` 主要用于只跑一个模式时的通用回退：

```bash
export KINGBASE_MYSQL_DSN='host=... port=... user=... password=... dbname=... sslmode=disable'
export KINGBASE_ORACLE_DSN='host=... port=... user=... password=... dbname=... sslmode=disable'

# 指向已验收且与 scripts/gokb-source-lock.json 完全一致的驱动源码
export GOKB_SOURCE_PATH='/path/to/gokb'

# 需要 Python 3、psql 和 Go（本期使用 1.22.5）。账号需有 CREATE SCHEMA 权限。
# 自动创建独立 schema 并清理。输出目录必须新建或为空。
# 原始 SQL 探测失败保持 FAIL，脚本相应非零退出。
./kingbase/test_all_kingbase.sh --output /tmp/kingbase-acceptance-001
```

官方 GORM V1 根目录测试文件保持原样，不在官方包中注入 Kingbase 分支。Kingbase 适配测试在本目录内以官方场景名重新组织，并由 `testutil.Open` 分别连接 MySQL/Oracle 模式：

```bash
# 执行 kingbase 目录下的方言、集成、回归、负向和非功能测试；
# 官方场景名对应的回归用例会在 MySQL/Oracle 两个模式子测试中各执行一次
./kingbase/test_all_kingbase.sh --output /tmp/kingbase-acceptance-002

# 直接运行 Kingbase 回归覆盖
REQUIRE_KINGBASE_DB=1 go test -v -race -count=1 ./kingbase/test/regression

# 按当前根目录源码检查具名落点（198 个 Test、2 个 Benchmark）
./kingbase/check_official_coverage.sh
```

`testutil.Open` 默认在连接失败时跳过实库用例，便于本地单元测试；设置 `REQUIRE_KINGBASE_DB=1`（脚本默认设置）后，连接失败、Ping 失败或模式不一致会直接失败。MySQL 模式若 DSN 未设置 `get_last_insert_id=yes`，测试入口会自动补上该驱动选项。

单项排查失败 SQL 时可额外设置 `DEBUG=true`。验收脚本固定 `-race -count=1`、强制实库和只读事务检查，逐个主测试启动独立进程；父项与叶子不重复计数。JSON 事件、可读日志、退出码、跳过项及清理结果均写入输出目录，Benchmark 不在默认功能验收范围。

验收脚本在临时 modfile 中指定传入的驱动路径，校验 31 个驱动文件的 SHA256，不修改根 `go.mod/go.sum` 或驱动文件。源码指纹只固定本期实际测试的驱动制品，不能替代金仓正式版本号或可分发制品。根模块原有本机绝对路径仍保留，直接运行普通 `go test` 仍受其影响；跨机器验收请使用脚本并另行提供匹配源码，正式交付仍须取得驱动发布版本和制品。

`mysql_test.go` 的三个主测试默认输出测试标题、通过/未通过结论，以及出错 SQL 的金仓返回日志，可分别运行：

```bash
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseMySQLMode_FullIntegration$'
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseMySQLMode_SpecialTypesAndFunctions$'
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseMySQLMode_AdvancedCompatibility$'
```

日志记录的是 `gokb` 驱动接口的真实返回，并保留 `database/sql` 的事务和预处理行为，不修改 GORM V1 或 `gokb` 源码：

- `[通过]` / `[未通过]`：测试或检查项的结论；正常 SQL、数据行、`error=<nil>` 和驱动调用次数不打印。
- `[金仓错误 #编号]`：非预期的驱动错误，会使测试失败。打印对应操作、SQL、参数、SQLSTATE 和金仓消息；有内容的详情、提示、约束名一并保留。`DEBUG=true` 可额外打印完整驱动错误结构。
- `[预期错误 #编号]`：唯一约束测试故意插入重复值，且返回的错误码和约束名符合预期；核验通过后显示“按预期返回冲突”。其他错误仍标为“金仓错误”并判失败。

`oracle_test.go` 使用相同日志和判定规则，共 3 个主测试、12 个子测试：连接测试（0 个）、完整集成测试（8 个）、函数专项（4 个）。

新增独立文件 `test/integration/oracle_adapter_test.go`，入口 `TestKingbaseOracleMode_AdapterBoundaries`，包含 7 项：类型/默认值/数据回读、引用索引名删除、修改列后的状态与数据、schema 元数据隔离、identity/默认插入/分页、ORM 外键生命周期、显式原生 identity。该入口不计入 `oracle_test.go` 原有 3 个主测试。

```bash
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseOracleMode_ConnectOnly$'
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseOracleMode_FullIntegration$'
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseOracleMode_FunctionCompatibility$'
REQUIRE_KINGBASE_DB=1 go test -v -count=1 ./kingbase/test/integration -run '^TestKingbaseOracleMode_AdapterBoundaries$'
```

Oracle 函数专项不依赖测试表，可单独执行。参考[金仓 Oracle 内置函数说明](https://docs.kingbase.com.cn/cn/KES-V9R1C10/application/application-develop-guide/kes-vs-oracle/kes-vs-oracle-2/)，按以下 4 组执行 37 项结果检查：

| 子测试 | 函数及检查内容 |
| --- | --- |
| `Null_And_Conditional` | NVL、NVL2、DECODE、COALESCE、NULLIF；包含 NULL、非空、空字符串和默认分支。 |
| `String_Functions` | CONCAT、SUBSTR、LENGTH、INSTR、UPPER、LOWER、TRIM、REPLACE；包含参数绑定、负数起点和中文字符。 |
| `Numeric_Functions` | ROUND、TRUNC、MOD、ABS、CEIL、FLOOR；包含正负数舍入和截断。 |
| `Date_And_Conversion` | TO_CHAR、TO_DATE、TO_TIMESTAMP、TO_NUMBER、日期 TRUNC、ADD_MONTHS、LAST_DAY、MONTHS_BETWEEN；使用固定日期和显式格式，核验闰日、月末和日期截断。 |

每项单独执行并核验实际值，成功只显示 `[通过] 函数/场景`；语法错误、执行/读取错误、意外 NULL 或结果不符均判失败，并继续检查后面的函数。不通过替换函数或跳过失败项来隐藏兼容问题；日期结果直接与日期字面量比较，避免依赖 TO_CHAR 才能判断其他日期函数。

即使 GORM V1 没有向 `.Error` 传递驱动错误，该子测试仍会失败。索引、字段修改和数据读写另有实际结果核验；函数逐个执行和核对值，一个函数失败不会遮住其余函数。删除索引失败后继续检查字段修改。

MySQL 无符号显式大值后的序列同步已补齐，自增专项 9 项通过；最终四组 ORM 实库回归为 43 项、41 PASS、2 FAIL。UINT64 自增全范围和 ENUM 大小写成员冲突仍保留失败。见[序列同步修补及限制](docs/mysql-sequence-sync-adaptation-2026-10-25.md)。

MySQL 字段属性专项入口 `TestKingbaseMySQLMode_FieldProperties` 包含 8 个子测试，首轮 6 PASS、2 FAIL；后续适配后 7 PASS、1 FAIL，自增显式 NULL/零值已修复，ENUM 大小写冲突仍保留失败。见 [字段属性实测与单项命令](docs/mysql-field-properties-live-results-2026-10-25.md)。

MySQL DDL 补测分为三个独立入口：`TestKingbaseMySQLMode_DDLStep1` 的 7 项真实 ORM 适配验收、`TestKingbaseMySQLMode_MySQLDDLCompatibility` 的 6 项原始 MySQL SQL 兼容探测，以及 `TestKingbaseMySQLMode_NativeDDLReference` 的 3 项金仓官方参考。原始 SQL 不替换、不回退；参考组不计入适配通过。见 [逐项命令与运行记录](docs/mysql-ddl-step1-runbook.md)。这些入口独立于上述三个 MySQL 主测试。2026-10-24 经用户授权已逐项实测辅助组（2 通过、4 失败）及本批 7 项 ORM DDL（均有通过结果）；外键名称截断及字段字面量改写已修复，详见 [实库排查记录](docs/mysql-ddl-live-results-2026-10-24.md)；其他项目仍待验证。

后续增加 `TestKingbaseMySQLMode_AdapterBoundaries`，包含 19 个独立边界子测试。当前实库 18 通过、UINT64 自增全范围 1 项失败；普通 UNSIGNED 范围、Offset、转义和 TINY/MEDIUM 边界已验证，LONG 1GB 上界未测。见 [2026-10-25 边界记录及单项命令](docs/mysql-adapter-live-results-2026-10-25.md)。

这些结果表示当前实例、模式和版本对具体测试 SQL 的支持情况。语法错误、权限不足、连接错误等应结合原始错误码分别判断，不能一律认定为金仓不支持该功能。

真实库用例覆盖连接与模式校验、AutoMigrate 建表/增列、按 schema 限定的元数据、索引创建/删除、唯一约束、字段修改、CRUD、复杂查询、Row/Rows/ScanRows、分页、软删除/恢复/硬删除、事务提交/回滚/闭包事务、隔离级别、脏读/不可重复读/幻读、锁等待/死锁/超时、Savepoint/`ROLLBACK TO`、嵌套事务、事务内写后读、context 取消、只读事务、`RollbackUnlessCommitted`、约束错误继续/提交、严格 DDL 回滚和连接关闭错误、外键级联、关联/预加载、多态关联、复合主键、多种回调、CLOB/BLOB/NULL/时区时间和 Oracle `DUAL`。

### 5.1 各模式测试覆盖

| 模式 | 测试文件 | 已覆盖场景 | 验收状态 |
| --- | --- | --- | --- |
| MySQL | `kingbase/test/integration/mysql_test.go`、`dialect_boundary_test.go`、`transaction_boundary_test.go` | `SHOW database_mode` 校验、AutoMigrate 建表/增列、字段和索引检查、CRUD、分页/聚合、表达式更新、事务、隔离级别、并发锁/死锁/超时、Savepoint、只读事务、约束错误继续提交、严格 DDL 回滚、连接关闭错误、软删除、MySQL 风格原生 SQL、类型映射、自增、唯一约束、索引生命周期、字段修改、常用函数。 | 本期重点；须在 V9R1C10 MySQL 模式实库执行。 |
| Oracle | `kingbase/test/integration/oracle_test.go`、`oracle_adapter_test.go`、`dialect_boundary_test.go`、`transaction_boundary_test.go` | `SHOW database_mode` 校验、AutoMigrate 建表/增列、schema 元数据、CRUD/主键回填、分页、隔离级别、并发锁/死锁/超时、Savepoint、只读事务、约束错误继续提交、严格 DDL 回滚、连接关闭错误、索引生命周期、字段修改、事务、软删除/恢复/硬删除、外键级联、原生 JSON/JSONB、CLOB/BLOB/NULL/时区时间、`DUAL`、空值/条件/字符串/数值/日期与类型转换函数。 | 当前实例已实测；仍有已记录的 GORM 回归失败及未覆盖范围，详见 Oracle 验收记录。 |

公共 GORM 官方语义回归位于：

- `kingbase/test/regression/official_coverage_test.go`：创建、查询、更新/删除、关联/预加载、多态、复合主键、嵌入/指针、Scanner/Valuer、回调、迁移和安全保护等场景，按 MySQL/Oracle 两种模式执行。
- `kingbase/test/regression/official_extended_coverage_test.go`：NowFunc、空结构、匿名 Scanner、自定义列、Scope/查询克隆、FirstOrInit/FirstOrCreate、闭包事务提交/回滚/panic、定制外键和元数据行为。
- `kingbase/test/regression/official_create_remaining_test.go`、`official_query_remaining_test.go`、`official_update_remaining_test.go`、`official_core_remaining_test.go`：按 Create、Query、Update/Delete 和核心运行时分类承接其余官方场景。
- `kingbase/test/regression/official_association_remaining_test.go`、`official_custom_relation_remaining_test.go`、`official_preload_remaining_test.go`：按普通关联、自定义关联和 Preload 分类承接 Association/JoinTable/Polymorphic/复合主键场景。
- `kingbase/test/regression/official_callback_remaining_test.go`、`official_migration_remaining_test.go`、`official_metadata_remaining_test.go`：分别承接 Callback、Migration 和 Scanner/Valuer/Scope/Search/命名/ModelStruct 等元数据场景。
- `kingbase/test/integration/transaction_boundary_test.go`：按 MySQL/Oracle 模式执行事务边界专项，包括隔离级别、ReadCommitted/RepeatableRead 可见性、并发可见性、锁等待/死锁/超时、Savepoint、嵌套事务、事务内写后读、context 取消、只读事务、`RollbackUnlessCommitted`、约束错误后的继续/提交、严格 DDL 原子性和关闭连接错误。
- `kingbase/test/negative/transaction_error_test.go`：通过驱动错误注入覆盖连接丢失时 Commit/Rollback 失败的错误传播，MySQL/Oracle 两种方言均执行。
- `kingbase/test/nonfunctional/performance_test.go`：保留项目性能测试，并新增官方 `BenchmarkGorm`、`BenchmarkRawSql` 的 Kingbase MySQL/Oracle 双模式实现。
- 根目录 `*_test.go`：官方 GORM V1 原始测试保持不变；对应场景在 Kingbase 回归文件中以 `TestCreate...`、`TestQuery...` 等子测试名承接。

早期清单以 `v1.9.16` 的 195 个测试统计；当前工作区根目录实际有 198 个 `Test*` 和 2 个 Benchmark。新增补齐 `TestSubQueryWithQueryOption`、`TestQueryExprWithQueryOption` 和 `TestFixFullTableScanWhenInsertIgnore`，位于 `test/regression/official_query_option_ignore_test.go`。两种表达式同时核验 SQL 构造与实库筛选结果；IGNORE 用新模型触发默认值回读边界并禁止无条件全表回读。当前具名静态差集为 0，表示有对应场景名称，不证明与上游逐条断言完全等同，也不证明实库通过。PostgreSQL 专属 `TestHstore` 和模式专属能力分别记录 SKIP。

当前状态、模式差异和现场验收命令见[双模式验收记录](docs/dual-mode-acceptance-2026-10-27.md)。

方言离线测试集中在 `kingbase/test/unit/`，通过公开 Dialect 接口及 GORM 调用验证 SQL、边界和错误传播，不连接实库。`kingbase/dialects/` 仅保留适配实现，没有测试文件，也没有为测试额外导出内部函数。主要的真实数据库验收仍在 `kingbase/test/integration/`。

真实库测试依赖可用 DSN；连接失败时部分用例会跳过。因此只有在目标 V9R1C10 实例上执行并保存结果后，才能认定为完成该模式验收。

目录职责见下文；当前回归、非功能及异常测试的执行方式和实测结果见[双模式验收记录](docs/dual-mode-acceptance-2026-10-27.md)。

## 6. 发布门禁

- 获取可交付的 `gokb` 正式版本或内部制品；当前验收脚本以临时 modfile 和源码指纹绕开本机路径，根模块本机 `replace` 尚未移除。
- 固定 GORM、`gokb` 和 KingbaseES 小版本，保存兼容矩阵及失败 SQL。
- 分别在 MySQL、Oracle 模式实例运行集成测试。
- 对业务实际使用的原生 SQL、函数、默认值、序列和批量写入另建回归清单。

系统上下文图位于 `kingbase/diagram/gorm-v1-kingbase-system-context.vsdx`。

## 7. 适配代码与测试文件职责

2026-10-25 按目录分离职责：`dialects/` 只放实现，离线测试全部迁到 `test/unit/`，实库、回归、异常及非功能测试保留在各自的 test 子目录。41 个原有离线测试入口保留；原来访问内部函数的检查改为通过公开接口和注册的 GORM 回调验证行为。实现声明内容、包路径和公开接口未变，GORM V1 根目录不参与调整。已有实库日志原样保留，其中的旧文件位置是当时验收的记录。

| 实现文件 | 职责 |
| --- | --- |
| [dialect.go](dialects/dialect.go) | 模式常量、方言注册和构造入口、共用名称解析 |
| [dialect_mysql.go](dialects/dialect_mysql.go) | MySQL 方言接口、委托、分页、DDL、标识符引用及自动名称 |
| [dialect_mysql_types.go](dialects/dialect_mysql_types.go) | 字段类型、默认值/注释保留及无符号范围定义 |
| [dialect_mysql_metadata.go](dialects/dialect_mysql_metadata.go) | MySQL 表/列/索引/外键目录查询及错误观察 |
| [dialect_mysql_callbacks.go](dialects/dialect_mysql_callbacks.go) | GORM Create 扩展回调，无符号回退列的 NULL/0 处理 |
| [dialect_callbacks.go](dialects/dialect_callbacks.go) | 两个模式共用的分组计数 RowQuery 修补 |
| [mysql_uint64.go](dialects/mysql_uint64.go) | MySQL 完整 UINT64 的显式迁移、拥有序列与默认值校验 |
| [migration.go](dialects/migration.go) | 两模式共用的原子迁移与 savepoint 辅助 |
| [dialect_oracle.go](dialects/dialect_oracle.go) | Oracle 方言接口、类型、分页、DDL 与插入回填 |
| [dialect_oracle_metadata.go](dialects/dialect_oracle_metadata.go) | Oracle information_schema/pg_indexes 查询、schema 范围与错误观察 |
| [oracle_migration.go](dialects/oracle_migration.go) | Oracle 特殊索引、安全错误返回、列注释和原子迁移入口 |
| [oracle_uint64.go](dialects/oracle_uint64.go) | Oracle 完整 uint64 主键范围的 NUMERIC 默认值与原生序列安装 |

| 离线单元测试文件 | 范围 |
| --- | --- |
| [dialect_test.go](test/unit/dialect_test.go) | 公共测试辅助函数、模式选择、两种模式共用的边界检查 |
| [dialect_mysql_test.go](test/unit/dialect_mysql_test.go) | MySQL 类型、分页、标识符、名称、DDL、元数据和错误传播 |
| [dialect_mysql_callbacks_test.go](test/unit/dialect_mysql_callbacks_test.go) | Create 扩展回调的模式、NULL/0、溢出、错误与方言隔离 |
| [dialect_oracle_test.go](test/unit/dialect_oracle_test.go) | Oracle 类型、分页、RETURNING、DDL 和元数据错误 |
| [oracle_migration_test.go](test/unit/oracle_migration_test.go) | Oracle 迁移入口的原始错误保留和错误后禁止 DDL |

离线测试使用 `go test ./kingbase/test/unit`；原测试函数名继续可用于 `-run` 过滤。`go test ./kingbase/dialects` 现在只检查实现包编译，不再运行离线测试。实库测试入口和命令保持原样。


目录用途：

```text
kingbase/
├── dialects/              # 仅适配实现
├── test/
│   ├── unit/              # 离线 SQL Mock 与公开接口测试
│   ├── integration/       # 实库功能和兼容性验收（主要测试）
│   ├── regression/        # GORM V1 原有行为回归
│   ├── negative/          # 错误与异常场景
│   ├── nonfunctional/     # 并发、性能、稳定性
│   └── testutil/          # 测试连接、日志及公共辅助
└── docs/                  # 说明、验收记录和历史日志
```
