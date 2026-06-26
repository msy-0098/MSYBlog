package router_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"testing"
)

func TestAdminContentCreatesPublishedPostVisibleToVisitors(t *testing.T) {
	engine := newAdminAuthTestEngine(t)
	token := loginAndGetToken(t, engine)

	category := createAdminCategory(t, engine, token, "AI Tools", "ai-tools")
	tag := createAdminTag(t, engine, token, "TDD", "tdd")

	createPost := performJSONRequest(engine, http.MethodPost, "/api/admin/posts", map[string]any{
		"title":       "Admin Published Post",
		"slug":        "admin-published-post",
		"summary":     "Created from admin tests",
		"content":     "# Admin Post\n\nPublished from the dashboard.",
		"cover":       "/uploads/admin-cover.png",
		"status":      "published",
		"categoryId":  category.ID,
		"tagIds":      []uint{tag.ID},
		"publishedAt": "2026-06-26T10:00:00Z",
	}, token)

	if createPost.Code != http.StatusOK {
		t.Fatalf("expected create post status 200, got %d with body %s", createPost.Code, createPost.Body.String())
	}

	var created struct {
		Code int `json:"code"`
		Data struct {
			ID     uint   `json:"id"`
			Slug   string `json:"slug"`
			Status string `json:"status"`
		} `json:"data"`
	}
	decodeJSON(t, createPost, &created)
	if created.Data.ID == 0 || created.Data.Slug != "admin-published-post" || created.Data.Status != "published" {
		t.Fatalf("unexpected created post: %#v", created.Data)
	}

	publicDetail := performRequest(engine, http.MethodGet, "/api/posts/admin-published-post")
	if publicDetail.Code != http.StatusOK {
		t.Fatalf("expected public detail status 200, got %d with body %s", publicDetail.Code, publicDetail.Body.String())
	}

	updatePost := performJSONRequest(engine, http.MethodPut, "/api/admin/posts/"+itoa(created.Data.ID), map[string]any{
		"title":      "Updated Admin Published Post",
		"slug":       "admin-published-post-updated",
		"summary":    "Updated summary",
		"content":    "# Updated",
		"status":     "draft",
		"categoryId": category.ID,
		"tagIds":     []uint{tag.ID},
	}, token)
	if updatePost.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d with body %s", updatePost.Code, updatePost.Body.String())
	}

	deletePost := performJSONRequest(engine, http.MethodDelete, "/api/admin/posts/"+itoa(created.Data.ID), nil, token)
	if deletePost.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d with body %s", deletePost.Code, deletePost.Body.String())
	}
}

