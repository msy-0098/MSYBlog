# Phase 4 Backend: Site-wide Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复评论和归档伪分页，限制公共低数量列表，并为用户、分类、标签、项目、友链与 IP 封禁提供稳定服务端分页。

**Architecture:** 新建通用页码和游标工具，数据库查询统一以唯一 ID 作为稳定排序尾键。公共长列表使用游标，管理列表使用页码；兼容期响应同时提供 list 和 items，AI 会话分页继续沿用第二阶段合同。

**Tech Stack:** Go 1.25、Gin、GORM/PostgreSQL、base64url cursor、httptest。

---

**配套前端计划：** frontend/docs/superpowers/plans/2026-07-23-phase-4-pagination-frontend.md。后端分页合同先完成。

### Task 1: 建立通用页码和游标工具

**Files:**
- Create: internal/pagination/page.go
- Create: internal/pagination/page_test.go
- Create: internal/pagination/cursor.go
- Create: internal/pagination/cursor_test.go
- Create: internal/handler/pagination_dto.go

- [ ] **Step 1: 写边界红灯测试**

~~~go
func TestParsePageClampsValues(t *testing.T) {
    got := pagination.ParsePage(url.Values{"page":{"0"}, "pageSize":{"999"}}, 20, 100)
    require.Equal(t, 1, got.Page)
    require.Equal(t, 100, got.PageSize)
}

func TestCursorRoundTrip(t *testing.T) {
    raw := pagination.Cursor{Time: time.Date(2026,7,23,12,0,0,0,time.UTC), ID:42}
    decoded, err := pagination.DecodeCursor(pagination.EncodeCursor(raw))
    require.NoError(t, err)
    require.Equal(t, raw, decoded)
}
~~~

- [ ] **Step 2: 运行红灯测试**

Run: go test ./internal/pagination ./internal/handler

Expected: FAIL。

- [ ] **Step 3: 实现工具和兼容 DTO**

~~~go
type PageResult[T any] struct {
    List []T   `json:"list"`
    Items []T  `json:"items"`
    Page int    `json:"page"`
    PageSize int `json:"pageSize"`
    Total int64 `json:"total"`
}

type CursorResult[T any] struct {
    Items []T        `json:"items"`
    NextCursor string `json:"nextCursor,omitempty"`
    HasMore bool      `json:"hasMore"`
}
~~~

游标使用 JSON + base64.RawURLEncoding，解码后严格校验时间、ID 和长度，错误返回 400。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: go test ./internal/pagination ./internal/handler

Expected: PASS。

~~~powershell
git status --short
git add internal/pagination internal/handler/pagination_dto.go
git diff --cached --check
git commit -m "feat(api): add stable pagination primitives"
git push origin master
git push gitee master
~~~

### Task 2: 修复游客评论、归档和首页项目查询

**Files:**
- Modify: internal/model/comment.go
- Modify: internal/model/blog.go
- Modify: internal/model/project.go
- Modify: internal/handler/comment_handler.go
- Modify: internal/handler/blog_handler.go
- Test: internal/router/visitor_comment_routes_test.go
- Test: internal/router/blog_routes_test.go

- [ ] **Step 1: 写评论游标和归档红灯测试**

测试创建 25 条同秒评论，第一页 20、第二页 5，ID 不重复；归档创建跨年月 55 篇文章，第一页 50、第二页 5，排序为 published_at DESC, id DESC。

~~~go
require.Len(t, first.Items, 20)
require.True(t, first.HasMore)
require.NotEmpty(t, first.NextCursor)
require.Empty(t, intersectIDs(first.Items, second.Items))
~~~

- [ ] **Step 2: 运行红灯测试**

Run: go test ./internal/router -run 'CommentsCursor|ArchiveCursor|ProjectsLimit'

Expected: FAIL。

- [ ] **Step 3: 实现数据库层游标查询**

评论条件：

~~~go
q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.Time, cursor.Time, cursor.ID).
    Order("created_at DESC, id DESC").Limit(limit + 1)
~~~

归档使用明确的 `published_at + id` 条件：

~~~go
q = q.Where("published_at < ? OR (published_at = ? AND id < ?)", cursor.Time, cursor.Time, cursor.ID).
    Order("published_at DESC, id DESC").Limit(limit + 1)
~~~

查询 `limit+1` 判断 `hasMore`，只把前 `limit` 转 DTO。归档在当前页内按年月分组，`total` 不再作为游标响应必需字段。

GET /api/projects?limit=3 默认仍可返回列表，但 limit 最大 50；/links、/categories、/tags 增加最大 100 的服务端上限，并将分类/标签计数改为聚合查询，消除 N+1。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: go test ./internal/router ./internal/model

Expected: PASS。

~~~powershell
git status --short
git add internal/model/comment.go internal/model/blog.go internal/model/project.go internal/handler/comment_handler.go internal/handler/blog_handler.go internal/router/visitor_comment_routes_test.go internal/router/blog_routes_test.go
git diff --cached --check
git commit -m "feat(blog): add cursor pagination for comments and archive"
git push origin master
git push gitee master
~~~

### Task 3: 为管理端六类列表增加页码分页

**Files:**
- Modify: internal/model/user.go
- Modify: internal/model/access.go
- Modify: internal/model/project.go
- Modify: internal/model/blog.go
- Modify: internal/handler/admin_content_handler.go
- Modify: internal/handler/admin_insight_handler.go
- Modify: internal/router/router.go
- Create: internal/router/admin_insight_routes_test.go
- Modify: internal/router/admin_content_routes_test.go

