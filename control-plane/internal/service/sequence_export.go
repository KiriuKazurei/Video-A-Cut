package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

// SequenceExportOptions selects a sequence delivery encode.
// An empty request, or preset_id program-native without overrides, keeps the
// historical behavior: copy the verified program preview. mp4-720p-v1 remains
// the separate workflow preview preset; passing it here re-encodes this
// sequence to that 720p ceiling and does not change the preview contract.
type SequenceExportOptions struct {
	PresetID     string `json:"preset_id,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	FPSNum       int    `json:"fps_num,omitempty"`
	FPSDen       int    `json:"fps_den,omitempty"`
	Codec        string `json:"codec,omitempty"`
	CRF          *int   `json:"crf,omitempty"`
	VideoBitrate string `json:"video_bitrate,omitempty"`
	Subtitles    string `json:"subtitles,omitempty"`
	Loudness     string `json:"loudness,omitempty"`
	Threads      int    `json:"threads,omitempty"`
}

type sequenceEncodeSpec struct {
	ID        string `json:"id"`
	Native    bool   `json:"native,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	MaxWidth  int    `json:"max_width,omitempty"`
	MaxHeight int    `json:"max_height,omitempty"`
	NoUpscale bool   `json:"no_upscale,omitempty"`
	FPSNum    int    `json:"fps_num,omitempty"`
	FPSDen    int    `json:"fps_den,omitempty"`
	Codec     string `json:"codec,omitempty"`
	CRF       *int   `json:"crf,omitempty"`
	Bitrate   string `json:"bitrate,omitempty"`
	Subtitles string `json:"subtitles,omitempty"`
	Loudness  string `json:"loudness,omitempty"`
	Threads   int    `json:"threads,omitempty"`
}

var exportBitrate = regexp.MustCompile(`^[1-9]\d{0,4}(\.\d)?[kKmM]$`)

func crfValue(v int) *int { return &v }

func presetEncode(id string) (sequenceEncodeSpec, bool) {
	switch id {
	case "", "program-native":
		return sequenceEncodeSpec{ID: "program-native", Native: true}, true
	case "mp4-720p-v1":
		return sequenceEncodeSpec{ID: id, MaxWidth: 1280, MaxHeight: 720, NoUpscale: true, Codec: "h264", CRF: crfValue(20), Subtitles: "burn"}, true
	case "1080p-landscape":
		return sequenceEncodeSpec{ID: id, Width: 1920, Height: 1080, Codec: "h264", CRF: crfValue(20), Subtitles: "burn"}, true
	case "1080x1920-vertical":
		return sequenceEncodeSpec{ID: id, Width: 1080, Height: 1920, Codec: "h264", CRF: crfValue(20), Subtitles: "burn"}, true
	case "2160p-4k":
		return sequenceEncodeSpec{ID: id, Width: 3840, Height: 2160, Codec: "h264", CRF: crfValue(20), Subtitles: "burn"}, true
	case "custom":
		return sequenceEncodeSpec{ID: id, Codec: "h264", CRF: crfValue(20), Subtitles: "burn"}, true
	default:
		return sequenceEncodeSpec{}, false
	}
}

func (o SequenceExportOptions) hasOverrides() bool {
	return o.Width != 0 || o.Height != 0 || o.FPSNum != 0 || o.FPSDen != 0 || o.Codec != "" || o.CRF != nil || o.VideoBitrate != "" || o.Subtitles != "" || o.Loudness != "" || o.Threads != 0
}

func evenExportSize(value int, label string) error {
	if value < 2 || value > 7680 || value%2 != 0 {
		return fmt.Errorf("%s须为 2–7680 的偶数", label)
	}
	return nil
}

