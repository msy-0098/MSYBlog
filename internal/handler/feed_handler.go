package handler

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type FeedHandler struct {
	db       *gorm.DB
	fallback config.SiteConfig
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

type urlSet struct {
	XMLName xml.Name  `xml:"urlset"`
	Xmlns   string    `xml:"xmlns,attr"`
	URLs    []siteURL `xml:"url"`
}

type siteURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

func NewFeedHandler(db *gorm.DB, cfg config.Config) FeedHandler {
	return FeedHandler{db: db, fallback: cfg.Site}
}

func (h FeedHandler) RSS(c *gin.Context) {
	profile, err := h.loadProfile()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	posts, err := h.listPublishedPosts(50)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	base := profile.baseURL
	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:       profile.title,
			Link:        base + "/",
			Description: profile.description,
			Language:    "zh-CN",
			Items:       make([]rssItem, 0, len(posts)),
		},
	}

	for _, post := range posts {
		link := base + "/posts/" + post.Slug
		feed.Channel.Items = append(feed.Channel.Items, rssItem{
			Title:       post.Title,
			Link:        link,
			GUID:        link,
			Description: post.Summary,
			PubDate:     post.PublishedAt.UTC().Format(time.RFC1123Z),
		})
	}

	writeXML(c, feed)
}

func (h FeedHandler) Sitemap(c *gin.Context) {
	profile, err := h.loadProfile()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	posts, err := h.listPublishedPosts(500)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	base := profile.baseURL
	urls := []siteURL{
		{Loc: base + "/", ChangeFreq: "daily", Priority: "1.0"},
		{Loc: base + "/posts", ChangeFreq: "daily", Priority: "0.9"},
		{Loc: base + "/categories", ChangeFreq: "weekly", Priority: "0.6"},
		{Loc: base + "/tags", ChangeFreq: "weekly", Priority: "0.6"},
		{Loc: base + "/archive", ChangeFreq: "weekly", Priority: "0.5"},
		{Loc: base + "/projects", ChangeFreq: "weekly", Priority: "0.7"},
		{Loc: base + "/about", ChangeFreq: "monthly", Priority: "0.5"},
	}

	for _, post := range posts {
		lastMod := post.UpdatedAt
		if lastMod.IsZero() {
			lastMod = post.PublishedAt
		}
		urls = append(urls, siteURL{
			Loc:        base + "/posts/" + post.Slug,
			LastMod:    lastMod.UTC().Format("2006-01-02"),
			ChangeFreq: "weekly",
			Priority:   "0.8",
		})
	}

	writeXML(c, urlSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	})
}

func (h FeedHandler) Robots(c *gin.Context) {
	profile, err := h.loadProfile()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	body := strings.Join([]string{
		"User-agent: *",
		"Allow: /",
		"Disallow: /admin",
		"Disallow: /api/admin",
		"",
		"Sitemap: " + profile.baseURL + "/api/sitemap.xml",
		"",
	}, "\n")

	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

type feedProfile struct {
	title       string
	description string
	baseURL     string
}

func (h FeedHandler) loadProfile() (feedProfile, error) {
	var rows []model.SiteSetting
	if err := h.db.Find(&rows).Error; err != nil {
		return feedProfile{}, err
	}

	settings := make(map[string]string, len(rows))
	for _, row := range rows {
		settings[row.Key] = row.Value
	}

	domain := firstNonEmpty(settings["domain"], h.fallback.Domain, "masenyu.top")
	return feedProfile{
		title:       firstNonEmpty(settings["siteTitle"], h.fallback.SiteTitle, "马森雨的技术博客"),
		description: firstNonEmpty(settings["description"], h.fallback.Description, "个人技术博客"),
		baseURL:     normalizeSiteBase(domain),
	}, nil
}

func (h FeedHandler) listPublishedPosts(limit int) ([]model.Post, error) {
	var posts []model.Post
	err := h.db.Where("status = ?", model.PostStatusPublished).
		Order("published_at desc").
		Limit(limit).
		Find(&posts).Error
	return posts, err
}

func normalizeSiteBase(domain string) string {
	value := strings.TrimSpace(domain)
	value = strings.TrimRight(value, "/")
	if value == "" {
		return "https://masenyu.top"
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "https://" + value
}

func writeXML(c *gin.Context, payload any) {
	output, err := xml.MarshalIndent(payload, "", "  ")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务端错误")
		return
	}

	c.Header("Cache-Control", "public, max-age=600")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", append([]byte(xml.Header), output...))
}