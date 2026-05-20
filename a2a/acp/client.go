package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is an ACP client that communicates with an ACP server via REST.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new ACP client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
}

// WithHTTPClient sets the underlying HTTP client.
func (c *Client) WithHTTPClient(hc *http.Client) *Client {
	c.httpClient = hc
	return c
}

// ListAgents returns all available agents from the server.
func (c *Client) ListAgents(ctx context.Context) ([]AgentManifest, error) {
	return c.ListAgentsWithOpts(ctx, 0, 0)
}

// ListAgentsWithOpts returns agents with pagination.
func (c *Client) ListAgentsWithOpts(ctx context.Context, limit, offset int) ([]AgentManifest, error) {
	u := c.url("/agents")
	if limit > 0 || offset > 0 {
		q := make(url.Values)
		if limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", limit))
		}
		if offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", offset))
		}
		u += "?" + q.Encode()
	}

	var resp struct {
		Agents []AgentManifest `json:"agents"`
	}
	if err := c.get(ctx, u, &resp); err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

// GetAgent returns the manifest for a specific agent.
func (c *Client) GetAgent(ctx context.Context, name string) (*AgentManifest, error) {
	var manifest AgentManifest
	if err := c.get(ctx, c.url("/agents/"+url.PathEscape(name)), &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// CreateRun initiates a new agent run.
func (c *Client) CreateRun(ctx context.Context, req RunCreateRequest) (*Run, error) {
	var run Run
	if err := c.post(ctx, c.url("/runs"), req, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// GetRun retrieves the current state of a run.
func (c *Client) GetRun(ctx context.Context, runID string) (*Run, error) {
	var run Run
	if err := c.get(ctx, c.url("/runs/"+url.PathEscape(runID)), &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ResumeRun resumes a paused run with additional input.
func (c *Client) ResumeRun(ctx context.Context, runID string, resume MessageAwaitResume, mode RunMode) (*Run, error) {
	req := RunResumeRequest{
		RunID:       runID,
		AwaitResume: resume,
		Mode:        mode,
	}
	var run Run
	if err := c.post(ctx, c.url("/runs/"+url.PathEscape(runID)), req, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// CancelRun cancels an ongoing run.
func (c *Client) CancelRun(ctx context.Context, runID string) (*Run, error) {
	var run Run
	if err := c.post(ctx, c.url("/runs/"+url.PathEscape(runID)+"/cancel"), nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// GetSession retrieves a session descriptor.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	var sess Session
	if err := c.get(ctx, c.url("/session/"+url.PathEscape(sessionID)), &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// RunSync executes an agent synchronously and waits for the result.
func (c *Client) RunSync(ctx context.Context, agentName string, input []Message) (*Run, error) {
	req := RunCreateRequest{
		AgentName: agentName,
		Input:     input,
		Mode:      ModeSync,
	}
	return c.CreateRun(ctx, req)
}

// RunAsync initiates an agent run in async mode and returns immediately.
func (c *Client) RunAsync(ctx context.Context, agentName string, input []Message) (*Run, error) {
	req := RunCreateRequest{
		AgentName: agentName,
		Input:     input,
		Mode:      ModeAsync,
	}
	return c.CreateRun(ctx, req)
}

// RunStream initiates a streaming run and returns an event channel.
func (c *Client) RunStream(ctx context.Context, agentName string, input []Message) (<-chan Event, error) {
	req := RunCreateRequest{
		AgentName: agentName,
		Input:     input,
		Mode:      ModeStream,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("acp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/runs"), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("acp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("acp: stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("acp: unexpected status %d", resp.StatusCode)
	}

	events := make(chan Event, 64)
	go c.readSSE(ctx, resp.Body, events)
	return events, nil
}

// --- internal helpers ---

func (c *Client) url(path string) string {
	return c.baseURL + path
}

func (c *Client) get(ctx context.Context, urlStr string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return fmt.Errorf("acp: %w", err)
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, urlStr string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("acp: marshal: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, body)
	if err != nil {
		return fmt.Errorf("acp: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("acp: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("acp: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("acp: decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) readSSE(ctx context.Context, r io.ReadCloser, events chan<- Event) {
	defer r.Close()
	defer close(events)

	scanner := bufio.NewScanner(r)
	var eventType string
	var dataBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
			continue
		}
		if line == "" {
			if dataBuf.Len() > 0 {
				evt := Event{Type: eventType}
				json.Unmarshal(dataBuf.Bytes(), &evt.Data)
				select {
				case events <- evt:
				case <-ctx.Done():
					return
				default:
				}
				eventType = ""
				dataBuf.Reset()
			}
			continue
		}
	}
}