func normalizeSequenceExport(opt *SequenceExportOptions) (sequenceEncodeSpec, error) {
	if opt == nil {
		return sequenceEncodeSpec{ID: "program-native", Native: true}, nil
	}
	spec, ok := presetEncode(opt.PresetID)
	if !ok {
		return spec, fmt.Errorf("未知导出预设")
	}
	if spec.Native {
		if opt.hasOverrides() {
			return spec, fmt.Errorf("当前节目预设不重新编码")
		}
		return spec, nil
	}
	if opt.Width != 0 || opt.Height != 0 {
		if err := evenExportSize(opt.Width, "宽度"); err != nil {
			return spec, err
		}
		if err := evenExportSize(opt.Height, "高度"); err != nil {
			return spec, err
		}
		spec.Width, spec.Height = opt.Width, opt.Height
		spec.MaxWidth, spec.MaxHeight, spec.NoUpscale = 0, 0, false
	}
	if spec.ID == "custom" && (spec.Width == 0 || spec.Height == 0) {
		return spec, fmt.Errorf("自定义预设需要宽度和高度")
	}
	if opt.FPSNum != 0 || opt.FPSDen != 0 {
		if opt.FPSDen < 1 || opt.FPSNum < 1 {
			return spec, fmt.Errorf("导出帧率无效")
		}
		fps := float64(opt.FPSNum) / float64(opt.FPSDen)
		if fps < 1 || fps > 120 {
			return spec, fmt.Errorf("导出帧率范围为 1–120")
		}
		spec.FPSNum, spec.FPSDen = opt.FPSNum, opt.FPSDen
	}
	if opt.Codec != "" {
		if opt.Codec != "h264" && opt.Codec != "hevc" {
			return spec, fmt.Errorf("编码只支持 H.264 或 HEVC")
		}
		spec.Codec = opt.Codec
	}
	if opt.VideoBitrate != "" && opt.CRF != nil {
		return spec, fmt.Errorf("码率与 CRF 只能选一个")
	}
	if opt.VideoBitrate != "" {
		if !exportBitrate.MatchString(opt.VideoBitrate) {
			return spec, fmt.Errorf("视频码率格式无效")
		}
		spec.Bitrate = opt.VideoBitrate
		spec.CRF = nil
	} else if opt.CRF != nil {
		if *opt.CRF < 0 || *opt.CRF > 51 {
			return spec, fmt.Errorf("CRF 范围为 0–51")
		}
		spec.CRF = crfValue(*opt.CRF)
		spec.Bitrate = ""
	}
	if opt.Subtitles != "" {
		if opt.Subtitles != "burn" && opt.Subtitles != "sidecar" {
			return spec, fmt.Errorf("字幕只支持烧录或外挂 SRT")
		}
		spec.Subtitles = opt.Subtitles
	}
	if opt.Loudness != "" {
		if opt.Loudness != "-14" && opt.Loudness != "-16" {
			return spec, fmt.Errorf("响度目标只支持 -14 或 -16 LUFS")
		}
		spec.Loudness = opt.Loudness
	}
	if opt.Threads != 0 {
		if opt.Threads < 1 || opt.Threads > 16 {
			return spec, fmt.Errorf("编码线程范围为 1–16")
		}
		spec.Threads = opt.Threads
	}
	if spec.Codec == "" {
		spec.Codec = "h264"
	}
	if spec.Subtitles == "" {
		spec.Subtitles = "burn"
	}
	return spec, nil
}

func (spec sequenceEncodeSpec) worker() map[string]any {
	body := map[string]any{"codec": spec.Codec, "subtitles": spec.Subtitles}
	if spec.Width > 0 && spec.Height > 0 {
		body["width"] = spec.Width
		body["height"] = spec.Height
	}
	if spec.MaxWidth > 0 && spec.MaxHeight > 0 {
		body["max_width"] = spec.MaxWidth
		body["max_height"] = spec.MaxHeight
		body["no_upscale"] = spec.NoUpscale
	}
	if spec.FPSNum > 0 && spec.FPSDen > 0 {
		body["fps_num"] = spec.FPSNum
		body["fps_den"] = spec.FPSDen
	}
	if spec.Bitrate != "" {
		body["bitrate"] = spec.Bitrate
	} else if spec.CRF != nil {
		body["crf"] = *spec.CRF
	}
	if spec.Loudness != "" {
		body["loudness"] = spec.Loudness
	}
	if spec.Threads > 0 {
		body["threads"] = spec.Threads
	}
	return body
}

