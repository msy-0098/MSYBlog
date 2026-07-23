# Phase 2 Backend: Admin AI Creation and Operations Assistant Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把现有 AI 能力补齐为可健康检查、有限流、可可靠停止/重试、支持润色预览和保存运营报告的管理员助手。

**Architecture:** 以 OpenAI 兼容 Provider 接口隔离上游实现，以 Runtime Service 统一超时、频率、并发和安全错误，再扩展会话消息状态、游标与报告模型。保留现有路由兼容，新增状态、健康检查、重试和报告 API。

**Tech Stack:** Go 1.25、Gin、GORM/PostgreSQL、SSE、OpenAI-compatible HTTP、context、sync semaphore。

---

**配套前端计划：** `frontend/docs/superpowers/plans/2026-07-23-phase-2-admin-ai-frontend.md`。后端 API 和模型先完成，再实施前端。

### Task 1: 抽象 Provider 与稳定错误分类

**Files:**
- Create: `internal/ai/provider.go`
- Create: `internal/ai/provider_error.go`
- Create: `internal/ai/openai_compatible.go`
- Create: `internal/ai/openai_compatible_test.go`
- Modify: `internal/ai/deepseek.go`
- Modify: `internal/ai/deepseek_test.go`
- Modify: `internal/ai/deepseek_stream_test.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: 写 Provider 合同红灯测试**

```go
type fakeProvider struct{}
func (fakeProvider) Chat(context.Context, ai.ChatRequest) (ai.ChatResult, error) { return ai.ChatResult{Content:"ok"}, nil }
func (fakeProvider) Stream(_ context.Context, _ ai.ChatRequest, emit func(ai.StreamChunk) error) (ai.Usage, error) { return ai.Usage{}, emit(ai.StreamChunk{Content:"ok"}) }
func (fakeProvider) Health(context.Context) ai.HealthResult { return ai.HealthResult{Healthy:true} }

var _ ai.Provider = fakeProvider{}
```

- [ ] **Step 2: 运行测试并确认红灯**

Run: `go test ./internal/ai ./internal/config`

Expected: FAIL，Provider 类型不存在。

- [ ] **Step 3: 实现接口和错误类型**

```go
type Provider interface {
    Chat(context.Context, ChatRequest) (ChatResult, error)
    Stream(context.Context, ChatRequest, func(StreamChunk) error) (Usage, error)
    Health(context.Context) HealthResult
}

