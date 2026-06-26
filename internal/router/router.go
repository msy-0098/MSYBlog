package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/handler"
	"masenyu.top/blog/backend/internal/response"
)

type Dependencies struct {
	Config   config.Config
	Database *gorm.DB
}

func New(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())

	siteHandler := handler.NewSiteHandler(deps.Database, deps.Config)
	blogHandler := handler.NewBlogHandler(deps.Database)

	api := engine.Group("/api")
	api.GET("/site", siteHandler.GetSite)
	api.GET("/health", func(c *gin.Context) {
		response.Success(c, gin.H{"status": "ok"})
	})
	api.GET("/posts", blogHandler.ListPosts)
	api.GET("/posts/:slug", blogHandler.GetPost)
	api.GET("/categories", blogHandler.ListCategories)
	api.GET("/categories/:slug/posts", blogHandler.ListCategoryPosts)
	api.GET("/tags", blogHandler.ListTags)
	api.GET("/tags/:slug/posts", blogHandler.ListTagPosts)
	api.GET("/archive", blogHandler.Archive)
	api.GET("/search", blogHandler.Search)

	engine.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, 404, "资源不存在")
	})

	return engine
}
