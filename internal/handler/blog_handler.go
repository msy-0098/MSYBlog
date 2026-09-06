package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type BlogHandler struct {
	db *gorm.DB
}

type PostSummary struct {
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Summary     string        `json:"summary"`
	Cover       string        `json:"cover"`
	ViewCount   int           `json:"viewCount"`
	LikeCount   int           `json:"likeCount"`
	Category    TaxonomyDTO   `json:"category"`
	Tags        []TaxonomyDTO `json:"tags"`
	PublishedAt string        `json:"publishedAt"`
}

type PostDetail struct {
	PostSummary
	Content string       `json:"content"`
	Prev    *PostPointer `json:"prev"`
	Next    *PostPointer `json:"next"`
}

type TaxonomyDTO struct {
	ID        uint   `json:"id,omitempty"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	PostCount int64  `json:"postCount,omitempty"`
}

type PostPointer struct {
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	PublishedAt string `json:"publishedAt"`
}

type PageDTO[T any] struct {
	List     []T   `json:"list"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
	Total    int64 `json:"total"`
}

type ListDTO[T any] struct {
	List []T `json:"list"`
}

type ArchiveYear struct {
	Year   int            `json:"year"`
	Months []ArchiveMonth `json:"months"`
}

type ArchiveMonth struct {
	Month int           `json:"month"`
	Posts []PostSummary `json:"posts"`
}

type postFilters struct {
	Slug     string
	Category string
	Tag      string
	Query    string
}

func NewBlogHandler(db *gorm.DB) BlogHandler {
	return BlogHandler{db: db}
}

// ListPosts 获取公开文章列表
// @Summary 获取公开文章列表
// @Description 分页查询已发布文章，支持分类、标签和搜索词过滤
// @Tags 博客公开接口
// @Produce json
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Param slug query string false "文章标识 slug"
// @Param category query string false "分类标识 slug"
// @Param tag query string false "标签标识 slug"
// @Param q query string false "搜索关键词"
// @Success 200 {object} response.Envelope{data=PageDTO[PostSummary]} "文章列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /posts [get]
func (h BlogHandler) ListPosts(c *gin.Context) {
	page, pageSize := pagination(c)
	result, err := h.paginatedPosts(postFilters{
		Slug:     strings.TrimSpace(c.Query("slug")),
		Category: strings.TrimSpace(c.Query("category")),
		Tag:      strings.TrimSpace(c.Query("tag")),
		Query:    strings.TrimSpace(c.Query("q")),
	}, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.SuccessPublic(c, result, 30*time.Second)
}

// GetPost 获取文章详情
// @Summary 获取文章详情
// @Description 根据文章 slug 获取单篇文章完整内容及上下文跳转
// @Tags 博客公开接口
// @Produce json
// @Param slug path string true "文章 slug"
// @Success 200 {object} response.Envelope{data=PostDetail} "文章详情"
// @Failure 404 {object} response.ErrorResponse "文章不存在"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /posts/{slug} [get]
func (h BlogHandler) GetPost(c *gin.Context) {
	var post model.Post
	if err := h.db.Preload("Category").Preload("Tags").
		Where("slug = ? AND status = ?", c.Param("slug"), model.PostStatusPublished).
		First(&post).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Error(c, http.StatusNotFound, 404, "文章不存在")
			return
		}
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	if err := h.db.Model(&model.Post{}).
		Where("id = ?", post.ID).
		UpdateColumn("view_count", gorm.Expr("view_count + ?", 1)).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "server error")
		return
	}
	post.ViewCount++

	prev, err := h.adjacentPost("published_at < ?", post.PublishedAt, "published_at desc")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	next, err := h.adjacentPost("published_at > ?", post.PublishedAt, "published_at asc")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.Success(c, PostDetail{
		PostSummary: postSummary(post),
		Content:     post.Content,
		Prev:        prev,
		Next:        next,
	})
}

