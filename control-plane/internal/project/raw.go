package project

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// RawDevelop is a non-destructive, versioned recipe. Original sensor bytes stay unchanged.
type RawDevelop struct {
	Exposure     float64 `json:"exposure"`
	WhiteBalance string  `json:"white_balance"`
	Contrast     float64 `json:"contrast"`
	Saturation   float64 `json:"saturation"`
}

func DefaultRawDevelop() RawDevelop {
	return RawDevelop{WhiteBalance: "camera", Contrast: 1, Saturation: 1}
}
func (p RawDevelop) Validate() error {
	for _, v := range []float64{p.Exposure, p.Contrast, p.Saturation} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("RAW 参数必须为有限数值")
		}
	}
	if p.Exposure < -2 || p.Exposure > 3 || p.Contrast < 0.5 || p.Contrast > 2 || p.Saturation < 0 || p.Saturation > 2 {
		return fmt.Errorf("RAW 参数超出范围")
	}
	if p.WhiteBalance != "camera" && p.WhiteBalance != "auto" && p.WhiteBalance != "daylight" {
		return fmt.Errorf("无效 RAW 白平衡")
	}
	return nil
}
func IsCameraRaw(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".dng", ".arw":
		return true
	}
	return false
}
func ResourceContentVersion(r Resource) string {
	if !IsCameraRaw(r.Source.OriginalPath) {
		return r.Source.SHA256
	}
	recipe := DefaultRawDevelop()
	if r.RawDevelop != nil {
		recipe = *r.RawDevelop
	}
	raw, _ := json.Marshal(struct {
		Source  string
		Recipe  RawDevelop
		Decoder string
	}{r.Source.SHA256, recipe, "rawpy-0.27-v1"})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