- [ ] **Step 1: 写页码和稳定排序红灯测试**

为用户、分类、标签、项目、友链和 IPBan 各创建 25 条，断言第一页 20、第二页 5、total=25，同时间记录按 ID 稳定排序。

~~~go
require.Equal(t, 20, len(first.Items))
require.Equal(t, int64(25), first.Total)
require.Equal(t, first.Items, first.List)
~~~

- [ ] **Step 2: 运行红灯测试**

Run: go test ./internal/router -run 'Admin.*Pagination'

Expected: FAIL。

- [ ] **Step 3: 实现查询与 options 合同**

所有列表默认 20、最大 100，返回 list/items/page/pageSize/total。排序补 ID：

- 用户：created_at DESC, id DESC；
- 分类、标签：created_at DESC, id DESC；
- 项目：sort DESC, updated_at DESC, id DESC；
- 友链：sort DESC, updated_at DESC, id DESC；
- IPBan：created_at DESC, id DESC。

为文章编辑器提供完整但受限的 options：

~~~text
GET /api/admin/categories/options
GET /api/admin/tags/options
~~~

options 最大 500，只返回 id/name/slug，避免编辑器只看到列表第一页。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: go test ./internal/router ./internal/handler ./internal/model

Expected: PASS。

~~~powershell
git status --short
git add internal/model internal/handler/admin_content_handler.go internal/handler/admin_insight_handler.go internal/router/admin_content_routes_test.go internal/router/admin_insight_routes_test.go internal/router/router.go
git diff --cached --check
git commit -m "feat(admin): paginate managed collections"
git push origin master
git push gitee master
~~~

### Task 4: 增加索引、全量验证和最终统一部署清单

**Files:**
- Modify: internal/database/schema.go
- Modify: internal/database/database_test.go
- Modify: internal/migration/sqlitepostgres/validate.go

- [ ] **Step 1: 写必要索引红灯测试**

~~~go
func TestPaginationIndexesExist(t *testing.T) {
    db := openIsolatedPostgres(t)
    require.NoError(t, database.AutoMigrate(db))
    migrator := db.Migrator()
    require.True(t, migrator.HasIndex(&model.Comment{}, "idx_comments_post_status_parent_created_id"))
    require.True(t, migrator.HasIndex(&model.Post{}, "idx_posts_status_published_id"))
    require.True(t, migrator.HasIndex(&model.AIConversation{}, "idx_ai_conversations_admin_updated_id"))
    require.True(t, migrator.HasIndex(&model.AIMessage{}, "idx_ai_messages_conversation_sequence"))
    require.True(t, migrator.HasIndex(&model.IPBan{}, "idx_ip_bans_created_id"))
}
~~~

迁移校验测试还要断言这些索引在 SQLite 历史迁移完成后由 PostgreSQL AutoMigrate 补齐，不要求源 SQLite 存在同名索引。

- [ ] **Step 2: 运行红灯测试**

Run: `go test ./internal/database ./internal/migration/sqlitepostgres -run 'PaginationIndexes|Validate'`

Expected: FAIL，稳定分页所需复合索引尚未全部创建或校验。

- [ ] **Step 3: 实现复合索引和迁移后校验**

确保 PostgreSQL 创建以下复合索引：

~~~text
comments(post_id, status, parent_id, created_at, id)
posts(status, published_at, id)
ai_conversations(admin_id, updated_at, id)
ai_messages(conversation_id, sequence)
ip_bans(created_at, id)
~~~

`schema.go` 使用固定索引名和列顺序，创建操作保持幂等；`validate.go` 在目标 PostgreSQL 校验阶段报告缺失索引，但不尝试修改只读源 SQLite。

- [ ] **Step 4: 运行后端全量测试和构建**

~~~powershell
go test -p 1 ./...
go build ./cmd/server
go build ./cmd/migrate-sqlite-to-postgres
go build ./cmd/update-dbip
~~~

Expected: PASS；需要 PostgreSQL 的测试必须使用独立 `BLOG_TEST_DATABASE_DSN`，不得指向开发或生产库。

- [ ] **Step 5: 提交索引并双推送**

~~~powershell
git status --short
git add internal/database/schema.go internal/database/database_test.go internal/migration/sqlitepostgres/validate.go
git diff --cached --name-status
git diff --cached --check
git commit -m "perf(db): index stable paginated queries"
git push origin master
git push gitee master
~~~

- [ ] **Step 6: 在前端 Phase 4 通过后统一部署四阶段成果**

部署前：确认两个仓库 origin/master 与 gitee/master 均等于本地 HEAD；备份 PostgreSQL；备份当前后端二进制、前端 release 目录、Nginx 与 systemd 配置；确认 MMDB 已在服务器运行时目录。

部署顺序：后端二进制与配置 → 数据库 AutoMigrate → 健康检查 → 前端静态资源 release → 切换软链接 → Nginx reload。

验收：/api/health、验证码与邮件、AI 状态/生成/停止/重试/报告、IP 查询、评论与归档加载更多、管理六类分页。

回滚：恢复旧后端二进制和前端 release 软链接并重启服务；新增列和表保持向后兼容，不在紧急回滚时执行破坏性 DROP；若 AutoMigrate 前后异常，使用部署前数据库备份恢复到独立实例验证后再切换。
