package config

import (
	"path/filepath"
	"testing"
)

func TestProfileGetWorkspaceDefaultsToMiyaWorkspace(t *testing.T) {
	old := ConfigPath
	ConfigPath = filepath.Join(t.TempDir(), ".miya")
	t.Cleanup(func() {
		ConfigPath = old
	})

	profile := &ProfileConfig{}

	if got, want := profile.GetWorkspace(), filepath.Join(ConfigPath, "workspace"); got != want {
		t.Fatalf("workspace = %q, want %q", got, want)
	}
}
