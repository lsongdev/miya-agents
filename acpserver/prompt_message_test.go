package acpserver

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/llm"
	"github.com/lsongdev/miya-agents/session"
)

func ptr[T any](v T) *T { return &v }

func TestPromptMessageKeepsTextAndImages(t *testing.T) {
	local := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(local, []byte("PNGDATA"), 0644); err != nil {
		t.Fatal(err)
	}
	message := promptMessage([]acp.ContentBlock{
		{Type: "text", Text: "compare these"},
		{Type: "text", Text: "carefully"},
		{Type: "image", MimeType: "image/png", Data: "AAAA", Name: "a.png"},
		{Type: "image", MimeType: "image/jpeg", URI: ptr("https://example.com/b.jpg")},
		{Type: "image", MimeType: "image/png", URI: ptr("file://" + local)},
		{Type: "text", Text: "thanks"},
	})

	if message.Role != llm.RoleUser || len(message.Content) != 5 {
		t.Fatalf("message = %#v", message)
	}
	if got := message.Content[0]; got.Type != llm.BlockText || got.Text != "compare these\ncarefully" {
		t.Fatalf("leading text = %#v", got)
	}
	if got := message.Content[1].Media; got.Data != "AAAA" || got.MimeType != "image/png" || got.Name != "a.png" || got.Size != 3 {
		t.Fatalf("inline image = %#v", got)
	}
	if got := message.Content[2].Media; got.URL != "https://example.com/b.jpg" || got.Data != "" {
		t.Fatalf("remote image = %#v", got)
	}
	// A provider cannot fetch a local file, so it is read and inlined.
	if got := message.Content[3].Media; got.Data != base64.StdEncoding.EncodeToString([]byte("PNGDATA")) || got.URL != "" || got.Size != 7 {
		t.Fatalf("local image = %#v", got)
	}
	if got := message.Content[4].Text; got != "thanks" {
		t.Fatalf("trailing text = %q", got)
	}
}

func TestPromptMessageDescribesWhatTheModelCannotSee(t *testing.T) {
	message := promptMessage([]acp.ContentBlock{
		{Type: "text", Text: "see attached"},
		{Type: "resource", Name: "report.pdf", MimeType: "application/pdf", Size: ptr(2048), URI: ptr("file:///tmp/report.pdf")},
		{Type: "audio", Name: "voice.ogg", MimeType: "audio/ogg", Data: "AAAA"},
		{Type: "image", MimeType: "image/png", URI: ptr("file:///no/such/file.png")},
	})

	want := "see attached\n" +
		"[The user attached a file named report.pdf (application/pdf, 2048 bytes), available at file:///tmp/report.pdf]\n" +
		"[The user attached an audio named voice.ogg (audio/ogg); its content was not provided to you]\n" +
		"[The user attached an image (image/png), available at file:///no/such/file.png]"
	if len(message.Content) != 1 || message.Content[0].Text != want {
		t.Fatalf("message =\n%s", message.Text())
	}
}

func TestPromptMessageIsNeverEmpty(t *testing.T) {
	message := promptMessage(nil)
	if len(message.Content) != 1 || message.Content[0].Type != llm.BlockText {
		t.Fatalf("message = %#v", message)
	}
}

func TestReplayShowsUserImages(t *testing.T) {
	sess := session.New("default")
	sess.AddMessage(llm.Message{Role: llm.RoleUser, Content: []llm.Block{
		llm.Text("what is this?"),
		{Type: llm.BlockImage, Media: &llm.Media{MimeType: "image/png", Data: "AAAA", Name: "a.png"}},
	}})
	sender := &recordingSender{}
	if err := replaySession(sess, 0, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.updates) != 2 {
		t.Fatalf("updates = %d", len(sender.updates))
	}
	image := sender.updates[1]
	if image.SessionUpdate != "user_message_chunk" || image.Content.Type != "image" || image.Content.Data != "AAAA" {
		t.Fatalf("image update = %#v", image)
	}
}