// ListCategories 获取公开分类列表
// @Summary 获取公开分类列表
// @Description 获取包含公开文章的分类列表及其文章数量
// @Tags 博客公开接口
// @Produce json
// @Success 200 {object} response.Envelope{data=ListDTO[TaxonomyDTO]} "分类列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /categories [get]
func (h BlogHandler) ListCategories(c *gin.Context) {
	var categories []model.Category
	if err := h.db.Order("name asc").Find(&categories).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	list := make([]TaxonomyDTO, 0, len(categories))
	for _, category := range categories {
		count, err := h.postCountForCategory(category.ID)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
			return
		}
		if count == 0 {
			continue
		}
		list = append(list, TaxonomyDTO{ID: category.ID, Name: category.Name, Slug: category.Slug, PostCount: count})
	}

	response.SuccessPublic(c, ListDTO[TaxonomyDTO]{List: list}, 30*time.Second)
}

// ListCategoryPosts 获取指定分类的文章列表
// @Summary 获取指定分类的文章列表
// @Description 分页获取属于某个分类的已发布文章
// @Tags 博客公开接口
// @Produce json
// @Param slug path string true "分类 slug"
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Success 200 {object} response.Envelope{data=PageDTO[PostSummary]} "文章列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /categories/{slug}/posts [get]
func (h BlogHandler) ListCategoryPosts(c *gin.Context) {
	page, pageSize := pagination(c)
	result, err := h.paginatedPosts(postFilters{Category: c.Param("slug")}, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.SuccessPublic(c, result, 30*time.Second)
}

// ListTags 获取公开标签列表
// @Summary 获取公开标签列表
// @Description 获取包含公开文章的标签列表及其文章数量
// @Tags 博客公开接口
// @Produce json
// @Success 200 {object} response.Envelope{data=ListDTO[TaxonomyDTO]} "标签列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /tags [get]
func (h BlogHandler) ListTags(c *gin.Context) {
	var tags []model.Tag
	if err := h.db.Order("name asc").Find(&tags).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	list := make([]TaxonomyDTO, 0, len(tags))
	for _, tag := range tags {
		count, err := h.postCountForTag(tag.ID)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
			return
		}
		if count == 0 {
			continue
		}
		list = append(list, TaxonomyDTO{ID: tag.ID, Name: tag.Name, Slug: tag.Slug, PostCount: count})
	}

	response.SuccessPublic(c, ListDTO[TaxonomyDTO]{List: list}, 30*time.Second)
}

