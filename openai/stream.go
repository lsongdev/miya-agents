package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Stream struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func NewStream(w http.ResponseWriter) *Stream {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	s := &Stream{w: w}
	if f, ok := w.(http.Flusher); ok {
		s.flusher = f
	}
	return s
}

func (s *Stream) Send(chunk ChatCompletionResponse) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.w, "data: %s\n\n", data)
	if err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func (s *Stream) Done() {
	fmt.Fprintf(s.w, "data: [DONE]\n\n")
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *Stream) SendChatCompletionChunk(messageID, model string, delta *ChatCompletionMessage) {
	s.Send(ChatCompletionResponse{
		ID:      messageID,
		Model:   model,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Choices: []ChatCompletionChoice{{
			Index: 0,
			Delta: delta,
		}},
	})
}

func (s *Stream) Stop(messageID, model, stopReason string, usage *CompletionUsage) {
	s.Send(ChatCompletionResponse{
		ID:      messageID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChoice{{
			FinishReason: stopReason,
		}},
		Usage: usage,
	})
}
