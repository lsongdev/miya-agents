//go:build !windows

package tools

import "os/exec"

func configureShellCommand(cmd *exec.Cmd) {}
