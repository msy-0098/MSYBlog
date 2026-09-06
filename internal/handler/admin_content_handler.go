package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type AdminContentHandler struct {
	db  *gorm.DB
	cfg config.Config
}

type TaxonomyRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type AdminPostRequest struct {
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Summary     string `json:"summary"`
	Content     string `json:"content"`
	Cover       string `json:"cover"`
	Status      string `json:"status"`
	CategoryID  uint   `json:"categoryId"`
	TagIDs      []uint `json:"tagIds"`
	PublishedAt string `json:"publishedAt"`
}

type AdminPostDTO struct {
	ID          uint          `json:"id"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Summary     string        `json:"summary"`
	Content     string        `json:"content"`
	Cover       string        `json:"cover"`
	Status      string        `json:"status"`
	ViewCount   int           `json:"viewCount"`
	LikeCount   int           `json:"likeCount"`
	CategoryID  uint          `json:"categoryId"`
	Category    TaxonomyDTO   `json:"category"`
	Tags        []TaxonomyDTO `json:"tags"`
	PublishedAt string        `json:"publishedAt"`
	CreatedAt   string        `json:"createdAt"`
	UpdatedAt   string        `json:"updatedAt"`
}

type ProjectRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Cover       string   `json:"cover"`
	TechStack   []string `json:"techStack"`
	Sort        int      `json:"sort"`
	Visible     bool     `json:"visible"`
}

type ProjectDTO struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Cover       string   `json:"cover"`
	TechStack   []string `json:"techStack"`
	Sort        int      `json:"sort"`
	Visible     bool     `json:"visible"`
}

type FriendLinkRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Logo        string `json:"logo"`
	Sort        int    `json:"sort"`
	Visible     bool   `json:"visible"`
}

type FriendLinkDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Logo        string `json:"logo"`
	Sort        int    `json:"sort"`
	Visible     bool   `json:"visible"`
}

func NewAdminContentHandler(db *gorm.DB, cfg config.Config) AdminContentHandler {
	return AdminContentHandler{db: db, cfg: cfg}
}

// ListAdminPosts 管理端文章列表
// @Summary 管理端文章列表
// @Description 分页查询管理后台全部文章（含草稿、已发布、隐藏）
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Success 200 {object} response.Envelope{data=PageDTO[AdminPostDTO]} "文章管理列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 403 {object} response.ErrorResponse "无权限"
// @Router /admin/posts [get]
func (h AdminContentHandler) ListAdminPosts(c *gin.Context) {
	page, pageSize := pagination(c)

	var total int64
	if err := h.db.Model(&model.Post{}).Count(&total).Error; err != nil {
		internalError(c)
		return
	}

	var posts []model.Post
	if err := h.db.Order("updated_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Preload("Category").
		Preload("Tags").
		Find(&posts).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]AdminPostDTO, 0, len(posts))
	for _, post := range posts {
		list = append(list, adminPostDTO(post))
	}

	response.Success(c, PageDTO[AdminPostDTO]{List: list, Page: page, PageSize: pageSize, Total: total})
}

// CreatePost 新建文章
// @Summary 新建文章
// @Description 创建新文章，支持关联分类、标签与设置发布状态
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body AdminPostRequest true "文章内容"
// @Success 200 {object} response.Envelope{data=AdminPostDTO} "创建成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 403 {object} response.ErrorResponse "无权限"
// @Router /admin/posts [post]
func (h AdminContentHandler) CreatePost(c *gin.Context) {
	var req AdminPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	post, err := h.postFromRequest(req, model.Post{})
	if err != nil {
		badRequest(c)
		return
	}

	if err := h.db.Create(&post).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	if err := h.replacePostTags(&post, req.TagIDs); err != nil {
		internalError(c)
		return
	}

	if err := h.db.Preload("Category").Preload("Tags").First(&post, post.ID).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, adminPostDTO(post))
}

// GetAdminPost 获取管理端文章详情
// @Summary 获取管理端文章详情
// @Description 根据文章 ID 查询文章详情以供后台编辑
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "文章 ID"
// @Success 200 {object} response.Envelope{data=AdminPostDTO} "文章详情"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "文章不存在"
// @Router /admin/posts/{id} [get]
func (h AdminContentHandler) GetAdminPost(c *gin.Context) {
	post, ok := h.findPost(c)
	if !ok {
		return
	}

	response.Success(c, adminPostDTO(post))
}

// UpdatePost 更新文章
// @Summary 更新文章
// @Description 根据文章 ID 更新文章属性、正文、分类、标签
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "文章 ID"
// @Param request body AdminPostRequest true "更新内容"
// @Success 200 {object} response.Envelope{data=AdminPostDTO} "更新成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "文章不存在"
// @Router /admin/posts/{id} [put]
func (h AdminContentHandler) UpdatePost(c *gin.Context) {
	post, ok := h.findPost(c)
	if !ok {
		return
	}

	var req AdminPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	updated, err := h.postFromRequest(req, post)
	if err != nil {
		badRequest(c)
		return
	}

	if err := h.db.Save(&updated).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	if err := h.replacePostTags(&updated, req.TagIDs); err != nil {
		internalError(c)
		return
	}

	if err := h.db.Preload("Category").Preload("Tags").First(&updated, updated.ID).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, adminPostDTO(updated))
}

// DeletePost 删除文章
// @Summary 删除文章
// @Description 根据 ID 删除指定文章及其标签关联
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "文章 ID"
// @Success 200 {object} response.Envelope{data=object} "删除成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "文章不存在"
// @Router /admin/posts/{id} [delete]
func (h AdminContentHandler) DeletePost(c *gin.Context) {
	post, ok := h.findPost(c)
	if !ok {
		return
	}

	if err := h.db.Select("Tags").Delete(&post).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

// ListAdminCategories 管理端分类列表
// @Summary 管理端分类列表
// @Description 查询全部文章分类及对应文章数量
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=ListDTO[TaxonomyDTO]} "分类列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/categories [get]
func (h AdminContentHandler) ListAdminCategories(c *gin.Context) {
	var categories []model.Category
	if err := h.db.Order("name asc").Find(&categories).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]TaxonomyDTO, 0, len(categories))
	for _, category := range categories {
		count, _ := h.postCountForCategory(category.ID)
		list = append(list, TaxonomyDTO{ID: category.ID, Name: category.Name, Slug: category.Slug, PostCount: count})
	}

	response.Success(c, ListDTO[TaxonomyDTO]{List: list})
}

// CreateCategory 新增文章分类
// @Summary 新增文章分类
// @Description 新建文章分类，slug 需唯一
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body TaxonomyRequest true "分类信息"
// @Success 200 {object} response.Envelope{data=TaxonomyDTO} "创建成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/categories [post]
func (h AdminContentHandler) CreateCategory(c *gin.Context) {
	var req TaxonomyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	category := model.Category{Name: strings.TrimSpace(req.Name), Slug: strings.TrimSpace(req.Slug)}
	if category.Name == "" || category.Slug == "" {
		badRequest(c)
		return
	}

	if err := h.db.Create(&category).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	response.Success(c, TaxonomyDTO{ID: category.ID, Name: category.Name, Slug: category.Slug})
}

// UpdateCategory 更新文章分类
// @Summary 更新文章分类
// @Description 根据分类 ID 更新名称与 slug
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "分类 ID"
// @Param request body TaxonomyRequest true "分类信息"
// @Success 200 {object} response.Envelope{data=TaxonomyDTO} "更新成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/categories/{id} [put]
func (h AdminContentHandler) UpdateCategory(c *gin.Context) {
	var category model.Category
	if !h.findByID(c, &category) {
		return
	}

	var req TaxonomyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	category.Name = strings.TrimSpace(req.Name)
	category.Slug = strings.TrimSpace(req.Slug)
	if category.Name == "" || category.Slug == "" {
		badRequest(c)
		return
	}

	if err := h.db.Save(&category).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	response.Success(c, TaxonomyDTO{ID: category.ID, Name: category.Name, Slug: category.Slug})
}

// DeleteCategory 删除文章分类
// @Summary 删除文章分类
// @Description 删除指定分类（若有关联文章则拒绝删除）
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "分类 ID"
// @Success 200 {object} response.Envelope{data=object} "删除成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 409 {object} response.ErrorResponse "分类已关联文章"
// @Router /admin/categories/{id} [delete]
func (h AdminContentHandler) DeleteCategory(c *gin.Context) {
	var category model.Category
	if !h.findByID(c, &category) {
		return
	}

	count, err := h.postCountForCategory(category.ID)
	if err != nil {
		internalError(c)
		return
	}
	if count > 0 {
		response.Error(c, http.StatusConflict, 409, "分类已关联文章")
		return
	}

	if err := h.db.Delete(&category).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

// ListAdminTags 管理端标签列表
// @Summary 管理端标签列表
// @Description 查询全部文章标签及对应文章数量
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=ListDTO[TaxonomyDTO]} "标签列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/tags [get]
func (h AdminContentHandler) ListAdminTags(c *gin.Context) {
	var tags []model.Tag
	if err := h.db.Order("name asc").Find(&tags).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]TaxonomyDTO, 0, len(tags))
	for _, tag := range tags {
		count, _ := h.postCountForTag(tag.ID)
		list = append(list, TaxonomyDTO{ID: tag.ID, Name: tag.Name, Slug: tag.Slug, PostCount: count})
	}

	response.Success(c, ListDTO[TaxonomyDTO]{List: list})
}

// CreateTag 新增文章标签
// @Summary 新增文章标签
// @Description 新建文章标签，slug 需唯一
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body TaxonomyRequest true "标签信息"
// @Success 200 {object} response.Envelope{data=TaxonomyDTO} "创建成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/tags [post]
func (h AdminContentHandler) CreateTag(c *gin.Context) {
	var req TaxonomyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	tag := model.Tag{Name: strings.TrimSpace(req.Name), Slug: strings.TrimSpace(req.Slug)}
	if tag.Name == "" || tag.Slug == "" {
		badRequest(c)
		return
	}

	if err := h.db.Create(&tag).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	response.Success(c, TaxonomyDTO{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
}

// UpdateTag 更新文章标签
// @Summary 更新文章标签
// @Description 根据标签 ID 更新名称与 slug
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "标签 ID"
// @Param request body TaxonomyRequest true "标签信息"
// @Success 200 {object} response.Envelope{data=TaxonomyDTO} "更新成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/tags/{id} [put]
func (h AdminContentHandler) UpdateTag(c *gin.Context) {
	var tag model.Tag
	if !h.findByID(c, &tag) {
		return
	}

	var req TaxonomyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	tag.Name = strings.TrimSpace(req.Name)
	tag.Slug = strings.TrimSpace(req.Slug)
	if tag.Name == "" || tag.Slug == "" {
		badRequest(c)
		return
	}

	if err := h.db.Save(&tag).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	response.Success(c, TaxonomyDTO{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
}

// DeleteTag 删除文章标签
// @Summary 删除文章标签
// @Description 删除指定文章标签
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "标签 ID"
// @Success 200 {object} response.Envelope{data=object} "删除成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/tags/{id} [delete]
func (h AdminContentHandler) DeleteTag(c *gin.Context) {
	var tag model.Tag
	if !h.findByID(c, &tag) {
		return
	}

	if err := h.db.Delete(&tag).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

// ListProjects 管理端项目列表
// @Summary 管理端项目列表
// @Description 管理后台查询全部项目（含隐藏项目）
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=ListDTO[ProjectDTO]} "项目列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/projects [get]
func (h AdminContentHandler) ListProjects(c *gin.Context) {
	var projects []model.Project
	if err := h.db.Order("sort desc").Order("updated_at desc").Find(&projects).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]ProjectDTO, 0, len(projects))
	for _, project := range projects {
		list = append(list, projectDTO(project))
	}

	response.Success(c, ListDTO[ProjectDTO]{List: list})
}

// CreateProject 新建项目
// @Summary 新建项目
// @Description 管理后台新增技术项目作品
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body ProjectRequest true "项目信息"
// @Success 200 {object} response.Envelope{data=ProjectDTO} "创建成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/projects [post]
func (h AdminContentHandler) CreateProject(c *gin.Context) {
	var req ProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	project, err := projectFromRequest(req, model.Project{})
	if err != nil {
		badRequest(c)
		return
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		if !req.Visible {
			return tx.Model(&project).Update("visible", false).Error
		}
		return nil
	}); err != nil {
		internalError(c)
		return
	}

	response.Success(c, projectDTO(project))
}

// UpdateProject 更新项目
// @Summary 更新项目
// @Description 根据 ID 更新项目作品信息
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "项目 ID"
// @Param request body ProjectRequest true "项目信息"
// @Success 200 {object} response.Envelope{data=ProjectDTO} "更新成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/projects/{id} [put]
func (h AdminContentHandler) UpdateProject(c *gin.Context) {
	var project model.Project
	if !h.findByID(c, &project) {
		return
	}

	var req ProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	updated, err := projectFromRequest(req, project)
	if err != nil {
		badRequest(c)
		return
	}
	if err := h.db.Save(&updated).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, projectDTO(updated))
}

// DeleteProject 删除项目
// @Summary 删除项目
// @Description 根据 ID 删除项目作品
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "项目 ID"
// @Success 200 {object} response.Envelope{data=object} "删除成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/projects/{id} [delete]
func (h AdminContentHandler) DeleteProject(c *gin.Context) {
	var project model.Project
	if !h.findByID(c, &project) {
		return
	}

	if err := h.db.Delete(&project).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

// ListLinks 管理端友链列表
// @Summary 管理端友链列表
// @Description 管理后台查询全部友情链接（含隐藏友链）
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=ListDTO[FriendLinkDTO]} "友链列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/links [get]
func (h AdminContentHandler) ListLinks(c *gin.Context) {
	var links []model.FriendLink
	if err := h.db.Order("sort desc").Order("id asc").Find(&links).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]FriendLinkDTO, 0, len(links))
	for _, link := range links {
		list = append(list, friendLinkDTO(link))
	}

	response.Success(c, ListDTO[FriendLinkDTO]{List: list})
}

// CreateLink 新增友情链接
// @Summary 新增友情链接
// @Description 管理后台新增友情链接
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body FriendLinkRequest true "友链信息"
// @Success 200 {object} response.Envelope{data=FriendLinkDTO} "创建成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/links [post]
func (h AdminContentHandler) CreateLink(c *gin.Context) {
	var req FriendLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	link, err := friendLinkFromRequest(req, model.FriendLink{})
	if err != nil {
		badRequest(c)
		return
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&link).Error; err != nil {
			return err
		}
		if !req.Visible {
			return tx.Model(&link).Update("visible", false).Error
		}
		return nil
	}); err != nil {
		internalError(c)
		return
	}

	response.Success(c, friendLinkDTO(link))
}

// UpdateLink 更新友情链接
// @Summary 更新友情链接
// @Description 根据 ID 更新友情链接
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "友链 ID"
// @Param request body FriendLinkRequest true "友链信息"
// @Success 200 {object} response.Envelope{data=FriendLinkDTO} "更新成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/links/{id} [put]
func (h AdminContentHandler) UpdateLink(c *gin.Context) {
	var link model.FriendLink
	if !h.findByID(c, &link) {
		return
	}

	var req FriendLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	updated, err := friendLinkFromRequest(req, link)
	if err != nil {
		badRequest(c)
		return
	}
	if err := h.db.Save(&updated).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, friendLinkDTO(updated))
}

// DeleteLink 删除友情链接
// @Summary 删除友情链接
// @Description 根据 ID 删除友情链接
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "友链 ID"
// @Success 200 {object} response.Envelope{data=object} "删除成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/links/{id} [delete]
func (h AdminContentHandler) DeleteLink(c *gin.Context) {
	var link model.FriendLink
	if !h.findByID(c, &link) {
		return
	}

	if err := h.db.Delete(&link).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

// GetSettings 获取站点系统设置
// @Summary 获取站点系统设置
// @Description 获取管理员配置的完整站点系统设置项（包含通知、备案、站长资料等）
// @Tags 内容管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=map[string]string} "系统配置项"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/settings [get]
func (h AdminContentHandler) GetSettings(c *gin.Context) {
	response.Success(c, h.effectiveSettings())
}

// UpdateSettings 更新站点系统设置
// @Summary 更新站点系统设置
// @Description 批量更新站点系统设置键值对
// @Tags 内容管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body map[string]string true "系统设置键值对"
// @Success 200 {object} response.Envelope{data=map[string]string} "更新后的系统配置项"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/settings [put]
func (h AdminContentHandler) UpdateSettings(c *gin.Context) {
	var values map[string]string
	if err := c.ShouldBindJSON(&values); err != nil {
		badRequest(c)
		return
	}

	allowed := map[string]bool{
		"siteTitle":       true,
		"subtitle":        true,
		"owner":           true,
		"domain":          true,
		"description":     true,
		"github":          true,
		"gitee":           true,
		"email":           true,
		"icp":             true,
		"navItems":        true,
		"notifyInApp":     true,
		"notifyEmail":     true,
		"notifySecurity":  true,
		"notifyUser":      true,
		"notifySystem":    true,
	}

	for key, value := range values {
		if !allowed[key] {
			continue
		}
		setting := model.SiteSetting{Key: key}
		if err := h.db.Where("key = ?", key).Assign(model.SiteSetting{Value: value}).FirstOrCreate(&setting).Error; err != nil {
			internalError(c)
			return
		}
	}

	response.Success(c, h.effectiveSettings())
}

func (h AdminContentHandler) effectiveSettings() map[string]string {
	settings := map[string]string{
		"siteTitle":      h.cfg.Site.SiteTitle,
		"subtitle":       h.cfg.Site.Subtitle,
		"owner":          h.cfg.Site.Owner,
		"domain":         h.cfg.Site.Domain,
		"description":    h.cfg.Site.Description,
		"navItems":       strings.Join(h.cfg.Site.NavItems, ","),
		"aiProvider":     h.cfg.AI.Provider,
		"aiModel":        h.cfg.AI.Model,
		"aiBaseURL":      h.cfg.AI.BaseURL,
		"aiConfigured":   strconv.FormatBool(strings.TrimSpace(h.cfg.AI.APIKey) != ""),
		"notifyInApp":    "true",
		"notifyEmail":    "true",
		"notifySecurity": "true",
		"notifyUser":     "true",
		"notifySystem":   "false",
	}
	for key, value := range h.readSettings() {
		settings[key] = value
	}
	return settings
}

func (h AdminContentHandler) postFromRequest(req AdminPostRequest, post model.Post) (model.Post, error) {
	post.Title = strings.TrimSpace(req.Title)
	post.Slug = strings.TrimSpace(req.Slug)
	post.Summary = strings.TrimSpace(req.Summary)
	post.Content = req.Content
	post.Cover = strings.TrimSpace(req.Cover)
	post.Status = strings.TrimSpace(req.Status)
	post.CategoryID = req.CategoryID
	if post.Status == "" {
		post.Status = model.PostStatusDraft
	}
	if post.Title == "" || post.Slug == "" || post.Summary == "" || post.Content == "" || post.CategoryID == 0 {
		return post, errors.New("missing required post fields")
	}
	if !validPostStatus(post.Status) {
		return post, errors.New("invalid status")
	}

	publishedAt, err := parsePublishedAt(req.PublishedAt)
	if err != nil {
		return post, err
	}
	if post.Status == model.PostStatusPublished && publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}
	post.PublishedAt = publishedAt

	return post, nil
}

func (h AdminContentHandler) replacePostTags(post *model.Post, tagIDs []uint) error {
	var tags []model.Tag
	if len(tagIDs) > 0 {
		if err := h.db.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
			return err
		}
	}

	return h.db.Model(post).Association("Tags").Replace(tags)
}

func (h AdminContentHandler) findPost(c *gin.Context) (model.Post, bool) {
	var post model.Post
	if !h.findByID(c, &post) {
		return post, false
	}
	if err := h.db.Preload("Category").Preload("Tags").First(&post, post.ID).Error; err != nil {
		internalError(c)
		return post, false
	}

	return post, true
}

func (h AdminContentHandler) findByID(c *gin.Context, target any) bool {
	return findByIDParam(c, h.db, target)
}

func (h AdminContentHandler) readSettings() map[string]string {
	var settings []model.SiteSetting
	if err := h.db.Find(&settings).Error; err != nil {
		return map[string]string{}
	}

	result := map[string]string{}
	for _, setting := range settings {
		result[setting.Key] = setting.Value
	}

	return result
}

func (h AdminContentHandler) postCountForCategory(categoryID uint) (int64, error) {
	var count int64
	err := h.db.Model(&model.Post{}).Where("category_id = ?", categoryID).Count(&count).Error
	return count, err
}

func (h AdminContentHandler) postCountForTag(tagID uint) (int64, error) {
	var count int64
	err := h.db.Model(&model.Post{}).
		Joins("JOIN post_tags ON post_tags.post_id = posts.id").
		Where("post_tags.tag_id = ?", tagID).
		Count(&count).Error
	return count, err
}

func adminPostDTO(post model.Post) AdminPostDTO {
	tags := make([]TaxonomyDTO, 0, len(post.Tags))
	for _, tag := range post.Tags {
		tags = append(tags, TaxonomyDTO{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
	}

	return AdminPostDTO{
		ID:          post.ID,
		Title:       post.Title,
		Slug:        post.Slug,
		Summary:     post.Summary,
		Content:     post.Content,
		Cover:       post.Cover,
		Status:      post.Status,
		ViewCount:   post.ViewCount,
		LikeCount:   post.LikeCount,
		CategoryID:  post.CategoryID,
		Category:    TaxonomyDTO{ID: post.Category.ID, Name: post.Category.Name, Slug: post.Category.Slug},
		Tags:        tags,
		PublishedAt: formatTime(post.PublishedAt),
		CreatedAt:   formatTime(post.CreatedAt),
		UpdatedAt:   formatTime(post.UpdatedAt),
	}
}

func projectFromRequest(req ProjectRequest, project model.Project) (model.Project, error) {
	project.Name = strings.TrimSpace(req.Name)
	project.Description = strings.TrimSpace(req.Description)
	project.URL = strings.TrimSpace(req.URL)
	project.Cover = strings.TrimSpace(req.Cover)
	project.Sort = req.Sort
	project.Visible = req.Visible
	if project.Name == "" || project.Description == "" {
		return project, errors.New("missing required project fields")
	}

	rawTechStack, err := json.Marshal(req.TechStack)
	if err != nil {
		return project, err
	}
	project.TechStack = string(rawTechStack)

	return project, nil
}

func projectDTO(project model.Project) ProjectDTO {
	var techStack []string
	if err := json.Unmarshal([]byte(project.TechStack), &techStack); err != nil {
		techStack = []string{}
	}

	return ProjectDTO{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		URL:         project.URL,
		Cover:       project.Cover,
		TechStack:   techStack,
		Sort:        project.Sort,
		Visible:     project.Visible,
	}
}

func friendLinkFromRequest(req FriendLinkRequest, link model.FriendLink) (model.FriendLink, error) {
	link.Name = strings.TrimSpace(req.Name)
	link.URL = strings.TrimSpace(req.URL)
	link.Description = strings.TrimSpace(req.Description)
	link.Logo = strings.TrimSpace(req.Logo)
	link.Sort = req.Sort
	link.Visible = req.Visible
	if link.Name == "" || link.URL == "" {
		return link, errors.New("missing required friend link fields")
	}
	if !strings.HasPrefix(link.URL, "http://") && !strings.HasPrefix(link.URL, "https://") {
		return link, errors.New("invalid friend link url")
	}
	return link, nil
}

func friendLinkDTO(link model.FriendLink) FriendLinkDTO {
	return FriendLinkDTO{
		ID:          link.ID,
		Name:        link.Name,
		URL:         link.URL,
		Description: link.Description,
		Logo:        link.Logo,
		Sort:        link.Sort,
		Visible:     link.Visible,
	}
}

func parsePublishedAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}

	return time.Parse(time.RFC3339, raw)
}

func validPostStatus(status string) bool {
	return status == model.PostStatusDraft || status == model.PostStatusPublished || status == model.PostStatusHidden
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.Format(time.RFC3339)
}

func badRequest(c *gin.Context) {
	response.Error(c, http.StatusBadRequest, 400, "参数错误")
}

func findByIDParam(c *gin.Context, db *gorm.DB, target any) bool {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		badRequest(c)
		return false
	}

	if err := db.First(target, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, http.StatusNotFound, 404, "资源不存在")
			return false
		}
		internalError(c)
		return false
	}

	return true
}

func internalError(c *gin.Context) {
	response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
}

func conflictOrInternal(c *gin.Context, err error) {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique") || strings.Contains(message, "constraint") || strings.Contains(message, "duplicate") {
		response.Error(c, http.StatusConflict, 409, "该邮箱已注册，请直接登录")
		return
	}

	internalError(c)
}
