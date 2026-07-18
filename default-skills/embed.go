package defaultskills

import (
	"embed"
	"io/fs"
)

//go:embed */SKILL.md
var embedded embed.FS

// FS returns the skills shipped with miya-agents.
func FS() fs.FS {
	return embedded
}
