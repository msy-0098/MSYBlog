# Phase 1 Backend: Verification, Friendly Errors, and Branded Mail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为验证码发送增加真实冷却与可恢复等待时间，建立安全错误契约，并发送带 CID MSY Logo 的纯文本/HTML 双版本邮件。

**Architecture:** 先扩展统一响应能力，再以独立 service 管理“邮箱+用途”冷却，并把邮件构建与 SMTP 发送从 Handler 拆开。路由级 IP 限流保留为粗粒度防刷，验证码完整冷却只在 SMTP 成功后登记。

**Tech Stack:** Go 1.25、Gin、GORM/PostgreSQL、net/smtp、mime/multipart、go:embed、testing/httptest。

---

**配套前端计划：** `frontend/docs/superpowers/plans/2026-07-23-phase-1-verification-errors-mail-frontend.md`。先完成并推送本计划，再执行前端计划。

### Task 1: 扩展安全错误响应与 retryAfter

**Files:**
- Create: `internal/response/error.go`
- Create: `internal/response/error_test.go`
- Modify: `internal/response/response.go`
- Modify: `internal/middleware/ratelimit.go`
- Test: `internal/middleware/ratelimit_test.go`

- [ ] **Step 1: 写失败测试，固定错误数据契约**

```go
func TestErrorWithData(t *testing.T) {
    recorder := httptest.NewRecorder()
    ctx, _ := gin.CreateTestContext(recorder)
    response.ErrorWithData(ctx, http.StatusTooManyRequests, "验证码发送过于频繁，请稍后再试", gin.H{"retryAfter": 37})

    var body struct {
        Code int            `json:"code"`
        Message string      `json:"message"`
        Data map[string]int `json:"data"`
    }
    require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
    require.Equal(t, 429, body.Code)
    require.Equal(t, 37, body.Data["retryAfter"])
}
```

- [ ] **Step 2: 运行测试并确认红灯**

Run: `go test ./internal/response ./internal/middleware`

Expected: FAIL，提示 `response.ErrorWithData` 未定义。

- [ ] **Step 3: 实现错误数据响应和限流剩余秒数**

```go
type Envelope struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    any    `json:"data"`
}

func ErrorWithData(c *gin.Context, status int, message string, data any) {
    c.AbortWithStatusJSON(status, Envelope{Code: status, Message: message, Data: data})
}

type LimitDecision struct {
    Allowed    bool
    RetryAfter time.Duration
}
```

让 `RateLimiter.Allow` 返回 `LimitDecision`；中间件拒绝请求时返回向上取整秒数，并修复现有乱码提示。不要把内部错误或路径写进响应。

- [ ] **Step 4: 运行单元测试并确认绿灯**

Run: `go test ./internal/response ./internal/middleware`

Expected: PASS。

- [ ] **Step 5: 检查、提交并双推送**

```powershell
git status --short
git add internal/response/response.go internal/response/error.go internal/response/error_test.go internal/middleware/ratelimit.go internal/middleware/ratelimit_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat(api): add safe retryable error responses"
git push origin master
git push gitee master
```

### Task 2: 增加验证码邮箱用途冷却

**Files:**
- Create: `internal/service/verification_code_limiter.go`
- Create: `internal/service/verification_code_limiter_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `config.yaml`
- Modify: `internal/handler/visitor_auth_handler.go`
- Modify: `internal/handler/visitor_auth_handler_test.go`
- Modify: `internal/router/router.go`

- [ ] **Step 1: 写 limiter 红灯测试**

```go
func TestVerificationCodeLimiterSeparatesPurpose(t *testing.T) {
    now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
    limiter := service.NewVerificationCodeLimiter(60*time.Second, func() time.Time { return now })

    limiter.MarkSent("USER@example.com", "register")
    require.Equal(t, 60*time.Second, limiter.RetryAfter("user@example.com", "register"))
    require.Zero(t, limiter.RetryAfter("user@example.com", "reset"))
}
```

同时覆盖过期清理、不同邮箱隔离和 SMTP 失败不登记冷却。

- [ ] **Step 2: 运行测试并确认红灯**

Run: `go test ./internal/service ./internal/handler`

Expected: FAIL，提示 `NewVerificationCodeLimiter` 未定义或 Handler 响应缺少冷却字段。

- [ ] **Step 3: 实现 limiter 和严格用途校验**

```go
type VerificationCodeLimiter struct {
    mu       sync.Mutex
    cooldown time.Duration
    now      func() time.Time
    sentAt   map[string]time.Time
}

func cooldownKey(email, purpose string) string {
    return strings.ToLower(strings.TrimSpace(email)) + "\x00" + purpose
}

