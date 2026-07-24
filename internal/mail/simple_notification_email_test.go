package mail

import (
	"strings"
	"testing"
)

func TestBuildSimpleNotificationEmail(t *testing.T) {
	raw, err := BuildSimpleNotificationEmail(SimpleNotificationEmail{
		From:        "noreply@masenyu.top",
		To:          "admin@masenyu.top",
		Subject:     "新评论",
		Title:       "新评论待处理",
		Body:        "访客说了你好",
		ActionURL:   "https://masenyu.top/admin/comments",
		ActionLabel: "打开审核台",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	text := string(raw)
	for _, needle := range []string{"新评论待处理", "打开审核台", "multipart/alternative", "admin@masenyu.top"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("message missing %q", needle)
		}
	}
}