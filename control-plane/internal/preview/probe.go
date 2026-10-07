package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MediaProber inspects a physical media file and extracts stream information.
type MediaProber interface {
	ProbeMedia(ctx context.Context, absPath string) (Media, error)
}

// FFprobeProber uses local ffprobe binary to extract stream information.
type FFprobeProber struct {
	Bin     string
	Timeout time.Duration
}

var DefaultProber MediaProber = &FFprobeProber{
	Bin:     "ffprobe",
	Timeout: 5 * time.Second,
}

type ffprobeStream struct {
	CodecType    string `json:"codec_type"`
	CodecName    string `json:"codec_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RFrameRate   string `json:"r_frame_rate"`
	AvgFrameRate string `json:"avg_frame_rate"`
	NBFrames     string `json:"nb_frames"`
	SampleRate   string `json:"sample_rate"`
	Channels     int    `json:"channels"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

func (p *FFprobeProber) ProbeMedia(ctx context.Context, absPath string) (Media, error) {
	bin := p.Bin
	if bin == "" {
		bin = "ffprobe"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin,
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,avg_frame_rate,nb_frames,sample_rate,channels:format=duration",
		"-of", "json",
		absPath,
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if cctx.Err() != nil {
			return Media{}, fmt.Errorf("ffprobe probe timed out on %s: %w", absPath, cctx.Err())
		}
		return Media{}, fmt.Errorf("ffprobe probe failed on %s: %w (stderr: %s)", absPath, err, stderr.String())
	}

	var out ffprobeOutput
	lr := io.LimitReader(&stdout, 1<<20) // max 1MB
	if err := json.NewDecoder(lr).Decode(&out); err != nil {
		return Media{}, fmt.Errorf("decode ffprobe output for %s: %w", absPath, err)
	}

	var media Media
	hasVideo := false
	hasAudio := false

	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			if !hasVideo {
				hasVideo = true
				media.VideoCodec = s.CodecName
				media.Width = s.Width
				media.Height = s.Height
				media.FPS = parseFPS(s.AvgFrameRate, s.RFrameRate)
				if s.NBFrames != "" {
					if n, err := strconv.ParseInt(s.NBFrames, 10, 64); err == nil {
						media.FrameCount = n
					}
				}
			}
		case "audio":
			if !hasAudio {
				hasAudio = true
				media.AudioCodec = s.CodecName
				media.Channels = s.Channels
				if sr, err := strconv.Atoi(s.SampleRate); err == nil {
					media.SampleRate = sr
				}
			}
		}
	}

	if out.Format.Duration != "" {
		if d, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil {
			media.DurationSeconds = d
			if media.FrameCount == 0 && media.FPS > 0 {
				media.FrameCount = int64(math.Round(d * float64(media.FPS)))
			}
		}
	}

	if !hasVideo {
		return Media{}, errors.New("ffprobe detected no video stream in output")
	}

	return media, nil
}

func parseFPS(rates ...string) float64 {
	for _, rate := range rates {
		if rate == "" || rate == "0/0" {
			continue
		}
		parts := strings.Split(rate, "/")
		if len(parts) == 2 {
			num, err1 := strconv.ParseFloat(parts[0], 64)
			den, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil && den > 0 {
				return num / den
			}
		} else if len(parts) == 1 {
			if num, err := strconv.ParseFloat(parts[0], 64); err == nil {
				return num
			}
		}
	}
	return 0
}
