package preview

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
)

//go:embed presets/mp4-720p-v1.json
var defaultPresetBytes []byte

var defaultPresetSHA256 string

func init() {
	h := sha256.Sum256(defaultPresetBytes)
	defaultPresetSHA256 = hex.EncodeToString(h[:])
}

// GetPreset returns the raw JSON bytes and SHA-256 digest of the requested preset.
func GetPreset(presetID string) ([]byte, string, error) {
	if presetID == "" || presetID == PresetID {
		return defaultPresetBytes, defaultPresetSHA256, nil
	}
	return nil, "", fmt.Errorf("unknown preset %q: %w", presetID, ErrInvalidInput)
}

// DefaultPresetSHA256 returns the SHA-256 digest of PresetID.
func DefaultPresetSHA256() string {
	return defaultPresetSHA256
}
