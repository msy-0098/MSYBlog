package response

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPrepareSSEAndWriteSSEFrame(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	PrepareSSE(context)
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected event stream content type, got %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("expected no-cache header, got %q", got)
	}
	if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("expected proxy buffering disabled, got %q", got)
	}
	if err := WriteSSE(context, "delta", map[string]string{"content": "你好"}); err != nil {
		t.Fatalf("write sse: %v", err)
	}
	if got, want := recorder.Body.String(), "event: delta\ndata: {\"content\":\"你好\"}\n\n"; got != want {
		t.Fatalf("unexpected sse body: %q", got)
	}
}
