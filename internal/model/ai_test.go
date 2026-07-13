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
