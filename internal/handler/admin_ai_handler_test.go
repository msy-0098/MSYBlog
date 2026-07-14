package handler

import (
	"encoding/json"
	"testing"

	"masenyu.top/blog/backend/internal/model"
)

func TestAIConversationDTOMarshalsMissingLastMessageAtAsNull(t *testing.T) {
	payload, err := json.Marshal(aiConversationDTO(model.AIConversation{}))
	if err != nil {
		t.Fatalf("marshal conversation DTO: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode conversation DTO: %v", err)
	}
	if got := string(decoded["lastMessageAt"]); got != "null" {
		t.Fatalf("lastMessageAt = %s, want null", got)
	}
}