func ParseCodePurpose(raw string) (string, bool) {
    switch strings.TrimSpace(raw) {
    case "register", "reset":
        return raw, true
    default:
        return "", false
    }
}
```

在 `SendEmailCode` 中按以下顺序执行：解析 JSON → 标准化邮箱 → 严格用途校验 → 查询邮箱用途冷却 → 防枚举判断 → 生成并发送 → SMTP 成功后 `MarkSent` → 返回 `sent/cooldownSeconds/expiresIn`。未知重置邮箱也返回相同结构，但仍登记冷却，避免枚举和轰炸。

- [ ] **Step 4: 固定响应测试**

```go
require.Equal(t, 60, body.Data.CooldownSeconds)
require.Equal(t, 600, body.Data.ExpiresIn)
require.True(t, body.Data.Sent)
```

Run: `go test ./internal/service ./internal/handler ./internal/router`

Expected: PASS；若未设置独立 `BLOG_TEST_DATABASE_DSN`，数据库集成测试允许显示 SKIP。

- [ ] **Step 5: 检查、提交并双推送**

```powershell
git status --short
git add config.yaml internal/config/config.go internal/config/config_test.go internal/service/verification_code_limiter.go internal/service/verification_code_limiter_test.go internal/handler/visitor_auth_handler.go internal/handler/visitor_auth_handler_test.go internal/router/router.go
git diff --cached --check
git commit -m "feat(auth): enforce verification code cooldown"
git push origin master
git push gitee master
```

### Task 3: 构建带 CID Logo 的双版本验证码邮件

**Files:**
- Create: `internal/mail/sender.go`
- Create: `internal/mail/smtp_sender.go`
- Create: `internal/mail/verification_email.go`
- Create: `internal/mail/verification_email_test.go`
- Create: `internal/mail/assets/msy-logo.png`
- Modify: `internal/handler/visitor_auth_handler.go`
- Modify: `internal/handler/visitor_auth_handler_test.go`

- [ ] **Step 1: 生成仓库内邮件专用 Logo**

使用仓库外用户提供的 PNG 作为输入，在仓库外运行以下一次性脚本，按亮度识别银色标识、增加 40px 黑色边距并压缩；只复制输出 `internal/mail/assets/msy-logo.png` 到仓库。

```python
from PIL import Image
src = Image.open(INPUT_PATH).convert("RGB")
gray = src.convert("L")
mask = gray.point(lambda p: 255 if p > 35 else 0)
bbox = mask.getbbox()
if bbox is None:
    raise RuntimeError("logo foreground not found")
left, top, right, bottom = bbox
pad = 40
crop = src.crop((max(0,left-pad), max(0,top-pad), min(src.width,right+pad), min(src.height,bottom+pad)))
crop.save(OUTPUT_PATH, format="PNG", optimize=True)
```

用图片查看工具确认无裁切、黑底完整、横向标识清晰；不得提交原图或聊天缓存路径。

- [ ] **Step 2: 写 MIME 红灯测试**

```go
func TestBuildVerificationEmailContainsAlternativeAndCIDLogo(t *testing.T) {
    msg, err := mail.BuildVerificationEmail(mail.VerificationEmail{
        To: "user@example.com", Subject: "注册验证码", Code: "123456", Purpose: "register",
    })
    require.NoError(t, err)
    text := string(msg)
    require.Contains(t, text, "multipart/related")
    require.Contains(t, text, "multipart/alternative")
    require.Contains(t, text, "Content-ID: <msy-logo>")
    require.Contains(t, text, "cid:msy-logo")
    require.Contains(t, text, "Content-Type: text/plain; charset=UTF-8")
    require.Contains(t, text, "Content-Type: text/html; charset=UTF-8")
}
```

- [ ] **Step 3: 运行测试并确认红灯**

Run: `go test ./internal/mail ./internal/handler`

Expected: FAIL，邮件包和构建函数尚不存在。

- [ ] **Step 4: 实现 Sender、Embed 与 MIME 构建**

```go
//go:embed assets/msy-logo.png
var logoPNG []byte

type Sender interface {
    Send(ctx context.Context, to string, message []byte) error
}

type VerificationEmail struct {
    To, Subject, Code, Purpose string
}
```

`BuildVerificationEmail` 使用随机 boundary，外层 `multipart/related`，内层 `multipart/alternative`，正文含 10 分钟有效期和安全提醒；图片部分使用 `Content-ID: <msy-logo>`、`Content-Disposition: inline` 和 base64 每 76 字符换行。`SMTPSender` 封装现有 `smtp.SendMail`。

- [ ] **Step 5: 注入 Sender 并验证失败不泄漏**

Handler 构造函数接收 `mail.Sender`；测试使用 fake sender。SMTP 失败仅返回“验证码暂时无法发送，请稍后再试”，日志不包含凭据或验证码。

Run: `go test ./internal/mail ./internal/handler`

Expected: PASS。

- [ ] **Step 6: 全阶段验证、提交并双推送**

```powershell
go test ./internal/response ./internal/middleware ./internal/service ./internal/mail ./internal/handler ./internal/router
go build ./cmd/server
git status --short
git add internal/mail internal/handler/visitor_auth_handler.go internal/handler/visitor_auth_handler_test.go
git diff --cached --name-status
git diff --cached --check
git commit -m "feat(mail): embed branded verification email"
git push origin master
git push gitee master
```

Expected: 测试与构建 PASS；未跟踪诊断脚本、数据库、上传文件、日志和配置秘密保持未暂存。
