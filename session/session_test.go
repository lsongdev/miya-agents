package session

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/llm"
)

func useTempSessions(t *testing.T) {
	t.Helper()
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })
	if err := os.MkdirAll(sessionsDir(), 0755); err != nil {
		t.Fatal(err)
	}
}

func toolCall(id, name string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.Block{
		{Type: llm.BlockToolCall, ToolCall: &llm.ToolCall{ID: id, Name: name, Arguments: `{}`}},
	}}
}

func TestDisplayTitle(t *testing.T) {
	sess := New("default")
	if got := sess.DisplayTitle(); got != "" {
		t.Fatalf("empty session title = %q", got)
	}
	sess.AddMessage(llm.AssistantText("greeting"), llm.UserText("  Implement session titles\nwith metadata  "))
	if got := sess.DisplayTitle(); got != "Implement session titles with metadata" {
		t.Fatalf("title = %q", got)
	}
	sess.SetTitle("Explicit title")
	if got := sess.DisplayTitle(); got != "Explicit title" {
		t.Fatalf("explicit title = %q", got)
	}
}

func TestTitleFromKeepsRunesWhole(t *testing.T) {
	title := titleFrom(strings.Repeat("会话标题", 30))
	if !strings.HasSuffix(title, "...") || len([]rune(title)) != 64 || strings.ContainsRune(title, '\uFFFD') {
		t.Fatalf("title = %q", title)
	}
}

func TestNewSessionLeavesNoFile(t *testing.T) {
	useTempSessions(t)

	sess := New("default")
	if err := sess.AddUser("hello"); err != nil {
		t.Fatal(err)
	}
	infos, err := List()
	if err != nil || len(infos) != 0 {
		t.Fatalf("List = %v, %v", infos, err)
	}
}

