package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestDrawArgumentsIdentifyUnsupportedImageField(t *testing.T) {
	var request DrawSaveRequest
	err := decodeSkillArgs(json.RawMessage(`{"composition":{"document":{"images":[{"secret":"do-not-display"}]}}}`), &request)
	if !errors.Is(err, model.ErrArgument) || !strings.Contains(err.Error(), "images") || strings.Contains(err.Error(), "do-not-display") {
		t.Fatalf("expected a safe actionable image field diagnostic, got %v", err)
	}
}

func TestDrawingFontDependencyChangesOnlyWithFontContents(t *testing.T) {
	root := t.TempDir()
	font := filepath.Join(root, "fixture.ttf")
	if e := os.WriteFile(font, []byte("font-a"), 0600); e != nil {
		t.Fatal(e)
	}
	a := drawingFontVersion([]string{root})
	if e := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("metadata"), 0600); e != nil {
		t.Fatal(e)
	}
	if b := drawingFontVersion([]string{root}); a != b {
		t.Fatal("unrelated metadata invalidated drawing pixels")
	}
	if e := os.WriteFile(font, []byte("font-changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if b := drawingFontVersion([]string{root}); a == b {
		t.Fatal("font content failed to invalidate frame environment")
	}
}
