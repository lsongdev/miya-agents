package openai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResponseRequest is an OpenAI /v1/responses API request.
type ResponseRequest struct {
	Model              string            `json:"model"`
	Input              ResponseInput     `json:"input"`
	Instructions       string            `json:"instructions,omitempty"`
	MaxOutputTokens    int               `json:"max_output_tokens,omitempty"`
	Temperature        *float64          `json:"temperature,omitempty"`
	TopP               *float64          `json:"top_p,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	Tools              []ResponseTool    `json:"tools,omitempty"`
	ToolChoice         any               `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	PreviousResponseID string            `json:"previous_response_id,omitempty"`
	User               string            `json:"user,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// ResponseInput holds the request input which may be a string or an array of input items.
type ResponseInput struct {
	Text  string
	Items []ResponseInputItem
}

func (in *ResponseInput) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		in.Text = s
		return nil
	}
	var items []ResponseInputItem
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("input must be a string or array of items: %v", err)
	}
	in.Items = items
	return nil
}

func (in ResponseInput) MarshalJSON() ([]byte, error) {
	if len(in.Items) > 0 {
		return json.Marshal(in.Items)
	}
	return json.Marshal(in.Text)
}

// ResponseInputItem is one item in the input array.
type ResponseInputItem struct {
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Content   ResponseInputContent  `json:"content,omitempty"`
	ID        string                `json:"id,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Output    string                `json:"output,omitempty"`
	Status    string                `json:"status,omitempty"`
}

// ResponseInputContent supports a string or an array of content parts.
type ResponseInputContent struct {
	Text  string
	Parts []ResponseContentPart
}

func (c *ResponseInputContent) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Text = s
		return nil
	}
	var parts []ResponseContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("content must be a string or array of parts: %v", err)
	}
	c.Parts = parts
	return nil
}

func (c ResponseInputContent) MarshalJSON() ([]byte, error) {
	if len(c.Parts) > 0 {
		return json.Marshal(c.Parts)
	}
	return json.Marshal(c.Text)
}

// String returns the textual content (joining text parts when needed).
func (c ResponseInputContent) String() string {
	if len(c.Parts) == 0 {
		return c.Text
	}
	var parts []string
	for _, p := range c.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ResponseContentPart is a content block inside a message item.
type ResponseContentPart struct {
	Type     string                  `json:"type"`              // input_text, output_text, input_image, refusal
	Text     string                  `json:"text,omitempty"`
	ImageURL string                  `json:"image_url,omitempty"`
	Refusal  string                  `json:"refusal,omitempty"`
	Annotations []ResponseAnnotation `json:"annotations,omitempty"`
}

// ResponseAnnotation is a citation or annotation on output text.
type ResponseAnnotation struct {
	Type string `json:"type"`
}

// ResponseTool defines a tool usable by the Responses API.
type ResponseTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      *bool          `json:"strict,omitempty"`
}

// ResponseObject is the /v1/responses non-streaming response (and the body
// embedded in stream events like response.created and response.completed).
type ResponseObject struct {
	ID                string                `json:"id"`
	Object            string                `json:"object"`
	CreatedAt         int64                 `json:"created_at"`
	Status            string                `json:"status"`
	Error             *ResponseError        `json:"error"`
	IncompleteDetails any                   `json:"incomplete_details"`
	Instructions      string                `json:"instructions"`
	Model             string                `json:"model"`
	Output            []ResponseOutputItem  `json:"output"`
	ParallelToolCalls bool                  `json:"parallel_tool_calls"`
	PreviousResponseID string               `json:"previous_response_id,omitempty"`
	Temperature       *float64              `json:"temperature,omitempty"`
	TopP              *float64              `json:"top_p,omitempty"`
	MaxOutputTokens   int                   `json:"max_output_tokens,omitempty"`
	ToolChoice        any                   `json:"tool_choice,omitempty"`
	Tools             []ResponseTool        `json:"tools,omitempty"`
	Usage             *ResponseUsage        `json:"usage,omitempty"`
	User              string                `json:"user,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
}

// ResponseError is an error embedded in a Response.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ResponseOutputItem is one item in the output array.
type ResponseOutputItem struct {
	Type      string                 `json:"type"`              // message, function_call, reasoning
	ID        string                 `json:"id"`
	Status    string                 `json:"status,omitempty"`
	Role      string                 `json:"role,omitempty"`
	Content   []ResponseContentPart  `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
	Summary   []ResponseSummaryItem  `json:"summary,omitempty"`
}

// ResponseSummaryItem is a part of a reasoning summary.
type ResponseSummaryItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ResponseUsage is the usage block in the Responses API.
type ResponseUsage struct {
	InputTokens         int                       `json:"input_tokens"`
	OutputTokens        int                       `json:"output_tokens"`
	TotalTokens         int                       `json:"total_tokens"`
	InputTokensDetails  ResponseInputUsageDetails `json:"input_tokens_details"`
	OutputTokensDetails ResponseOutputUsageDetails `json:"output_tokens_details"`
}

type ResponseInputUsageDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type ResponseOutputUsageDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ---------------------------------------------------------------------------
// Conversion: ResponseRequest -> ChatCompletionRequest
// ---------------------------------------------------------------------------

