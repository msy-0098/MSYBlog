package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/router"
)

func TestSiteEndpointReturnsDefaultSiteProfile(t *testing.T) {
	cfg := testDatabaseConfig(t)
	resetPostgresSchema(t, cfg)

	db, err := database.Open(database.Options{Config: cfg})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	trackSQLDatabase(t, db)

	engine := router.New(router.Dependencies{
		Config:   cfg,
		Database: db,
	})

	request := httptest.NewRequest(http.MethodGet, "/api/site", nil)
	response := httptest.NewRecorder()

	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			SiteTitle   string   `json:"siteTitle"`
			Subtitle    string   `json:"subtitle"`
			Owner       string   `json:"owner"`
			Domain      string   `json:"domain"`
			Description string   `json:"description"`
			NavItems    []string `json:"navItems"`
		} `json:"data"`
	}

	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.Code != 0 {
		t.Fatalf("expected code 0, got %d", body.Code)
	}
	if body.Message != "success" {
		t.Fatalf("expected success message, got %q", body.Message)
	}
	if body.Data.SiteTitle != "马森雨的技术博客" {
		t.Fatalf("unexpected site title %q", body.Data.SiteTitle)
	}
	if body.Data.Owner != "马森雨" {
		t.Fatalf("unexpected owner %q", body.Data.Owner)
	}
	if body.Data.Domain != "masenyu.top" {
		t.Fatalf("unexpected domain %q", body.Data.Domain)
	}
	if len(body.Data.NavItems) < 5 {
		t.Fatalf("expected visitor navigation items, got %#v", body.Data.NavItems)
	}
}

func TestSwaggerEndpoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:swagger_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite memory db: %v", err)
	}
	_ = db.AutoMigrate(&model.User{}, &model.AccessLog{}, &model.IPBan{})

	cfg := config.Config{
		Auth: config.AuthConfig{JWTSecret: "test-secret-key-1234567890123456"},
		Site: config.SiteConfig{Domain: "masenyu.top"},
	}

	engine := router.New(router.Dependencies{
		Config:   cfg,
		Database: db,
	})

	// 1. 测试 /swagger/index.html
	req1 := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	resp1 := httptest.NewRecorder()
	engine.ServeHTTP(resp1, req1)
	if resp1.Code != http.StatusOK {
		t.Fatalf("expected /swagger/index.html status 200, got %d", resp1.Code)
	}

	// 2. 测试 /swagger/doc.json
	req2 := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	resp2 := httptest.NewRecorder()
	engine.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusOK {
		t.Fatalf("expected /swagger/doc.json status 200, got %d", resp2.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(resp2.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal /swagger/doc.json failed: %v", err)
	}
	if doc["swagger"] != "2.0" {
		t.Fatalf("expected swagger version 2.0, got %v", doc["swagger"])
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) < 10 {
		t.Fatalf("expected at least 10 paths in swagger doc, got %d", len(paths))
	}

	// 3. 测试 /api/swagger/index.html
	req3 := httptest.NewRequest(http.MethodGet, "/api/swagger/index.html", nil)
	resp3 := httptest.NewRecorder()
	engine.ServeHTTP(resp3, req3)
	if resp3.Code != http.StatusOK {
		t.Fatalf("expected /api/swagger/index.html status 200, got %d", resp3.Code)
	}
}

