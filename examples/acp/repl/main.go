package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"

	"github.com/lsongdev/miya-agents/acp"
)

type replReceiver struct {
	acp.DefaultNotificationReceiver
	streaming  *atomic.Bool
	hasPrinted *atomic.Bool
}

func (r *replReceiver) UserMessageChunk(sessionID acp.SessionID, content acp.ContentBlock, messageID *acp.MessageID) {
	r.printText(content)
}

func (r *replReceiver) AgentMessageChunk(sessionID acp.SessionID, content acp.ContentBlock, messageID *acp.MessageID) {
	r.printText(content)
}

func (r *replReceiver) AgentThoughtChunk(sessionID acp.SessionID, thought string, content acp.ContentBlock) {
	r.streaming.Store(true)
}

func (r *replReceiver) ToolCall(sessionID acp.SessionID, toolCall *acp.ToolCall) {
	if toolCall == nil {
		return
	}
	r.ensureLine()
	fmt.Fprintf(os.Stderr, "[%s] %s (%s) ", toolCall.Kind, toolCall.Title, toolCall.Status)
}

func (r *replReceiver) UsageUpdate(sessionID acp.SessionID, usage *acp.UsageUpdate) {
	if r.streaming.Load() {
		fmt.Fprintln(os.Stderr)
		r.streaming.Store(false)
		r.hasPrinted.Store(false)
	}
}

func (r *replReceiver) printText(content acp.ContentBlock) {
	if content.Type != "text" || content.Text == "" {
		return
	}
	r.ensureLine()
	fmt.Fprint(os.Stderr, content.Text)
}

func (r *replReceiver) ensureLine() {
	if r.hasPrinted.CompareAndSwap(false, true) {
		r.streaming.Store(true)
		fmt.Fprint(os.Stderr, "  ")
	}
}

func main() {
	command := flag.String("cmd", "opencode acp", "ACP agent command, for example: \"miya acp\" or \"opencode acp\"")
	cwd := flag.String("cwd", "", "session working directory")
	flag.Parse()

	parts := strings.Fields(*command)
	if len(parts) == 0 {
		fmt.Fprintln(os.Stderr, "error: empty command")
		os.Exit(1)
	}
	sessionCwd := *cwd
	if sessionCwd == "" {
		var err error
		sessionCwd, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cwd: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "Connecting to ACP agent: %s\n", *command)
	client, err := acp.DialStdio(parts[0], parts[1:]...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	var streaming atomic.Bool
	var hasPrinted atomic.Bool
	client.OnNotification(acp.NewNotificationHandler(&replReceiver{
		streaming:  &streaming,
		hasPrinted: &hasPrinted,
	}))
	client.OnRequest(&acp.DefaultClientHandler{})

	fmt.Fprint(os.Stderr, "Initializing... ")
	initResp, err := client.Initialize(&acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: acp.DefaultClientCapabilities(),
		ClientInfo:         &acp.Implementation{Name: "acp-repl", Version: "0.1.0"},
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

	sessionID, err := newSession(client, sessionCwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "new session: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "ACP REPL. Type prompts, /new for a new session, /exit to quit. Ctrl+C cancels the active prompt.\n\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "read: %v\n", err)
			}
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "/exit", "/quit":
			return
		case "/new":
			sessionID, err = newSession(client, sessionCwd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "new session: %v\n", err)
			}
			continue
		}

		done := make(chan error, 1)
		go func() {
			_, err := client.Prompt(&acp.PromptRequest{
				SessionID: sessionID,
				Prompt:    []acp.ContentBlock{{Type: "text", Text: line}},
			})
			done <- err
		}()

		select {
		case <-sigCh:
			_ = client.CancelSession(sessionID)
			fmt.Fprintln(os.Stderr)
			streaming.Store(false)
			hasPrinted.Store(false)
		case err := <-done:
			if streaming.Load() {
				fmt.Fprintln(os.Stderr)
				streaming.Store(false)
				hasPrinted.Store(false)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "prompt: %v\n", err)
			}
		}
	}
}

func newSession(client *acp.Client, cwd string) (acp.SessionID, error) {
	fmt.Fprint(os.Stderr, "Creating session... ")
	resp, err := client.NewSession(&acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "OK (session: %s)\n", resp.SessionID)
	return resp.SessionID, nil
}
