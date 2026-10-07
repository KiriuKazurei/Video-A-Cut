package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IsRelativePath checks wire syntax only, not filesystem containment or symlinks.
func IsRelativePath(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '\\' || r == ':' || r < 32 || r == 127 {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Fingerprint hashes the same length-prefixed UTF-8 atoms as preview-protocol.mjs.
// preview_id/task_id are excluded so transport retries/new attempts share the input identity.
// This helper does not check actual files, preset contents, authorization or media readiness.
func Fingerprint(in Input) (string, error) {
	if in.SchemaVersion != ProtocolVersion || in.PresetID != PresetID || in.AssetID == "" || in.RunID == "" || in.RevisionID == "" || !IsRelativePath(in.PackageRef) || len(in.Files) == 0 {
		return "", ErrInvalidInput
	}
	atoms := []string{"vac.mp4-preview.input.v1", in.AssetID, in.RunID, in.RevisionID, in.PackageRef}
	for _, hash := range []string{in.EDLSHA256, in.ManifestSHA256, in.PresetSHA256} {
		if !shaPattern.MatchString(hash) {
			return "", ErrInvalidInput
		}
		atoms = append(atoms, hash)
	}
	atoms = append(atoms, in.PresetID, strconv.Itoa(len(in.Files)))
	files := slices.Clone(in.Files)
	slices.SortFunc(files, func(a, b InputFile) int { return strings.Compare(a.Path, b.Path) })
	seen := map[string]bool{}
	for _, file := range files {
		if !IsRelativePath(file.Path) || seen[file.Path] || !shaPattern.MatchString(file.SHA256) || file.Size < 0 || file.Size > 9007199254740991 {
			return "", ErrInvalidInput
		}
		switch file.Kind {
		case "edl", "delivery_manifest", "source_video", "packaged_source", "game_audio", "voice", "music", "srt":
		default:
			return "", ErrInvalidInput
		}
		seen[file.Path] = true
		atoms = append(atoms, file.Kind, file.Path, file.SHA256, strconv.FormatInt(file.Size, 10))
	}
	hash := sha256.New()
	for _, atom := range atoms {
		fmt.Fprintf(hash, "%d:", len([]byte(atom)))
		hash.Write([]byte(atom))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
