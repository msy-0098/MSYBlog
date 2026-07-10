# 访客认证与评论审核闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成安全验证码注册、访客登录、待审核评论提交和管理端通过/隐藏/删除的完整链路。

**Architecture:** 后端以数据库验证码记录和小型进程内限流器约束注册入口，评论增加 `pending` 状态并由公开查询过滤。前端继续复用现有文章评论组件和管理端评论页，增加字段校验、验证码冷却、会话失效与审核状态交互。

**Tech Stack:** Go、Gin、GORM、PostgreSQL、JWT、Vue 3、TypeScript、Element Plus、Vitest、pnpm

---

## File Map

- Create: `internal/security/keyed_limiter.go` - 线程安全的键控固定窗口限流器。
- Create: `internal/security/keyed_limiter_test.go` - 限流窗口和键隔离测试。
- Modify: `internal/handler/visitor_auth_handler.go` - 安全验证码、冷却、清理和输入校验。
- Modify: `internal/handler/visitor_auth_handler_test.go` - 验证码生成、邮箱和 SMTP 行为测试。
- Modify: `internal/model/comment.go` - 新增 `pending` 状态并调整默认值。
- Modify: `internal/handler/comment_handler.go` - 待审核创建与管理状态机。
- Modify: `internal/router/router.go` - 为访客认证处理器注入共享限流器。
- Modify: `internal/router/visitor_comment_routes_test.go` - 端到端注册和评论审核测试。
- Modify: `../frontend/src/api/blog.ts` - 评论状态类型与访客 401 处理。
- Modify: `../frontend/src/api/blog.test.ts` - API 契约测试。
- Create: `../frontend/src/components/blog/PostComments.test.ts` - 登录注册、冷却、待审核和会话失效测试。
- Modify: `../frontend/src/components/blog/PostComments.vue` - 评论闭环交互。
- Modify: `../frontend/src/api/admin.ts` - 管理评论 `pending` 类型。
- Create: `../frontend/src/views/admin/AdminCommentsView.test.ts` - 审核操作测试。
- Modify: `../frontend/src/views/admin/AdminCommentsView.vue` - 通过、隐藏和删除交互。

### Task 1: 用测试定义键控限流器

- [ ] **Step 1: 创建失败测试**

`internal/security/keyed_limiter_test.go`：

```go
func TestKeyedLimiterBlocksAfterLimitAndResetsAfterWindow(t *testing.T) {
    now := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
    limiter := NewKeyedLimiter(2, time.Minute)
    limiter.now = func() time.Time { return now }

    if !limiter.Allow("ip:email") || !limiter.Allow("ip:email") {
        t.Fatal("expected first two requests to pass")
    }
    if limiter.Allow("ip:email") {
        t.Fatal("expected third request to be limited")
    }
    now = now.Add(time.Minute)
    if !limiter.Allow("ip:email") {
        t.Fatal("expected request after window to pass")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/security -run TestKeyedLimiter -v
```

Expected: FAIL，包或 `NewKeyedLimiter` 尚不存在。

- [ ] **Step 3: 实现最小限流器**

```go
type KeyedLimiter struct {
    mu sync.Mutex
    limit int
    window time.Duration
    now func() time.Time
    entries map[string]entry
}

func (l *KeyedLimiter) Allow(key string) bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    current := l.now()
    item := l.entries[key]
    if item.started.IsZero() || current.Sub(item.started) >= l.window {
        l.entries[key] = entry{started: current, count: 1}
        return true
    }
    if item.count >= l.limit { return false }
    item.count++
    l.entries[key] = item
    return true
}
```

- [ ] **Step 4: 运行测试确认通过**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/security -v
```

Expected: PASS，且不同 key 互不影响的测试也通过。

### Task 2: 用测试定义安全验证码规则

- [ ] **Step 1: 在 handler 测试中增加验证码格式与非固定性测试**

```go
func TestGenerateEmailCodeReturnsSixDigits(t *testing.T) {
    first, err := generateEmailCode()
    if err != nil || !regexp.MustCompile(`^\d{6}$`).MatchString(first) {
        t.Fatalf("unexpected code %q: %v", first, err)
    }
    different := false
    for i := 0; i < 10; i++ {
        next, _ := generateEmailCode()
        different = different || next != first
    }
    if !different { t.Fatal("expected cryptographically generated codes to vary") }
}
```

再增加无效邮箱、同邮箱一分钟冷却、旧记录清理、验证码单次使用，以及同一 IP 与邮箱组合连续登录失败后返回 HTTP 429 的测试。测试通过直接准备 `EmailVerificationCode.CodeHash`，不依赖公开固定验证码。

- [ ] **Step 2: 运行测试确认失败**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/handler -run 'TestGenerateEmailCode|TestVisitorEmailCode' -v
```

