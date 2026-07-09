package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"masenyu.top/blog/backend/internal/database"
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
