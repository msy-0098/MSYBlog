package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/mail"
	"masenyu.top/blog/backend/internal/model"
)

const (
	settingNotifyInApp     = "notifyInApp"
	settingNotifyEmail     = "notifyEmail"
	settingNotifySecurity  = "notifySecurity"
	settingNotifyUser      = "notifyUser"
	settingNotifySystem    = "notifySystem"
	defaultNotifyPreference = "true"
)

// NotificationService creates and lists administrator notifications.
type NotificationService struct {
	db     *gorm.DB
	mailer mail.Sender
	from   string
	base   string
	logger *slog.Logger
}

func NewNotificationService(db *gorm.DB, mailer mail.Sender, from string, baseURL string, logger *slog.Logger) *NotificationService {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationService{
		db:     db,
		mailer: mailer,
		from:   strings.TrimSpace(from),
		base:   normalizeBaseURL(baseURL),
		logger: logger,
	}
}

type NotifyInput struct {
	Kind    string
	Title   string
	Body    string
	RefType string
	RefID   uint
	// EmailSubject/EmailBody optional overrides for mail channel.
	EmailSubject string
	EmailBody    string
	// ActionPath relative path for CTA, e.g. /admin/comments
	ActionPath string
}

func (s *NotificationService) NotifyAdmins(ctx context.Context, input NotifyInput) {
	if s == nil || s.db == nil {
		return
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		return
	}
	prefs := s.loadPreferences()
	if !preferenceEnabled(prefs, settingNotifyInApp, true) {
		return
	}
	if kind == model.NotificationKindSecurity && !preferenceEnabled(prefs, settingNotifySecurity, true) {
		return
	}
	if kind == model.NotificationKindUser && !preferenceEnabled(prefs, settingNotifyUser, true) {
		return
	}
	if kind == model.NotificationKindSystem && !preferenceEnabled(prefs, settingNotifySystem, false) {
		return
	}

	var admins []model.User
	if err := s.db.WithContext(ctx).Where("role = ?", model.UserRoleAdmin).Find(&admins).Error; err != nil {
		s.logger.Error("list admins for notification", "error", err)
		return
	}
	if len(admins) == 0 {
		return
	}

	now := time.Now()
	rows := make([]model.Notification, 0, len(admins))
	for _, admin := range admins {
		rows = append(rows, model.Notification{
			UserID:    admin.ID,
			Kind:      kind,
			Title:     strings.TrimSpace(input.Title),
			Body:      strings.TrimSpace(input.Body),
			RefType:   strings.TrimSpace(input.RefType),
			RefID:     input.RefID,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if err := s.db.WithContext(ctx).Create(&rows).Error; err != nil {
		s.logger.Error("create notifications", "error", err, "kind", kind)
		return
	}

	if !preferenceEnabled(prefs, settingNotifyEmail, true) {
		return
	}
	// MVP: email only for comment events (matches product prototype).
	if kind != model.NotificationKindComment || s.mailer == nil {
		return
	}
	subject := strings.TrimSpace(input.EmailSubject)
	if subject == "" {
		subject = input.Title
	}
	body := strings.TrimSpace(input.EmailBody)
	if body == "" {
		body = input.Body
	}
	actionURL := s.base + "/admin/comments"
	if path := strings.TrimSpace(input.ActionPath); path != "" {
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			actionURL = path
		} else if strings.HasPrefix(path, "/") {
			actionURL = s.base + path
		}
	}
	for _, admin := range admins {
		to := strings.TrimSpace(admin.Email)
		if to == "" {
			to = strings.TrimSpace(admin.Username)
		}
		if to == "" || !strings.Contains(to, "@") {
			continue
		}
		message, err := mail.BuildSimpleNotificationEmail(mail.SimpleNotificationEmail{
			From:      s.from,
			To:        to,
			Subject:   subject,
			Title:     input.Title,
			Body:      body,
			ActionURL: actionURL,
			ActionLabel: "打开审核台",
		})
		if err != nil {
			s.logger.Error("build notification email", "error", err, "to", to)
			continue
		}
		if err := s.mailer.Send(ctx, to, message); err != nil {
			s.logger.Error("send notification email", "error", err, "to", to)
		}
	}
}

func (s *NotificationService) List(ctx context.Context, adminID uint, kind string, unreadOnly bool, page, pageSize int) ([]model.Notification, int64, error) {
	query := s.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ?", adminID)
	if kind = strings.TrimSpace(kind); kind != "" && kind != "all" {
		query = query.Where("kind = ?", kind)
	}
	if unreadOnly {
		query = query.Where("read_at IS NULL")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Notification
	err := query.Order("created_at desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&list).Error
	return list, total, err
}

func (s *NotificationService) UnreadCount(ctx context.Context, adminID uint) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", adminID).
		Count(&count).Error
	return count, err
}

func (s *NotificationService) MarkRead(ctx context.Context, adminID, id uint) error {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", id, adminID).
		Update("read_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// Already read or missing: treat as success if owned row exists.
		var count int64
		if err := s.db.WithContext(ctx).Model(&model.Notification{}).
			Where("id = ? AND user_id = ?", id, adminID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, adminID uint) (int64, error) {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", adminID).
		Update("read_at", now)
	return result.RowsAffected, result.Error
}

func (s *NotificationService) loadPreferences() map[string]string {
	prefs := map[string]string{
		settingNotifyInApp:    defaultNotifyPreference,
		settingNotifyEmail:    defaultNotifyPreference,
		settingNotifySecurity: defaultNotifyPreference,
		settingNotifyUser:     defaultNotifyPreference,
		settingNotifySystem:   "false",
	}
	var rows []model.SiteSetting
	if err := s.db.Where("key IN ?", []string{
		settingNotifyInApp,
		settingNotifyEmail,
		settingNotifySecurity,
		settingNotifyUser,
		settingNotifySystem,
	}).Find(&rows).Error; err != nil {
		return prefs
	}
	for _, row := range rows {
		prefs[row.Key] = row.Value
	}
	return prefs
}

func preferenceEnabled(prefs map[string]string, key string, fallback bool) bool {
	raw, ok := prefs[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// Format helpers used by handlers.
func CommentNotificationBody(author, postTitle, excerpt string) string {
	excerpt = strings.TrimSpace(excerpt)
	if utf8Len(excerpt) > 120 {
		excerpt = string([]rune(excerpt)[:120]) + "…"
	}
	if excerpt == "" {
		return fmt.Sprintf("访客「%s」评论了《%s》", author, postTitle)
	}
	return fmt.Sprintf("访客「%s」评论了《%s》：%s", author, postTitle, excerpt)
}

func utf8Len(s string) int {
	return len([]rune(s))
}

func normalizeBaseURL(raw string) string {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "https://" + value
}
