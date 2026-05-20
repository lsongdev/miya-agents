package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ResponsesEvent is a single SSE event in a /v1/responses stream.
type ResponsesEvent struct {
	Type string `json:"type"`
	// Common positional fields. Only the fields relevant to the event type
	// are populated; the rest stay at their zero values and are omitted.
	SequenceNumber int    `json:"sequence_number"`
	OutputIndex    int    `json:"output_index,omitempty"`
	ContentIndex   int    `json:"content_index,omitempty"`
	ItemID         string `json:"item_id,omitempty"`
	// Response payload for response.created / response.in_progress / response.completed
	Response *ResponseObject `json:"response,omitempty"`
	// Item payload for response.output_item.added / .done
	Item *ResponseOutputItem `json:"item,omitempty"`
	// Part payload for response.content_part.added / .done
	Part *ResponseContentPart `json:"part,omitempty"`
	// Deltas
	Delta string `json:"delta,omitempty"`
	Text  string `json:"text,omitempty"`
	// Function call argument done payload
	Arguments string `json:"arguments,omitempty"`
}

// ResponsesWriter writes /v1/responses SSE events to an http.ResponseWriter.
type ResponsesWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	seq     int
}

// NewResponsesWriter writes /v1/responses SSE headers and returns a writer.
func NewResponsesWriter(w http.ResponseWriter) *ResponsesWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	rw := &ResponsesWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		rw.flusher = f
	}
	return rw
}

func (rw *ResponsesWriter) send(evt ResponsesEvent) {
	rw.seq++
	evt.SequenceNumber = rw.seq
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	fmt.Fprintf(rw.w, "event: %s\ndata: %s\n\n", evt.Type, data)
	if rw.flusher != nil {
		rw.flusher.Flush()
	}
}