Expected: FAIL，当前使用 `math/rand` 且未实现冷却和清理。

- [ ] **Step 3: 替换验证码生成与邮箱校验**

```go
import cryptorand "crypto/rand"

func generateEmailCode() (string, error) {
    limit := big.NewInt(1_000_000)
    value, err := cryptorand.Int(cryptorand.Reader, limit)
    if err != nil { return "", err }
    return fmt.Sprintf("%06d", value.Int64()), nil
}

func validEmail(value string) bool {
    parsed, err := mail.ParseAddress(value)
    return err == nil && parsed.Address == value && len(value) <= 180
}
```

`SendEmailCode` 在创建前查询最近一分钟记录，命中时返回 HTTP 429；随后删除该邮箱 `used_at IS NOT NULL OR expires_at <= now` 的记录。验证码发送限流 key 使用 `"code:" + c.ClientIP() + ":" + email`，每分钟最多 3 次。

- [ ] **Step 4: 收紧注册字段**

昵称限制 1-80 个 rune，密码限制 8-128 字节；验证码成功后再创建用户。`Login` 在查询用户前使用 `"login:" + c.ClientIP() + ":" + email`，每分钟最多 5 次，超限返回 HTTP 429。`router.New` 创建并注入验证码与登录两个限流器，生产 SMTP 校验保持现有规则。

- [ ] **Step 5: 运行 handler 与 config 测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/handler ./internal/config -v
```

Expected: PASS。

### Task 3: 用路由测试定义待审核评论状态机

- [ ] **Step 1: 修改端到端测试预期**

创建评论后断言：

```go
if createCommentBody.Data.Status != model.CommentStatusPending {
    t.Fatalf("expected pending comment, got %q", createCommentBody.Data.Status)
}
publicBeforeApproval := performRequest(engine, http.MethodGet, postCommentsURL)
decodeJSON(t, publicBeforeApproval, &publicBody)
if publicBody.Data.Total != 0 { t.Fatal("pending comment must not be public") }
```

管理员 PUT `{"status":"approved"}` 后断言公开列表为 1，再 PUT `{"status":"hidden"}` 后断言为 0。

- [ ] **Step 2: 运行路由测试确认失败**

```powershell
if (-not $env:BLOG_TEST_DATABASE_DSN) { Write-Output 'BLOG_TEST_DATABASE_DSN 未配置，路由集成测试将按设计跳过' }
& 'D:\AllTools\Go\go\bin\go.exe' test ./internal/router -run 'TestVisitorRegistrationLoginAndCommentFlow|TestAdminDashboardAndCommentManagement' -v
```

Expected: FAIL，当前创建状态为 `approved`。

- [ ] **Step 3: 实现评论状态**

```go
const (
    CommentStatusPending  = "pending"
    CommentStatusApproved = "approved"
    CommentStatusHidden   = "hidden"
)
```

模型默认值改为 `pending`；创建评论显式写入 `pending`；公开列表继续只查 `approved`；管理更新只接受上述三种状态，并允许待审核转通过或隐藏。

- [ ] **Step 4: 管理列表优先待审核**

查询排序改为：

```go
Order("CASE WHEN status = 'pending' THEN 0 ELSE 1 END, created_at DESC")
```

- [ ] **Step 5: 运行后端完整测试**

```powershell
& 'D:\AllTools\Go\go\bin\go.exe' test ./...
```

Expected: PASS；若本机未配置 `BLOG_TEST_DATABASE_DSN`，PostgreSQL 路由集成测试明确显示 SKIP，其余测试通过。

### Task 4: 提交并推送后端认证与审核改动

- [ ] **Step 1: 检查、提交和推送**

```powershell
git status --short
git diff --check
git add internal/security internal/handler/visitor_auth_handler.go internal/handler/visitor_auth_handler_test.go internal/model/comment.go internal/handler/comment_handler.go internal/router/router.go internal/router/visitor_comment_routes_test.go
git commit -m "feat: secure visitor comment review flow"
git push origin master
git push gitee master
```

Expected: 仅后端源码和测试进入提交，两个远端推送成功。

### Task 5: 用前端失败测试定义访客评论交互

- [ ] **Step 1: 创建 `PostComments.test.ts`**

至少覆盖：

```ts
it('shows pending feedback without adding the comment publicly', async () => {
  localStorage.setItem('visitor_token', 'visitor-token')
  localStorage.setItem('visitor_user', JSON.stringify(visitor))
  vi.mocked(createPostComment).mockResolvedValue(makeComment('pending'))
  const wrapper = mountComments()
  await flushPromises()

  await wrapper.get('[data-test="comment-content"]').setValue('等待审核的评论')
  await wrapper.get('[data-test="submit-comment"]').trigger('click')
  await flushPromises()

  expect(wrapper.text()).toContain('审核通过后会公开显示')
  expect(wrapper.text()).not.toContain('等待审核的评论')
})
```

另加注册字段校验、发送验证码 60 秒冷却、401 后清理会话并打开登录面板的测试。

- [ ] **Step 2: 更新 API 类型失败断言**

将 `PostComment.status` 与 `AdminComment.status` 扩展为 `'pending' | 'approved' | 'hidden'`，API 测试模拟创建返回 `pending`。

- [ ] **Step 3: 运行测试确认失败**

```powershell
pnpm exec vitest run src/components/blog/PostComments.test.ts src/api/blog.test.ts src/api/admin.test.ts
```

Expected: FAIL，当前没有测试标识、冷却和待审核反馈。

### Task 6: 实现访客端交互

- [ ] **Step 1: 拆分消息状态并增加表单校验**

在 `PostComments.vue` 使用 `notice` 和 `error` 两个 ref；注册前校验邮箱、昵称、8-128 位密码和六位验证码，登录前校验邮箱和密码。

- [ ] **Step 2: 实现验证码倒计时**

```ts
const codeCooldown = ref(0)
let cooldownTimer: number | undefined

