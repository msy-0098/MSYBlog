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

func NewAdminContentHandler(db *gorm.DB, cfg config.Config) AdminContentHandler {
	return AdminContentHandler{db: db, cfg: cfg}
}

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

func (h AdminContentHandler) GetAdminPost(c *gin.Context) {
	post, ok := h.findPost(c)
	if !ok {
		return
	}

	response.Success(c, adminPostDTO(post))
}

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

func (h AdminContentHandler) GetSettings(c *gin.Context) {
	response.Success(c, h.effectiveSettings())
}

func (h AdminContentHandler) UpdateSettings(c *gin.Context) {
	var values map[string]string
	if err := c.ShouldBindJSON(&values); err != nil {
		badRequest(c)
		return
	}

	allowed := map[string]bool{
		"siteTitle":   true,
		"subtitle":    true,
		"owner":       true,
		"domain":      true,
		"description": true,
		"github":      true,
		"gitee":       true,
		"email":       true,
		"icp":         true,
		"navItems":    true,
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
		"siteTitle":    h.cfg.Site.SiteTitle,
		"subtitle":     h.cfg.Site.Subtitle,
		"owner":        h.cfg.Site.Owner,
		"domain":       h.cfg.Site.Domain,
		"description":  h.cfg.Site.Description,
		"navItems":     strings.Join(h.cfg.Site.NavItems, ","),
		"aiProvider":   h.cfg.AI.Provider,
		"aiModel":      h.cfg.AI.Model,
		"aiBaseURL":    h.cfg.AI.BaseURL,
		"aiConfigured": strconv.FormatBool(strings.TrimSpace(h.cfg.AI.APIKey) != ""),
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
	if strings.Contains(message, "unique") || strings.Contains(message, "constraint") {
		response.Error(c, http.StatusConflict, 409, "数据冲突")
		return
	}

	internalError(c)
}
