package router_test

import (
	"net/http"
	"testing"
)

func TestVisitorRegistrationLoginAndCommentFlow(t *testing.T) {
	engine := newAdminAuthTestEngine(t)

	sendCode := performJSONRequest(engine, http.MethodPost, "/api/auth/email-code", map[string]string{
		"email": "reader@example.com",
	}, "")
	if sendCode.Code != http.StatusOK {
		t.Fatalf("expected email code status 200, got %d with body %s", sendCode.Code, sendCode.Body.String())
	}

	register := performJSONRequest(engine, http.MethodPost, "/api/auth/register", map[string]string{
		"email":    "reader@example.com",
		"nickname": "认真读者",
		"password": "reader-password",
		"code":     "000000",
	}, "")
	if register.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d with body %s", register.Code, register.Body.String())
	}

	var registerBody struct {
		Data struct {
			Token string `json:"token"`
			User  struct {
				Email    string `json:"email"`
				Nickname string `json:"nickname"`
				Role     string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	decodeJSON(t, register, &registerBody)
	if registerBody.Data.Token == "" {
		t.Fatal("expected visitor token after registration")
	}
	if registerBody.Data.User.Email != "reader@example.com" || registerBody.Data.User.Nickname != "认真读者" || registerBody.Data.User.Role != "visitor" {
		t.Fatalf("unexpected registered user payload: %#v", registerBody.Data.User)
	}

	login := performJSONRequest(engine, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "reader@example.com",
		"password": "reader-password",
	}, "")
	if login.Code != http.StatusOK {
		t.Fatalf("expected visitor login status 200, got %d with body %s", login.Code, login.Body.String())
	}

	var loginBody struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeJSON(t, login, &loginBody)
	if loginBody.Data.Token == "" {
		t.Fatal("expected visitor login token")
	}

	comment := performJSONRequest(engine, http.MethodPost, "/api/posts/go-gin-postgresql-blog/comments", map[string]string{
		"content": "这篇文章的部署思路很清楚呀",
	}, loginBody.Data.Token)
	if comment.Code != http.StatusOK {
		t.Fatalf("expected comment create status 200, got %d with body %s", comment.Code, comment.Body.String())
	}

	comments := performRequest(engine, http.MethodGet, "/api/posts/go-gin-postgresql-blog/comments")
	if comments.Code != http.StatusOK {
		t.Fatalf("expected comments list status 200, got %d with body %s", comments.Code, comments.Body.String())
	}

	var commentsBody struct {
		Data struct {
			List []struct {
				Content string `json:"content"`
				Author  struct {
					Email    string `json:"email"`
					Nickname string `json:"nickname"`
				} `json:"author"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	decodeJSON(t, comments, &commentsBody)
	if commentsBody.Data.Total != 1 || len(commentsBody.Data.List) != 1 {
		t.Fatalf("expected one visible comment, got %#v", commentsBody.Data)
	}
	if commentsBody.Data.List[0].Content != "这篇文章的部署思路很清楚呀" || commentsBody.Data.List[0].Author.Nickname != "认真读者" {
		t.Fatalf("unexpected comment payload: %#v", commentsBody.Data.List[0])
	}
}

func TestVisitorCommentRequiresLogin(t *testing.T) {
	engine := newAdminAuthTestEngine(t)

	comment := performJSONRequest(engine, http.MethodPost, "/api/posts/go-gin-postgresql-blog/comments", map[string]string{
		"content": "未登录不应该评论",
	}, "")
	if comment.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated comment status 401, got %d with body %s", comment.Code, comment.Body.String())
	}
}

func TestVisitorCannotLoginThroughAdminEndpoint(t *testing.T) {
	engine := newAdminAuthTestEngine(t)

	sendCode := performJSONRequest(engine, http.MethodPost, "/api/auth/email-code", map[string]string{
		"email": "visitor-admin-wall@example.com",
	}, "")
	if sendCode.Code != http.StatusOK {
		t.Fatalf("expected email code status 200, got %d with body %s", sendCode.Code, sendCode.Body.String())
	}

	register := performJSONRequest(engine, http.MethodPost, "/api/auth/register", map[string]string{
		"email":    "visitor-admin-wall@example.com",
		"nickname": "访客",
		"password": "reader-password",
		"code":     "000000",
	}, "")
	if register.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d with body %s", register.Code, register.Body.String())
	}

	adminLogin := performJSONRequest(engine, http.MethodPost, "/api/admin/login", map[string]string{
		"username": "visitor-admin-wall@example.com",
		"password": "reader-password",
	}, "")
	if adminLogin.Code != http.StatusUnauthorized {
		t.Fatalf("expected visitor admin login status 401, got %d with body %s", adminLogin.Code, adminLogin.Body.String())
	}
}

func TestAdminDashboardAndCommentManagement(t *testing.T) {
	engine := newAdminAuthTestEngine(t)
	adminToken := loginAndGetToken(t, engine)

	sendCode := performJSONRequest(engine, http.MethodPost, "/api/auth/email-code", map[string]string{
		"email": "stats-reader@example.com",
	}, "")
	if sendCode.Code != http.StatusOK {
		t.Fatalf("expected email code status 200, got %d with body %s", sendCode.Code, sendCode.Body.String())
	}

	register := performJSONRequest(engine, http.MethodPost, "/api/auth/register", map[string]string{
		"email":    "stats-reader@example.com",
		"nickname": "统计读者",
		"password": "reader-password",
		"code":     "000000",
	}, "")
	if register.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d with body %s", register.Code, register.Body.String())
	}

	var registerBody struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeJSON(t, register, &registerBody)

	createComment := performJSONRequest(engine, http.MethodPost, "/api/posts/go-gin-postgresql-blog/comments", map[string]string{
		"content": "后台统计需要看到这条评论",
	}, registerBody.Data.Token)
	if createComment.Code != http.StatusOK {
		t.Fatalf("expected create comment status 200, got %d with body %s", createComment.Code, createComment.Body.String())
	}

	var createCommentBody struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	decodeJSON(t, createComment, &createCommentBody)
	if createCommentBody.Data.ID == 0 {
		t.Fatalf("expected created comment id, got body %s", createComment.Body.String())
	}

	dashboard := performJSONRequest(engine, http.MethodGet, "/api/admin/dashboard", nil, adminToken)
	if dashboard.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d with body %s", dashboard.Code, dashboard.Body.String())
	}

	var dashboardBody struct {
		Data struct {
			Stats struct {
				PostCount    int64 `json:"postCount"`
				TotalViews   int64 `json:"totalViews"`
				CommentCount int64 `json:"commentCount"`
				VisitorCount int64 `json:"visitorCount"`
			} `json:"stats"`
			// AI analysis lives on the dedicated insight endpoint, not dashboard.
			RecentComments []struct {
				Content string `json:"content"`
			} `json:"recentComments"`
		} `json:"data"`
	}
	decodeJSON(t, dashboard, &dashboardBody)
	if dashboardBody.Data.Stats.PostCount == 0 || dashboardBody.Data.Stats.CommentCount != 1 || dashboardBody.Data.Stats.VisitorCount != 1 {
		t.Fatalf("unexpected dashboard stats: %#v", dashboardBody.Data.Stats)
	}
	if len(dashboardBody.Data.RecentComments) != 1 {
		t.Fatalf("expected one recent comment, got %#v", dashboardBody.Data.RecentComments)
	}

	adminComments := performJSONRequest(engine, http.MethodGet, "/api/admin/comments", nil, adminToken)
	if adminComments.Code != http.StatusOK {
		t.Fatalf("expected admin comments status 200, got %d with body %s", adminComments.Code, adminComments.Body.String())
	}

	hideComment := performJSONRequest(engine, http.MethodPut, "/api/admin/comments/"+itoa(createCommentBody.Data.ID), map[string]string{
		"status": "hidden",
	}, adminToken)
	if hideComment.Code != http.StatusOK {
		t.Fatalf("expected hide comment status 200, got %d with body %s", hideComment.Code, hideComment.Body.String())
	}

	publicComments := performRequest(engine, http.MethodGet, "/api/posts/go-gin-postgresql-blog/comments")
	if publicComments.Code != http.StatusOK {
		t.Fatalf("expected public comments status 200, got %d with body %s", publicComments.Code, publicComments.Body.String())
	}

	var publicBody struct {
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	decodeJSON(t, publicComments, &publicBody)
	if publicBody.Data.Total != 0 {
		t.Fatalf("expected hidden comment to disappear publicly, got total %d", publicBody.Data.Total)
	}
}
