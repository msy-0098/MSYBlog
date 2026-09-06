package handler

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/security"
)

type AdminAuthHandler struct {
	db        *gorm.DB
	jwtSecret string
	lock      *security.AccountLock
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type AdminUserDTO struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Nickname    string `json:"nickname"`
	Role        string `json:"role"`
	TotpEnabled bool   `json:"totpEnabled"`
	CreatedAt   string `json:"createdAt"`
}

type LoginResponse struct {
	User        AdminUserDTO `json:"user,omitempty"`
	Requires2FA bool         `json:"requires2FA,omitempty"`
}

func NewAdminAuthHandler(db *gorm.DB, jwtSecret string) AdminAuthHandler {
	return AdminAuthHandler{
		db:        db,
		jwtSecret: jwtSecret,
		lock:      security.NewAccountLock(5, 15*time.Minute, 15*time.Minute),
	}
}

// Login 管理员登录
// @Summary 管理员登录
// @Description 校验管理员账号与密码，若启用 2FA 则返回 requires2FA 标志并下发临时待验证凭据
// @Tags 管理员认证与安全
// @Accept json
// @Produce json
// @Param request body LoginRequest true "管理员登录表单"
// @Success 200 {object} response.Envelope{data=LoginResponse} "登录成功或要求2FA"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "账号或密码错误"
// @Failure 429 {object} response.ErrorResponse "尝试过于频繁"
// @Router /admin/login [post]
func (h AdminAuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "参数错误")
		return
	}

	username := strings.TrimSpace(req.Username)
	if remaining := h.lock.Check(username); remaining > 0 {
		seconds := int(math.Ceil(remaining.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		response.ErrorWithData(c, http.StatusTooManyRequests, "账号暂时锁定，请稍后再试", gin.H{"retryAfter": seconds})
		return
	}

	var user model.User
	if err := h.db.Where("username = ? AND role = ?", username, model.UserRoleAdmin).First(&user).Error; err != nil {
		if remaining := h.lock.Fail(username); remaining > 0 {
			seconds := int(math.Ceil(remaining.Seconds()))
			response.ErrorWithData(c, http.StatusTooManyRequests, "账号暂时锁定，请稍后再试", gin.H{"retryAfter": seconds})
			return
		}
		response.Error(c, http.StatusUnauthorized, 401, "账号或密码错误")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		if remaining := h.lock.Fail(username); remaining > 0 {
			seconds := int(math.Ceil(remaining.Seconds()))
			response.ErrorWithData(c, http.StatusTooManyRequests, "账号暂时锁定，请稍后再试", gin.H{"retryAfter": seconds})
			return
		}
		response.Error(c, http.StatusUnauthorized, 401, "账号或密码错误")
		return
	}

	if user.TotpEnabled && strings.TrimSpace(user.TotpSecret) != "" {
		// Password ok; require Google Authenticator code in a second step.
		pending, err := auth.GeneratePending2FAToken(h.jwtSecret, user.ID, time.Now())
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
			return
		}
		middleware.SetAuthCookie(c, middleware.Pending2FACookie, pending)
		response.Success(c, LoginResponse{Requires2FA: true})
		return
	}

	h.completeLogin(c, user, username)
}

type Login2FARequest struct {
	Code string `json:"code"`
}

// Login2FA 2FA 二次验证登录
// @Summary 2FA 二次验证登录
// @Description 提交动态验证码完成 2FA 登录，验证成功后签发管理端 Token 与 CSRF Token
// @Tags 管理员认证与安全
// @Accept json
// @Produce json
// @Param request body Login2FARequest true "TOTP 6位验证码"
// @Success 200 {object} response.Envelope{data=LoginResponse} "2FA 验证成功并完成登录"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "验证码错误或临时凭据失效"
// @Router /admin/login/2fa [post]
func (h AdminAuthHandler) Login2FA(c *gin.Context) {
	var req Login2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "参数错误")
		return
	}
	raw, err := c.Cookie(middleware.Pending2FACookie)
	if err != nil || strings.TrimSpace(raw) == "" {
		response.Error(c, http.StatusUnauthorized, 401, "请先完成密码登录")
		return
	}
	claims, err := auth.ParsePending2FAToken(h.jwtSecret, strings.TrimSpace(raw))
	if err != nil {
		middleware.ClearAuthCookie(c, middleware.Pending2FACookie)
		response.Error(c, http.StatusUnauthorized, 401, "二次验证已过期，请重新登录")
		return
	}

	var user model.User
	if err := h.db.First(&user, claims.UserID).Error; err != nil || user.Role != model.UserRoleAdmin {
		response.Error(c, http.StatusUnauthorized, 401, "用户不存在或 token 已失效")
		return
	}
	if !user.TotpEnabled || !auth.ValidateTOTP(user.TotpSecret, req.Code, time.Now()) {
		if remaining := h.lock.Fail(user.Username); remaining > 0 {
			seconds := int(math.Ceil(remaining.Seconds()))
			response.ErrorWithData(c, http.StatusTooManyRequests, "账号暂时锁定，请稍后再试", gin.H{"retryAfter": seconds})
			return
		}
		response.Error(c, http.StatusUnauthorized, 401, "验证码错误")
		return
	}

	middleware.ClearAuthCookie(c, middleware.Pending2FACookie)
	h.completeLogin(c, user, user.Username)
}