func TestCreateWritesThroughAndLoadsBack(t *testing.T) {
	useTempSessions(t)

	sess, err := Create("coder")
	if err != nil {
		t.Fatal(err)
	}
	usage := &llm.Usage{InputTokens: 12, OutputTokens: 3}
	answer := llm.AssistantText("hi")
	result := llm.ToolResultMessage(llm.ToolResult{ID: "c1", Name: "attach", Content: "attached"})
	file := llm.Media{Name: "a.png", MimeType: "image/png", Size: 3, Data: "AAAA"}
	for _, err := range []error{
		sess.AddUser("hello"),
		sess.SetTitle("Greeting"),
		sess.Add(Entry{Message: &answer, Usage: usage}),
		sess.Add(Entry{Message: &result, Files: []llm.Media{file}}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	// Every Add is on disk already; nothing needs to be saved or closed.
	loaded, err := Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AgentName != "coder" || loaded.Title != "Greeting" {
		t.Fatalf("metadata = %q %q", loaded.AgentName, loaded.Title)
	}
	if len(loaded.Entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(loaded.Entries))
	}
	for i, entry := range loaded.Entries {
		if entry.ID != i+1 || entry.Time.IsZero() {
			t.Fatalf("entry %d = %#v", i, entry)
		}
	}
	if got := loaded.Entries[3].Usage; got == nil || *got != *usage {
		t.Fatalf("usage = %#v", got)
	}
	if got := loaded.Entries[4].Files; len(got) != 1 || got[0] != file {
		t.Fatalf("files = %#v", got)
	}
	if !reflect.DeepEqual(loaded.Context(), sess.Context()) {
		t.Fatalf("context = %#v", loaded.Context())
	}

	// A loaded session keeps appending to the same log.
	if err := loaded.AddUser("again"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(sess.ID)
	if err != nil || len(reloaded.Entries) != 6 {
		t.Fatalf("reload = %v, entries = %d", err, len(reloaded.Entries))
	}
}

func TestLoadDropsInterruptedLastLine(t *testing.T) {
	useTempSessions(t)

	sess, err := Create("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.AddUser("hello"); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(sessionPath(sess.ID), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(`{"id":3,"type":"message","message":{"role":"assis`)
	file.Close()

	loaded, err := Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(loaded.Entries))
	}
	if err := loaded.AddUser("next"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(sess.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.Context(); len(got) != 2 || got[1].Text() != "next" {
		t.Fatalf("context after repair = %#v", got)
	}
}

func TestLoadRejectsCorruptMiddleLine(t *testing.T) {
	useTempSessions(t)

	path := sessionPath("bad")
	log := `{"id":1,"type":"meta","meta":{"agent_name":"default"}}` + "\n" + `{not json}` + "\n" + `{"id":3,"type":"meta","meta":{}}` + "\n"
	if err := os.WriteFile(path, []byte(log), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("bad"); err == nil {
		t.Fatal("Load accepted a corrupt line in the middle of the log")
	}
}

func TestContextAnswersInterruptedToolCalls(t *testing.T) {
	sess := New("default")
	two := toolCall("c1", "read")
	two.Content = append(two.Content, toolCall("c2", "exec").Content...)
	sess.AddMessage(
		llm.UserText("go"),
		two,
		llm.ToolResultMessage(llm.ToolResult{ID: "c1", Name: "read", Content: "one"}),
		// c2 never got a result: the turn was cut short here.
		llm.UserText("are you there?"),
		toolCall("c3", "read"),
	)

	context := sess.Context()
	var shape []string
	for _, message := range context {
		label := string(message.Role)
		if result := message.Content[0].ToolResult; result != nil {
			label += ":" + result.ID
			if result.IsError {
				label += ":interrupted"
			}
		}
		shape = append(shape, label)
	}
	want := "user assistant tool:c1 tool:c2:interrupted user assistant tool:c3:interrupted"
	if got := strings.Join(shape, " "); got != want {
		t.Fatalf("context = %q\nwant      %q", got, want)
	}
	if len(sess.Entries) != 6 {
		t.Fatalf("Context changed the log: %d entries", len(sess.Entries))
	}
}

func TestListReadsMetadataNewestFirst(t *testing.T) {
	useTempSessions(t)

	older, _ := Create("default")
	older.AddUser("first question")
	newer, _ := Create("coder")
	newer.AddUser("second question")
	newer.SetTitle("Named")
	newer.AddMessage(llm.AssistantText("answer"))

	infos, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("sessions = %d", len(infos))
	}
	if got := infos[0]; got.ID != newer.ID || got.AgentName != "coder" || got.Title != "Named" || got.Messages != 2 {
		t.Fatalf("newest = %#v", got)
	}
	// With no recorded title, the first user message stands in.
	if got := infos[1]; got.ID != older.ID || got.Title != "first question" || got.Messages != 1 {
		t.Fatalf("oldest = %#v", got)
	}

	latest, err := Latest()
	if err != nil || latest.ID != newer.ID {
		t.Fatalf("Latest = %v, %v", latest, err)
	}
	if err := Delete(newer.ID); err != nil {
		t.Fatal(err)
	}
	if infos, _ := List(); len(infos) != 1 {
		t.Fatalf("after delete: %d sessions", len(infos))
	}
}

func TestCompactionReplacesOlderContextButNotTheLog(t *testing.T) {
	useTempSessions(t)

	sess, err := Create("default")
	if err != nil {
		t.Fatal(err)
	}
	sess.AddMessage(
		llm.UserText("first"),           // id 2
		llm.AssistantText("answer one"), // id 3
		llm.UserText("second"),          // id 4
		llm.AssistantText("answer two"), // id 5
	)
	if err := sess.Add(Entry{Compaction: &Compaction{Summary: "They talked.", FirstKept: 4, TokensBefore: 900}}); err != nil {
		t.Fatal(err)
	}
	sess.AddUser("third")

	check := func(s *Session) {
		t.Helper()
		var texts []string
		for _, message := range s.Context() {
			texts = append(texts, message.Text())
		}
		want := []string{SummaryPrefix + "They talked.", "second", "answer two", "third"}
		if !reflect.DeepEqual(texts, want) {
			t.Fatalf("context = %q", texts)
		}
		if got := s.Context()[0].Role; got != llm.RoleUser {
			t.Fatalf("summary role = %q", got)
		}
		if len(s.Entries) != 7 {
			t.Fatalf("log has %d entries, want all 7", len(s.Entries))
		}
	}
	check(sess)
	loaded, err := Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	check(loaded)

	// A later compaction supersedes the earlier one.
	loaded.Add(Entry{Compaction: &Compaction{Summary: "Shorter.", FirstKept: 7}})
	if got := loaded.Context(); len(got) != 2 || got[0].Text() != SummaryPrefix+"Shorter." || got[1].Text() != "third" {
		t.Fatalf("context after second compaction = %#v", got)
	}
}
