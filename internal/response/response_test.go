package response_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/response"
)

func TestSuccessWritesUnifiedEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	response.Success(context, gin.H{"hello": "world"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	expected := `{"code":0,"message":"success","data":{"hello":"world"}}`
	if recorder.Body.String() != expected {
		t.Fatalf("expected %s, got %s", expected, recorder.Body.String())
	}
}
