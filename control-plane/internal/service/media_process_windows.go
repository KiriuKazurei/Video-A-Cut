//go:build windows

package service

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Stop the owned Python/Node process together with any FFmpeg descendants.
func configureMediaCommand(cmd *exec.Cmd) {
	hideTTSCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		kill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		hideTTSCommand(kill)
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
