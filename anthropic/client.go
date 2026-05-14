package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Configuration holds the API endpoint and key for the Anthropic client.
type Configuration struct {
	API    string
	APIKey string
}

// Client is a standard Anthropic Messages API client.
type Client struct {
	config *Configuration
	client *http.Client
}

// NewClient creates a new Anthropic client.
func NewClient(config *Configuration) *Client {
	return &Client{
		config: config,
		client: http.DefaultClient,
	}
}

// SetHTTPClient allows customizing the underlying HTTP client.
func (c *Client) SetHTTPClient(client *http.Client) {
	c.client = client
}

func (c *Client) makeRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("json marshal error: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.config.API+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-api-key", c.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %s: %s", resp.Status, string(respBody))
	}

	return resp, nil
}

// CreateMessage sends a non-streaming message request.
func (c *Client) CreateMessage(ctx context.Context, req *Request) (*Response, error) {
	resp, err := c.makeRequest(ctx, "POST", "/v1/messages", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result Response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MessageStream represents a streaming response channel.
type MessageStream struct {
	Events chan Event
	Done   chan struct{}
}

// CreateMessageStream sends a streaming message request and returns a channel of SSE events.
func (c *Client) CreateMessageStream(ctx context.Context, req *Request) (*MessageStream, error) {
	req.Stream = true
	resp, err := c.makeRequest(ctx, "POST", "/v1/messages", req)
	if err != nil {
		return nil, err
	}

	stream := &MessageStream{
		Events: make(chan Event),
		Done:   make(chan struct{}),
	}

	go func() {
		defer resp.Body.Close()
		defer close(stream.Events)
		defer close(stream.Done)

		reader := bufio.NewReader(resp.Body)
		for {
			// Read event line
			eventLine, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			eventLine = strings.TrimSpace(eventLine)
			if eventLine == "" {
				continue
			}
			if !strings.HasPrefix(eventLine, "event: ") {
				continue
			}
			eventType := strings.TrimPrefix(eventLine, "event: ")

			// Read data line
			dataLine, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			dataLine = strings.TrimSpace(dataLine)
			if dataLine == "" {
				continue
			}
			if !strings.HasPrefix(dataLine, "data: ") {
				continue
			}
			data := strings.TrimPrefix(dataLine, "data: ")

			var event Event
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			event.Type = eventType

			stream.Events <- event
		}
	}()

	return stream, nil
}
