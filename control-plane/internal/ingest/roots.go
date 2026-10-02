// Package ingest holds the phase-7 media-ingest contracts: configured source
// roots, path confinement, the versioned media policy, analysis and selection
// plans, and the result documents an ingester submits. It contains no
// storage or transport code; service owns every state transition.
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// Root is one administrator-configured directory recordings may come from.
// Path never leaves the control plane; browsers only see ID and Name.
type Root struct {
	ID   string `json:"root_id"`
	Name string `json:"name"`
	Path string `json:"-"`
}

// PublicRoot is the browser projection of a Root.
type PublicRoot struct {
	ID   string `json:"root_id"`
	Name string `json:"name"`
}

var rootID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

const maxRelativeRunes = 1024

// ValidateRoots rejects duplicate IDs, relative or UNC paths and names that
// cannot be shown safely.
func ValidateRoots(roots []Root) error {
	seen := map[string]bool{}
	for _, r := range roots {
		if !rootID.MatchString(r.ID) || seen[r.ID] {
			return fmt.Errorf("ingest root id must be unique [a-z0-9_-]{1,64}: %w", model.ErrArgument)
		}
		seen[r.ID] = true
		if r.Name == "" || utf8.RuneCountInString(r.Name) > 100 || strings.ContainsAny(r.Name, "\x00\r\n") {
			return fmt.Errorf("ingest root %s needs a display name of 1..100 characters: %w", r.ID, model.ErrArgument)
		}
		if !filepath.IsAbs(r.Path) || strings.HasPrefix(r.Path, `\\`) || strings.HasPrefix(r.Path, "//") {
			return fmt.Errorf("ingest root %s must be a local absolute directory: %w", r.ID, model.ErrArgument)
		}
	}
	return nil
}

// RootsFingerprint identifies the root mapping. The ingester computes the
// same value from its own configuration; claims require both to match.
func RootsFingerprint(roots []Root) string {
	sorted := append([]Root(nil), roots...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b strings.Builder
	for _, r := range sorted {
		b.WriteString(r.ID)
		b.WriteByte('\t')
		b.WriteString(NormalizeRootPath(r.Path))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// NormalizeRootPath is the textual form both sides hash.
func NormalizeRootPath(p string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(p)))
}

var reservedDevice = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9]|conin\$|conout\$)(\..*)?$`)

// CleanRelative validates a browser-supplied path inside a root and returns
// its slash form. It never touches the filesystem.
func CleanRelative(rel string) (string, error) {
	bad := func(reason string) (string, error) {
		return "", fmt.Errorf("relative_path %s: %w", reason, model.ErrArgument)
	}
	if rel == "" || utf8.RuneCountInString(rel) > maxRelativeRunes || !utf8.ValidString(rel) {
		return bad("must be 1..1024 characters")
	}
	rel = strings.ReplaceAll(rel, `\`, "/")
	if strings.HasPrefix(rel, "/") {
		return bad("must not be absolute or UNC")
	}
	if strings.ContainsAny(rel, `:*?"<>|`) {
		return bad("must not contain a drive, URL, stream or wildcard character")
	}
	for _, r := range rel {
		if r < 0x20 || r == 0x7f {
			return bad("must not contain control characters")
		}
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return bad("must not contain empty, . or .. segments")
		}
		if strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
			return bad("segments must not end with a space or dot")
		}
		if reservedDevice.MatchString(part) {
			return bad("must not name a device")
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return bad("must stay inside the root")
	}
	return rel, nil
}

// FindRoot returns the configured root by id.
func FindRoot(roots []Root, id string) (Root, error) {
	for _, r := range roots {
		if r.ID == id {
			return r, nil
		}
	}
	return Root{}, fmt.Errorf("ingest root is not configured: %w", model.ErrNotFound)
}

// SourceFile is a confined regular file and its change marker.
type SourceFile struct {
	Path    string
	Size    int64
	MTimeNs int64
}

// ResolveSource walks rel component by component below root and refuses
// symlinks, junctions and other reparse points before checking that the
// resolved file is still inside the resolved root.
func ResolveSource(root Root, rel string) (SourceFile, error) {
	clean, err := CleanRelative(rel)
	if err != nil {
		return SourceFile{}, err
	}
	base, err := filepath.EvalSymlinks(root.Path)
	if err != nil {
		return SourceFile{}, fmt.Errorf("ingest root %s is unavailable: %w", root.ID, model.ErrInvalidState)
	}
	if info, e := os.Stat(base); e != nil || !info.IsDir() {
		return SourceFile{}, fmt.Errorf("ingest root %s is not a directory: %w", root.ID, model.ErrInvalidState)
	}
	cur := base
	parts := strings.Split(clean, "/")
	var info fs.FileInfo
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err = os.Lstat(cur)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return SourceFile{}, fmt.Errorf("recording file does not exist in root %s: %w", root.ID, model.ErrNotFound)
			}
			return SourceFile{}, fmt.Errorf("recording path cannot be inspected: %w", model.ErrArgument)
		}
		mode := info.Mode()
		if mode&fs.ModeSymlink != 0 || mode&fs.ModeIrregular != 0 {
			return SourceFile{}, fmt.Errorf("recording path crosses a symlink or junction: %w", model.ErrForbidden)
		}
		last := i == len(parts)-1
		if !last && !mode.IsDir() {
			return SourceFile{}, fmt.Errorf("recording path has a non-directory segment: %w", model.ErrArgument)
		}
		if last && !mode.IsRegular() {
			return SourceFile{}, fmt.Errorf("recording path must name a regular file: %w", model.ErrArgument)
		}
	}
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return SourceFile{}, fmt.Errorf("recording path cannot be resolved: %w", model.ErrArgument)
	}
	within, err := filepath.Rel(base, real)
	if err != nil || !filepath.IsLocal(within) {
		return SourceFile{}, fmt.Errorf("recording path escapes root %s: %w", root.ID, model.ErrForbidden)
	}
	return SourceFile{Path: real, Size: info.Size(), MTimeNs: info.ModTime().UnixNano()}, nil
}

// SourceVersion is the server-computed identity of a registered source. It
// changes whenever the file's length or modification time changes.
func SourceVersion(rootID, rel string, size, mtimeNs int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("vac-source-v1\n%s\n%s\n%d\n%d", rootID, rel, size, mtimeNs)))
	return hex.EncodeToString(sum[:])
}
