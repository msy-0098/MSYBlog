package database

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
)

//go:embed career_blog_content.json
var careerBlogContent []byte

type careerSeedPost struct {
	Title        string   `json:"title"`
	Slug         string   `json:"slug"`
	Summary      string   `json:"summary"`
	Category     string   `json:"category"`
	CategorySlug string   `json:"category_slug"`
	Tags         []string `json:"tags"`
	PublishedAt  string   `json:"published_at"`
	Cover        string   `json:"cover"`
	ViewCount    int      `json:"view_count"`
	Content      string   `json:"content"`
}

type careerSeedTaxonomy struct {
	Name string
	Slug string
}

var careerCategories = []careerSeedTaxonomy{
	{Name: "Java\u540e\u7aef", Slug: "java-backend"},
	{Name: "\u6570\u636e\u4e0e\u6027\u80fd", Slug: "data-performance"},
	{Name: "\u524d\u7aef\u5de5\u7a0b\u5316", Slug: "frontend-engineering"},
	{Name: "\u5fae\u670d\u52a1\u67b6\u6784", Slug: "microservices"},
	{Name: "AI\u5de5\u7a0b\u5316", Slug: "ai-engineering"},
	{Name: "\u5de5\u7a0b\u5316\u4e0e\u90e8\u7f72", Slug: "devops"},
}

var careerTags = []careerSeedTaxonomy{
	{Name: "Java", Slug: "java"}, {Name: "Go", Slug: "go"}, {Name: "Seata", Slug: "seata"}, {Name: "Spring Boot", Slug: "spring-boot"}, {Name: "Spring Cloud", Slug: "spring-cloud"},
	{Name: "Spring AI", Slug: "spring-ai"}, {Name: "Spring AI Alibaba", Slug: "spring-ai-alibaba"}, {Name: "JDK 17", Slug: "jdk17"},
	{Name: "JDK 21", Slug: "jdk21"}, {Name: "Jakarta", Slug: "jakarta"}, {Name: "migration", Slug: "migration"},
	{Name: "MySQL", Slug: "mysql"}, {Name: "PostgreSQL", Slug: "postgres"}, {Name: "pgvector", Slug: "pgvector"},
	{Name: "Elasticsearch", Slug: "elasticsearch"}, {Name: "Redis", Slug: "redis"}, {Name: "Redisson", Slug: "redisson"},
	{Name: "performance", Slug: "performance"}, {Name: "RabbitMQ", Slug: "rabbitmq"}, {Name: "async", Slug: "async"},
	{Name: "WebSocket", Slug: "websocket"}, {Name: "Nacos", Slug: "nacos"}, {Name: "Sentinel", Slug: "sentinel"},
	{Name: "Gateway", Slug: "gateway"}, {Name: "Vue", Slug: "vue"}, {Name: "frontend", Slug: "frontend"},
	{Name: "engineering", Slug: "engineering"}, {Name: "Gitea", Slug: "gitea"}, {Name: "Jenkins", Slug: "jenkins"},
	{Name: "Docker", Slug: "docker"}, {Name: "CI/CD", Slug: "cicd"}, {Name: "Nginx", Slug: "nginx"},
	{Name: "deploy", Slug: "deploy"}, {Name: "AI", Slug: "ai"}, {Name: "Prompt", Slug: "prompt"},
	{Name: "RAG", Slug: "rag"}, {Name: "observability", Slug: "observability"}, {Name: "LangChain4j", Slug: "langchain4j"},
	{Name: "LangGraph", Slug: "langgraph"}, {Name: "DeerFlow", Slug: "deerflow"}, {Name: "Agent", Slug: "agent"},
	{Name: "MCP", Slug: "mcp"}, {Name: "LLM", Slug: "llm"}, {Name: "security", Slug: "security"},
	{Name: "SSE", Slug: "sse"}, {Name: "JSON Schema", Slug: "json-schema"}, {Name: "backend", Slug: "backend"},
}

func SeedCareerTimelinePosts(db *gorm.DB) error {
	var posts []careerSeedPost
	if err := json.Unmarshal(careerBlogContent, &posts); err != nil {
		return fmt.Errorf("decode career blog content: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, item := range careerCategories {
			var category model.Category
			if err := tx.Where("slug = ?", item.Slug).First(&category).Error; err != nil {
				if err != gorm.ErrRecordNotFound {
					return fmt.Errorf("find category %q: %w", item.Slug, err)
				}
				category = model.Category{Slug: item.Slug}
			}
			category.Name = item.Name
			if err := tx.Save(&category).Error; err != nil {
				return fmt.Errorf("seed category %q: %w", item.Slug, err)
			}
		}
		for _, item := range careerTags {
			var tag model.Tag
			if err := tx.Where("slug = ?", item.Slug).First(&tag).Error; err != nil {
				if err != gorm.ErrRecordNotFound {
					return fmt.Errorf("find tag %q: %w", item.Slug, err)
				}
				tag = model.Tag{Slug: item.Slug}
			}
			tag.Name = item.Name
			if err := tx.Save(&tag).Error; err != nil {
				return fmt.Errorf("seed tag %q: %w", item.Slug, err)
			}
		}
		for _, seed := range posts {
			publishedAt, err := time.Parse(time.RFC3339, seed.PublishedAt)
			if err != nil {
				return fmt.Errorf("parse published_at for %q: %w", seed.Slug, err)
			}
			var category model.Category
			if err := tx.Where("slug = ?", categorySlug(seed)).First(&category).Error; err != nil {
				return fmt.Errorf("find category %q for %q: %w", categorySlug(seed), seed.Slug, err)
			}
			var post model.Post
			isNewPost := false
			if err := tx.Where("slug = ?", seed.Slug).First(&post).Error; err != nil {
				if err != gorm.ErrRecordNotFound {
					return fmt.Errorf("find post %q: %w", seed.Slug, err)
				}
				post = model.Post{Slug: seed.Slug, Status: model.PostStatusPublished, ViewCount: seed.ViewCount}
				isNewPost = true
			}
			post.Title, post.Summary, post.Content, post.Cover = seed.Title, seed.Summary, seed.Content, seed.Cover
			post.CategoryID, post.PublishedAt = category.ID, publishedAt
			if isNewPost {
				post.Status = model.PostStatusPublished
			}
			if err := tx.Save(&post).Error; err != nil {
				return fmt.Errorf("save post %q: %w", seed.Slug, err)
			}
			var tags []model.Tag
			if err := tx.Where("slug IN ?", seed.Tags).Find(&tags).Error; err != nil {
				return fmt.Errorf("find tags for %q: %w", seed.Slug, err)
			}
			if len(tags) != len(seed.Tags) {
				return fmt.Errorf("post %q references %d tags but only found %d", seed.Slug, len(seed.Tags), len(tags))
			}
			if err := tx.Model(&post).Association("Tags").Replace(tags); err != nil {
				return fmt.Errorf("replace tags for %q: %w", seed.Slug, err)
			}
		}
		return nil
	})
}

func categorySlug(seed careerSeedPost) string {
	if seed.CategorySlug != "" {
		return seed.CategorySlug
	}
	for _, category := range careerCategories {
		if category.Name == seed.Category {
			return category.Slug
		}
	}
	return seed.Category
}
