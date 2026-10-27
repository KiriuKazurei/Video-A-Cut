package service

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type drawingFontFile struct {
	Size     int64
	Modified int64
	SHA      string
}

var drawingFonts = struct {
	sync.Mutex
	Files map[string]drawingFontFile
}{Files: map[string]drawingFontFile{}}

// System fallback fonts are legitimate source dependencies too. Cache file
// digests, but recheck metadata at each cache boundary; no wall-clock TTL is a
// substitute for a content dependency. Paths never leave the local service.
func drawingFontVersion(roots []string) string {
	drawingFonts.Lock()
	defer drawingFonts.Unlock()
	entries := []string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".ttf" && ext != ".ttc" && ext != ".otf" {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return nil
			}
			cached, ok := drawingFonts.Files[path]
			if !ok || cached.Size != info.Size() || cached.Modified != info.ModTime().UnixNano() {
				sha, e := sequenceFileDigest(path)
				if e != nil {
					return nil
				}
				cached = drawingFontFile{info.Size(), info.ModTime().UnixNano(), sha}
				drawingFonts.Files[path] = cached
			}
			entries = append(entries, path+":"+cached.SHA)
			return nil
		})
	}
	sort.Strings(entries)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(entries, "\n"))))
}
func drawingEnvironmentVersion() string {
	roots := []string{"/usr/share/fonts", "/usr/local/share/fonts"}
	if runtime.GOOS == "windows" {
		roots = []string{filepath.Join(os.Getenv("WINDIR"), "Fonts"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts")}
	}
	return "vac-unified-1:hf0.8.193:remotion4.0.538:srgb-straight:" + drawingFontVersion(roots)
}
