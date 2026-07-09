package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestProjectTechStackColumnUsesPortableDefault(t *testing.T) {
	field, ok := reflect.TypeOf(Project{}).FieldByName("TechStack")
	if !ok {
		t.Fatal("Project.TechStack field is missing")
	}

	tag := string(field.Tag.Get("gorm"))
	if strings.Contains(tag, "type:text") && strings.Contains(tag, "default:") {
		t.Fatalf("TechStack must not use a TEXT column with a default value: %q", tag)
	}
	if !strings.Contains(tag, "default:'[]'") {
		t.Fatalf("TechStack should keep the empty JSON array default, got %q", tag)
	}
}
