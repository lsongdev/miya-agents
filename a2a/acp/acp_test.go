package acp

import (
	"context"
	"testing"
)

type mockAgent struct{}

func (m *mockAgent) ListAgents(ctx context.Context) ([]AgentManifest, error) {
	return []AgentManifest{{Name: "test-agent"}}, nil
}

func (m *mockAgent) GetAgent(ctx context.Context, name string) (*AgentManifest, error) {
	return &AgentManifest{Name: name}, nil
}

func (m *mockAgent) CreateRun(ctx context.Context, req RunCreateRequest) (*Run, error) {
	return &Run{RunID: "test-run", AgentName: req.AgentName}, nil
}

func (m *mockAgent) GetRun(ctx context.Context, runID string) (*Run, error) {
	return &Run{RunID: runID}, nil
}

func (m *mockAgent) ResumeRun(ctx context.Context, runID string, resume MessageAwaitResume) (*Run, error) {
	return &Run{RunID: runID}, nil
}

func (m *mockAgent) CancelRun(ctx context.Context, runID string) (*Run, error) {
	return &Run{RunID: runID, Status: StatusCancelled}, nil
}

func (m *mockAgent) GetSession(ctx context.Context, sessionID string) (any, error) {
	return map[string]string{"session_id": sessionID}, nil
}

func (m *mockAgent) StreamRun(ctx context.Context, runID string) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

func TestNewServer(t *testing.T) {
	server := NewServer(&mockAgent{})
	if server == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestListAgents(t *testing.T) {
	ag := &mockAgent{}
	agents, err := ag.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "test-agent" {
		t.Errorf("unexpected agents: %v", agents)
	}
}