function startCodeCooldown() {
  codeCooldown.value = 60
  cooldownTimer = window.setInterval(() => {
    codeCooldown.value -= 1
    if (codeCooldown.value <= 0 && cooldownTimer) window.clearInterval(cooldownTimer)
  }, 1000)
}
```

组件卸载时清理 timer；按钮文案为 `60 秒后重发`，冷却期间禁用。

- [ ] **Step 3: 实现待审核和 401 行为**

评论成功后只清空输入并设置提示，不立即把返回对象加入公开列表。错误对象响应状态为 401 时调用 `logoutVisitor()` 并打开登录面板。

- [ ] **Step 4: 增加稳定测试标识**

正文输入、提交、消息分别使用 `comment-content`、`submit-comment`、`comment-notice`。

- [ ] **Step 5: 运行访客端测试**

```powershell
pnpm exec vitest run src/components/blog/PostComments.test.ts src/views/PostDetailView.test.ts src/api/blog.test.ts
```

Expected: PASS。

### Task 7: 实现管理端评论审核交互

- [ ] **Step 1: 创建管理页失败测试**

测试待审核评论显示“待审核”，点击“通过”调用：

```ts
expect(updateAdminComment).toHaveBeenCalledWith(9, 'approved')
```

再验证已通过评论可隐藏、三种状态标签文案正确。

- [ ] **Step 2: 运行测试确认失败**

```powershell
pnpm exec vitest run src/views/admin/AdminCommentsView.test.ts
```

Expected: FAIL，当前只区分显示和隐藏。

- [ ] **Step 3: 实现审核按钮与状态标签**

`pending` 行显示“通过”和“隐藏”；`approved` 行显示“隐藏”；`hidden` 行显示“重新通过”。所有操作成功后重新加载当前页。

- [ ] **Step 4: 运行完整前端测试与构建**

```powershell
pnpm test
pnpm build
```

Expected: 全部测试通过且构建成功。

### Task 8: 提交并推送前端认证与审核改动

- [ ] **Step 1: 检查、提交和推送**

```powershell
git status --short
git diff --check
git add src/api/blog.ts src/api/blog.test.ts src/api/admin.ts src/api/admin.test.ts src/components/blog/PostComments.vue src/components/blog/PostComments.test.ts src/views/admin/AdminCommentsView.vue src/views/admin/AdminCommentsView.test.ts src/views/PostDetailView.test.ts
git commit -m "feat: complete visitor comment review experience"
git push origin master
git push gitee master
```

Expected: 不含依赖、构建、缓存或敏感文件，两个前端远端推送成功。
