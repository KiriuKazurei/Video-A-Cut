package deliveryvariant

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed specs/ass-basic-v1.json
var SpecBytes []byte
var SpecSHA = digest(SpecBytes)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func ValidSpec(raw json.RawMessage, sha string) bool {
	var a, b any
	return sha == SpecSHA && json.Unmarshal(raw, &a) == nil && json.Unmarshal(SpecBytes, &b) == nil && reflect.DeepEqual(a, b)
}
func Fingerprint(in Input) (string, error) {
	if in.SchemaVersion != 1 || in.Format != "ass" || in.SpecID != SpecID || in.AssetID == "" || in.RunID == "" || in.RevisionID == "" || !preview.IsRelativePath(in.ParentPackageRef) || len(in.SourceFiles) == 0 {
		return "", fmt.Errorf("invalid variant input")
	}
	atoms := []string{"vac.delivery-variant.input.v1", "1", in.AssetID, in.RunID, in.RevisionID, in.ParentPackageRef, in.EDLSHA256, in.ParentManifestSHA256, in.Format, in.SpecID, in.SpecSHA256, strconv.Itoa(len(in.SourceFiles))}
	for _, h := range []string{in.EDLSHA256, in.ParentManifestSHA256, in.SpecSHA256} {
		if !shaPattern.MatchString(h) {
			return "", fmt.Errorf("invalid variant hash")
		}
	}
	files := slices.Clone(in.SourceFiles)
	slices.SortFunc(files, func(a, b InputFile) int { return strings.Compare(a.Path, b.Path) })
	seen := map[string]bool{}
	for _, f := range files {
		alias := strings.ToLower(f.Path)
		if !preview.IsRelativePath(f.Path) || seen[alias] || f.Kind == "" || !shaPattern.MatchString(f.SHA256) || f.Size < 0 || f.Size > 9007199254740991 {
			return "", fmt.Errorf("invalid variant file")
		}
		seen[alias] = true
		atoms = append(atoms, f.Kind, f.Path, f.SHA256, strconv.FormatInt(f.Size, 10))
	}
	hash := sha256.New()
	for _, a := range atoms {
		fmt.Fprintf(hash, "%d:", len([]byte(a)))
		hash.Write([]byte(a))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
