# SQLite 到 PostgreSQL 安全迁移与发布 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 提供只读、可 dry-run、事务化、可校验的 SQLite 历史数据迁移命令，并通过手动审批 workflow 在生产执行。

**Architecture:** 服务运行时保持 PostgreSQL-only。SQLite driver 只允许迁移包使用；`database.Open` 分离为连接、AutoMigrate、SeedDefaults，迁移绝不 seed。复制保留 ID，在一个 PostgreSQL transaction 中完成，随后重置序列和验证。

**Tech Stack:** Go、GORM、PostgreSQL、SQLite migration-only driver、GitHub Actions、SSH。

---

## File Map

- Create: `cmd/migrate-sqlite-to-postgres/main.go`
- Create: `internal/migration/sqlitepostgres/{migrate,source,target,tables,copy,sequence,validate}.go`
- Create: `internal/migration/sqlitepostgres/{readonly,migrate,sequence,validate}_test.go`
- Create: `internal/migration/sqlitepostgres/testdata/sqlite_fixture.sql`
- Create: `.github/workflows/migrate-sqlite-to-postgres.yml`
- Modify: `go.mod`, `go.sum`, `internal/database/schema.go`, `internal/database/database.go`, `internal/database/database_test.go`, `.github/workflows/ci-cd.yml`

### Task 1: 分离 AutoMigrate 与 seed，避免目标库被默认数据污染

**Files:**
- Modify: `internal/database/schema.go`, `internal/database/database.go`, `internal/database/database_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestAutoMigrateCreatesOnlySchema(t *testing.T) {
    db := testPostgresDatabase(t)
    require.NoError(t, database.AutoMigrate(db))
    var users int64
    require.NoError(t, db.Model(&model.User{}).Count(&users).Error)
    require.Zero(t, users)
}
func TestSeedDefaultsRunsAfterSchema(t *testing.T) {
    db := testPostgresDatabase(t)
    require.NoError(t, database.AutoMigrate(db))
    require.NoError(t, database.SeedDefaults(db, testConfig(t)))
    var settings int64
    require.NoError(t, db.Model(&model.SiteSetting{}).Count(&settings).Error)
    require.Positive(t, settings)
}
```

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/database -run 'AutoMigrate|SeedDefaults' -v
```

Expected: FAIL，因为公共函数尚不存在。

- [ ] **Step 3: 实现 schema 边界**

```go
func Models() []any { return []any{&model.SiteSetting{}, &model.User{}, &model.EmailVerificationCode{}, &model.Category{}, &model.Tag{}, &model.Post{}, &model.Comment{}, &model.Project{}, &model.Upload{}, &model.AccessLog{}, &model.IPBan{}, &model.AIConversation{}, &model.AIMessage{}} }
func AutoMigrate(db *gorm.DB) error { return db.AutoMigrate(Models()...) }
func SeedDefaults(db *gorm.DB, cfg config.Config) error {
    if err := SeedInitialAdmin(db, cfg); err != nil { return err }
    if err := SeedDefaultSiteSettings(db, cfg); err != nil { return err }
    return SeedDefaultBlogContent(db, cfg)
}
```

`Open` 保持 `connect -> AutoMigrate -> SeedDefaults`。迁移命令只能调用 `AutoMigrate`。

- [ ] **Step 4: 验证并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/database -v
git status --short
git add internal/database/schema.go internal/database/database.go internal/database/database_test.go
git commit -m "refactor: separate database schema and seeds"
git push origin master
git push gitee master
```

### Task 2: 只读 SQLite、dry-run 与非空 PostgreSQL 防护

**Files:**
- Create: `internal/migration/sqlitepostgres/source.go`, `target.go`, `migrate.go`, `readonly_test.go`, `migrate_test.go`, `testdata/sqlite_fixture.sql`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: 写安全失败测试**

