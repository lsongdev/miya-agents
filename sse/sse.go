package sse

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Event struct {
	Type string
	Data string
}

type Stream struct {
	Events chan Event
	err    chan error
}

func NewStream(events chan Event, err chan error) *Stream {
	return &Stream{Events: events, err: err}
}

func (s *Stream) Close() {
	close(s.Events)
	close(s.err)
}

func (s *Stream) Err() <-chan error {
	return s.err
}

func Do(ctx context.Context, client *http.Client, req *http.Request) (*Stream, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sse request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sse unexpected status %s: %s", resp.Status, string(body))
	}

	stream := NewStream(make(chan Event), make(chan error, 1))

	go func() {
		defer resp.Body.Close()
		defer stream.Close()

		reader := bufio.NewReader(resp.Body)
		var currentEvent Event

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					stream.err <- err
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				if currentEvent.Type != "" || currentEvent.Data != "" {
					stream.Events <- currentEvent
					currentEvent = Event{}
				}
				continue
			}

			if strings.HasPrefix(line, "event:") {
				currentEvent.Type = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				currentEvent.Data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			} else if strings.HasPrefix(line, ":") {
				continue
			}
		}
	}()

	return stream, nil
}