func TestAdminContentManagesCategoryTagProjectAndSettings(t *testing.T) {
	engine := newAdminAuthTestEngine(t)
	token := loginAndGetToken(t, engine)

	category := createAdminCategory(t, engine, token, "Backend", "admin-backend")
	updateCategory := performJSONRequest(engine, http.MethodPut, "/api/admin/categories/"+itoa(category.ID), map[string]string{
		"name": "Backend Notes",
		"slug": "admin-backend-notes",
	}, token)
	if updateCategory.Code != http.StatusOK {
		t.Fatalf("expected category update status 200, got %d with body %s", updateCategory.Code, updateCategory.Body.String())
	}

	tag := createAdminTag(t, engine, token, "Go", "admin-go")
	updateTag := performJSONRequest(engine, http.MethodPut, "/api/admin/tags/"+itoa(tag.ID), map[string]string{
		"name": "Go Language",
		"slug": "admin-go-language",
	}, token)
	if updateTag.Code != http.StatusOK {
		t.Fatalf("expected tag update status 200, got %d with body %s", updateTag.Code, updateTag.Body.String())
	}

	project := performJSONRequest(engine, http.MethodPost, "/api/admin/projects", map[string]any{
		"name":        "Blog Admin",
		"description": "Admin dashboard",
		"url":         "https://masenyu.top",
		"cover":       "/uploads/project.png",
		"techStack":   []string{"Go", "Vue"},
		"sort":        10,
		"visible":     true,
	}, token)
	if project.Code != http.StatusOK {
		t.Fatalf("expected project create status 200, got %d with body %s", project.Code, project.Body.String())
	}

	settings := performJSONRequest(engine, http.MethodPut, "/api/admin/settings", map[string]string{
		"siteTitle": "Admin Updated Blog",
		"subtitle":  "Updated from admin",
		"owner":     "马森雨",
		"domain":    "masenyu.top",
		"navItems":  "Home,Posts,Projects",
	}, token)
	if settings.Code != http.StatusOK {
		t.Fatalf("expected settings update status 200, got %d with body %s", settings.Code, settings.Body.String())
	}

	readSettings := performJSONRequest(engine, http.MethodGet, "/api/admin/settings", nil, token)
	if readSettings.Code != http.StatusOK {
		t.Fatalf("expected settings read status 200, got %d with body %s", readSettings.Code, readSettings.Body.String())
	}

	var readSettingsBody struct {
		Data map[string]string `json:"data"`
	}
	decodeJSON(t, readSettings, &readSettingsBody)
	if readSettingsBody.Data["navItems"] != "Home,Posts,Projects" {
		t.Fatalf("expected navItems setting to be saved, got %#v", readSettingsBody.Data)
	}

	publicSite := performRequest(engine, http.MethodGet, "/api/site")
	if publicSite.Code != http.StatusOK {
		t.Fatalf("expected public site status 200, got %d with body %s", publicSite.Code, publicSite.Body.String())
	}

	var publicSiteBody struct {
		Data struct {
			NavItems []string `json:"navItems"`
		} `json:"data"`
	}
	decodeJSON(t, publicSite, &publicSiteBody)
	if len(publicSiteBody.Data.NavItems) != 3 || publicSiteBody.Data.NavItems[0] != "Home" || publicSiteBody.Data.NavItems[2] != "Projects" {
		t.Fatalf("expected public navItems to use admin settings, got %#v", publicSiteBody.Data.NavItems)
	}
}

func TestPublicProjectsEndpointReturnsOnlyVisibleProjects(t *testing.T) {
	engine := newAdminAuthTestEngine(t)
	token := loginAndGetToken(t, engine)

	visible := performJSONRequest(engine, http.MethodPost, "/api/admin/projects", map[string]any{
		"name":        "Visible Public Project",
		"description": "Shown on the visitor projects page",
		"url":         "https://masenyu.top/visible",
		"cover":       "/uploads/visible.png",
		"techStack":   []string{"Go", "Vue"},
		"sort":        20,
		"visible":     true,
	}, token)
	if visible.Code != http.StatusOK {
		t.Fatalf("expected visible project create status 200, got %d with body %s", visible.Code, visible.Body.String())
	}

	hidden := performJSONRequest(engine, http.MethodPost, "/api/admin/projects", map[string]any{
		"name":        "Hidden Draft Project",
		"description": "Kept out of the visitor projects page",
		"url":         "https://masenyu.top/hidden",
		"cover":       "/uploads/hidden.png",
		"techStack":   []string{"Go"},
		"sort":        10,
		"visible":     false,
	}, token)
	if hidden.Code != http.StatusOK {
		t.Fatalf("expected hidden project create status 200, got %d with body %s", hidden.Code, hidden.Body.String())
	}

	publicProjects := performRequest(engine, http.MethodGet, "/api/projects")
	if publicProjects.Code != http.StatusOK {
		t.Fatalf("expected public projects status 200, got %d with body %s", publicProjects.Code, publicProjects.Body.String())
	}

	var body struct {
		Data struct {
			List []struct {
				Name      string   `json:"name"`
				Visible   bool     `json:"visible"`
				TechStack []string `json:"techStack"`
			} `json:"list"`
		} `json:"data"`
	}
	decodeJSON(t, publicProjects, &body)

	var foundVisible bool
	for _, project := range body.Data.List {
		if project.Name == "Hidden Draft Project" {
			t.Fatalf("hidden project leaked into public list: %#v", project)
		}
		if project.Name == "Visible Public Project" {
			foundVisible = true
			if !project.Visible || len(project.TechStack) != 2 {
				t.Fatalf("unexpected visible project payload: %#v", project)
			}
		}
	}
	if !foundVisible {
		t.Fatalf("expected visible project in public list, got %#v", body.Data.List)
	}
}

