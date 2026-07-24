package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
)

type captureMailer struct {
	to      []string
	payload [][]byte
}

func (m *captureMailer) Send(_ context.Context, to string, message []byte) error {
	m.to = append(m.to, to)
	m.payload = append(m.payload, append([]byte(nil), message...))
	return nil
}

func openNotificationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:notification_service_%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Notification{}, &model.SiteSetting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestNotifyAdminsCreatesRowsAndSendsCommentEmail(t *testing.T) {
	db := openNotificationTestDB(t)
	admin := model.User{Username: "admin@example.com", Email: "admin@example.com", Role: model.UserRoleAdmin, PasswordHash: "x"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	mailer := &captureMailer{}
	svc := NewNotificationService(db, mailer, "noreply@example.com", "https://masenyu.top", nil)
	svc.NotifyAdmins(context.Background(), NotifyInput{
		Kind:         model.NotificationKindComment,
		Title:        "新评论待处理",
		Body:         "访客评论了文章",
		RefType:      "comment",
		RefID:        9,
		EmailSubject: "subject",
		EmailBody:    "body",
		ActionPath:   "/admin/comments",
	})
	var count int64
	if err := db.Model(&model.Notification{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("notifications = %d, want 1", count)
	}
	if len(mailer.to) != 1 || mailer.to[0] != "admin@example.com" {
		t.Fatalf("mail recipients = %#v", mailer.to)
	}
	unread, err := svc.UnreadCount(context.Background(), admin.ID)
	if err != nil || unread != 1 {
		t.Fatalf("unread = %d err=%v", unread, err)
	}
	if err := svc.MarkRead(context.Background(), admin.ID, 1); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	unread, err = svc.UnreadCount(context.Background(), admin.ID)
	if err != nil || unread != 0 {
		t.Fatalf("unread after mark = %d err=%v", unread, err)
	}
}

func TestNotifyAdminsRespectsEmailPreferenceOff(t *testing.T) {
	db := openNotificationTestDB(t)
	admin := model.User{Username: "admin2@example.com", Email: "admin2@example.com", Role: model.UserRoleAdmin, PasswordHash: "x"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := db.Create(&model.SiteSetting{Key: "notifyEmail", Value: "false"}).Error; err != nil {
		t.Fatalf("pref: %v", err)
	}
	mailer := &captureMailer{}
	svc := NewNotificationService(db, mailer, "noreply@example.com", "https://masenyu.top", nil)
	svc.NotifyAdmins(context.Background(), NotifyInput{
		Kind:  model.NotificationKindComment,
		Title: "t",
		Body:  "b",
	})
	var count int64
	_ = db.Model(&model.Notification{}).Count(&count)
	if count != 1 {
		t.Fatalf("still want in-app notification, got %d", count)
	}
	if len(mailer.to) != 0 {
		t.Fatalf("expected no email when notifyEmail=false")
	}
}

func TestMarkAllRead(t *testing.T) {
	db := openNotificationTestDB(t)
	admin := model.User{Username: "admin3@example.com", Email: "admin3@example.com", Role: model.UserRoleAdmin, PasswordHash: "x"}
	_ = db.Create(&admin)
	svc := NewNotificationService(db, nil, "", "", nil)
	now := time.Now()
	for i := 0; i < 3; i++ {
		_ = db.Create(&model.Notification{UserID: admin.ID, Kind: model.NotificationKindSystem, Title: "t", Body: "b", CreatedAt: now})
	}
	updated, err := svc.MarkAllRead(context.Background(), admin.ID)
	if err != nil || updated != 3 {
		t.Fatalf("updated=%d err=%v", updated, err)
	}
}