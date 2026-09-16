package agent

import (
	"testing"

	"github.com/lsongdev/miya-agents/config"
	"github.com/lsongdev/miya-agents/session"
)

func TestRecordingSinkPersistsOnDone(t *testing.T) {
	oldConfigPath := config.ConfigPath
	config.ConfigPath = t.TempDir()
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	sess := session.New("default")
	sink := NewRecordingSink(sess, discardSink{})
	if err := sink.AssistantDelta("hello"); err != nil {
		t.Fatal(err)
	}
	if err := sink.Done(); err != nil {
		t.Fatal(err)
	}

	loaded, err := session.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(loaded.Events))
	}
}
