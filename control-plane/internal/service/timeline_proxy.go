package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

type proxyVideoStream struct {
	CodecType     string `json:"codec_type"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	PixelFormat   string `json:"pix_fmt"`
	Start         string `json:"start_time"`
	Duration      string `json:"duration"`
	Frames        string `json:"nb_frames"`
	ColorTransfer string `json:"color_transfer"`
	Disposition   struct {
		AttachedPicture int `json:"attached_pic"`
	} `json:"disposition"`
	Tags     map[string]string `json:"tags"`
	SideData []json.RawMessage `json:"side_data_list"`
}

func timelineProxySource(r project.Resource) (proxyVideoStream, string) {
	var stream proxyVideoStream
	if r.Kind != "video" || r.Draw != nil || r.Missing || r.Changed || r.Removed || r.Source.SHA256 == "" {
		return stream, ""
	}
	raw, _ := json.Marshal(r.Metadata["streams"])
	var streams []proxyVideoStream
	if json.Unmarshal(raw, &streams) != nil {
		return stream, ""
	}
	count := 0
	for _, candidate := range streams {
		if candidate.CodecType == "video" {
			stream = candidate
			count++
		}
	}
	start, startErr := strconv.ParseFloat(stream.Start, 64)
	duration, durationErr := strconv.ParseFloat(stream.Duration, 64)
	// Keep formats whose original media time cannot be represented safely by
	// this proxy recipe on the existing original/segmented playback path.
	if count != 1 || min(stream.Width, stream.Height) < 2 || max(stream.Width, stream.Height) <= 960 ||
		(stream.PixelFormat != "yuv420p" && stream.PixelFormat != "yuvj420p") ||
		stream.ColorTransfer == "smpte2084" || stream.ColorTransfer == "arib-std-b67" ||
		stream.Disposition.AttachedPicture != 0 || stream.Tags["rotate"] != "" || len(stream.SideData) != 0 ||
		startErr != nil || start != 0 || durationErr != nil || !(duration > 0) || math.IsInf(duration, 0) {
		return stream, ""
	}
	return stream, skillHash([]string{"timeline-video-v1-long960", r.Source.SHA256})
}

func (s *Service) timelineProxyFile(version string) string {
	return filepath.Join(s.workspace.root, "previews", "timeline-video", version+".mp4")
}

func (s *Service) readyTimelineProxy(version string) string {
	if version == "" {
		return ""
	}
	path := s.timelineProxyFile(version)
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return path
	}
	return ""
}

func (s *Service) TimelineProxyPath(projectID, id, version string) (string, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return "", err
	}
	_, current := timelineProxySource(r)
	if current == "" || version != current {
		return "", model.ErrNotFound
	}
	if path := s.readyTimelineProxy(current); path != "" {
		return path, nil
	}
	return "", model.ErrNotFound
}

// The caller owns cancellation. Cache lookup is immediate and a single encoder
// is shared by all editor requests, so a burst of seeks cannot start a queue
// of full-file transcodes.
func (s *Service) CreateTimelineProxy(ctx context.Context, projectID, id, version string) (bool, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return false, err
	}
	source, current := timelineProxySource(r)
	if current == "" || current != version {
		return false, model.ErrConflict
	}
	if s.readyTimelineProxy(current) != "" {
		return true, nil
	}
	release, err := s.acquireScript("timeline-video-proxy")
	if err != nil {
		return false, nil
	}
	defer release()
	if s.readyTimelineProxy(current) != "" {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	path := s.timelineProxyFile(current)
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	f, err := os.CreateTemp(dir, current+".partial-*.mp4")
	if err != nil {
		return false, err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Close(); err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file,pipe", "-threads", "1", "-i", r.Source.OriginalPath,
		"-map", "0:v:0", "-an", "-sn", "-dn", "-filter_threads", "1", "-vf",
		"scale=w='if(gte(iw,ih),960,-2)':h='if(gte(iw,ih),-2,960)'",
		"-fps_mode", "passthrough", "-enc_time_base", "demux", "-c:v", "libx264", "-preset", "ultrafast", "-bf", "0",
		"-crf", "25", "-threads", "1", "-pix_fmt", "yuv420p", "-force_key_frames",
		"expr:gte(t,n_forced*1)", "-movflags", "+faststart", temp)
	configureMediaCommand(cmd)
	cmd.Env = toolEnvironment()
	var log scriptOutput
	cmd.Stderr = &log
	if err = cmd.Run(); err != nil {
		return false, fmt.Errorf("生成时间线代理: %w %s", err, log.String())
	}
	if err = verifyTimelineProxy(ctx, temp, source); err != nil {
		return false, err
	}
	r, err = s.ProjectResource(projectID, id)
	_, latest := timelineProxySource(r)
	if err != nil || latest != current || ctx.Err() != nil {
		return false, model.ErrConflict
	}
	if err = os.Rename(temp, path); err != nil {
		return false, err
	}
	return true, nil
}

func verifyTimelineProxy(ctx context.Context, path string, source proxyVideoStream) error {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_streams", "-of", "json", path)
	configureMediaCommand(cmd)
	cmd.Env = toolEnvironment()
	raw, err := cmd.Output()
	if err != nil {
		return err
	}
	var probe struct {
		Streams []proxyVideoStream `json:"streams"`
	}
	if json.Unmarshal(raw, &probe) != nil || len(probe.Streams) != 1 {
		return model.ErrConflict
	}
	video := probe.Streams[0]
	srcDuration, _ := strconv.ParseFloat(source.Duration, 64)
	duration, durationErr := strconv.ParseFloat(video.Duration, 64)
	start, startErr := strconv.ParseFloat(video.Start, 64)
	if video.CodecType != "video" || min(video.Width, video.Height) < 2 || max(video.Width, video.Height) != 960 ||
		durationErr != nil || !(math.Abs(duration-srcDuration) <= .01) || startErr != nil || start != 0 ||
		(source.Frames != "" && source.Frames != "N/A" && source.Frames != video.Frames) {
		return model.ErrConflict
	}
	return nil
}
