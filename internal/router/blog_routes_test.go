package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/router"
)

func TestPostListEndpointReturnsPublishedPostsWithPagination(t *testing.T) {
	engine := newTestEngine(t)

	recorder := performRequest(engine, http.MethodGet, "/api/posts?page=1&pageSize=2")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Title    string `json:"title"`
				Slug     string `json:"slug"`
				Category struct {
					Name string `json:"name"`
					Slug string `json:"slug"`
				} `json:"category"`
				Tags []struct {
					Name string `json:"name"`
					Slug string `json:"slug"`
				} `json:"tags"`
			} `json:"list"`
			Page     int   `json:"page"`
			PageSize int   `json:"pageSize"`
			Total    int64 `json:"total"`
		} `json:"data"`
	}

	decodeJSON(t, recorder, &body)

	if body.Code != 0 {
		t.Fatalf("expected success code, got %d", body.Code)
	}
	if body.Data.Page != 1 || body.Data.PageSize != 2 {
		t.Fatalf("unexpected pagination metadata: %#v", body.Data)
	}
	if body.Data.Total < 3 {
		t.Fatalf("expected seeded published posts, got total %d", body.Data.Total)
	}
	if len(body.Data.List) != 2 {
		t.Fatalf("expected 2 posts on first page, got %d", len(body.Data.List))
	}
	if body.Data.List[0].Slug == "" || body.Data.List[0].Category.Slug == "" || len(body.Data.List[0].Tags) == 0 {
		t.Fatalf("expected post summary to include slug, category, and tags: %#v", body.Data.List[0])
	}
}

func TestPostDetailEndpointReturnsMarkdownAndAdjacentPosts(t *testing.T) {
	engine := newTestEngine(t)

	recorder := performRequest(engine, http.MethodGet, "/api/posts/go-gin-postgresql-blog")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Code int `json:"code"`
		Data struct {
			Title   string `json:"title"`
			Slug    string `json:"slug"`
			Content string `json:"content"`
			Prev    *struct {
				Slug string `json:"slug"`
			} `json:"prev"`
			Next *struct {
				Slug string `json:"slug"`
			} `json:"next"`
		} `json:"data"`
	}

	decodeJSON(t, recorder, &body)

	if body.Data.Slug != "go-gin-postgresql-blog" {
		t.Fatalf("unexpected slug %q", body.Data.Slug)
	}
	if body.Data.Content == "" {
		t.Fatal("expected markdown content")
	}
	if body.Data.Prev == nil && body.Data.Next == nil {
		t.Fatal("expected at least one adjacent post")
	}
}

func TestPostDetailEndpointIncrementsViewCount(t *testing.T) {
	engine := newTestEngine(t)

	first := performRequest(engine, http.MethodGet, "/api/posts/go-gin-postgresql-blog")
	if first.Code != http.StatusOK {
		t.Fatalf("expected first detail status 200, got %d with body %s", first.Code, first.Body.String())
	}
	firstCount := decodePostViewCount(t, first)

	second := performRequest(engine, http.MethodGet, "/api/posts/go-gin-postgresql-blog")
	if second.Code != http.StatusOK {
		t.Fatalf("expected second detail status 200, got %d with body %s", second.Code, second.Body.String())
	}
	secondCount := decodePostViewCount(t, second)

	if secondCount != firstCount+1 {
		t.Fatalf("expected view count to increment from %d to %d, got %d", firstCount, firstCount+1, secondCount)
	}
}

func TestCategoryTagSearchAndArchiveEndpoints(t *testing.T) {
	engine := newTestEngine(t)

	categories := performRequest(engine, http.MethodGet, "/api/categories")
	if categories.Code != http.StatusOK {
		t.Fatalf("expected categories status 200, got %d", categories.Code)
	}
	assertListHasItems(t, categories)

	categoryPosts := performRequest(engine, http.MethodGet, "/api/categories/go/posts")
	if categoryPosts.Code != http.StatusOK {
		t.Fatalf("expected category posts status 200, got %d", categoryPosts.Code)
	}
	assertListHasItems(t, categoryPosts)

	tags := performRequest(engine, http.MethodGet, "/api/tags")
	if tags.Code != http.StatusOK {
		t.Fatalf("expected tags status 200, got %d", tags.Code)
	}
	assertListHasItems(t, tags)

	tagPosts := performRequest(engine, http.MethodGet, "/api/tags/backend/posts")
	if tagPosts.Code != http.StatusOK {
		t.Fatalf("expected tag posts status 200, got %d", tagPosts.Code)
	}
	assertListHasItems(t, tagPosts)

	search := performRequest(engine, http.MethodGet, "/api/search?q=PostgreSQL")
	if search.Code != http.StatusOK {
		t.Fatalf("expected search status 200, got %d", search.Code)
	}
	assertListHasItems(t, search)

	archive := performRequest(engine, http.MethodGet, "/api/archive")
	if archive.Code != http.StatusOK {
		t.Fatalf("expected archive status 200, got %d", archive.Code)
	}
	assertListHasItems(t, archive)
}

func TestPublicProjectsEndpointReturnsReadableServerError(t *testing.T) {
	engine, sqlDB := newTestEngineWithDatabase(t)

	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	recorder := performRequest(engine, http.MethodGet, "/api/projects")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected projects status 500, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	decodeJSON(t, recorder, &body)

	if body.Code != 500 {
		t.Fatalf("expected response code 500, got %d", body.Code)
	}
	if body.Message != "服务端错误" {
		t.Fatalf("unexpected server error message %q", body.Message)
	}
}

func newTestEngine(t *testing.T) http.Handler {
	t.Helper()

	engine, _ := newTestEngineWithDatabase(t)
	return engine
}

func newTestEngineWithDatabase(t *testing.T) (http.Handler, interface{ Close() error }) {
	t.Helper()

	cfg := testDatabaseConfig(t)
	resetPostgresSchema(t, cfg)

	db, err := database.Open(database.Options{Config: cfg})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	sqlDB := trackSQLDatabase(t, db)

	return router.New(router.Dependencies{
		Config:   cfg,
		Database: db,
	}), sqlDB
}

func performRequest(handler http.Handler, method string, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	return recorder
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func decodePostViewCount(t *testing.T, recorder *httptest.ResponseRecorder) int {
	t.Helper()

	var body struct {
		Code int `json:"code"`
		Data struct {
			ViewCount int `json:"viewCount"`
		} `json:"data"`
	}
	decodeJSON(t, recorder, &body)

	if body.Code != 0 {
		t.Fatalf("expected success code, got %d", body.Code)
	}

	return body.Data.ViewCount
}

func assertListHasItems(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	var body struct {
		Code int `json:"code"`
		Data struct {
			List []json.RawMessage `json:"list"`
		} `json:"data"`
	}
	decodeJSON(t, recorder, &body)

	if body.Code != 0 {
		t.Fatalf("expected success code, got %d", body.Code)
	}
	if len(body.Data.List) == 0 {
		t.Fatalf("expected non-empty list, got body %s", recorder.Body.String())
	}
}
