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
}

type AdminUserDTO struct {
	ID        uint   `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Nickname  string `json:"nickname"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type LoginResponse struct {
	User AdminUserDTO `json:"user"`
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

	token, err := auth.GenerateTokenWithRole(h.jwtSecret, user.ID, user.Username, user.Role, user.TokenVersion, time.Now())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}

	h.lock.Success(username)
	middleware.SetAuthCookie(c, middleware.AdminTokenCookie, token)
	middleware.IssueCSRFToken(c)
	response.Success(c, LoginResponse{User: adminUserDTO(user)})
}

func (h AdminAuthHandler) Logout(c *gin.Context) {
	if claims, ok := parseOptionalClaims(c, h.jwtSecret, middleware.AdminTokenCookie); ok {
		_ = h.db.Model(&model.User{}).Where("id = ?", claims.UserID).UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
	}
	middleware.ClearAuthCookie(c, middleware.AdminTokenCookie)
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
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	if err := h.db.Model(&user).Update("password_hash", hash).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}
	if err := h.db.Model(&user).UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}
	middleware.ClearAuthCookie(c, middleware.AdminTokenCookie)

	response.Success(c, gin.H{"updated": true})
}

func adminUserDTO(user model.User) AdminUserDTO {
	return AdminUserDTO{ID: user.ID, Username: user.Username, Email: user.Email, Nickname: user.Nickname, Role: user.Role, CreatedAt: formatTime(user.CreatedAt)}
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
