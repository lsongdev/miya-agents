package skills

import (
	"path/filepath"
	"testing"
)

func TestDefaultSkillsLoad(t *testing.T) {
	disk, err := LoadSkillsFromDirectory(filepath.Join("..", "default-skills"))
	if err != nil {
		t.Fatalf("load default skills: %v", err)
	}
	loaded, err := LoadDefaultSkills()
	if err != nil {
		t.Fatalf("load embedded default skills: %v", err)
	}
	if len(loaded) != len(disk) {
		t.Fatalf("embedded skills = %d, disk skills = %d", len(loaded), len(disk))
	}
	if len(loaded) == 0 {
		t.Fatal("expected default skills to load")
	}
	for _, skill := range loaded {
		if skill.Name == "" {
			t.Fatal("default skill name is required")
		}
		if skill.Prompt == "" {
			t.Fatalf("default skill %q prompt is required", skill.Name)
		}
	}
}
