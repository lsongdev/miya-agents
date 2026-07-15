package agent

import (
	"testing"

	"github.com/lsongdev/miya-agents/config"
)

func TestResolveAgentNameFallsBackWhenSessionProfileMissing(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"coding": {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})

	got, err := m.resolveAgentName("default")
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "coding" {
		t.Fatalf("resolveAgentName = %q, want coding", got)
	}
}

func TestResolveAgentNamePrefersDefault(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles: map[string]*config.ProfileConfig{
			"default": {Provider: "openai", ModelName: "gpt-4"},
			"coding":  {Provider: "openai", ModelName: "gpt-4"},
		},
		Providers: map[string]*config.ProviderConfig{},
	})

	got, err := m.resolveAgentName("")
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "default" {
		t.Fatalf("resolveAgentName = %q, want default", got)
	}
}

func TestResolveAgentNameReportsEmptyProfiles(t *testing.T) {
	m := NewAgentManager(&config.Config{
		Profiles:  map[string]*config.ProfileConfig{},
		Providers: map[string]*config.ProviderConfig{},
	})

	if _, err := m.resolveAgentName("default"); err == nil {
		t.Fatal("resolveAgentName succeeded, want error")
	}
}
