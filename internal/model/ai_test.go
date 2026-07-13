package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestAIConversationDefinesRequiredFieldsAndConstants(t *testing.T) {
	if AIConversationTitleModeAuto != "auto" {
		t.Fatalf("unexpected automatic title mode: %q", AIConversationTitleModeAuto)
	}
	if AIConversationTitleModeManual != "manual" {
		t.Fatalf("unexpected manual title mode: %q", AIConversationTitleModeManual)
	}

	conversationType := reflect.TypeOf(AIConversation{})
	for _, fieldName := range []string{
		"ID", "Title", "TitleMode", "CreatedBy", "Model", "MessageCount", "LastMessageAt", "CreatedAt", "UpdatedAt",
	} {
		if _, ok := conversationType.FieldByName(fieldName); !ok {
			t.Fatalf("AIConversation.%s is missing", fieldName)
		}
	}
}

func TestAIMessageDefinesRequiredFieldsAndConversationSequenceIndex(t *testing.T) {
	if AIMessageRoleUser != "user" || AIMessageRoleAssistant != "assistant" {
		t.Fatalf("unexpected AI message roles: %q, %q", AIMessageRoleUser, AIMessageRoleAssistant)
	}
	if AIMessageStatusStreaming != "streaming" ||
		AIMessageStatusCompleted != "completed" ||
		AIMessageStatusAborted != "aborted" ||
		AIMessageStatusFailed != "failed" {
		t.Fatal("AI message statuses must define streaming, completed, aborted, and failed")
	}

	messageType := reflect.TypeOf(AIMessage{})
	for _, fieldName := range []string{
		"ID", "ConversationID", "Role", "Content", "Status", "Sequence", "Model", "ErrorMessage", "CreatedAt", "UpdatedAt",
	} {
		if _, ok := messageType.FieldByName(fieldName); !ok {
			t.Fatalf("AIMessage.%s is missing", fieldName)
		}
	}

	for _, fieldName := range []string{"ConversationID", "Sequence"} {
		field, ok := messageType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("AIMessage.%s is missing", fieldName)
		}
		if !strings.Contains(field.Tag.Get("gorm"), "uniqueIndex:idx_ai_message_sequence") {
			t.Fatalf("AIMessage.%s must use the conversation sequence unique index, got %q", fieldName, field.Tag.Get("gorm"))
		}
	}
}

func TestAIModelsDefineCascadeRelationships(t *testing.T) {
	conversationType := reflect.TypeOf(AIConversation{})
	creatorField, ok := conversationType.FieldByName("Creator")
	if !ok {
		t.Fatal("AIConversation.Creator association is missing")
	}
	if creatorField.Type != reflect.TypeOf(User{}) {
		t.Fatalf("AIConversation.Creator must be a User, got %s", creatorField.Type)
	}
	creatorTag := creatorField.Tag.Get("gorm")
	for _, requirement := range []string{"foreignKey:CreatedBy", "OnDelete:CASCADE"} {
		if !strings.Contains(creatorTag, requirement) {
			t.Fatalf("AIConversation.Creator tag must contain %q, got %q", requirement, creatorTag)
		}
	}

	messageType := reflect.TypeOf(AIMessage{})
	conversationField, ok := messageType.FieldByName("Conversation")
	if !ok {
		t.Fatal("AIMessage.Conversation association is missing")
	}
	if conversationField.Type != reflect.TypeOf(AIConversation{}) {
		t.Fatalf("AIMessage.Conversation must be an AIConversation, got %s", conversationField.Type)
	}
	conversationTag := conversationField.Tag.Get("gorm")
	for _, requirement := range []string{"foreignKey:ConversationID", "OnDelete:CASCADE"} {
		if !strings.Contains(conversationTag, requirement) {
			t.Fatalf("AIMessage.Conversation tag must contain %q, got %q", requirement, conversationTag)
		}
	}
}