func (h AdminAuthHandler) completeLogin(c *gin.Context, user model.User, lockKey string) {
	token, err := auth.GenerateTokenWithRole(h.jwtSecret, user.ID, user.Username, user.Role, user.TokenVersion, time.Now())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	h.lock.Success(lockKey)
	middleware.SetAuthCookie(c, middleware.AdminTokenCookie, token)
	middleware.IssueCSRFToken(c)
	response.Success(c, LoginResponse{User: adminUserDTO(user)})
}

// Logout 管理员退出登录
// @Summary 管理员退出登录
// @Description 清除管理员 Session Cookie 与 CSRF 令牌
// @Tags 管理员认证与安全
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=object} "退出成功"
// @Router /admin/logout [post]
func (h AdminAuthHandler) Logout(c *gin.Context) {
	if claims, ok := parseOptionalClaims(c, h.jwtSecret, middleware.AdminTokenCookie); ok {
		_ = h.db.Model(&model.User{}).Where("id = ?", claims.UserID).UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
	}
	middleware.ClearAuthCookie(c, middleware.AdminTokenCookie)
	middleware.ClearAuthCookie(c, middleware.Pending2FACookie)
	middleware.ClearCSRFToken(c)
	response.Success(c, gin.H{"loggedOut": true})
}

// Profile 获取当前登录管理员信息
// @Summary 获取当前管理员资料
// @Description 获取当前已登录管理员的个人信息、权限及2FA状态
// @Tags 管理员认证与安全
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=AdminUserDTO} "个人资料"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/profile [get]
func (h AdminAuthHandler) Profile(c *gin.Context) {
	claims, ok := c.MustGet(middleware.CurrentUserKey).(*auth.Claims)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return
	}

	var user model.User
	if err := h.db.First(&user, claims.UserID).Error; err != nil {
		response.Error(c, http.StatusUnauthorized, 401, "用户不存在或 token 已失效")
		return
	}
	response.Success(c, adminUserDTO(user))
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// ChangePassword 管理员修改密码
// @Summary 管理员修改密码
// @Description 校验原密码并更新管理员账号密码，更新成功后使旧会话失效
// @Tags 管理员认证与安全
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body ChangePasswordRequest true "修改密码请求"
// @Success 200 {object} response.Envelope{data=object} "修改密码成功"
// @Failure 400 {object} response.ErrorResponse "参数错误或密码不合规"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/password [put]
func (h AdminAuthHandler) ChangePassword(c *gin.Context) {
	claims, ok := c.MustGet(middleware.CurrentUserKey).(*auth.Claims)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "参数错误")
		return
	}

	currentPassword := strings.TrimSpace(req.CurrentPassword)
	newPassword := strings.TrimSpace(req.NewPassword)
	if currentPassword == "" || len(newPassword) < 8 {
		response.Error(c, http.StatusBadRequest, 400, "新密码至少 8 位")
		return
	}
	if currentPassword == newPassword {
		response.Error(c, http.StatusBadRequest, 400, "新密码不能与当前密码相同")
		return
	}

	var user model.User
	if err := h.db.First(&user, claims.UserID).Error; err != nil {
		response.Error(c, http.StatusUnauthorized, 401, "用户不存在或 token 已失效")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, currentPassword) {
		response.Error(c, http.StatusBadRequest, 400, "当前密码错误")
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}

	if err := h.db.Model(&user).Update("password_hash", hash).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	if err := h.db.Model(&user).UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	middleware.ClearAuthCookie(c, middleware.AdminTokenCookie)

	response.Success(c, gin.H{"updated": true})
}

type Setup2FAResponse struct {
	Secret    string `json:"secret"`
	OTPAuthURL string `json:"otpauthUrl"`
}

