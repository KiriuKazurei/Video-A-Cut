package service_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestResourceLoudnessRealWorkerCacheAndAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("real Node/FFmpeg loudness")
	}
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	wd, _ := os.Getwd()
	if err := svc.ConfigureTTS("python3", filepath.Join(wd, "..", "..", "..", "workers", "python")); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "loud.avp"), Name: "Loud"})
	if err != nil {
		t.Fatal(err)
	}
	fixture, _ := filepath.Abs("../../../workers/node/timeline-cli/fixtures/media/clip_01.mp4")
	view, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, fixture)
	if err != nil {
		t.Fatal(err)
	}
	pid, rid := view.Document.Project.ID, view.Document.Resources[len(view.Document.Resources)-1].ID
	raw, err := svc.ResourceLoudness(ctx, pid, rid, 1)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Version       int     `json:"version"`
		StreamIndex   int     `json:"stream_index"`
		BucketSeconds float64 `json:"bucket_seconds"`
		Buckets       int     `json:"buckets"`
		Peak          string  `json:"peak"`
		RMS           string  `json:"rms"`
		Encoding      string  `json:"encoding"`
	}
	if err = json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	peak, _ := base64.StdEncoding.DecodeString(env.Peak)
	rms, _ := base64.StdEncoding.DecodeString(env.RMS)
	if env.Version != 2 || env.StreamIndex != 1 || env.Encoding != "u8-dbfs" || env.Buckets < 10 || len(peak) != env.Buckets || len(rms) != env.Buckets || env.BucketSeconds <= 0 {
		t.Fatalf("envelope %+v", env)
	}
	start := time.Now()
	again, err := svc.ResourceLoudness(ctx, pid, rid, 1)
	if err != nil || !jsonEqual(again, raw) || time.Since(start) > time.Second {
		t.Fatal("second request must come from cache", err, time.Since(start))
	}
	if _, err = svc.ResourceLoudness(ctx, pid, rid, 0); err == nil {
		t.Fatal("video stream must be rejected")
	}
	h := api.New(svc).Handler()
	for path, want := range map[string]int{
		"/api/projects/" + pid + "/resources/" + rid + "/loudness?stream=1": 200,
		"/api/projects/" + pid + "/resources/" + rid + "/loudness?stream=x": 400,
		"/api/projects/" + pid + "/resources/" + rid + "/loudness?stream=0": 400,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host, req.RemoteAddr = "127.0.0.1:8080", "127.0.0.1:50000"
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
