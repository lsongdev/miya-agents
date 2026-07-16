package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSkillsToolAllowsMissingWorkspace(t *testing.T) {
	tool := &SkillsTool{Workspace: "/path/that/does/not/exist"}

	got := tool.Run(context.Background(), `{}`)

	if got != "No skills registered." {
		t.Fatalf("Run = %q", got)
	}
}

func TestSkillsToolReportsMissingSkill(t *testing.T) {
	tool := &SkillsTool{Workspace: t.TempDir()}

	got := tool.Run(context.Background(), `{"name":"missing"}`)

	if !strings.Contains(got, `Error: skill "missing" not found`) {
		t.Fatalf("Run = %q", got)
	}
}
