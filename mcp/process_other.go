//go:build !windows

package mcp

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