```go
func TestRunDryRunDoesNotChangePostgres(t *testing.T) {
    source := createSQLiteFixture(t)
    target := newEmptyPostgresSchema(t)
    before := tableCounts(t, target)
    report, err := sqlitepostgres.Run(context.Background(), sqlitepostgres.Options{SQLitePath: source, PostgresDSN: dsnFor(t, target), DryRun: true})
    require.NoError(t, err)
    require.NotEmpty(t, report.Tables)
    require.Equal(t, before, tableCounts(t, target))
}
func TestRunRejectsNonEmptyPostgres(t *testing.T) {
    _, err := sqlitepostgres.Run(context.Background(), sqlitepostgres.Options{SQLitePath: createSQLiteFixture(t), PostgresDSN: dsnFor(t, newPostgresSchemaWithRow(t))})
    require.ErrorContains(t, err, "target database is not empty")
}
```

另加 missing file、只读打开、`.db-wal`/`.db-shm` 未修改测试。

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/migration/sqlitepostgres -run 'DryRun|NonEmpty|ReadOnly' -v
```

Expected: FAIL，因为迁移包不存在。

- [ ] **Step 3: 实现安全打开与 Run 骨架**

```go
type Options struct { SQLitePath string; PostgresDSN string; DryRun bool }
type Report struct { Tables []TableReport }
func OpenSQLiteReadOnly(path string) (*gorm.DB, error) {
    if _, err := os.Stat(path); err != nil { return nil, fmt.Errorf("sqlite source: %w", err) }
    return gorm.Open(sqlite.Open("file:"+filepath.ToSlash(path)+"?mode=ro"), &gorm.Config{})
}
func Run(ctx context.Context, options Options) (Report, error) { source, err := OpenSQLiteReadOnly(options.SQLitePath); if err != nil { return Report{}, err }; target, err := OpenPostgres(options.PostgresDSN); if err != nil { return Report{}, err }; if err := EnsureTargetEmpty(target, ExistingBusinessTables()); err != nil { return Report{}, err }; if options.DryRun { return Scan(source, ExistingBusinessTables()) }; return CopyAndValidate(ctx, source, target) }
```

新增 SQLite driver 只能被 `internal/migration/sqlitepostgres` 导入；禁止给 `database.Open` 加 sqlite 分支；禁止目标非空覆盖开关。

- [ ] **Step 4: 验证并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/migration/sqlitepostgres -run 'DryRun|NonEmpty|ReadOnly' -v
git status --short
git add go.mod go.sum internal/migration/sqlitepostgres
git commit -m "feat: add safe SQLite migration foundation"
git push origin master
git push gitee master
```

### Task 3: 全表复制、关联校验和序列重置

**Files:**
- Create: `internal/migration/sqlitepostgres/tables.go`, `copy.go`, `sequence.go`, `validate.go`, `sequence_test.go`, `validate_test.go`

- [ ] **Step 1: 写迁移失败测试**

```go
func TestRunMigratesBusinessTablesAndResetsSequences(t *testing.T) {
    target := newEmptyPostgresSchema(t)
    report, err := sqlitepostgres.Run(context.Background(), sqlitepostgres.Options{SQLitePath: createSQLiteFixture(t), PostgresDSN: dsnFor(t, target)})
    require.NoError(t, err)
    require.Contains(t, report.TableNames(), "post_tags")
    require.Equal(t, int64(2), countRows(t, target, "posts"))
    require.Equal(t, int64(2), countRows(t, target, "post_tags"))
    require.Greater(t, insertedPostID(t, target), uint(6))
}
func TestRunRollsBackBrokenRelations(t *testing.T) {
    target := newEmptyPostgresSchema(t)
    _, err := sqlitepostgres.Run(context.Background(), sqlitepostgres.Options{SQLitePath: createBrokenSQLiteFixture(t), PostgresDSN: dsnFor(t, target)})
    require.Error(t, err)
    require.Zero(t, countRows(t, target, "posts"))
}
```

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/migration/sqlitepostgres -run 'Migrates|Resets|RollsBack|Validate' -v
```

Expected: FAIL，因为复制/校验不存在。

- [ ] **Step 3: 实现表规范、事务和验证**

```go
type TableSpec struct { Name string; Model any; HasSequence bool }
func ExistingBusinessTables() []TableSpec { return []TableSpec{siteSettingsSpec, usersSpec, emailCodesSpec, categoriesSpec, tagsSpec, postsSpec, postTagsSpec, commentsSpec, projectsSpec, uploadsSpec, accessLogsSpec, ipBansSpec, aiConversationsSpec, aiMessagesSpec} }
func CopyAll(ctx context.Context, tx, source *gorm.DB) ([]TableReport, error)
func ResetSequences(tx *gorm.DB, tables []TableSpec) error
func ValidateMigration(ctx context.Context, source, target *gorm.DB) (Report, error)
```

先 AutoMigrate，后按依赖顺序保留原 ID 复制。`post_tags` 用显式行结构复制；上传仅迁移数据库记录。一个 PostgreSQL transaction 内完成复制、sequence、count/key/relation/digest 校验；失败 rollback。验证 posts.category、post_tags、comments 关系及 slug/正文/设置摘要。

- [ ] **Step 4: 运行迁移包完整测试并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/migration/sqlitepostgres -v
git status --short
git add internal/migration/sqlitepostgres
git commit -m "feat: migrate SQLite data to PostgreSQL"
git push origin master
git push gitee master
```

