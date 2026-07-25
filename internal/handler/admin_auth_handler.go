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

func (h AdminAuthHandler) Logout(c *gin.Context) {
	if claims, ok := parseOptionalClaims(c, h.jwtSecret, middleware.AdminTokenCookie); ok {
		_ = h.db.Model(&model.User{}).Where("id = ?", claims.UserID).UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
	}
	middleware.ClearAuthCookie(c, middleware.AdminTokenCookie)
	middleware.ClearAuthCookie(c, middleware.Pending2FACookie)
	middleware.ClearCSRFToken(c)
	response.Success(c, gin.H{"loggedOut": true})
}

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
