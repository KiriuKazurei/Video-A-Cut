package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

type ScriptDeliveryFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size_bytes"`
	SHA256 string `json:"sha256"`
}
type ScriptDeliveryInfo struct {
	ScriptID      string               `json:"script_id"`
	Version       int                  `json:"version"`
	VisibleSetSHA string               `json:"visible_set_sha"`
	Width         int                  `json:"width"`
	Height        int                  `json:"height"`
	Duration      float64              `json:"duration"`
	Frames        int                  `json:"frames"`
	SHA256        string               `json:"sha256"`
	TTSEnabled    bool                 `json:"tts_enabled"`
	Files         []ScriptDeliveryFile `json:"files"`
}

func readScriptDelivery(path string) (ScriptDeliveryInfo, error) {
	var info ScriptDeliveryInfo
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return info, err
	}
	if len(raw) > 2<<20 || json.Unmarshal(raw, &info) != nil {
		return info, model.ErrInvalidState
	}
	return info, nil
}

// Check actual files before publishing a renderer's package.
func (s *Service) verifyScriptDelivery(ctx context.Context, ref, id string, version int, sha string) (ScriptDeliveryInfo, error) {
	path, _, err := s.deliveryPath(filepath.Join(filepath.FromSlash(ref), "script-delivery.json"), false)
	if err != nil {
		return ScriptDeliveryInfo{}, err
	}
	info, err := readScriptDelivery(path)
	if err != nil {
		return info, err
	}
	if info.ScriptID != id || info.Version != version || info.VisibleSetSHA != sha || info.Width != 1280 || info.Height != 720 || info.Frames < 1 || !finiteScriptTime(info.Duration) || info.Duration <= 0 || len(info.Files) < 3 || len(info.Files) > 500 {
		return info, model.ErrInvalidState
	}
	seen := map[string]bool{}
	for _, file := range info.Files {
		if err := ctx.Err(); err != nil {
			return info, err
		}
		if !filepath.IsLocal(file.Path) || seen[file.Path] || file.Size < 0 || len(file.SHA256) != 64 {
			return info, model.ErrArgument
		}
		seen[file.Path] = true
		full, stat, err := s.deliveryPath(filepath.Join(filepath.FromSlash(ref), filepath.FromSlash(file.Path)), false)
		if err != nil {
			return info, err
		}
		if stat.Size() != file.Size {
			return info, model.ErrConflict
		}
		f, err := os.Open(full)
		if err != nil {
			return info, err
		}
		digest := sha256.New()
		_, err = io.Copy(digest, f)
		closeErr := f.Close()
		if err != nil {
			return info, err
		}
		if closeErr != nil {
			return info, closeErr
		}
		if hex.EncodeToString(digest.Sum(nil)) != file.SHA256 {
			return info, model.ErrConflict
		}
		if (file.Path == "final.mp4" || file.Path == "media/video_1_final.mp4") && file.SHA256 != info.SHA256 {
			return info, model.ErrConflict
		}
	}
	for _, required := range []string{"final.mp4", "media/video_1_final.mp4", "audio_Game.wav", "edit.xml", "timeline.otio", "script.json", "IMPORT.txt"} {
		if !seen[required] {
			return info, fmt.Errorf("交付缺少 %s: %w", required, model.ErrInvalidState)
		}
	}
	return info, nil
}

func (s *Service) ScriptDeliveryInfo(ctx context.Context, id string, version int) (ScriptDeliveryInfo, error) {
	root, err := s.ScriptDeliveryPath(ctx, id, "", version)
	if err != nil {
		return ScriptDeliveryInfo{}, err
	}
	return s.scriptDeliveryInfoAt(root, id, version)
}

func (s *Service) scriptDeliveryInfoAt(root, id string, version int) (ScriptDeliveryInfo, error) {
	path, _, err := s.previewPackageFile(root, "script-delivery.json")
	if err != nil {
		return ScriptDeliveryInfo{}, err
	}
	info, err := readScriptDelivery(path)
	if err != nil {
		return info, err
	}
	if info.ScriptID != id || info.Version < 1 || (version > 0 && info.Version != version) || len(info.SHA256) != 64 || len(info.Files) == 0 {
		return info, fmt.Errorf("请重新生成当前修订的交付清单: %w", model.ErrInvalidState)
	}
	return info, nil
}

// Resolve one immutable package root, so a same-revision rerender cannot pair
// the old player's hash with a newer package between two lookups.
func (s *Service) ScriptDownloadPath(ctx context.Context, id, name string, version int, sha string) (string, error) {
	if name != "" && name != "final.mp4" {
		return "", model.ErrArgument
	}
	root, err := s.ScriptDeliveryPath(ctx, id, "", version)
	if err != nil {
		return "", err
	}
	info, err := s.scriptDeliveryInfoAt(root, id, version)
	if err != nil {
		return "", err
	}
	if sha != "" && sha != info.SHA256 {
		return "", model.ErrConflict
	}
	if name == "" {
		return root, nil
	}
	path, _, err := s.previewPackageFile(root, name)
	return path, err
}
