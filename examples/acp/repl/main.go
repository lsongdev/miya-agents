package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"

	"github.com/lsongdev/miya-agents/acp"
)

var (
	streaming  atomic.Bool
	hasPrinted atomic.Bool
)

func main() {
	cmd := flag.String("cmd", "", "ACP agent command (e.g. \"opencode acp\")")
	url := flag.String("url", "", "ACP agent HTTP URL (e.g. \"http://127.0.0.1:8090\")")
	flag.Parse()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	var client CloseClient
	var sessionID acp.SessionID
	var cwd string

	if *url != "" {
		client = newHTTPClient(*url)
		cwd = "."
	} else {
		command := *cmd
		if command == "" {
			args := flag.Args()
			if len(args) > 0 {
				command = strings.Join(args, " ")
			}
		}
		if command == "" {
			command = "opencode acp"
		}
		parts := strings.Fields(command)
		if len(parts) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: acp-repl -cmd <command> | -url <http-url>")
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "Connecting to ACP agent: %s\n", command)
		var err error
		stdioClient, err := acp.DialStdio(parts[0], parts[1:]...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dial: %v\n", err)
			os.Exit(1)
		}
		defer stdioClient.Close()
		client = stdioClient
		cwd, _ = os.Getwd()
	}

	// 1. Initialize
	fmt.Fprint(os.Stderr, "Initializing... ")
	initResp, err := client.Initialize(&acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: acp.DefaultClientCapabilities(),
		ClientInfo: &acp.Implementation{
			Name:    "acp-repl",
			Version: "0.1.0",
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	agentInfo := "unknown"
	if initResp.AgentInfo != nil {
		agentInfo = fmt.Sprintf("%s v%s", initResp.AgentInfo.Name, initResp.AgentInfo.Version)
	}
	fmt.Fprintf(os.Stderr, "OK (agent: %s)\n", agentInfo)

	// 2. New Session
	fmt.Fprint(os.Stderr, "Creating session... ")
	sessResp, err := client.NewSession(&acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	sessionID = sessResp.SessionID
	fmt.Fprintf(os.Stderr, "OK (session: %s)\n\n", sessionID)
	fmt.Fprintf(os.Stderr, "ACP REPL — type your prompts below. Ctrl+C to cancel, Ctrl+D to exit.\n\n")

	// Register notification handler for streaming output
		if stdioClient, ok := client.(*acp.Client); ok {
		stdioClient.OnNotification(func(method string, params json.RawMessage) {
			var n struct {
				SessionID acp.SessionID `json:"sessionId"`
				Update    struct {
					SessionUpdate string           `json:"sessionUpdate"`
					Content       acp.ContentBlock `json:"content,omitempty"`
					ToolCall      *acp.ToolCall    `json:"toolCall,omitempty"`
				} `json:"update"`
			}
			if err := json.Unmarshal(params, &n); err != nil {
				return
			}
			switch n.Update.SessionUpdate {
			case "agent_message_chunk", "user_message_chunk":
				if !hasPrinted.Load() {
					hasPrinted.Store(true)
					streaming.Store(true)
					fmt.Fprint(os.Stderr, "  ")
				}
				fmt.Fprint(os.Stderr, n.Update.Content.Text)
			case "agent_thought_chunk":
				streaming.Store(true)
			case "tool_call":
				if !streaming.Load() {
					streaming.Store(true)
					fmt.Fprint(os.Stderr, "  ")
				}
				if n.Update.ToolCall != nil {
					fmt.Fprintf(os.Stderr, "[%s] %s (%s) ", n.Update.ToolCall.Kind, n.Update.ToolCall.Title, n.Update.ToolCall.Status)
				}
			case "usage_update":
				if streaming.Load() {
					fmt.Fprintln(os.Stderr)
					streaming.Store(false)
				}
			}
		})
	}

	// Input scanner
	scanner := newStdinScanner()
	prompt := "> "

	fmt.Fprint(os.Stderr, prompt)
	for {
		line, err := scanner.readLine()
		if err != nil {
			fmt.Fprintln(os.Stderr)
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Fprint(os.Stderr, prompt)
			continue
		}
		if line == "/exit" || line == "/quit" {
			break
		}
		if line == "/new" {
			fmt.Fprint(os.Stderr, "Creating new session... ")
			sessResp, err := client.NewSession(&acp.NewSessionRequest{
				Cwd:        cwd,
				McpServers: []acp.McpServer{},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			} else {
				sessionID = sessResp.SessionID
				fmt.Fprintf(os.Stderr, "OK (session: %s)\n", sessionID)
			}
			fmt.Fprint(os.Stderr, prompt)
			continue
		}

		// Send prompt
		done := make(chan struct{})
		var promptErr error

		go func() {
			defer close(done)
			_, promptErr = client.Prompt(&acp.PromptRequest{
			SessionID: sessionID,
			Prompt:    []acp.ContentBlock{{Type: "text", Text: line}},
		})
		}()

		select {
		case <-sigCh:
			client.CancelSession(sessionID)
			fmt.Fprintf(os.Stderr, "\n")
		case <-done:
			if streaming.Load() {
				streaming.Store(false)
				hasPrinted.Store(false)
				fmt.Fprintln(os.Stderr)
			}
			if promptErr != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", promptErr)
			}
		}
		fmt.Fprint(os.Stderr, prompt)
	}

	if httpClient, ok := client.(*httpClient); ok {
		httpClient.Close()
	}
	fmt.Fprintln(os.Stderr)
}

// CloseClient is the subset of acp functionality needed by the REPL.
type CloseClient interface {
	Initialize(req *acp.InitializeRequest) (*acp.InitializeResponse, error)
	NewSession(req *acp.NewSessionRequest) (*acp.NewSessionResponse, error)
	Prompt(req *acp.PromptRequest) (*acp.PromptResponse, error)
	CancelSession(sessionID acp.SessionID) error
}

// --- stdin scanner (no raw mode, just line-by-line) ---

type stdinScanner struct {
	scanner *bytes.Buffer
	reader  io.Reader
}

func newStdinScanner() *stdinScanner {
	return &stdinScanner{reader: os.Stdin}
}

func (s *stdinScanner) readLine() (string, error) {
	buf := make([]byte, 1)
	var line []byte
	for {
		n, err := s.reader.Read(buf)
		if err != nil {
			return string(line), err
		}
		if n == 0 {
			continue
		}
		if buf[0] == '\n' {
			return string(line), nil
		}
		if buf[0] == '\r' {
			continue
		}
		if buf[0] == 4 { // Ctrl+D
			return string(line), io.EOF
		}
		line = append(line, buf[0])
	}
}

// --- HTTP client for ACP over Streamable HTTP ---

type httpClient struct {
	baseURL string
	http    http.Client
	id      atomic.Int64
}

func newHTTPClient(baseURL string) *httpClient {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (c *httpClient) call(method string, params, result any) error {
	id := c.id.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpResp, err := c.http.Post(c.baseURL+"/"+method, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer httpResp.Body.Close()
	var resp jsonRPCResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return fmt.Errorf("http decode: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("rpc: %s", resp.Error.Message)
	}
	if result != nil && resp.Result != nil {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

func (c *httpClient) Initialize(req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	var resp acp.InitializeResponse
	if err := c.call("initialize", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *httpClient) NewSession(req *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	var resp acp.NewSessionResponse
	if err := c.call("session/new", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *httpClient) Prompt(req *acp.PromptRequest) (*acp.PromptResponse, error) {
	var resp acp.PromptResponse
	if err := c.call("session/prompt", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *httpClient) CancelSession(sessionID acp.SessionID) error {
	return c.call("session/cancel", map[string]any{"sessionId": sessionID}, nil)
}

func (c *httpClient) Close() {}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