func (s *Service) renderSequenceExport(ctx context.Context, projectID, sequenceID string, view TimelineView, spec sequenceEncodeSpec) (SequencePreview, string, error) {
	var report SequencePreview
	if spec.Native {
		return report, "", model.ErrArgument
	}
	sources, err := s.sequenceSources(ctx, projectID, view.Sequence)
	if err != nil {
		return report, "", err
	}
	key := skillHash(struct {
		Version string
		Spec    sequenceEncodeSpec
	}{view.ContentVersion, spec})
	output := filepath.Join(s.workspace.root, "sequences", projectID, sequenceID, "export-"+key[:16])
	var release func()
	for {
		release, err = s.acquireScript("sequence-export:" + projectID + ":" + sequenceID + ":" + key[:16])
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return report, "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer release()
	mp4 := filepath.Join(output, "program.mp4")
	raw, readErr := os.ReadFile(filepath.Join(output, "program.json"))
	valid := readErr == nil && json.Unmarshal(raw, &report) == nil && report.ContentVersion == view.ContentVersion
	if valid {
		digest, digestErr := sequenceFileDigest(mp4)
		valid = digestErr == nil && digest == report.SHA256
	}
	if !valid {
		if err = os.MkdirAll(output, 0o700); err != nil {
			return report, "", err
		}
		raw, err = s.scriptProcess(ctx, true, "sequence", map[string]any{
			"sequence": view.Sequence, "resources": sources, "output_dir": output,
			"content_version": view.ContentVersion, "encode": spec.worker(),
		})
		if err != nil {
			return report, "", err
		}
		if err = json.Unmarshal(raw, &report); err != nil {
			return report, "", err
		}
	}
	current, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return report, "", err
	}
	if current.Session != view.Session || current.ContentVersion != view.ContentVersion {
		return report, "", fmt.Errorf("序列已改变，旧导出结果已丢弃: %w", model.ErrConflict)
	}
	report.PresetID = spec.ID
	report.Codec = spec.Codec
	report.Subtitles = spec.Subtitles
	report.Loudness = spec.Loudness
	if spec.Subtitles == "sidecar" {
		if info, statErr := os.Stat(filepath.Join(output, "subtitles.srt")); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			report.SubtitleSidecar = filepath.Join(output, "subtitles.srt")
		}
	}
	return report, mp4, nil
}

func (s *Service) exportEncodedSequence(ctx context.Context, projectID, sequenceID string, req SequencePreviewRequest, spec sequenceEncodeSpec) (SequencePreview, error) {
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return SequencePreview{}, err
	}
	if req.Session != view.Session || req.ExpectedRevision != view.Sequence.Revision || req.ContentVersion != view.ContentVersion {
		return SequencePreview{}, model.ErrConflict
	}
	report, src, err := s.renderSequenceExport(ctx, projectID, sequenceID, view, spec)
	if err != nil {
		return report, err
	}
	w := s.workspace
	if err = w.lockMutation(ctx); err != nil {
		return report, err
	}
	defer w.op.Unlock()
	current, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return report, err
	}
	if current.Session != req.Session || current.ContentVersion != req.ContentVersion {
		return report, model.ErrConflict
	}
	digest, err := sequenceFileDigest(src)
	if err != nil {
		return report, err
	}
	if digest != report.SHA256 {
		return report, model.ErrConflict
	}
	if err = project.CopyAtomic(req.Path, src); err != nil {
		return report, err
	}
	if report.SubtitleSidecar != "" {
		dest := strings.TrimSuffix(req.Path, filepath.Ext(req.Path)) + ".srt"
		if err = project.CopyAtomic(dest, report.SubtitleSidecar); err != nil {
			return report, err
		}
		report.SubtitleSidecar = dest
	}
	return report, nil
}
