//go:build !windows

package service

import "os/exec"

func hideTTSCommand(cmd *exec.Cmd) {}
