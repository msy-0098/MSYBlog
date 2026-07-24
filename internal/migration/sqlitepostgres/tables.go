package sqlitepostgres

import "masenyu.top/blog/backend/internal/model"

// TableSpec defines a business table copied by this migration. The table list is
// deliberately explicit so no application-internal or GORM bookkeeping tables
// are copied accidentally.
type TableSpec struct {
	Name           string
	Model          any
	OrderColumn    string
	HasSequence    bool
	SourceOptional bool
}

type postTag struct {
	PostID uint `gorm:"column:post_id;primaryKey"`
	TagID  uint `gorm:"column:tag_id;primaryKey"`
}

func (postTag) TableName() string { return "post_tags" }

// ExistingBusinessTables returns the complete, dependency-ordered migration
// surface. Do not add runtime-only SQLite tables here.
func ExistingBusinessTables() []TableSpec {
	return []TableSpec{
		{Name: "site_settings", Model: &model.SiteSetting{}, OrderColumn: "key"},
		{Name: "users", Model: &model.User{}, OrderColumn: "id", HasSequence: true},
		{Name: "email_verification_codes", Model: &model.EmailVerificationCode{}, OrderColumn: "id", HasSequence: true},
		{Name: "categories", Model: &model.Category{}, OrderColumn: "id", HasSequence: true},
		{Name: "tags", Model: &model.Tag{}, OrderColumn: "id", HasSequence: true},
		{Name: "posts", Model: &model.Post{}, OrderColumn: "id", HasSequence: true},
		{Name: "post_tags", Model: &postTag{}, OrderColumn: "post_id, tag_id"},
		{Name: "post_likes", Model: &model.PostLike{}, OrderColumn: "id", HasSequence: true, SourceOptional: true},
		{Name: "comments", Model: &model.Comment{}, OrderColumn: "id", HasSequence: true},
		{Name: "projects", Model: &model.Project{}, OrderColumn: "id", HasSequence: true},
		{Name: "friend_links", Model: &model.FriendLink{}, OrderColumn: "id", HasSequence: true, SourceOptional: true},
		{Name: "uploads", Model: &model.Upload{}, OrderColumn: "id", HasSequence: true},
		{Name: "access_logs", Model: &model.AccessLog{}, OrderColumn: "id", HasSequence: true},
		{Name: "ip_bans", Model: &model.IPBan{}, OrderColumn: "id", HasSequence: true},
		{Name: "ai_conversations", Model: &model.AIConversation{}, OrderColumn: "id", HasSequence: true, SourceOptional: true},
		{Name: "ai_messages", Model: &model.AIMessage{}, OrderColumn: "id", HasSequence: true, SourceOptional: true},
		{Name: "notifications", Model: &model.Notification{}, OrderColumn: "id", HasSequence: true, SourceOptional: true},
	}
}
