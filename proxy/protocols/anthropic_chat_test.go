package protocols

import (
	"encoding/json"
	"testing"

	"github.com/lsongdev/miya-agents/anthropic"
)

func TestAnthropicToOpenAIResponse(t *testing.T) {
	anthJSON := `{
		"id": "msg_01abc",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-7-sonnet-20250219",
		"content": [
			{"type": "thinking", "thinking": "Let me think..."},
			{"type": "text", "text": "Hello world"}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5},
		"stop_reason": "end_turn"
	}`
	var authResp anthropic.Response
	json.Unmarshal([]byte(anthJSON), &authResp)
	oaiResp := anthropicResponseToChat(&authResp)
	if oaiResp.ID != "msg_01abc" {
		t.Errorf("id = %q, want msg_01abc", oaiResp.ID)
	}
	if oaiResp.Choices[0].Message.Content != "Hello world" {
		t.Errorf("content = %q, want Hello world", oaiResp.Choices[0].Message.Content)
	}
	if oaiResp.Choices[0].Message.ReasoningContent != "Let me think..." {
		t.Errorf("reasoning_content = %q, want Let me think...", oaiResp.Choices[0].Message.ReasoningContent)
	}
	if oaiResp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", oaiResp.Choices[0].FinishReason)
	}
	if oaiResp.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", oaiResp.Usage.PromptTokens)
	}
	if oaiResp.Usage.CompletionTokens != 5 {
		t.Errorf("completion_tokens = %d, want 5", oaiResp.Usage.CompletionTokens)
	}
	if oaiResp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", oaiResp.Usage.TotalTokens)
	}
}

func TestMapAnthropicStopReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := anthropicFinishReason(tt.input)
		if got != tt.expected {
			t.Errorf("anthropicFinishReason(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
