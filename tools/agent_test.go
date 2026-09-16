package tools

import (
	"context"
	"strings"
	"testing"
)

func TestDelegateToolCallsFunction(t *testing.T) {
	var gotAgent, gotTask string
	tool := NewDelegateTool(func(_ context.Context, agent, task string) (string, error) {
		gotAgent, gotTask = agent, task
		return "done", nil
	}, map[string]string{"researcher": "research the web"})

	if got := tool.Run(context.Background(), `{"agent":"researcher","task":"find it"}`); got != "done" {
		t.Fatalf("Run = %q", got)
	}
	if gotAgent != "researcher" || gotTask != "find it" {
		t.Fatalf("delegate = (%q, %q)", gotAgent, gotTask)
	}
	def := tool.Def()
	if def.Function.Name != "delegate" || !strings.Contains(def.Function.Description, "researcher: research the web") {
		t.Fatalf("definition = %#v", def.Function)
	}
}
