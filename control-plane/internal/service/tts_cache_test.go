package service

import (
	"context"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTTSCacheExpiresAndKeepsOnlyNewestBudget(t *testing.T) {
	st, e := store.Open(filepath.Join(t.TempDir(), "cache.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	s := New(st)
	root := t.TempDir()
	if e = s.ConfigureDeliveryRoot(root); e != nil {
		t.Fatal(e)
	}
	sentinel := filepath.Join(root, "business.wav")
	os.WriteFile(sentinel, []byte("retain business data"), 0600)
	// Test accounting with small files and synthetic metadata, without allocating 120 MiB.
	for i, id := range []string{"ttl_old", "budget_old", "budget_middle", "budget_latest"} {
		dir := filepath.Join(root, ".tts-previews", id)
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(dir, "audio.wav"), []byte("test-cache"), 0600)
		age := time.Duration(3-i) * time.Minute
		if i == 0 {
			age = 25 * time.Hour
		}
		p := tts.Preview{JobID: id, Status: "succeeded", CreatedAt: time.Now().Add(-age), SizeBytes: 40 * 1024 * 1024}
		if e = st.PutTTSPreview(context.Background(), p); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.trimTTSCache(); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"ttl_old", "budget_old"} {
		p, _ := s.GetTTSPreview(t.Context(), id)
		if !p.Expired {
			t.Fatal("did not expire", id)
		}
		if _, e = s.TTSPreviewAudio(t.Context(), id); !errors.Is(e, model.ErrInvalidState) {
			t.Fatal("expired served", e)
		}
		if _, e = os.Stat(filepath.Join(root, ".tts-previews", id)); !os.IsNotExist(e) {
			t.Fatal("cache not removed", e)
		}
	}
	for _, id := range []string{"budget_middle", "budget_latest"} {
		if _, e = s.TTSPreviewAudio(t.Context(), id); e != nil {
			t.Fatal("newest removed", id, e)
		}
	}
	if b, e := os.ReadFile(sentinel); e != nil || string(b) != "retain business data" {
		t.Fatal("business path changed", e)
	}
}
