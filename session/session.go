// Package session stores a conversation as an append-only log of typed
// entries. Entries are never rewritten or deleted; what the model sees and
// what a client replays are both views derived from the same log.
package session

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lsongdev/miya-agents/llm"
)

type EntryType string

const (
	// EntryMeta sets session metadata. A later meta entry overrides the
	// fields it sets.
	EntryMeta EntryType = "meta"
	// EntryMessage is one complete user, assistant or tool message.
	EntryMessage EntryType = "message"
	// EntryCompaction replaces everything before a point in the log with a
	// summary, as far as the model is concerned.
	EntryCompaction EntryType = "compaction"
)

// Entry is one line of the log. Type selects which of the payload fields is
// set.
type Entry struct {
	// ID is the entry's position in the log, starting at 1.
	ID   int       `json:"id"`
	Type EntryType `json:"type"`
	Time time.Time `json:"time"`

	Meta    *Meta        `json:"meta,omitempty"`
	Message *llm.Message `json:"message,omitempty"`
	// Usage is the token usage of the model call that produced an assistant
	// message.
	Usage *llm.Usage `json:"usage,omitempty"`
	// Files are the files a tool handed to the user, recorded with its
	// result message. The model does not see them.
	Files      []llm.Media `json:"files,omitempty"`
	Compaction *Compaction `json:"compaction,omitempty"`
}

// Compaction records that the model's view of the log now starts with
// Summary, followed by the messages from entry FirstKept onward. Earlier
// entries stay in the log and are still replayed to clients.
type Compaction struct {
	Summary string `json:"summary"`
	// FirstKept is the ID of the first entry the model still sees in full.
	FirstKept int `json:"first_kept"`
	// TokensBefore is the estimated context size that prompted this.
	TokensBefore int `json:"tokens_before,omitempty"`
}

// Meta is session metadata. Empty fields leave the current value unchanged.
type Meta struct {
	AgentName string `json:"agent_name,omitempty"`
	Title     string `json:"title,omitempty"`
}

// Session is a conversation log, held in memory and, when it has a backing
// file, written through on every Add.
type Session struct {
	ID        string
	AgentName string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Entries   []Entry

	// path is the backing file, or empty for a session that lives only in
	// memory.
	path string
}

// New returns a session that lives only in memory, for work that should leave
// no trace, such as a delegated task.
func New(agentName string) *Session {
	s := &Session{ID: newUUID()}
	_ = s.Add(Entry{Meta: &Meta{AgentName: agentName}}) // cannot fail without a file
	return s
}

// Create returns a new session backed by a file in the sessions directory.
func Create(agentName string) (*Session, error) {
	s := &Session{ID: newUUID()}
	s.path = sessionPath(s.ID)
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	if err := s.Add(Entry{Meta: &Meta{AgentName: agentName}}); err != nil {
		return nil, err
	}
	return s, nil
}

// Load reads the session with the given id from the sessions directory.
func Load(id string) (*Session, error) {
	path := sessionPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}
	s := &Session{ID: id, path: path}
	valid, err := eachLine(data, func(line []byte) error {
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		s.Entries = append(s.Entries, entry)
		s.apply(entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse session %s: %w", id, err)
	}
	// A crash can leave a partial last line. Drop it so the next entry
	// starts on a line of its own.
	if valid < len(data) {
		if err := os.Truncate(path, int64(valid)); err != nil {
			return nil, fmt.Errorf("repair session %s: %w", id, err)
		}
	}
	return s, nil
}

// eachLine calls fn for every non-empty line of a log. It returns the length
// of the prefix that parsed. Only an unterminated last line may fail to
// parse, which is what an interrupted write leaves behind; a bad line
// anywhere else is an error.
func eachLine(data []byte, fn func(line []byte) error) (int, error) {
	offset := 0
	for offset < len(data) {
		end := bytes.IndexByte(data[offset:], '\n')
		terminated := end >= 0
		if !terminated {
			end = len(data) - offset
		}
		line := bytes.TrimSpace(data[offset : offset+end])
		if len(line) > 0 {
			if err := fn(line); err != nil {
				if !terminated {
					return offset, nil
				}
				return offset, err
			}
		}
		offset += end
		if terminated {
			offset++
		}
	}
	return offset, nil
}

// Add appends an entry to the log, writing it to the backing file first when
// there is one. ID and Time are assigned here; Type is inferred from the
// payload when left empty.
func (s *Session) Add(entry Entry) error {
	if entry.Type == "" {
		switch {
		case entry.Message != nil:
			entry.Type = EntryMessage
		case entry.Meta != nil:
			entry.Type = EntryMeta
		case entry.Compaction != nil:
			entry.Type = EntryCompaction
		default:
			return errors.New("session entry has no payload")
		}
	}
	entry.ID = len(s.Entries) + 1
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	if s.path != "" {
		line, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode session entry: %w", err)
		}
		if err := appendLine(s.path, line); err != nil {
			return fmt.Errorf("write session %s: %w", s.ID, err)
		}
	}
	s.Entries = append(s.Entries, entry)
	s.apply(entry)
	return nil
}

