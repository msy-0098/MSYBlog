package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type AdminAuthHandler struct {
	db        *gorm.DB
	jwtSecret string
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
	Token string       `json:"token"`
	User  AdminUserDTO `json:"user"`
}

func NewAdminAuthHandler(db *gorm.DB, jwtSecret string) AdminAuthHandler {
	return AdminAuthHandler{db: db, jwtSecret: jwtSecret}
}

func (h AdminAuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "参数错误")
		return
	}

	username := strings.TrimSpace(req.Username)
	var user model.User
	if err := h.db.Where("username = ? AND role = ?", username, model.UserRoleAdmin).First(&user).Error; err != nil {
		response.Error(c, http.StatusUnauthorized, 401, "账号或密码错误")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		response.Error(c, http.StatusUnauthorized, 401, "账号或密码错误")
		return
	}

	token, err := auth.GenerateTokenWithRole(h.jwtSecret, user.ID, user.Username, user.Role, time.Now())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.Success(c, LoginResponse{
		Token: token,
		User:  adminUserDTO(user),
	})
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

func adminUserDTO(user model.User) AdminUserDTO {
	return AdminUserDTO{ID: user.ID, Username: user.Username, Email: user.Email, Nickname: user.Nickname, Role: user.Role, CreatedAt: formatTime(user.CreatedAt)}
}
