package router

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/handler"
	blogmail "masenyu.top/blog/backend/internal/mail"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

type Dependencies struct {
	Config   config.Config
	Database *gorm.DB
	AIClient ai.ChatClient
}

func New(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery(), middleware.NewAccessTracker(deps.Database).Middleware(), middleware.PublicReadCache())
	engine.Static("/uploads", "uploads")

	aiClient := deps.AIClient
	if aiClient == nil {
		var err error
		aiClient, err = ai.NewConfiguredClient(ai.Config{Provider: deps.Config.AI.Provider, APIKey: deps.Config.AI.APIKey, Model: deps.Config.AI.Model, BaseURL: deps.Config.AI.BaseURL})
		if err != nil {
			panic("invalid AI provider configuration")
		}
	}
	siteHandler := handler.NewSiteHandler(deps.Database, deps.Config)
	blogHandler := handler.NewBlogHandler(deps.Database)
	feedHandler := handler.NewFeedHandler(deps.Database, deps.Config)
	commentHandler := handler.NewCommentHandler(deps.Database)
	verificationCodeLimiter := service.NewVerificationCodeLimiter(deps.Config.VerificationCode.Cooldown, time.Now)
	visitorAuthHandler := handler.NewVisitorAuthHandler(deps.Database, deps.Config, verificationCodeLimiter, configuredMailSender(deps.Config))
	adminAuthHandler := handler.NewAdminAuthHandler(deps.Database, deps.Config.Auth.JWTSecret)
	adminContentHandler := handler.NewAdminContentHandler(deps.Database, deps.Config)
	adminDashboardHandler := handler.NewAdminDashboardHandler(deps.Database)
	adminInsightHandler := handler.NewAdminInsightHandler(deps.Database, aiClient, deps.Config.AI.Model)
	adminAIHandler := handler.NewAdminAIHandler(service.NewAIConversationService(deps.Database, aiClient, deps.Config.AI.Model))
	limiter := middleware.NewRateLimiter()

	// SEO discovery endpoints (also proxied by nginx as /rss.xml /sitemap.xml /robots.txt).
	engine.GET("/rss.xml", feedHandler.RSS)
	engine.GET("/sitemap.xml", feedHandler.Sitemap)
	engine.GET("/robots.txt", feedHandler.Robots)

	api := engine.Group("/api")
	api.GET("/site", siteHandler.GetSite)
	api.GET("/health", func(c *gin.Context) { response.Success(c, gin.H{"status": "ok"}) })
	api.GET("/rss.xml", feedHandler.RSS)
	api.GET("/sitemap.xml", feedHandler.Sitemap)
	api.GET("/robots.txt", feedHandler.Robots)
	api.GET("/posts", blogHandler.ListPosts)
	api.GET("/posts/:slug", blogHandler.GetPost)
	api.GET("/categories", blogHandler.ListCategories)
	api.GET("/categories/:slug/posts", blogHandler.ListCategoryPosts)
	api.GET("/tags", blogHandler.ListTags)
	api.GET("/tags/:slug/posts", blogHandler.ListTagPosts)
	api.GET("/archive", blogHandler.Archive)
	api.GET("/projects", blogHandler.ListProjects)
	api.GET("/links", blogHandler.ListLinks)
	api.GET("/search", blogHandler.Search)
	api.POST("/auth/email-code", limiter.Limit(3, time.Minute), visitorAuthHandler.SendEmailCode)
	api.POST("/auth/register", limiter.Limit(10, time.Minute), visitorAuthHandler.Register)
	api.POST("/auth/login", limiter.Limit(10, time.Minute), visitorAuthHandler.Login)
	api.POST("/auth/reset-password", limiter.Limit(10, time.Minute), visitorAuthHandler.ResetPassword)
	api.POST("/auth/logout", visitorAuthHandler.Logout)
	api.GET("/posts/:slug/comments", commentHandler.ListPostComments)
	api.POST("/posts/:slug/comments", middleware.RequireAuth(deps.Config.Auth.JWTSecret), commentHandler.CreatePostComment)
	api.POST("/posts/:slug/like", limiter.Limit(20, time.Minute), blogHandler.LikePost)
	api.POST("/admin/login", limiter.Limit(5, time.Minute), adminAuthHandler.Login)

	admin := api.Group("/admin", middleware.RequireAdmin(deps.Config.Auth.JWTSecret))
	admin.GET("/profile", adminAuthHandler.Profile)
	admin.POST("/logout", adminAuthHandler.Logout)
	admin.PUT("/password", adminAuthHandler.ChangePassword)
	admin.GET("/dashboard", adminDashboardHandler.GetDashboard)
	admin.GET("/analytics", adminInsightHandler.GetAnalytics)
	admin.GET("/trends", adminDashboardHandler.GetTrends)
	admin.GET("/users", adminInsightHandler.ListUsers)
	admin.GET("/ip-bans", adminInsightHandler.ListBans)
	admin.POST("/ip-bans", adminInsightHandler.CreateBan)
	admin.DELETE("/ip-bans/:id", adminInsightHandler.RemoveBan)
	admin.POST("/ai/insights/generate", adminInsightHandler.GenerateInsights)
	admin.POST("/ai/chat", adminInsightHandler.Chat)
	admin.POST("/ai/beautify", adminInsightHandler.Beautify)
	admin.GET("/ai/conversations", adminAIHandler.List)
	admin.POST("/ai/conversations", adminAIHandler.Create)
	admin.DELETE("/ai/conversations", adminAIHandler.Clear)
	admin.GET("/ai/conversations/:id", adminAIHandler.Get)
	admin.PATCH("/ai/conversations/:id", adminAIHandler.Rename)
	admin.DELETE("/ai/conversations/:id", adminAIHandler.Delete)
	admin.POST("/ai/conversations/:id/messages/stream", adminAIHandler.StreamMessage)
	admin.GET("/comments", commentHandler.ListAdminComments)
	admin.PUT("/comments/:id", commentHandler.UpdateAdminComment)
	admin.DELETE("/comments/:id", commentHandler.DeleteAdminComment)
	admin.GET("/posts", adminContentHandler.ListAdminPosts)
	admin.POST("/posts", adminContentHandler.CreatePost)
	admin.GET("/posts/:id", adminContentHandler.GetAdminPost)
	admin.PUT("/posts/:id", adminContentHandler.UpdatePost)
	admin.DELETE("/posts/:id", adminContentHandler.DeletePost)
	admin.GET("/categories", adminContentHandler.ListAdminCategories)
	admin.POST("/categories", adminContentHandler.CreateCategory)
	admin.PUT("/categories/:id", adminContentHandler.UpdateCategory)
	admin.DELETE("/categories/:id", adminContentHandler.DeleteCategory)
	admin.GET("/tags", adminContentHandler.ListAdminTags)
	admin.POST("/tags", adminContentHandler.CreateTag)
	admin.PUT("/tags/:id", adminContentHandler.UpdateTag)
	admin.DELETE("/tags/:id", adminContentHandler.DeleteTag)
	admin.GET("/projects", adminContentHandler.ListProjects)
	admin.POST("/projects", adminContentHandler.CreateProject)
	admin.PUT("/projects/:id", adminContentHandler.UpdateProject)
	admin.DELETE("/projects/:id", adminContentHandler.DeleteProject)
	admin.GET("/links", adminContentHandler.ListLinks)
	admin.POST("/links", adminContentHandler.CreateLink)
	admin.PUT("/links/:id", adminContentHandler.UpdateLink)
	admin.DELETE("/links/:id", adminContentHandler.DeleteLink)
	admin.GET("/settings", adminContentHandler.GetSettings)
	admin.PUT("/settings", adminContentHandler.UpdateSettings)
	admin.POST("/upload", adminContentHandler.UploadImage)

	engine.NoRoute(func(c *gin.Context) { response.Error(c, http.StatusNotFound, 404, "资源不存在") })
	return engine
}

func configuredMailSender(cfg config.Config) blogmail.Sender {
	mailConfig := cfg.Mail
	if mailConfig.SMTPHost == "" || mailConfig.SMTPPort == "" || mailConfig.Username == "" || mailConfig.Password == "" {
		return nil
	}
	from := mailConfig.From
	if from == "" {
		from = mailConfig.Username
	}
	sender, err := blogmail.NewSMTPSender(
		mailConfig.SMTPHost,
		mailConfig.SMTPPort,
		mailConfig.Username,
		mailConfig.Password,
		from,
	)
	if err != nil {
		panic("invalid mail configuration: " + err.Error())
	}
	return sender
}