// Setup2FA 获取2FA配置密钥
// @Summary 生成 2FA 密钥与配置二维码
// @Description 为当前管理员生成未激活的 TOTP 密钥和二维码配置 URL
// @Tags 管理员认证与安全
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=Setup2FAResponse} "生成成功"
// @Failure 400 {object} response.ErrorResponse "两步验证已启用"
// @Router /admin/2fa/setup [post]
func (h AdminAuthHandler) Setup2FA(c *gin.Context) {
	user, ok := h.currentAdmin(c)
	if !ok {
		return
	}
	if user.TotpEnabled {
		response.Error(c, http.StatusBadRequest, 400, "两步验证已启用")
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	if err := h.db.Model(&user).Update("totp_secret", secret).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	account := user.Username
	if account == "" {
		account = user.Email
	}
	response.Success(c, Setup2FAResponse{
		Secret:     secret,
		OTPAuthURL: auth.BuildOTPAuthURL("MSYBlog", account, secret),
	})
}

type Enable2FARequest struct {
	Code string `json:"code"`
}

// Enable2FA 开启两步验证
// @Summary 校验并启用两步验证
// @Description 输入 TOTP 验证码确认绑定，正式激活 2FA
// @Tags 管理员认证与安全
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body Enable2FARequest true "TOTP 验证码"
// @Success 200 {object} response.Envelope{data=object} "启用成功"
// @Failure 400 {object} response.ErrorResponse "验证码错误或未生成密钥"
// @Router /admin/2fa/enable [post]
func (h AdminAuthHandler) Enable2FA(c *gin.Context) {
	user, ok := h.currentAdmin(c)
	if !ok {
		return
	}
	var req Enable2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "参数错误")
		return
	}
	if strings.TrimSpace(user.TotpSecret) == "" {
		response.Error(c, http.StatusBadRequest, 400, "请先生成密钥")
		return
	}
	if !auth.ValidateTOTP(user.TotpSecret, req.Code, time.Now()) {
		response.Error(c, http.StatusBadRequest, 400, "验证码错误")
		return
	}
	if err := h.db.Model(&user).Update("totp_enabled", true).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	response.Success(c, gin.H{"enabled": true})
}

type Disable2FARequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// Disable2FA 关闭两步验证
// @Summary 关闭两步验证
// @Description 校验当前密码及动态码，关闭管理员 2FA 功能
// @Tags 管理员认证与安全
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body Disable2FARequest true "密码与动态码"
// @Success 200 {object} response.Envelope{data=object} "关闭成功"
// @Failure 400 {object} response.ErrorResponse "密码或验证码错误"
// @Router /admin/2fa/disable [post]
func (h AdminAuthHandler) Disable2FA(c *gin.Context) {
	user, ok := h.currentAdmin(c)
	if !ok {
		return
	}
	var req Disable2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "参数错误")
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		response.Error(c, http.StatusBadRequest, 400, "当前密码错误")
		return
	}
	if user.TotpEnabled && !auth.ValidateTOTP(user.TotpSecret, req.Code, time.Now()) {
		response.Error(c, http.StatusBadRequest, 400, "验证码错误")
		return
	}
	if err := h.db.Model(&user).Updates(map[string]any{
		"totp_enabled": false,
		"totp_secret":  "",
	}).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}
	response.Success(c, gin.H{"disabled": true})
}

func (h AdminAuthHandler) currentAdmin(c *gin.Context) (model.User, bool) {
	claims, ok := c.MustGet(middleware.CurrentUserKey).(*auth.Claims)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return model.User{}, false
	}
	var user model.User
	if err := h.db.First(&user, claims.UserID).Error; err != nil {
		response.Error(c, http.StatusUnauthorized, 401, "用户不存在或 token 已失效")
		return model.User{}, false
	}
	return user, true
}

func adminUserDTO(user model.User) AdminUserDTO {
	return AdminUserDTO{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Nickname:    user.Nickname,
		Role:        user.Role,
		TotpEnabled: user.TotpEnabled,
		CreatedAt:   formatTime(user.CreatedAt),
	}
}

func parseOptionalClaims(c *gin.Context, secret string, cookieName string) (*auth.Claims, bool) {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		claims, err := auth.ParseToken(secret, strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err == nil {
			return claims, true
		}
	}
	raw, err := c.Cookie(cookieName)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	claims, err := auth.ParseToken(secret, strings.TrimSpace(raw))
	if err != nil {
		return nil, false
	}
	return claims, true
}
