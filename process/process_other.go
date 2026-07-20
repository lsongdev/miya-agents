//go:build !windows

package process

import "os/exec"

func ConfigureCommand(cmd *exec.Cmd) {}