type ProviderErrorKind string
const (
    ProviderErrorConfig ProviderErrorKind = "config"
    ProviderErrorRateLimit ProviderErrorKind = "rate_limit"
    ProviderErrorTimeout ProviderErrorKind = "timeout"
    ProviderErrorUnavailable ProviderErrorKind = "unavailable"
)
```

把现有 DeepSeek 客户端适配为 `OpenAICompatibleProvider`；Provider 配置真正决定构造逻辑，但本期只接受 `deepseek` 和 `openai-compatible`，未知值启动失败并给出安全配置错误。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: `go test ./internal/ai ./internal/config`

Expected: PASS。

```powershell
git status --short
git add internal/ai internal/config/config.go internal/config/config_test.go
git diff --cached --check
git commit -m "refactor(ai): introduce provider abstraction"
git push origin master
git push gitee master
```

### Task 2: 增加 Runtime 限制与健康检查

**Files:**
- Create: `internal/service/ai_runtime_service.go`
- Create: `internal/service/ai_runtime_service_test.go`
- Create: `internal/handler/admin_ai_status_handler.go`
- Modify: `internal/router/router.go`
- Test: `internal/router/admin_ai_routes_test.go`

- [ ] **Step 1: 写并发、长度与健康检查红灯测试**

```go
func TestAIRuntimeRejectsSecondConcurrentStream(t *testing.T) {
    runtime := service.NewAIRuntime(fakeBlockingProvider{}, service.AILimits{MaxInputChars:12000, MaxConcurrentPerAdmin:1})
    release := make(chan struct{})
    go runtime.Stream(context.Background(), 7, ai.ChatRequest{Messages: []ai.Message{{Role:"user", Content:"first"}}}, func(ai.StreamChunk) error { <-release; return nil })
    require.Eventually(t, func() bool { return runtime.ActiveForAdmin(7) == 1 }, time.Second, 10*time.Millisecond)
    _, err := runtime.Stream(context.Background(), 7, ai.ChatRequest{Messages: []ai.Message{{Role:"user", Content:"second"}}}, func(ai.StreamChunk) error { return nil })
    require.ErrorIs(t, err, service.ErrAIConcurrentLimit)
    close(release)
}
```

- [ ] **Step 2: 运行测试并确认红灯**

Run: `go test ./internal/service ./internal/router -run 'AI(Runtime|Status|Health)'`

Expected: FAIL。

- [ ] **Step 3: 实现 Runtime 和接口**

Runtime 必须执行：输入字符数、上下文字符数、管理员频率、单管理员并发、全局并发、Provider 超时和 Context 取消。健康检查使用独立短超时，不进入普通会话历史。

新增：

```text
GET  /api/admin/ai/status
POST /api/admin/ai/health-check
```

状态 DTO 只返回：`enabled/provider/model/configured/baseURLSummary/lastHealth`，绝不返回完整 API Key。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: `go test ./internal/service ./internal/handler ./internal/router`

Expected: PASS 或数据库相关测试在未配置独立测试库时 SKIP。

```powershell
git status --short
git add internal/service/ai_runtime_service.go internal/service/ai_runtime_service_test.go internal/handler/admin_ai_status_handler.go internal/router/router.go internal/router/admin_ai_routes_test.go
git diff --cached --check
git commit -m "feat(ai): add runtime limits and health checks"
git push origin master
git push gitee master
```

### Task 3: 扩展消息可靠性、重试关系、用量与游标分页

**Files:**
- Modify: `internal/model/ai.go`
- Modify: `internal/model/ai_test.go`
- Modify: `internal/database/schema.go`
- Modify: `internal/database/database_test.go`
- Modify: `internal/migration/sqlitepostgres/tables.go`
- Modify: `internal/migration/sqlitepostgres/copy.go`
- Modify: `internal/migration/sqlitepostgres/validate.go`
- Modify: `internal/migration/sqlitepostgres/migrate_test.go`
- Modify: `internal/service/ai_conversation_service.go`
- Modify: `internal/service/ai_conversation_service_test.go`
- Modify: `internal/handler/admin_ai_handler.go`
- Modify: `internal/handler/admin_ai_handler_test.go`
- Modify: `internal/router/admin_ai_routes_test.go`

- [ ] **Step 1: 写模型和上下文红灯测试**

```go
func TestContextMessagesExcludeAbortedAndFailed(t *testing.T) {
    messages := []model.AIMessage{
        {Role:"user", Content:"q", Status:"completed", Sequence:1},
        {Role:"assistant", Content:"partial", Status:"aborted", Sequence:2},
        {Role:"assistant", Content:"error", Status:"failed", Sequence:3},
    }
    got := contextMessages(messages, 20)
    require.Len(t, got, 1)
    require.Equal(t, "q", got[0].Content)
}
```

增加会话 `updated_at+id` 游标和消息 `sequence` 反向游标测试，断言无重复、稳定顺序、`hasMore/nextCursor` 正确。

- [ ] **Step 2: 运行测试并确认红灯**

Run: `go test ./internal/model ./internal/service ./internal/router -run 'AI|Conversation|Message'`

Expected: FAIL。

- [ ] **Step 3: 实现模型和服务扩展**

```go
type AIMessage struct {
    ID uint `gorm:"primaryKey"`
    ConversationID uint `gorm:"not null;index:idx_ai_message_sequence,priority:1"`
    Sequence int `gorm:"not null;index:idx_ai_message_sequence,priority:2"`
    ParentMessageID *uint `gorm:"index"`
    RetryOfID *uint `gorm:"index"`
    Role string
    Content string `gorm:"type:text"`
    Status string
    InputTokens int
    OutputTokens int
    TotalTokens int
    DurationMS int64
    Provider string
    Model string
    ErrorCode string
}
```

对外 DTO 只返回安全 `errorCode` 和友好 `errorMessage`，不返回 `streamErr.Error()`。重试接口基于原用户消息和原完成上下文创建新尝试。停止生成复用现有活动流注册表并写入 `aborted`。

新增：

```text
POST /api/admin/ai/conversations/:id/messages/:messageId/retry
POST /api/admin/ai/conversations/:id/stop
```

- [ ] **Step 4: 兼容历史 SQLite 迁移**

迁移读取只复制源库实际存在列；新字段使用零值。摘要校验对源列和目标列取交集，避免旧 SQLite 因缺少新列失败。

Run: `go test ./internal/model ./internal/service ./internal/migration/sqlitepostgres ./internal/router`

Expected: PASS；PostgreSQL 集成测试只可使用独立 `BLOG_TEST_DATABASE_DSN`。

- [ ] **Step 5: 提交并双推送**

```powershell
git status --short
git add internal/model/ai.go internal/model/ai_test.go internal/database internal/migration/sqlitepostgres internal/service/ai_conversation_service.go internal/service/ai_conversation_service_test.go internal/handler/admin_ai_handler.go internal/handler/admin_ai_handler_test.go internal/router/admin_ai_routes_test.go
git diff --cached --check
git commit -m "feat(ai): add reliable retries usage and cursors"
git push origin master
git push gitee master
```

### Task 4: 完善文章润色候选结果

**Files:**
- Modify: `internal/handler/admin_insight_handler.go`
- Modify: `internal/router/admin_dashboard_routes_test.go`
- Test: `internal/service/ai_runtime_service_test.go`

- [ ] **Step 1: 写红灯测试**

测试 `POST /api/admin/ai/beautify`：超长输入返回 422；Provider 失败返回安全错误；成功仅返回 `title/summary/content` 候选和 `usage/durationMS`，不会修改文章表。

```go
require.Equal(t, originalContent, reloadPost(t, db, post.ID).Content)
require.Equal(t, "completed", body.Data.Status)
require.NotZero(t, body.Data.DurationMS)
```

- [ ] **Step 2: 运行红灯测试**

Run: `go test ./internal/handler ./internal/router -run Beautify`

Expected: FAIL。

- [ ] **Step 3: 实现 Runtime 接入并返回候选 DTO**

```go
type BeautifyResultDTO struct {
    Title string `json:"title"`
    Summary string `json:"summary"`
    Content string `json:"content"`
    Status string `json:"status"`
    Usage ai.Usage `json:"usage"`
    DurationMS int64 `json:"durationMS"`
}
```

- [ ] **Step 4: 运行测试、提交并双推送**

Run: `go test ./internal/handler ./internal/router`

Expected: PASS。

```powershell
git status --short
git add internal/handler/admin_insight_handler.go internal/router/admin_dashboard_routes_test.go internal/service/ai_runtime_service_test.go
git diff --cached --check
git commit -m "feat(ai): return safe beautify previews"
git push origin master
git push gitee master
```

### Task 5: 增加运营报告模型与 CRUD

**Files:**
- Create: `internal/model/ai_report.go`
- Create: `internal/service/ai_report_service.go`
- Create: `internal/service/ai_report_service_test.go`
- Create: `internal/handler/admin_ai_report_handler.go`
- Create: `internal/router/admin_ai_report_routes_test.go`
- Modify: `internal/database/schema.go`
- Modify: `internal/migration/sqlitepostgres/tables.go`
- Modify: `internal/migration/sqlitepostgres/copy.go`
- Modify: `internal/migration/sqlitepostgres/validate.go`
- Modify: `internal/router/router.go`

- [ ] **Step 1: 写报告保留与隐私红灯测试**

```go
func TestAIReportServiceKeepsLatestTwenty(t *testing.T) {
    for i := 0; i < 21; i++ { createReport(t, service, 30) }
    reports, total := listReports(t, service, 1, 50)
    require.Equal(t, int64(20), total)
    require.Len(t, reports, 20)
}
```

Prompt 测试断言只包含聚合访问量、热门文章、搜索词、评论计数和异常 IP 摘要，不包含 User-Agent、邮箱、评论者 IP 明细。

- [ ] **Step 2: 运行红灯测试**

Run: `go test ./internal/service ./internal/router -run AIReport`

Expected: FAIL。

- [ ] **Step 3: 实现模型、服务和路由**

```go
type AIReport struct {
    ID uint `gorm:"primaryKey"`
    AdminID uint `gorm:"not null;index"`
    RangeDays int `gorm:"not null"`
    Status string `gorm:"size:24;not null"`
    Markdown string `gorm:"type:text"`
    Provider string
    Model string
    InputTokens int
    OutputTokens int
    TotalTokens int
    DurationMS int64
    ErrorCode string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

新增 `POST/GET /api/admin/ai/reports`、`GET/DELETE /api/admin/ai/reports/:id`、`POST /api/admin/ai/reports/:id/regenerate`。列表默认 10、最大 20。成功生成后事务内清理第 21 份及更早记录。

- [ ] **Step 4: 运行阶段全测与构建**

```powershell
go test ./internal/ai ./internal/model ./internal/service ./internal/handler ./internal/router ./internal/database ./internal/migration/sqlitepostgres
go build ./cmd/server
```

Expected: PASS；需要数据库的测试只指向独立测试库。

- [ ] **Step 5: 提交并双推送**

```powershell
git status --short
git add internal/model/ai_report.go internal/service/ai_report_service.go internal/service/ai_report_service_test.go internal/handler/admin_ai_report_handler.go internal/router/admin_ai_report_routes_test.go internal/database/schema.go internal/migration/sqlitepostgres internal/router/router.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat(ai): add operations report history"
git push origin master
git push gitee master
```
