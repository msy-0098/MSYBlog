# 管理端 AI 会话与 SSE 流式服务 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 PostgreSQL 持久化 AI 会话与消息、DeepSeek SSE 流式输出，并让 dashboard 不再等待 AI。

**Architecture:** `ai.Client` 增加可注入的流式接口；会话 service 负责消息事务、数据库上下文和状态；handler 分离 JSON 会话 CRUD 与 SSE。dashboard 仅取普通统计，AI 洞察迁为独立请求。

**Tech Stack:** Go、Gin、GORM、PostgreSQL、DeepSeek SSE、httptest、GitHub Actions。

---

## File Map

- Create: `internal/model/ai.go`, `internal/model/ai_test.go`
- Create: `internal/service/ai_conversation_service.go`, `internal/service/ai_conversation_service_test.go`
- Create: `internal/handler/admin_ai_handler.go`
- Create: `internal/response/sse.go`, `internal/response/sse_test.go`
- Create: `internal/ai/deepseek_stream_test.go`
- Create: `internal/router/admin_ai_routes_test.go`, `internal/router/admin_dashboard_routes_test.go`
- Modify: `internal/ai/deepseek.go`, `internal/database/schema.go`, `internal/database/database.go`
- Modify: `internal/router/test_database_test.go`, `internal/router/router.go`
- Modify: `internal/handler/admin_dashboard_handler.go`, `internal/handler/admin_insight_handler.go`

### Task 1: AI 模型与统一迁移注册

**Files:**
- Create: `internal/model/ai.go`, `internal/model/ai_test.go`
- Modify: `internal/database/schema.go`, `internal/database/database.go`, `internal/router/test_database_test.go`

- [ ] **Step 1: 写失败模型测试**

```go
func TestAIModelsEnforceConversationSequence(t *testing.T) {
    db := testPostgresDatabase(t)
    require.NoError(t, database.AutoMigrate(db))
    conversation := model.AIConversation{Title: "新对话", TitleMode: model.AIConversationTitleModeAuto, CreatedBy: 1, Model: "deepseek-chat"}
    require.NoError(t, db.Create(&conversation).Error)
    require.NoError(t, db.Create(&model.AIMessage{ConversationID: conversation.ID, Role: model.AIMessageRoleUser, Content: "hi", Status: model.AIMessageStatusCompleted, Sequence: 1}).Error)
    require.Error(t, db.Create(&model.AIMessage{ConversationID: conversation.ID, Role: model.AIMessageRoleAssistant, Content: "duplicate", Status: model.AIMessageStatusCompleted, Sequence: 1}).Error)
}
```

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/model -run TestAIModelsEnforceConversationSequence -v
```

Expected: FAIL，因为模型和 schema registry 尚不存在。

- [ ] **Step 3: 实现模型与 registry**

```go
const (
    AIConversationTitleModeAuto = "auto"
    AIConversationTitleModeManual = "manual"
    AIMessageRoleUser = "user"
    AIMessageRoleAssistant = "assistant"
    AIMessageStatusStreaming = "streaming"
    AIMessageStatusCompleted = "completed"
    AIMessageStatusAborted = "aborted"
    AIMessageStatusFailed = "failed"
)

type AIConversation struct { ID uint `gorm:"primaryKey"`; Title string `gorm:"not null"`; TitleMode string `gorm:"not null"`; CreatedBy uint `gorm:"index;not null"`; Model string `gorm:"not null"`; MessageCount int `gorm:"not null;default:0"`; LastMessageAt *time.Time; CreatedAt time.Time; UpdatedAt time.Time }
type AIMessage struct { ID uint `gorm:"primaryKey"`; ConversationID uint `gorm:"not null;index;uniqueIndex:idx_ai_message_sequence"`; Role string `gorm:"not null"`; Content string `gorm:"not null"`; Status string `gorm:"not null"`; Sequence int `gorm:"not null;uniqueIndex:idx_ai_message_sequence"`; Model string; ErrorMessage string; CreatedAt time.Time; UpdatedAt time.Time }
```

`database.Models()` ??? `SiteSetting`?`User`?`EmailVerificationCode`?`Category`?`Tag`?`Post`?`Comment`?`Project`?`Upload`?`AccessLog`?`IPBan`?`AIConversation`?`AIMessage` ????????`AutoMigrate(db)` ?? `db.AutoMigrate(Models()...)`?`database.Open` ??????router ??? `ai_messages`?`ai_conversations`?`users` ??????

- [ ] **Step 4: 验证通过并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/model ./internal/database -v
git status --short
git add internal/model/ai.go internal/model/ai_test.go internal/database/schema.go internal/database/database.go internal/router/test_database_test.go
git commit -m "feat: add AI conversation models"
git push origin master
git push gitee master
```

