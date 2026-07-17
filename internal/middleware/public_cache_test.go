package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicReadCacheSetsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PublicReadCache())
	r.GET("/api/posts", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Cache-Control"); got == "" {
		t.Fatalf("expected Cache-Control header")
	}
}

func TestPublicReadCacheSkipsPostDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PublicReadCache())
	r.GET("/api/posts/:slug", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/posts/hello", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("post detail should not be auto-cached, got %q", got)
	}
}