// ConvertChatCompletionStreamToResponsesStream consumes OpenAI ChatCompletion
// stream chunks and writes equivalent /v1/responses SSE events to the writer.
// Returns the assembled ChatCompletionResponse so callers can pass it to
// OnResponse hooks.
func ConvertChatCompletionStreamToResponsesStream(
	req *ResponseRequest,
	chunks <-chan ChatCompletionResponse,
	w http.ResponseWriter,
) *ChatCompletionResponse {
	rw := NewResponsesWriter(w)
	assembler := NewResponseAssembler()

	// Build a skeleton response that mirrors the request; it is reused across
	// response.created / response.in_progress / response.completed events.
	skeleton := &ResponseObject{
		Object: "response",
		Status: "in_progress",
		Output: []ResponseOutputItem{},
	}
	if req != nil {
		skeleton.Model = req.Model
		skeleton.Instructions = req.Instructions
		skeleton.Temperature = req.Temperature
		skeleton.TopP = req.TopP
		skeleton.MaxOutputTokens = req.MaxOutputTokens
		skeleton.ToolChoice = req.ToolChoice
		skeleton.Tools = req.Tools
		skeleton.User = req.User
		skeleton.Metadata = req.Metadata
		if req.ParallelToolCalls != nil {
			skeleton.ParallelToolCalls = *req.ParallelToolCalls
		} else {
			skeleton.ParallelToolCalls = true
		}
		skeleton.PreviousResponseID = req.PreviousResponseID
	}

	// State for the current open message/output items.
	var (
		respID         string
		messageItemID  string
		messageOpened  bool
		textOpened     bool
		messageBuf     string
		nextOutputIdx  int
		messageOutIdx  int
		// Tool-call streaming state, keyed by choice tool_call index.
		toolStates = map[int]*toolCallStreamState{}
	)

	closeMessageItem := func(status string) {
		if !messageOpened {
			return
		}
		if textOpened {
			rw.send(ResponsesEvent{
				Type:         "response.output_text.done",
				ItemID:       messageItemID,
				OutputIndex:  messageOutIdx,
				ContentIndex: 0,
				Text:         messageBuf,
			})
			rw.send(ResponsesEvent{
				Type:         "response.content_part.done",
				ItemID:       messageItemID,
				OutputIndex:  messageOutIdx,
				ContentIndex: 0,
				Part: &ResponseContentPart{
					Type: "output_text",
					Text: messageBuf,
				},
			})
			textOpened = false
		}
		item := &ResponseOutputItem{
			Type:   "message",
			ID:     messageItemID,
			Status: "completed",
			Role:   RoleAssistant,
			Content: []ResponseContentPart{{
				Type: "output_text",
				Text: messageBuf,
			}},
		}
		rw.send(ResponsesEvent{
			Type:        "response.output_item.done",
			OutputIndex: messageOutIdx,
			Item:        item,
		})
		messageOpened = false
		_ = status
	}

	// Send response.created with a skeleton.
	rw.send(ResponsesEvent{Type: "response.created", Response: skeleton})
	rw.send(ResponsesEvent{Type: "response.in_progress", Response: skeleton})

	for chunk := range chunks {
		assembler.Update(chunk)
		if chunk.ID != "" && respID == "" {
			respID = responseObjectID(chunk.ID)
			skeleton.ID = respID
		}
		if chunk.Model != "" && skeleton.Model == "" {
			skeleton.Model = chunk.Model
		}
		if chunk.Created != 0 && skeleton.CreatedAt == 0 {
			skeleton.CreatedAt = chunk.Created
		}

		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if delta != nil {
				if delta.Content != "" {
					if !messageOpened {
						if respID == "" {
							respID = responseObjectID("")
							skeleton.ID = respID
						}
						messageOutIdx = nextOutputIdx
						nextOutputIdx++
						messageItemID = newMessageOutputID(respID, messageOutIdx)
						rw.send(ResponsesEvent{
							Type:        "response.output_item.added",
							OutputIndex: messageOutIdx,
							Item: &ResponseOutputItem{
								Type:    "message",
								ID:      messageItemID,
								Status:  "in_progress",
								Role:    RoleAssistant,
								Content: []ResponseContentPart{},
							},
						})
						messageOpened = true
					}
					if !textOpened {
						rw.send(ResponsesEvent{
							Type:         "response.content_part.added",
							ItemID:       messageItemID,
							OutputIndex:  messageOutIdx,
							ContentIndex: 0,
							Part: &ResponseContentPart{
								Type: "output_text",
								Text: "",
							},
						})
						textOpened = true
					}
					rw.send(ResponsesEvent{
						Type:         "response.output_text.delta",
						ItemID:       messageItemID,
						OutputIndex:  messageOutIdx,
						ContentIndex: 0,
						Delta:        delta.Content,
					})
					messageBuf += delta.Content
				}

				for _, tc := range delta.ToolCalls {
					if respID == "" {
						respID = responseObjectID("")
						skeleton.ID = respID
					}
					state, ok := toolStates[tc.Index]
					if !ok {
						// Need to finalize the open message item before opening a new output item.
						closeMessageItem("completed")
						state = &toolCallStreamState{
							outputIndex: nextOutputIdx,
							itemID:      newFunctionCallOutputID(respID, nextOutputIdx),
						}
						nextOutputIdx++
						if tc.ID != "" {
							state.callID = tc.ID
						}
						if tc.Function.Name != "" {
							state.name = tc.Function.Name
						}
						toolStates[tc.Index] = state
						rw.send(ResponsesEvent{
							Type:        "response.output_item.added",
							OutputIndex: state.outputIndex,
							Item: &ResponseOutputItem{
								Type:      "function_call",
								ID:        state.itemID,
								Status:    "in_progress",
								CallID:    state.callID,
								Name:      state.name,
								Arguments: "",
							},
						})
					} else {
						if tc.ID != "" && state.callID == "" {
							state.callID = tc.ID
						}
						if tc.Function.Name != "" && state.name == "" {
							state.name = tc.Function.Name
						}
					}
					if tc.Function.Arguments != "" {
						state.arguments += tc.Function.Arguments
						rw.send(ResponsesEvent{
							Type:        "response.function_call_arguments.delta",
							ItemID:      state.itemID,
							OutputIndex: state.outputIndex,
							Delta:       tc.Function.Arguments,
						})
					}
				}
			}
			if choice.FinishReason != "" {
				// finish reason -> close any open items
			}
		}
	}

	// Close any open items.
	closeMessageItem("completed")
	for _, state := range toolStates {
		rw.send(ResponsesEvent{
			Type:        "response.function_call_arguments.done",
			ItemID:      state.itemID,
			OutputIndex: state.outputIndex,
			Arguments:   state.arguments,
		})
		rw.send(ResponsesEvent{
			Type:        "response.output_item.done",
			OutputIndex: state.outputIndex,
			Item: &ResponseOutputItem{
				Type:      "function_call",
				ID:        state.itemID,
				Status:    "completed",
				CallID:    state.callID,
				Name:      state.name,
				Arguments: state.arguments,
			},
		})
	}

	chatResp := assembler.Build()
	if chatResp.ID == "" {
		chatResp.ID = respID
	}
	finalResp := NewResponseObjectFromChatCompletionResponse(req, chatResp)
	rw.send(ResponsesEvent{Type: "response.completed", Response: finalResp})

	return chatResp
}

type toolCallStreamState struct {
	outputIndex int
	itemID      string
	callID      string
	name        string
	arguments   string
}
