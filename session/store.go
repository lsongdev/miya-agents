package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
)

const logExt = ".jsonl"

func sessionsDir() string {
	return filepath.Join(config.ConfigPath, "sessions")
}

func sessionPath(id string) string {
	return filepath.Join(sessionsDir(), id+logExt)
}

// Info is a session's metadata, without its history.
type Info struct {
	ID        string
	AgentName string
	// Title is the display title: the recorded one, or the start of the
	// first user message.
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Messages is the number of messages in the log.
	Messages int
}

// List returns every session in the sessions directory, most recently
// updated first. Unreadable sessions are skipped.
func List() ([]Info, error) {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var infos []Info
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != logExt {
			continue
		}
		id := strings.TrimSuffix(e.Name(), logExt)
		if info, err := readInfo(id); err == nil {
			infos = append(infos, info)
		}
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
	})
	return infos, nil
}

// Latest returns the most recently updated session, or nil if none exist.
func Latest() (*Session, error) {
	infos, err := List()
	if err != nil || len(infos) == 0 {
		return nil, err
	}
	return Load(infos[0].ID)
}

// Delete removes a session. Deleting a session that does not exist is not an
// error.
func Delete(id string) error {
	if err := os.Remove(sessionPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// readInfo scans a log for its metadata. Message bodies are left undecoded,
// apart from the first user message when no title was recorded.
func readInfo(id string) (Info, error) {
	data, err := os.ReadFile(sessionPath(id))
	if err != nil {
		return Info{}, err
	}
	info := Info{ID: id}
	var firstUser json.RawMessage
	_, err = eachLine(data, func(line []byte) error {
		var entry struct {
			Time    time.Time       `json:"time"`
			Meta    *Meta           `json:"meta"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		if info.CreatedAt.IsZero() {
			info.CreatedAt = entry.Time
		}
		info.UpdatedAt = entry.Time
		if meta := entry.Meta; meta != nil {
			if meta.AgentName != "" {
				info.AgentName = meta.AgentName
			}
			if meta.Title != "" {
				info.Title = meta.Title
			}
		}
		if entry.Message != nil {
			info.Messages++
			if firstUser == nil && info.Title == "" {
				var role struct {
					Role llm.Role `json:"role"`
				}
				if json.Unmarshal(entry.Message, &role) == nil && role.Role == llm.RoleUser {
					firstUser = entry.Message
				}
			}
		}
		return nil
	})
	if err != nil {
		return Info{}, err
	}
	if info.Title == "" && firstUser != nil {
		var message llm.Message
		if json.Unmarshal(firstUser, &message) == nil {
			info.Title = titleFrom(message.Text())
		}
	}
	return info, nil
}