// ListTagPosts 获取指定标签的文章列表
// @Summary 获取指定标签的文章列表
// @Description 分页获取具有某个标签的已发布文章
// @Tags 博客公开接口
// @Produce json
// @Param slug path string true "标签 slug"
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Success 200 {object} response.Envelope{data=PageDTO[PostSummary]} "文章列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /tags/{slug}/posts [get]
func (h BlogHandler) ListTagPosts(c *gin.Context) {
	page, pageSize := pagination(c)
	result, err := h.paginatedPosts(postFilters{Tag: c.Param("slug")}, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.SuccessPublic(c, result, 30*time.Second)
}

// Search 站内文章搜索
// @Summary 站内文章搜索
// @Description 根据关键词模糊搜索文章标题、摘要和内容
// @Tags 博客公开接口
// @Produce json
// @Param q query string false "搜索关键词"
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Success 200 {object} response.Envelope{data=PageDTO[PostSummary]} "搜索结果"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /search [get]
func (h BlogHandler) Search(c *gin.Context) {
	page, pageSize := pagination(c)
	result, err := h.paginatedPosts(postFilters{Query: strings.TrimSpace(c.Query("q"))}, page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.SuccessPublic(c, result, 30*time.Second)
}

// Archive 获取文章归档列表
// @Summary 获取文章归档列表
// @Description 获取按年份和月份聚合的全部已发布文章归档
// @Tags 博客公开接口
// @Produce json
// @Success 200 {object} response.Envelope{data=ListDTO[ArchiveYear]} "归档文章列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /archive [get]
func (h BlogHandler) Archive(c *gin.Context) {
	var posts []model.Post
	if err := h.postQuery(postFilters{}).
		Order("posts.published_at desc").
		Preload("Category").
		Preload("Tags").
		Find(&posts).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	years := make([]ArchiveYear, 0)
	yearIndex := map[int]int{}
	monthIndex := map[string]int{}

	for _, post := range posts {
		year, month, _ := post.PublishedAt.Date()
		if _, ok := yearIndex[year]; !ok {
			yearIndex[year] = len(years)
			years = append(years, ArchiveYear{Year: year, Months: []ArchiveMonth{}})
		}

		yearPosition := yearIndex[year]
		monthKey := strconv.Itoa(year) + "-" + strconv.Itoa(int(month))
		if _, ok := monthIndex[monthKey]; !ok {
			monthIndex[monthKey] = len(years[yearPosition].Months)
			years[yearPosition].Months = append(years[yearPosition].Months, ArchiveMonth{Month: int(month), Posts: []PostSummary{}})
		}

		monthPosition := monthIndex[monthKey]
		years[yearPosition].Months[monthPosition].Posts = append(years[yearPosition].Months[monthPosition].Posts, postSummary(post))
	}

	response.SuccessPublic(c, ListDTO[ArchiveYear]{List: years}, 30*time.Second)
}

// ListProjects 获取精选项目列表
// @Summary 获取精选项目列表
// @Description 获取公开可见的个人技术项目列表
// @Tags 博客公开接口
// @Produce json
// @Success 200 {object} response.Envelope{data=ListDTO[ProjectDTO]} "项目列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /projects [get]
func (h BlogHandler) ListProjects(c *gin.Context) {
	var projects []model.Project
	if err := h.db.Where("visible = ?", true).
		Order("sort desc").
		Order("updated_at desc").
		Find(&projects).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	list := make([]ProjectDTO, 0, len(projects))
	for _, project := range projects {
		list = append(list, projectDTO(project))
	}

	response.SuccessPublic(c, ListDTO[ProjectDTO]{List: list}, 30*time.Second)
}

// ListLinks 获取友情链接列表
// @Summary 获取友情链接列表
// @Description 获取公开可见的友情链接列表
// @Tags 博客公开接口
// @Produce json
// @Success 200 {object} response.Envelope{data=ListDTO[FriendLinkDTO]} "友链列表"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /links [get]
func (h BlogHandler) ListLinks(c *gin.Context) {
	var links []model.FriendLink
	if err := h.db.Where("visible = ?", true).
		Order("sort desc").
		Order("id asc").
		Find(&links).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	list := make([]FriendLinkDTO, 0, len(links))
	for _, link := range links {
		list = append(list, friendLinkDTO(link))
	}

	response.SuccessPublic(c, ListDTO[FriendLinkDTO]{List: list}, 30*time.Second)
}

// LikePost records an IP-unique like for a published post.
// @Summary 文章点赞
// @Description 根据客户端 IP 对公开文章进行点赞（重复点赞返回当前点赞状态）
// @Tags 博客公开接口
// @Produce json
// @Param slug path string true "文章 slug"
// @Success 200 {object} response.Envelope{data=object} "点赞结果"
// @Failure 404 {object} response.ErrorResponse "文章不存在"
// @Failure 500 {object} response.ErrorResponse "服务端错误"
// @Router /posts/{slug}/like [post]
func (h BlogHandler) LikePost(c *gin.Context) {
	var post model.Post
	if err := h.db.Where("slug = ? AND status = ?", c.Param("slug"), model.PostStatusPublished).
		First(&post).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Error(c, http.StatusNotFound, 404, "文章不存在")
			return
		}
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	ip := middleware.ClientIP(c)
	if strings.TrimSpace(ip) == "" {
		ip = "unknown"
	}

	like := model.PostLike{PostID: post.ID, IP: ip}
	err := h.db.Where("post_id = ? AND ip = ?", post.ID, ip).FirstOrCreate(&like).Error
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	// FirstOrCreate returns existing without creating when already liked.
	var count int64
	if err := h.db.Model(&model.PostLike{}).Where("post_id = ?", post.ID).Count(&count).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}
	if err := h.db.Model(&post).Update("like_count", count).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	response.Success(c, gin.H{
		"likeCount": count,
		"liked":     true,
	})
}

