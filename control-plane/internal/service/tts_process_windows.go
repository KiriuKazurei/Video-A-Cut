//go:build windows

package service

import (
	"os/exec"
	"syscall"
)

func hideTTSCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
