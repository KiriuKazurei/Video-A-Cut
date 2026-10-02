package preparation

import (
	"context"
	"os/exec"
	"time"
)

// LocalProbe checks only programs on this machine. It does not call a model,
// enumerate voices, or read another worker's environment.
type LocalProbe struct {
	FFmpeg  string
	FFprobe string
	Timeout time.Duration
}

func NewLocalProbe() LocalProbe {
	return LocalProbe{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Timeout: 3 * time.Second}
}

func (p LocalProbe) Probe(ctx context.Context, _ Profile) ([]Check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ffmpeg := p.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	ffprobe := p.FFprobe
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	return []Check{
		runVersion(ctx, timeout, "ffmpeg", ffmpeg),
		runVersion(ctx, timeout, "ffprobe", ffprobe),
	}, nil
}

func runVersion(ctx context.Context, timeout time.Duration, code, bin string) Check {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-version")
	cmd.WaitDelay = 200 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if cctx.Err() != nil {
			return Check{Code: code, Status: "blocked", Message: code + " 探测超时，未调用模型"}
		}
		return Check{Code: code, Status: "blocked", Message: code + " 不可调用"}
	}
	return Check{Code: code, Status: "passed", Message: code + " 可调用"}
}