func (h BlogHandler) paginatedPosts(filters postFilters, page int, pageSize int) (PageDTO[PostSummary], error) {
	var total int64
	if err := h.postQuery(filters).Distinct("posts.id").Count(&total).Error; err != nil {
		return PageDTO[PostSummary]{}, err
	}

	var posts []model.Post
	if err := h.postQuery(filters).
		Order("posts.published_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Preload("Category").
		Preload("Tags").
		Find(&posts).Error; err != nil {
		return PageDTO[PostSummary]{}, err
	}

	list := make([]PostSummary, 0, len(posts))
	for _, post := range posts {
		list = append(list, postSummary(post))
	}

	return PageDTO[PostSummary]{
		List:     list,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

func (h BlogHandler) postQuery(filters postFilters) *gorm.DB {
	query := h.db.Model(&model.Post{}).Where("posts.status = ?", model.PostStatusPublished)

	if filters.Slug != "" {
		query = query.Where("posts.slug = ?", filters.Slug)
	}

	if filters.Category != "" {
		query = query.Joins("JOIN categories ON categories.id = posts.category_id").Where("categories.slug = ?", filters.Category)
	}

	if filters.Tag != "" {
		query = query.Joins("JOIN post_tags ON post_tags.post_id = posts.id").Joins("JOIN tags ON tags.id = post_tags.tag_id").Where("tags.slug = ?", filters.Tag)
	}

	if filters.Query != "" {
		like := "%" + filters.Query + "%"
		query = query.Where("posts.title LIKE ? OR posts.summary LIKE ? OR posts.content LIKE ?", like, like, like)
	}

	return query
}

func (h BlogHandler) adjacentPost(condition string, publishedAt time.Time, order string) (*PostPointer, error) {
	var post model.Post
	err := h.db.Where("status = ?", model.PostStatusPublished).
		Where(condition, publishedAt).
		Order(order).
		First(&post).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &PostPointer{
		Title:       post.Title,
		Slug:        post.Slug,
		PublishedAt: formatDate(post.PublishedAt),
	}, nil
}

func (h BlogHandler) postCountForCategory(categoryID uint) (int64, error) {
	var count int64
	err := h.db.Model(&model.Post{}).
		Where("category_id = ? AND status = ?", categoryID, model.PostStatusPublished).
		Count(&count).Error
	return count, err
}

func (h BlogHandler) postCountForTag(tagID uint) (int64, error) {
	var count int64
	err := h.db.Model(&model.Post{}).
		Joins("JOIN post_tags ON post_tags.post_id = posts.id").
		Where("post_tags.tag_id = ? AND posts.status = ?", tagID, model.PostStatusPublished).
		Count(&count).Error
	return count, err
}

func pagination(c *gin.Context) (int, int) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("pageSize"), 10)
	if pageSize > 50 {
		pageSize = 50
	}

	return page, pageSize
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func postSummary(post model.Post) PostSummary {
	tags := make([]TaxonomyDTO, 0, len(post.Tags))
	for _, tag := range post.Tags {
		tags = append(tags, TaxonomyDTO{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
	}

	return PostSummary{
		Title:       post.Title,
		Slug:        post.Slug,
		Summary:     post.Summary,
		Cover:       post.Cover,
		ViewCount:   post.ViewCount,
		LikeCount:   post.LikeCount,
		Category:    TaxonomyDTO{ID: post.Category.ID, Name: post.Category.Name, Slug: post.Category.Slug},
		Tags:        tags,
		PublishedAt: formatDate(post.PublishedAt),
	}
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}
