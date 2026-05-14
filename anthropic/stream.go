package anthropic

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Stream writes Anthropic-format SSE events to an http.ResponseWriter.
// Events are written as:
//
//	event: <type>
//	data: <json>
//
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

func (s *Stream) Send(event Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event.Type, data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// Done is a no-op for Anthropic SSE. The stream ends when the connection closes.
func (s *Stream) Done() {}