func appendLine(path string, line []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// apply folds an entry into the session's derived fields.
func (s *Session) apply(entry Entry) {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = entry.Time
	}
	s.UpdatedAt = entry.Time
	if meta := entry.Meta; meta != nil {
		if meta.AgentName != "" {
			s.AgentName = meta.AgentName
		}
		if meta.Title != "" {
			s.Title = meta.Title
		}
	}
}

// AddMessage appends messages to the log.
func (s *Session) AddMessage(messages ...llm.Message) error {
	for _, message := range messages {
		if err := s.Add(Entry{Message: &message}); err != nil {
			return err
		}
	}
	return nil
}

// AddUser appends a user text message.
func (s *Session) AddUser(text string) error {
	return s.AddMessage(llm.UserText(text))
}

// SetTitle records a new title.
func (s *Session) SetTitle(title string) error {
	return s.Add(Entry{Meta: &Meta{Title: title}})
}

// SummaryPrefix introduces a compaction summary where it stands in for the
// messages it replaced.
const SummaryPrefix = "Earlier parts of this conversation were replaced by the following summary to save context:\n\n"

// Active returns what the model still sees of the log: the summary of the
// latest compaction, if any, and the message entries that follow it.
func (s *Session) Active() (summary string, entries []Entry) {
	firstKept := 0
	for _, entry := range s.Entries {
		if c := entry.Compaction; c != nil {
			summary, firstKept = c.Summary, c.FirstKept
		}
	}
	for _, entry := range s.Entries {
		if entry.Message != nil && entry.ID >= firstKept {
			entries = append(entries, entry)
		}
	}
	return summary, entries
}

// Context is the view of the log sent to the model: the latest compaction
// summary as a user message, then the messages still active, in order.
//
// A turn can be cut short between a tool call and its result, by a crash or
// a cancelled request. Providers reject a call with no result, so each one
// is answered here with a placeholder. The log itself is left as it is.
func (s *Session) Context() []llm.Message {
	summary, entries := s.Active()
	var messages []llm.Message
	if summary != "" {
		messages = append(messages, llm.UserText(SummaryPrefix+summary))
	}
	var pending []llm.ToolCall // calls of the last assistant message still unanswered
	closePending := func() {
		for _, call := range pending {
			messages = append(messages, llm.ToolResultMessage(llm.ToolResult{
				ID:      call.ID,
				Name:    call.Name,
				Content: "Error: the tool call was interrupted before it produced a result.",
				IsError: true,
			}))
		}
		pending = nil
	}
	for _, entry := range entries {
		message := entry.Message
		if message.Role == llm.RoleTool {
			for _, block := range message.Content {
				if block.ToolResult != nil {
					pending = slices.DeleteFunc(pending, func(call llm.ToolCall) bool {
						return call.ID == block.ToolResult.ID
					})
				}
			}
		} else {
			closePending()
		}
		messages = append(messages, *message)
		if message.Role == llm.RoleAssistant {
			pending = message.ToolCalls()
		}
	}
	closePending()
	return messages
}

// DisplayTitle returns the title, falling back to the start of the first
// user message.
func (s *Session) DisplayTitle() string {
	if title := strings.TrimSpace(s.Title); title != "" {
		return title
	}
	for _, entry := range s.Entries {
		if entry.Message != nil && entry.Message.Role == llm.RoleUser {
			return titleFrom(entry.Message.Text())
		}
	}
	return ""
}

// titleFrom shortens text to a single-line title.
func titleFrom(text string) string {
	title := strings.Join(strings.Fields(text), " ")
	if runes := []rune(title); len(runes) > 64 {
		title = string(runes[:61]) + "..."
	}
	return title
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
