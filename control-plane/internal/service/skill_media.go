package service

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func (s *Service) FFmpegCapabilities(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	report := map[string]any{"available": false, "ffprobe": false, "version": "", "libx264": false, "aac": false, "subtitles": false}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return report, nil
	}
	probe := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		hideTTSCommand(cmd)
		var out scriptOutput
		cmd.Stdout = &out
		cmd.Stderr = &out
		if cmd.Run() != nil {
			return ""
		}
		return out.String()
	}
	version := probe("-version")
	encoders := probe("-hide_banner", "-encoders")
	filters := probe("-hide_banner", "-filters")
	if version != "" {
		report["version"] = strings.SplitN(version, "\n", 2)[0]
		report["available"] = true
	}
	_, e := exec.LookPath("ffprobe")
	report["ffprobe"] = e == nil
	report["libx264"] = strings.Contains(encoders, "libx264")
	report["aac"] = strings.Contains(encoders, " aac ")
	report["subtitles"] = strings.Contains(filters, " ass ") || strings.Contains(filters, " subtitles ")
	report["hardware_acceleration"] = probe("-hide_banner", "-hwaccels")
	return report, nil
}
