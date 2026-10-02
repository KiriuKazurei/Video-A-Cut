//go:build !windows

package service

import (
	"fmt"
	"os"
	"strings"
)

func executionProcessLive(p runtimeProcessIdentity) (bool, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", p.PID))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pos := strings.LastIndex(string(raw), ")")
	if pos < 0 {
		return false, fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(raw)[pos+1:])
	if len(fields) <= 19 {
		return false, fmt.Errorf("invalid process identity")
	}
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", p.PID))
	return fields[19] == p.Created && path == p.Path, err
}
