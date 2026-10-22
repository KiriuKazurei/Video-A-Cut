package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// loudnessProject imports a generated file with two audio streams: stream 1 is
// silent, stream 2 a loud tone. Long enough that a computation is observable.
func loudnessProject(t *testing.T, seconds string) (*service.Service, string, string, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("real Node/FFmpeg loudness")
	}
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	wd, _ := os.Getwd()
	if err := svc.ConfigureTTS("python3", filepath.Join(wd, "..", "..", "..", "workers", "python")); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root, "two-streams.mkv")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=gray:s=32x32:r=1:d="+seconds,
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo:d="+seconds, "-f", "lavfi", "-i", "sine=f=440:sample_rate=48000:d="+seconds,
		"-map", "0", "-map", "1", "-map", "2", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "pcm_s16le", "-shortest", media)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", out, err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "loud.avp"), Name: "Loud"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, media)
	if err != nil {
		t.Fatal(err)
	}
	r := view.Document.Resources[len(view.Document.Resources)-1]
	return svc, view.Document.Project.ID, r.ID, r.Source.SHA256, media
}

func peakMax(t *testing.T, raw []byte) (int, string) {
	var env struct {
		Peak    string `json:"peak"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return len(env.Peak), strings.TrimRight(env.Peak, "A=")
}

func TestResourceLoudnessStreamsHaveSeparateVersionedCaches(t *testing.T) {
	svc, pid, rid, sha, _ := loudnessProject(t, "5")
	ctx := t.Context()
	silent, err := svc.ResourceLoudness(ctx, pid, rid, 1)
	if err != nil {
		t.Fatal(err)
	}
	loud, err := svc.ResourceLoudness(ctx, pid, rid, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, nonzero := peakMax(t, silent); nonzero != "" {
		t.Fatal("stream 1 is silent but envelope is not", nonzero[:10])
	}
	if _, nonzero := peakMax(t, loud); nonzero == "" {
		t.Fatal("stream 2 envelope must not be served from stream 1 cache")
	}
	for _, stream := range []int{1, 2} {
		path := svc.LoudnessCacheFile(sha, stream)
		if !strings.Contains(filepath.Base(path), "loudness-v2") || !strings.Contains(filepath.Base(path), sha) {
			t.Fatal("cache key must carry hash and version", path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("cache file missing", path, err)
		}
	}
	if svc.LoudnessCacheFile(sha, 1) == svc.LoudnessCacheFile(sha, 2) {
		t.Fatal("streams share a cache key")
	}
	again, err := svc.ResourceLoudness(ctx, pid, rid, 2)
	if err != nil || !jsonEqual(again, loud) {
		t.Fatal("stream 2 cache mismatch", err)
	}
}

func TestResourceLoudnessChangedSourceIsRejectedEvenWhenCached(t *testing.T) {
	svc, pid, rid, _, media := loudnessProject(t, "3")
	ctx := t.Context()
	if _, err := svc.ResourceLoudness(ctx, pid, rid, 2); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(media, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("changed"))
	_ = f.Close()
	if _, err = svc.ResourceLoudness(ctx, pid, rid, 2); !errors.Is(err, model.ErrArgument) {
		t.Fatal("changed resource must be rejected, got", err)
	}
}

func TestResourceLoudnessConcurrentConflictAndCancelReleasesLock(t *testing.T) {
	svc, pid, rid, _, _ := loudnessProject(t, "5400")
	// Concurrent: while the first computation runs, the same key conflicts.
	first, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := svc.ResourceLoudness(first, pid, rid, 2); done <- err }()
	// Give the first request time to take the lock and start FFmpeg.
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatal("first computation finished too early to observe conflict", err)
	default:
	}
	_, err := svc.ResourceLoudness(t.Context(), pid, rid, 2)
	conflict := errors.Is(err, model.ErrConflict)
	if !conflict {
		t.Fatal("concurrent request for the same key must return ErrConflict")
	}
	// Cancel (client disconnect) must release the lock.
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled computation must fail")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancel did not stop the computation")
	}
	ctx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	_, err = svc.ResourceLoudness(ctx, pid, rid, 2)
	if errors.Is(err, model.ErrConflict) {
		t.Fatal("lock still held after cancel")
	}
}
