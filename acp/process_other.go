//go:build !windows

package acp

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