Expected: PASS 或未配置 PostgreSQL 测试 DSN 时明确 SKIP。

### Task 2: DeepSeek StreamChat 与上游 SSE 解析

**Files:**
- Modify: `internal/ai/deepseek.go`
- Create: `internal/ai/deepseek_stream_test.go`

- [ ] **Step 1: 写失败的 stream:true 测试**

```go
func TestClientStreamChatEmitsDeltas(t *testing.T) {
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var body chatRequest
        require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
        require.True(t, body.Stream)
        w.Header().Set("Content-Type", "text/event-stream")
        fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
        fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n\ndata: [DONE]\n\n")
    }))
    defer upstream.Close()
    var got strings.Builder
    client := NewClient(Config{APIKey: "test", Model: "deepseek-chat", BaseURL: upstream.URL})
    require.NoError(t, client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error { _, err := got.WriteString(delta); return err }))
    require.Equal(t, "你好世界", got.String())
}
```

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/ai -run Stream -v
```

Expected: FAIL，因为 `StreamChat` 不存在。

- [ ] **Step 3: 实现接口和增量读取**

```go
type ChatClient interface {
    Configured() bool
    Chat(context.Context, []Message) (string, error)
    StreamChat(context.Context, []Message, func(string) error) error
}
var _ ChatClient = (*Client)(nil)
```

`StreamChat` 必须使用 `http.NewRequestWithContext` 与 `stream:true`，`bufio.Scanner` 逐行解析 `data:`，忽略空行和 keepalive，遇 `[DONE]` 返回。追加非 2xx、坏 JSON、callback error、context cancel 测试。

- [ ] **Step 4: 验证并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/ai -v
git status --short
git add internal/ai/deepseek.go internal/ai/deepseek_stream_test.go
git commit -m "feat: stream DeepSeek chat responses"
git push origin master
git push gitee master
```

### Task 3: 会话 service 的 CRUD、上下文和消息状态

**Files:**
- Create: `internal/service/ai_conversation_service.go`, `internal/service/ai_conversation_service_test.go`

- [ ] **Step 1: 写失败 service 测试**

```go
func TestConversationServicePersistsUserAndStreamedAssistant(t *testing.T) {
    service, db := newAIConversationService(t, fakeChatClient{Deltas: []string{"第一段", "第二段"}})
    conversation, err := service.Create(context.Background(), 7)
    require.NoError(t, err)
    message, err := service.StreamMessage(context.Background(), 7, conversation.ID, "分析流量", func(string) error { return nil })
    require.NoError(t, err)
    require.Equal(t, model.AIMessageStatusCompleted, message.Status)
    require.Equal(t, "第一段第二段", message.Content)
    var rows []model.AIMessage
    require.NoError(t, db.Where("conversation_id = ?", conversation.ID).Order("sequence").Find(&rows).Error)
    require.Equal(t, []string{"user", "assistant"}, []string{rows[0].Role, rows[1].Role})
}
```

同时覆盖：不同管理员不可读取、手动标题不被覆盖、delete 级联、clear 只删本人、callback 中止为 `aborted`、上游失败为 `failed`。

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/service -run Conversation -v
```

Expected: FAIL，因为 service 不存在。

- [ ] **Step 3: 最小实现 service**

```go
type AIConversationService struct { db *gorm.DB; aiClient ai.ChatClient; model string }
func (s *AIConversationService) List(ctx context.Context, adminID uint) ([]model.AIConversation, error)
func (s *AIConversationService) Create(ctx context.Context, adminID uint) (model.AIConversation, error)
func (s *AIConversationService) Get(ctx context.Context, adminID, conversationID uint) (model.AIConversation, []model.AIMessage, error)
func (s *AIConversationService) Rename(ctx context.Context, adminID, conversationID uint, title string) (model.AIConversation, error)
func (s *AIConversationService) Delete(ctx context.Context, adminID, conversationID uint) error
func (s *AIConversationService) Clear(ctx context.Context, adminID uint) error
func (s *AIConversationService) StreamMessage(ctx context.Context, adminID, conversationID uint, content string, onDelta func(string) error) (model.AIMessage, error)
```

事务中写 user 和 `streaming` assistant；提交后读取数据库最近 20 条成功/中止消息，后端追加 system prompt，再调用 `StreamChat`。内存累积 delta，完成/中止/失败更新同一 assistant 记录、会话计数和 `LastMessageAt`。

- [ ] **Step 4: 验证并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/service -v
git status --short
git add internal/service/ai_conversation_service.go internal/service/ai_conversation_service_test.go
git commit -m "feat: persist AI conversations"
git push origin master
git push gitee master
```

### Task 4: JSON 会话 API、SSE handler 与 router 注入

