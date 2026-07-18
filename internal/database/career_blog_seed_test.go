package database

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCareerBlogContentCoverage(t *testing.T) {
	if !utf8.Valid(careerBlogContent) {
		t.Fatal("career blog content is not valid UTF-8")
	}

	var posts []careerSeedPost
	if err := json.Unmarshal(careerBlogContent, &posts); err != nil {
		t.Fatalf("decode embedded career blog content: %v", err)
	}
	if len(posts) != 36 {
		t.Fatalf("expected 36 career timeline posts, got %d", len(posts))
	}

	counts := map[int]int{}
	slugs := map[string]struct{}{}
	for _, post := range posts {
		publishedAt, err := time.Parse(time.RFC3339, post.PublishedAt)
		if err != nil {
			t.Fatalf("invalid published_at for %q: %v", post.Slug, err)
		}
		counts[publishedAt.Year()]++
		if _, exists := slugs[post.Slug]; exists {
			t.Fatalf("duplicate post slug: %q", post.Slug)
		}
		slugs[post.Slug] = struct{}{}
		if post.Title == "" || post.Slug == "" || post.Summary == "" || post.Content == "" || post.Category == "" || post.CategorySlug == "" {
			t.Fatalf("incomplete post seed: %+v", post)
		}
		for _, text := range []string{post.Title, post.Summary, post.Content, post.Category} {
			if strings.ContainsAny(text, "?\ufffd") {
				t.Fatalf("mojibake or placeholder text in %q", post.Slug)
			}
		}
		if len(post.Tags) == 0 {
			t.Fatalf("post %q must have at least one tag", post.Slug)
		}
	}

	for year, want := range map[int]int{2021: 5, 2022: 5, 2023: 5, 2024: 5} {
		if counts[year] < want {
			t.Fatalf("expected at least %d posts in %d, got %d", want, year, counts[year])
		}
	}
	if counts[2025]+counts[2026] < 15 {
		t.Fatalf("expected at least 15 AI engineering posts across 2025-2026, got %d", counts[2025]+counts[2026])
	}
}