// NewChatCompletionRequestFromResponseRequest converts an OpenAI Responses API
// request into an equivalent ChatCompletionRequest that can be sent to a
// standard /v1/chat/completions endpoint.
func NewChatCompletionRequestFromResponseRequest(req *ResponseRequest) *ChatCompletionRequest {
	chatReq := &ChatCompletionRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		MaxTokens:   req.MaxOutputTokens,
		User:        req.User,
	}

	if req.Instructions != "" {
		chatReq.Messages = append(chatReq.Messages, SystemMessage(req.Instructions))
	}

	// Input may be a string or an array of items
	if len(req.Input.Items) == 0 {
		if req.Input.Text != "" {
			chatReq.Messages = append(chatReq.Messages, UserMessage(req.Input.Text))
		}
	} else {
		// Group function_call items by id so their tool_calls stay on a single
		// assistant message when they arrive consecutively.
		var pendingToolCalls []ToolCall
		flushAssistant := func(content string) {
			if content == "" && len(pendingToolCalls) == 0 {
				return
			}
			msg := ChatCompletionMessage{Role: RoleAssistant, Content: content}
			if len(pendingToolCalls) > 0 {
				msg.ToolCalls = pendingToolCalls
				pendingToolCalls = nil
			}
			chatReq.Messages = append(chatReq.Messages, msg)
		}

		for _, item := range req.Input.Items {
			switch item.Type {
			case "message":
				role := item.Role
				if role == "" {
					role = RoleUser
				}
				content := item.Content.String()
				if role == RoleAssistant {
					flushAssistant(content)
				} else {
					chatReq.Messages = append(chatReq.Messages, ChatCompletionMessage{
						Role:    role,
						Content: content,
					})
				}
			case "function_call":
				callID := item.CallID
				if callID == "" {
					callID = item.ID
				}
				pendingToolCalls = append(pendingToolCalls, ToolCall{
					ID:   callID,
					Type: "function",
					Function: FunctionCall{
						Name:      item.Name,
						Arguments: item.Arguments,
					},
				})
			case "function_call_output":
				flushAssistant("")
				chatReq.Messages = append(chatReq.Messages, ToolResultMessage(item.CallID, item.Name, item.Output))
			}
		}
		flushAssistant("")
	}

	for _, t := range req.Tools {
		if t.Type != "function" {
			continue
		}
		chatReq.Tools = append(chatReq.Tools, ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	return chatReq
}

// ---------------------------------------------------------------------------
// Conversion: ChatCompletionResponse -> ResponseObject
// ---------------------------------------------------------------------------

// NewResponseObjectFromChatCompletionResponse converts an OpenAI chat completion
// response to a Responses API response object.
func NewResponseObjectFromChatCompletionResponse(req *ResponseRequest, chatResp *ChatCompletionResponse) *ResponseObject {
	resp := &ResponseObject{
		ID:        responseObjectID(chatResp.ID),
		Object:    "response",
		CreatedAt: chatResp.Created,
		Status:    "completed",
		Model:     chatResp.Model,
		Output:    []ResponseOutputItem{},
	}
	if resp.CreatedAt == 0 {
		resp.CreatedAt = time.Now().Unix()
	}

	if req != nil {
		resp.Instructions = req.Instructions
		resp.Temperature = req.Temperature
		resp.TopP = req.TopP
		resp.MaxOutputTokens = req.MaxOutputTokens
		resp.ToolChoice = req.ToolChoice
		resp.Tools = req.Tools
		resp.User = req.User
		resp.Metadata = req.Metadata
		if req.ParallelToolCalls != nil {
			resp.ParallelToolCalls = *req.ParallelToolCalls
		} else {
			resp.ParallelToolCalls = true
		}
		resp.PreviousResponseID = req.PreviousResponseID
	}

	if chatResp.Error != nil {
		resp.Status = "failed"
		resp.Error = &ResponseError{
			Code:    chatResp.Error.Code,
			Message: chatResp.Error.Message,
		}
	}

	for _, choice := range chatResp.Choices {
		msg := choice.Message
		if msg == nil {
			msg = choice.Delta
		}
		if msg == nil {
			continue
		}
		if msg.Content != "" || msg.ReasoningContent != "" {
			item := ResponseOutputItem{
				Type:   "message",
				ID:     newMessageOutputID(resp.ID, len(resp.Output)),
				Status: "completed",
				Role:   RoleAssistant,
			}
			if msg.Content != "" {
				item.Content = append(item.Content, ResponseContentPart{
					Type: "output_text",
					Text: msg.Content,
				})
			}
			resp.Output = append(resp.Output, item)
		}
		for _, tc := range msg.ToolCalls {
			resp.Output = append(resp.Output, ResponseOutputItem{
				Type:      "function_call",
				ID:        newFunctionCallOutputID(resp.ID, len(resp.Output)),
				Status:    "completed",
				CallID:    tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		if choice.FinishReason == "length" {
			resp.Status = "incomplete"
			resp.IncompleteDetails = map[string]string{"reason": "max_output_tokens"}
		}
	}

	if chatResp.Usage != nil {
		resp.Usage = &ResponseUsage{
			InputTokens:  chatResp.Usage.PromptTokens,
			OutputTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:  chatResp.Usage.TotalTokens,
		}
		resp.Usage.InputTokensDetails.CachedTokens = chatResp.Usage.PromptTokensDetails.CachedTokens
		resp.Usage.OutputTokensDetails.ReasoningTokens = chatResp.Usage.CompletionTokensDetails.ReasoningTokens
	}

	return resp
}

func responseObjectID(chatID string) string {
	if chatID == "" {
		return fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	if strings.HasPrefix(chatID, "resp_") {
		return chatID
	}
	return "resp_" + strings.TrimPrefix(chatID, "chatcmpl-")
}

func newMessageOutputID(respID string, idx int) string {
	return fmt.Sprintf("msg_%s_%d", strings.TrimPrefix(respID, "resp_"), idx)
}

func newFunctionCallOutputID(respID string, idx int) string {
	return fmt.Sprintf("fc_%s_%d", strings.TrimPrefix(respID, "resp_"), idx)
}
