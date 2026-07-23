package response_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/response"
)

func TestErrorWithDataWritesStatusCodeAndData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	response.ErrorWithData(
		context,
		http.StatusTooManyRequests,
		"请求过于频繁，请稍后再试",
		gin.H{"retryAfter": 2},
	)

	if !context.IsAborted() {
		t.Fatal("expected context to be aborted")
	}

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", recorder.Code)
	}

	expected := `{"code":429,"message":"请求过于频繁，请稍后再试","data":{"retryAfter":2}}`
	if recorder.Body.String() != expected {
		t.Fatalf("expected %s, got %s", expected, recorder.Body.String())
	}
}
