package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const streamableHTTPProtocolVersion = "2025-03-26"

type StreamableHTTPTransport struct {
	url      string
	headers  map[string]string
	client   *http.Client
	timeout  time.Duration
	lastResp []byte
	session  string
	mu       sync.Mutex
}

func NewStreamableHTTPTransport(url string, headers map[string]string) *StreamableHTTPTransport {
	return &StreamableHTTPTransport{
		url:     url,
		headers: headers,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		timeout: 60 * time.Second,
	}
}

func (t *StreamableHTTPTransport) Send(msg any) error {
	resp, err := t.call(msg)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.lastResp = resp
	t.mu.Unlock()
	return nil
}

func (t *StreamableHTTPTransport) Recv() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastResp == nil {
		return nil, fmt.Errorf("no response available")
	}
	resp := t.lastResp
	t.lastResp = nil
	return resp, nil
}

func (t *StreamableHTTPTransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}

func (t *StreamableHTTPTransport) call(msg any) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("mcp streamable http: marshal message: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("mcp streamable http: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", streamableHTTPProtocolVersion)
	t.mu.Lock()
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}
	t.mu.Unlock()
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp streamable http: post message: %w", err)
	}
	defer resp.Body.Close()

	if session := resp.Header.Get("Mcp-Session-Id"); session != "" {
		t.mu.Lock()
		t.session = session
		t.mu.Unlock()
	}

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mcp streamable http: status %s: %s", resp.Status, string(body))
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "text/event-stream") {
		return readStreamableHTTPResponse(resp.Body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp streamable http: read response: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	return body, nil
}

func readStreamableHTTPResponse(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var event string
	var data []string
	flush := func() ([]byte, bool, error) {
		if len(data) == 0 {
			event = ""
			return nil, false, nil
		}
		eventName := event
		payload := []byte(strings.Join(data, "\n"))
		event = ""
		data = nil
		if !streamableHTTPEventCanCarryMessage(eventName) {
			return nil, false, nil
		}
		var probe struct {
			ID *json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(payload, &probe); err != nil {
			return nil, false, err
		}
		if probe.ID != nil {
			return payload, true, nil
		}
		return nil, false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if payload, ok, err := flush(); ok || err != nil {
				return payload, err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if payload, ok, err := flush(); ok || err != nil {
		return payload, err
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp streamable http: read event stream: %w", err)
	}
	return nil, fmt.Errorf("mcp streamable http: response event not received")
}

func streamableHTTPEventCanCarryMessage(event string) bool {
	return event == "" || event == "message"
}
