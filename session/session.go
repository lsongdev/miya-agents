package session

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/openai"
)

type Event struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Update    acp.SessionUpdate `json:"update"`
}

type Compaction struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	MessageStart int       `json:"message_start"`
	MessageEnd   int       `json:"message_end"`
	KeepRecent   int       `json:"keep_recent"`
	Summary      string    `json:"summary"`
}

type Session struct {
	ID          string                         `json:"id"`
	AgentName   string                         `json:"agent_name"`
	Title       string                         `json:"title,omitempty"`
	Summary     string                         `json:"summary,omitempty"`
	CreatedAt   time.Time                      `json:"created_at"`
	UpdatedAt   time.Time                      `json:"updated_at,omitempty"`
	Messages    []openai.ChatCompletionMessage `json:"messages"`
	Events      []Event                        `json:"events"`
	Compactions []Compaction                   `json:"compactions"`
}

func sessionsDir() string {
	return filepath.Join(config.ConfigPath, "sessions")
}

func sessionPath(id string) string {
	return filepath.Join(sessionsDir(), id+".json")
}

// newUUID returns a RFC 4122 v4 UUID string.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based id; collisions astronomically unlikely.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// New creates a new session with a generated UUID for the given agent.
func New(agentName string) *Session {
	return &Session{
		ID:        newUUID(),
		AgentName: agentName,
		CreatedAt: time.Now(),
		Messages:  []openai.ChatCompletionMessage{},
		Events:    []Event{},
	}
}

// Load reads the session with the given id from disk.
func Load(id string) (*Session, error) {
	data, err := os.ReadFile(sessionPath(id))
	if err != nil {
		return nil, fmt.Errorf("failed to read session %s: %w", id, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse session %s: %w", id, err)
	}
	if s.ID == "" {
		s.ID = id
	}
	if s.Messages == nil {
		s.Messages = []openai.ChatCompletionMessage{}
	}
	if s.Events == nil {
		s.Events = []Event{}
	}
	if s.Compactions == nil {
		s.Compactions = []Compaction{}
	}
	return &s, nil
}

// List returns all sessions on disk, sorted by file mtime descending (newest first).
func List() ([]*Session, error) {
	dir := sessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type entryInfo struct {
		session *Session
		mtime   time.Time
	}
	infos := make([]entryInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-len(".json")]
		s, err := Load(id)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		infos = append(infos, entryInfo{session: s, mtime: fi.ModTime()})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].mtime.After(infos[j].mtime)
	})

	out := make([]*Session, len(infos))
	for i, info := range infos {
		out[i] = info.session
	}
	return out, nil
}

// Latest returns the most recently modified session, or nil if none exist.
func Latest() (*Session, error) {
	sessions, err := List()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return sessions[0], nil
}

func (s *Session) AppendRequest(request string) {
	s.Messages = append(s.Messages, openai.UserMessage(request))
}

func (s *Session) AppendResponse(response openai.ChatCompletionMessage) {
	s.Messages = append(s.Messages, response)
}

func (s *Session) AppendEvent(update acp.SessionUpdate) {
	s.Events = append(s.Events, Event{
		ID:        fmt.Sprintf("evt_%06d", len(s.Events)+1),
		CreatedAt: time.Now(),
		Update:    update,
	})
}

// SaveMessages writes the session (including metadata) to disk.
// Name kept for backwards compatibility with the agent loop.
func (s *Session) SaveMessages() {
	if err := s.Save(); err != nil {
		fmt.Printf("Error saving session: %v\n", err)
	}
}

// Save writes the session to disk and returns any error.
func (s *Session) Save() error {
	if s.ID == "" {
		s.ID = newUUID()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.Messages == nil {
		s.Messages = []openai.ChatCompletionMessage{}
	}
	if s.Events == nil {
		s.Events = []Event{}
	}
	if s.Compactions == nil {
		s.Compactions = []Compaction{}
	}
	s.UpdatedAt = time.Now()
	path := sessionPath(s.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}
	return nil
}

// FirstUserMessage returns the first user message content, useful for previews.
func (s *Session) FirstUserMessage() string {
	for _, m := range s.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

func (s *Session) DisplayTitle() string {
	if title := strings.TrimSpace(s.Title); title != "" {
		return title
	}
	title := strings.Join(strings.Fields(s.FirstUserMessage()), " ")
	if title == "" {
		return ""
	}
	if len(title) > 64 {
		title = title[:61] + "..."
	}
	return title
}
