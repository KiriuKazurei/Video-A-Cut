package service

import (
	"errors"
	"golang.org/x/sys/windows"
	"strconv"
	"strings"
)

// A recycled pid is not the former process. Access failures are not death proof.
func executionProcessLive(p runtimeProcessIdentity) (bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.PID))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return false, err
	}
	if exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return false, nil
	}
	stamp := strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10)
	if stamp != p.Created {
		return false, nil
	}
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return false, err
	}
	return strings.EqualFold(windows.UTF16ToString(buf[:size]), p.Path), nil
}