**Files:**
- Create: `internal/response/sse.go`, `internal/response/sse_test.go`
- Create: `internal/handler/admin_ai_handler.go`
- Create: `internal/router/admin_ai_routes_test.go`
- Modify: `internal/router/router.go`

- [ ] **Step 1: 写失败路由测试**

```go
func TestAdminAIRoutesRequireJWTAndEmitSSE(t *testing.T) {
    engine := newAdminAITestEngine(t, fakeChatClient{Deltas: []string{"A", "B"}})
    require.Equal(t, http.StatusUnauthorized, performRequest(engine, http.MethodGet, "/api/admin/ai/conversations").Code)
    token := loginAndGetToken(t, engine)
    created := performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, token)
    id := decodeConversationID(t, created)
    streamed := performJSONRequest(engine, http.MethodPost, fmt.Sprintf("/api/admin/ai/conversations/%d/messages/stream", id), map[string]any{"content": "hi"}, token)
    require.Equal(t, "text/event-stream", strings.Split(streamed.Header().Get("Content-Type"), ";")[0])
    require.Contains(t, streamed.Body.String(), "event: meta")
    require.Contains(t, streamed.Body.String(), "event: delta")
    require.Contains(t, streamed.Body.String(), "event: done")
}
```

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/router -run AdminAI -v
```

Expected: FAIL，因为 handler 和 routes 不存在。

- [ ] **Step 3: 实现 SSE helper 和 handler**

```go
func PrepareSSE(c *gin.Context) { c.Header("Content-Type", "text/event-stream"); c.Header("Cache-Control", "no-cache"); c.Header("Connection", "keep-alive"); c.Header("X-Accel-Buffering", "no") }
func WriteSSE(c *gin.Context, event string, data any) error { encoded, err := json.Marshal(data); if err != nil { return err }; _, err = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, encoded); c.Writer.Flush(); return err }
```

`router.Dependencies` 加 `AIClient ai.ChatClient`，空值时构造 `ai.NewClient`。在现有 JWT admin group 注册 GET/POST/PATCH/DELETE conversations、POST stream。SSE 不使用 JSON envelope；所有权、JSON 参数和异常仍要有稳定错误码。

- [ ] **Step 4: 验证并提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/response ./internal/router -run 'SSE|AdminAI' -v
git status --short
git add internal/response/sse.go internal/response/sse_test.go internal/handler/admin_ai_handler.go internal/router/router.go internal/router/admin_ai_routes_test.go
git commit -m "feat: expose streaming AI conversation API"
git push origin master
git push gitee master
```

### Task 5: Dashboard 与 AI 洞察解耦

**Files:**
- Modify: `internal/handler/admin_dashboard_handler.go`, `internal/handler/admin_insight_handler.go`, `internal/router/router.go`
- Create: `internal/router/admin_dashboard_routes_test.go`

- [ ] **Step 1: 写失败回归测试**

```go
func TestDashboardDoesNotCallAIClient(t *testing.T) {
    engine := newAdminDashboardTestEngine(t, fakeChatClient{PanicOnChat: true})
    token := loginAndGetToken(t, engine)
    response := performJSONRequest(engine, http.MethodGet, "/api/admin/dashboard", nil, token)
    require.Equal(t, http.StatusOK, response.Code)
    require.NotContains(t, response.Body.String(), "aiAnalysis")
}
```

追加 `POST /api/admin/ai/insights/generate` 成功和 AI 未配置失败用例。

- [ ] **Step 2: 运行失败测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/router -run Dashboard -v
```

Expected: FAIL，因为 `GetDashboard` 仍调用 `analyze`。

- [ ] **Step 3: 最小实现与路由替换**

从 `DashboardDTO` 移除 `AIAnalysis`，Dashboard handler 仅持有 db。`AdminInsightHandler.GenerateInsights` 独立读取 stats/analytics 后使用普通 `Chat` 返回 JSON envelope；保留 beautify。删除旧 `/ai/chat` 路由或返回 410，前端以后仅用 conversations stream。

- [ ] **Step 4: 最终后端验证与提交**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./...
& 'D:\AllTools\Go\go\bin\go.exe' build ./cmd/server
git status --short
git add internal/handler/admin_dashboard_handler.go internal/handler/admin_insight_handler.go internal/router/router.go internal/router/admin_dashboard_routes_test.go
git commit -m "feat: load admin AI insights separately"
git push origin master
git push gitee master
```

## Final Verification

- [ ] Run:

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./...
$env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'; & 'D:\AllTools\Go\go\bin\go.exe' build -trimpath -ldflags='-s -w' -o blog-server ./cmd/server
```

Expected: PASS；删除验证产生的 `blog-server`，且不提交二进制、数据库、环境文件或日志。