Expected: PASS；每个测试使用独立 PostgreSQL schema 并只清理自身 schema。

### Task 4: 命令入口、普通 CI 和人工审批迁移 workflow

**Files:**
- Create: `cmd/migrate-sqlite-to-postgres/main.go`, `.github/workflows/migrate-sqlite-to-postgres.yml`
- Modify: `.github/workflows/ci-cd.yml`

- [ ] **Step 1: 写命令失败测试**

```go
func TestCommandRejectsMissingSQLitePath(t *testing.T) {
    result := runMigrationCommand(t, "--dry-run")
    require.NotEqual(t, 0, result.ExitCode)
    require.Contains(t, result.Stderr, "--sqlite-path is required")
}
```

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./cmd/migrate-sqlite-to-postgres -v
```

Expected: FAIL，因为命令不存在。

- [ ] **Step 3: 实现命令与 CI build**

```go
func main() {
    source := flag.String("sqlite-path", "", "read-only SQLite source path")
    dryRun := flag.Bool("dry-run", false, "validate without writing target")
    flag.Parse()
    if strings.TrimSpace(*source) == "" { log.Fatal("--sqlite-path is required") }
    dsn := os.Getenv("BLOG_DATABASE_DSN")
    if strings.TrimSpace(dsn) == "" { log.Fatal("BLOG_DATABASE_DSN is required") }
    report, err := sqlitepostgres.Run(context.Background(), sqlitepostgres.Options{SQLitePath: *source, PostgresDSN: dsn, DryRun: *dryRun})
    if err != nil { log.Fatal(err) }
    json.NewEncoder(os.Stdout).Encode(report)
}
```

?? CI ?? `go test ./...`??? `go build ./cmd/migrate-sqlite-to-postgres`?verify job ????????????????????????

- [ ] **Step 4: 写手动 workflow**

```yaml
name: Migrate SQLite to PostgreSQL
on:
  workflow_dispatch:
    inputs:
      confirm:
        required: true
        type: string
jobs:
  migrate:
    if: github.event.inputs.confirm == 'MIGRATE'
    environment: production
```

workflow 使用既有 SSH secrets，在服务器上：备份 → dry-run → 正式迁移 → 校验 → 重启 `blog.service` → `/api/site` 健康检查。禁止 `push:` trigger、打印 DSN、删除 SQLite/WAL/SHM、自动清空 PostgreSQL。

- [ ] **Step 5: 完整验证与提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./...
& 'D:\AllTools\Go\go\bin\go.exe' build ./cmd/migrate-sqlite-to-postgres
Get-Content -Raw .github/workflows/migrate-sqlite-to-postgres.yml
git status --short
git add cmd/migrate-sqlite-to-postgres/main.go .github/workflows/ci-cd.yml .github/workflows/migrate-sqlite-to-postgres.yml
git commit -m "ci: add approved database migration workflow"
git push origin master
git push gitee master
```

## Final Verification

- [ ] Run:

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./...
git status --short
```

Expected: all tests pass; no `.db`, `.db-wal`, `.db-shm`, executable, `.env`, DSN, upload, log or test artifact is staged.
