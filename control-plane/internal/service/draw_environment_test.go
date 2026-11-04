package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

func TestSequenceVersionTracksOnlyRenderedSources(t *testing.T) {
	var doc project.AvpDocument
	var seq project.Sequence
	if err := json.Unmarshal([]byte(`{"resources":[{"id":"visible","source":{"sha256":"a"}},{"id":"hidden","source":{"sha256":"b"}}]}`), &doc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"scope_group":"active","tracks":[{"id":"v","kind":"video","group_id":"active","clips":[{"id":"c","resource_id":"visible","duration":30}]},{"id":"h","kind":"video","group_id":"active","hidden":true,"clips":[{"id":"d","resource_id":"hidden","duration":60}]},{"id":"outside","kind":"video","group_id":"other","clips":[{"id":"e","resource_id":"hidden","duration":90}]}]}`), &seq); err != nil {
		t.Fatal(err)
	}
	before := sequenceContentVersion(&doc, seq)
	doc.Resources[1].Source.SHA256 = "hidden-changed"
	seq.Tracks[1].Clips[0].Duration++
	seq.Tracks[2].Clips[0].Duration++
	if got := sequenceContentVersion(&doc, seq); got != before {
		t.Fatal("hidden or out-of-scope media invalidated the rendered sequence")
	}
	doc.Resources[0].Source.SHA256 = "visible-changed"
	if got := sequenceContentVersion(&doc, seq); got == before {
		t.Fatal("visible source changes did not invalidate the rendered sequence")
	}
	doc.Resources[0].Source.SHA256 = "a"
	seq.Tracks[1].Hidden = false
	if got := sequenceContentVersion(&doc, seq); got == before {
		t.Fatal("revealing a track did not invalidate the rendered sequence")
	}
}

func TestSegmentInputsKeepMigratedTransitionsAndCodeAudio(t *testing.T) {
	seq := project.NewSequence("segment")
	seq.Tracks = []project.Track{
		{ID: "legacy-video", Kind: "video", VisualGroup: "legacy", Clips: []project.Clip{{ID: "before", ResourceID: "video", Duration: 60}}},
		{ID: "legacy-code", Kind: "code", VisualGroup: "legacy", Clips: []project.Clip{{ID: "after", ResourceID: "code", At: 60, Duration: 60, Effects: project.Effects{Transition: 12}, Code: &project.CodeClip{Description: "画面"}}}},
	}
	sources := []sequenceSource{{ID: "video"}, {ID: "code", Draw: &project.DrawDocument{AudioBindings: []project.DrawAudioBinding{{ResourceID: "sound"}}}}, {ID: "sound", SHA256: "audio-v1"}, {ID: "unrelated"}}
	key, selected := segmentInputs(seq, sources, 60, 120)
	if len(selected) != 3 || selected[0].ID != "video" || selected[1].ID != "code" || selected[2].ID != "sound" {
		t.Fatalf("transition/audio dependency dropped: %+v", selected)
	}
	seq.Tracks[1].Clips[0].Code.Description, seq.Tracks[1].Clips[0].Code.RunID = "new draft", "task"
	if updated, _ := segmentInputs(seq, sources, 60, 120); updated != key {
		t.Fatal("description/task metadata invalidated the pixel cache")
	}
	sources[2].SHA256 = "audio-v2"
	if updated, _ := segmentInputs(seq, sources, 60, 120); updated == key {
		t.Fatal("audio changes reused stale segment")
	}
}

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
