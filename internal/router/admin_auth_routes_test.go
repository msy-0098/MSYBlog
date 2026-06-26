package router_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAuthLoginReturnsTokenForValidCredentials(t *testing.T) {
	engine := newAdminAuthTestEngine(t)

	recorder := performJSONRequest(engine, http.MethodPost, "/api/admin/login", map[string]string{
		"username": "masenyu812@gmail.com",
		"password": "admin-test-password",
	}, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
			User  struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"data"`
	}
	decodeJSON(t, recorder, &body)

	if body.Code != 0 {
		t.Fatalf("expected success code, got %d", body.Code)
	}
	if body.Data.Token == "" {
		t.Fatal("expected login token")
	}
	if body.Data.User.Username != "masenyu812@gmail.com" {
		t.Fatalf("unexpected username %q", body.Data.User.Username)
	}
}

func TestAdminAuthLoginRejectsInvalidCredentials(t *testing.T) {
	engine := newAdminAuthTestEngine(t)

	recorder := performJSONRequest(engine, http.MethodPost, "/api/admin/login", map[string]string{
		"username": "masenyu812@gmail.com",
		"password": "wrong-password",
	}, "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	decodeJSON(t, recorder, &body)

	if body.Code != 401 {
		t.Fatalf("expected response code 401, got %d", body.Code)
	}
	if body.Message == "" {
		t.Fatal("expected a generic failure message")
	}
}

func TestAdminAuthSeedsDefaultDevelopmentAdminForFreshDatabase(t *testing.T) {
	engine := newTestEngine(t)

	recorder := performJSONRequest(engine, http.MethodPost, "/api/admin/login", map[string]string{
		"username": "masenyu812@gmail.com",
		"password": "local-development-admin-password",
	}, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected default development admin login status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminProfileRequiresBearerToken(t *testing.T) {
	engine := newAdminAuthTestEngine(t)

	recorder := performRequest(engine, http.MethodGet, "/api/admin/profile")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d with body %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminProfileReturnsCurrentUserWithToken(t *testing.T) {
	engine := newAdminAuthTestEngine(t)
	token := loginAndGetToken(t, engine)

	recorder := performJSONRequest(engine, http.MethodGet, "/api/admin/profile", nil, token)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Code int `json:"code"`
		Data struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	decodeJSON(t, recorder, &body)

	if body.Data.Username != "masenyu812@gmail.com" {
		t.Fatalf("unexpected profile username %q", body.Data.Username)
	}
}

func loginAndGetToken(t *testing.T, handler http.Handler) string {
	t.Helper()

	recorder := performJSONRequest(handler, http.MethodPost, "/api/admin/login", map[string]string{
		"username": "masenyu812@gmail.com",
		"password": "admin-test-password",
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("login failed with status %d and body %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeJSON(t, recorder, &body)
	if body.Data.Token == "" {
		t.Fatal("expected login token")
	}

	return body.Data.Token
}

func newAdminAuthTestEngine(t *testing.T) http.Handler {
	t.Helper()

	t.Setenv("BLOG_ADMIN_INITIAL_PASSWORD", "admin-test-password")
	t.Setenv("BLOG_JWT_SECRET", "admin-test-secret")

	return newTestEngine(t)
}

func performJSONRequest(handler http.Handler, method string, target string, payload any, token string) *httptest.ResponseRecorder {
	body := bytes.NewBuffer(nil)
	if payload != nil {
		if err := json.NewEncoder(body).Encode(payload); err != nil {
			panic(err)
		}
	}

	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}
