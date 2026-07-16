package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSkillsToolIncludesBuiltinSessionMaintenance(t *testing.T) {
	tool := &SkillsTool{Workspace: t.TempDir()}

	got := tool.Run(context.Background(), `{"name":"session-maintenance"}`)

	if !strings.Contains(got, "# Skill: session-maintenance") {
		t.Fatalf("missing builtin skill: %s", got)
	}
	if !strings.Contains(got, "never modify events") {
		t.Fatalf("missing session safety guidance: %s", got)
	}
}

func TestSkillsToolAllowsMissingWorkspace(t *testing.T) {
	tool := &SkillsTool{Workspace: "/path/that/does/not/exist"}

	got := tool.Run(context.Background(), `{}`)

	if !strings.Contains(got, "session-maintenance") {
		t.Fatalf("missing builtin skill from list: %s", got)
	}
}
