package router_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestRSSFeedReturnsPublishedPostsXML(t *testing.T) {
	engine := newTestEngine(t)

	recorder := performRequest(engine, http.MethodGet, "/api/rss.xml")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `<?xml`) {
		t.Fatalf("expected xml declaration, got %s", body)
	}
	if !strings.Contains(body, "<rss") {
		t.Fatalf("expected rss root, got %s", body)
	}
	if !strings.Contains(body, "/posts/") {
		t.Fatalf("expected post links in feed, got %s", body)
	}
}

func TestSitemapIncludesCoreRoutesAndPosts(t *testing.T) {
	engine := newTestEngine(t)

	recorder := performRequest(engine, http.MethodGet, "/sitemap.xml")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	body := recorder.Body.String()
	for _, needle := range []string{"urlset", "/posts", "/about", "https://masenyu.top"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected sitemap to contain %q, got %s", needle, body)
		}
	}
}

func TestRobotsTxtPointsToSitemap(t *testing.T) {
	engine := newTestEngine(t)

	recorder := performRequest(engine, http.MethodGet, "/robots.txt")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "User-agent: *") {
		t.Fatalf("expected robots user-agent, got %s", body)
	}
	if !strings.Contains(body, "Sitemap: https://masenyu.top/api/sitemap.xml") {
		t.Fatalf("expected sitemap directive, got %s", body)
	}
	if !strings.Contains(body, "Disallow: /admin") {
		t.Fatalf("expected admin disallow, got %s", body)
	}
}