func TestAdminUploadAcceptsImagesAndRejectsTextFiles(t *testing.T) {
	t.Chdir(t.TempDir())

	engine := newAdminAuthTestEngine(t)
	token := loginAndGetToken(t, engine)

	imageContent := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x00, 0x00, '\r', 'I', 'H', 'D', 'R'}
	imageUpload := performMultipartUpload(t, engine, "/api/admin/upload", "image.png", "image/png", imageContent, token)
	if imageUpload.Code != http.StatusOK {
		t.Fatalf("expected image upload status 200, got %d with body %s", imageUpload.Code, imageUpload.Body.String())
	}

	var uploadBody struct {
		Data struct {
			Path     string `json:"path"`
			MimeType string `json:"mimeType"`
		} `json:"data"`
	}
	decodeJSON(t, imageUpload, &uploadBody)
	if uploadBody.Data.MimeType != "image/png" {
		t.Fatalf("expected detected image/png MIME type, got %q", uploadBody.Data.MimeType)
	}

	uploadedImage := performRequest(engine, http.MethodGet, uploadBody.Data.Path)
	if uploadedImage.Code != http.StatusOK {
		t.Fatalf("expected uploaded image to be served, got %d with body %s", uploadedImage.Code, uploadedImage.Body.String())
	}
	if !bytes.Equal(uploadedImage.Body.Bytes(), imageContent) {
		t.Fatalf("expected uploaded image bytes to be served back")
	}

	textUpload := performMultipartUpload(t, engine, "/api/admin/upload", "note.txt", "text/plain", []byte("hello"), token)
	if textUpload.Code != http.StatusBadRequest {
		t.Fatalf("expected text upload status 400, got %d with body %s", textUpload.Code, textUpload.Body.String())
	}

	spoofedUpload := performMultipartUpload(t, engine, "/api/admin/upload", "spoof.png", "image/png", []byte("not really an image"), token)
	if spoofedUpload.Code != http.StatusBadRequest {
		t.Fatalf("expected spoofed image upload status 400, got %d with body %s", spoofedUpload.Code, spoofedUpload.Body.String())
	}
}

type createdTaxonomy struct {
	ID   uint
	Name string
	Slug string
}

func createAdminCategory(t *testing.T, handler http.Handler, token string, name string, slug string) createdTaxonomy {
	t.Helper()

	recorder := performJSONRequest(handler, http.MethodPost, "/api/admin/categories", map[string]string{
		"name": name,
		"slug": slug,
	}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected category create status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	return decodeCreatedTaxonomy(t, recorder)
}

func createAdminTag(t *testing.T, handler http.Handler, token string, name string, slug string) createdTaxonomy {
	t.Helper()

	recorder := performJSONRequest(handler, http.MethodPost, "/api/admin/tags", map[string]string{
		"name": name,
		"slug": slug,
	}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected tag create status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	return decodeCreatedTaxonomy(t, recorder)
}

func decodeCreatedTaxonomy(t *testing.T, recorder *httptest.ResponseRecorder) createdTaxonomy {
	t.Helper()

	var body struct {
		Data createdTaxonomy `json:"data"`
	}
	decodeJSON(t, recorder, &body)
	if body.Data.ID == 0 || body.Data.Slug == "" {
		t.Fatalf("expected created taxonomy, got %s", recorder.Body.String())
	}

	return body.Data
}

func itoa(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

func performMultipartUpload(t *testing.T, handler http.Handler, target string, filename string, contentType string, content []byte, token string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}
