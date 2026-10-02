package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// The Python ingester asserts the same vector (tests/test_ingest.py).
func TestRootsFingerprintVector(t *testing.T) {
	roots := []Root{{ID: "rec", Name: "r", Path: `E:\Data\Rec\`}, {ID: "b", Name: "b", Path: `D:/Video Files/OBS`}}
	if runtime.GOOS != "windows" {
		t.Skip("vector uses Windows paths")
	}
	want := "b\td:/video files/obs\nrec\te:/data/rec\n"
	sum := sha256.Sum256([]byte(want))
	if got := RootsFingerprint(roots); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %s, want sha256(%q)", got, want)
	}
}

func TestCleanRelative(t *testing.T) {
	for _, bad := range []string{"", "../x.mkv", "a/../../x", "C:/x.mkv", `\\srv\share\x`, "/abs", "a/con.mkv", "a//b", "x.mkv.", "a/b:s", "a/*.mkv", "a\x01b"} {
		if _, err := CleanRelative(bad); !errors.Is(err, model.ErrArgument) {
			t.Errorf("CleanRelative(%q) err = %v", bad, err)
		}
	}
	got, err := CleanRelative(`录屏 2026\a b.mkv`)
	if err != nil || got != "录屏 2026/a b.mkv" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestResolveSourceRefusesLinksAndDirectories(t *testing.T) {
	base := t.TempDir()
	root := Root{ID: "rec", Name: "r", Path: filepath.Join(base, "root")}
	outside := filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root.Path, "sub 目录"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root.Path, "sub 目录", "a.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.mkv"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := ResolveSource(root, "sub 目录/a.mkv")
	if err != nil || f.Size != 1 || f.MTimeNs == 0 {
		t.Fatalf("resolve = %+v %v", f, err)
	}
	if _, err := ResolveSource(root, "sub 目录"); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("directory err = %v", err)
	}
	if _, err := ResolveSource(root, "missing.mkv"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	link := filepath.Join(root.Path, "escape")
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
			t.Skipf("cannot create junction: %v %s", err, out)
		}
	} else if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if _, err := ResolveSource(root, "escape/secret.mkv"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("junction escape err = %v", err)
	}
}

func TestFrameQuota(t *testing.T) {
	q, err := FrameQuota([]int64{10, 30, 20}, 8, 5)
	if err != nil || q[0] != 2 || q[1] != 3 || q[2] != 3 {
		t.Fatalf("quota = %v %v", q, err)
	}
	q, _ = FrameQuota([]int64{1, 2}, 100, 5)
	if q[0] != 5 || q[1] != 5 {
		t.Fatalf("per-segment cap not applied: %v", q)
	}
	if _, err := FrameQuota([]int64{1, 2, 3}, 2, 5); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("over budget err = %v", err)
	}
}

func TestPolicyBounds(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	body, sum, err := p.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePolicy(body, sum); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePolicy(body, strings.Repeat("0", 64)); err == nil {
		t.Fatal("tampered fingerprint accepted")
	}
	p.MaxCandidates = 4096
	if err := p.Validate(); err == nil {
		t.Fatal("policy above the documented ceiling accepted")
	}
	p = DefaultPolicy()
	p.MaxSelected = 3
	if err := p.Validate(); err != nil {
		t.Fatalf("smaller budget refused: %v", err)
	}
}

func probeFixture() Probe {
	start := int64(0)
	return Probe{SchemaVersion: 1, ProbeVersion: "t", Container: "matroska,webm", SizeBytes: 10, DurationUs: 60_000_000,
		Streams: []ProbeStream{{Index: 0, Type: "video", Width: 320, Height: 240, StartUs: &start}, {Index: 1, Type: "audio", SampleRate: 48000, Channels: 2}}}
}

func TestValidateSelection(t *testing.T) {
	pr, p := probeFixture(), DefaultPolicy()
	sel := Selection{SchemaVersion: 1, BasePlanRevision: 1, Output: Output{FPS: 30, SampleRate: 48000},
		SelectedSegments: []SelectedSegment{{SegmentID: "a", StartUs: 0, EndUs: 4_000_000}, {SegmentID: "b", StartUs: 5_000_000, EndUs: 9_000_000}}}
	if err := ValidateSelection(sel, pr, p); err != nil {
		t.Fatal(err)
	}
	bad := []func(s *Selection){
		func(s *Selection) { s.SelectedSegments[1].StartUs = 3_000_000 },
		func(s *Selection) { s.SelectedSegments[1].EndUs = 61_000_000 },
		func(s *Selection) { s.Output.FPS = 25 },
		func(s *Selection) { s.SelectedSegments[1].SegmentID = "a" },
		func(s *Selection) { s.SelectedSegments = nil },
		func(s *Selection) { s.SelectedSegments[0].EndUs = 10 },
	}
	for i, mutate := range bad {
		s := sel
		s.SelectedSegments = append([]SelectedSegment(nil), sel.SelectedSegments...)
		mutate(&s)
		if err := ValidateSelection(s, pr, p); !errors.Is(err, model.ErrArgument) {
			t.Errorf("case %d accepted: %v", i, err)
		}
	}
	p.MaxOutputUs = 5_000_000
	if err := ValidateSelection(sel, pr, p); err == nil {
		t.Fatal("output budget not enforced")
	}
}

func TestParseSegmentsRequiresCompleteGapFreeCoverage(t *testing.T) {
	plan := AnalysisPlan{SchemaVersion: 1, SourceRangeUs: [2]int64{0, 10_000_000},
		Segmentation: Segmentation{Method: "scene_change", Threshold: 0.3, MinSegmentUs: 1_000_000, MaxSegmentUs: 600_000_000}}
	doc := `{"schema_version":1,"method":"scene_change","method_version":"m","plan_sha256":"P","range_us":[0,10000000],
"config":{"threshold":0.3,"min_segment_us":1000000,"max_segment_us":600000000,"chunk_us":60000000,"overlap_us":1000000,"max_edge":640,"min_cut_gap_us":250000},
"chunks_total":1,"chunks_done":1,"complete":true,"cuts":[{"t_us":4000000,"score":0.9}],
"candidates":[{"segment_id":"seg_0001","start_us":0,"end_us":4000000,"score":0,"reason":"range_start","thumbnail":"thumbnails/seg_0001.jpg"},
{"segment_id":"seg_0002","start_us":4000000,"end_us":10000000,"score":0.9,"reason":"scene_change","thumbnail":""}]}`
	if _, err := ParseSegments([]byte(doc), plan, "P", DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"partial": func(s string) string { return strings.Replace(s, `"complete":true`, `"complete":false`, 1) },
		"gap": func(s string) string {
			return strings.Replace(s, `"start_us":4000000,"end_us":10000000`, `"start_us":4500000,"end_us":10000000`, 1)
		},
		"short": func(s string) string {
			return strings.Replace(s, `"end_us":10000000,"score":0.9`, `"end_us":9000000,"score":0.9`, 1)
		},
		"plan":     func(s string) string { return strings.Replace(s, `"plan_sha256":"P"`, `"plan_sha256":"Q"`, 1) },
		"thumb":    func(s string) string { return strings.Replace(s, `thumbnails/seg_0001.jpg`, `../x.jpg`, 1) },
		"unknown":  func(s string) string { return strings.Replace(s, `"complete":true`, `"complete":true,"extra":1`, 1) },
		"settings": func(s string) string { return strings.Replace(s, `"threshold":0.3,"min`, `"threshold":0.4,"min`, 1) },
	} {
		if _, err := ParseSegments([]byte(mutate(doc)), plan, "P", DefaultPolicy()); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	p := DefaultPolicy()
	p.MaxCandidates = 1
	if _, err := ParseSegments([]byte(doc), plan, "P", p); err == nil {
		t.Error("candidate budget not enforced")
	}
}

func TestValidateAnalysis(t *testing.T) {
	pr := probeFixture()
	audio := 1
	plan := AnalysisPlan{SchemaVersion: 1, VideoStreamIndex: 0, GameAudioStreamIndex: &audio, SourceRangeUs: [2]int64{0, 60_000_000},
		Segmentation: Segmentation{Method: "scene_change", Threshold: 0.3, MinSegmentUs: 1_000_000, MaxSegmentUs: 600_000_000}}
	if err := ValidateAnalysis(plan, pr); err != nil {
		t.Fatal(err)
	}
	wrong := 0
	plan.GameAudioStreamIndex = &wrong
	if err := ValidateAnalysis(plan, pr); err == nil {
		t.Fatal("video stream accepted as game audio")
	}
	plan.GameAudioStreamIndex = nil
	plan.SourceRangeUs = [2]int64{0, 61_000_000}
	if err := ValidateAnalysis(plan, pr); err == nil {
		t.Fatal("range beyond the probe accepted")
	}
}
