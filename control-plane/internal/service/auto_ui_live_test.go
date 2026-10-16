package service_test

// Live backend for the web UI's real-backend run (webui/auto_ui_live.mjs).
//
// It is not a test of its own: with VAC_AUTO_UI_DIST unset it skips. With it
// set, it builds the same offline world as the B4 end-to-end tests (real
// control plane, real Python/Node workers, loopback vision/narration/stepfun-TTS
// stub with a dummy token, simulated 720p renderer), serves the built web UI and
// the REST/SSE API from one local port, and drives the auto driver and the
// workers in a loop until told to stop. The browser script talks to that port
// and steers the model stub through /__live/stub.
//
//	VAC_AUTO_UI_DIST=<webui/dist> VAC_AUTO_UI_READY=<file> go test -run TestAutoUILive -count=1 -timeout 30m ./internal/service

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
)

func TestAutoUILive(t *testing.T) {
	dist := os.Getenv("VAC_AUTO_UI_DIST")
	if dist == "" {
		t.Skip("VAC_AUTO_UI_DIST is not set")
	}
	e := newE2EEnv(t)
	stop := make(chan struct{})
	delay := 800 * time.Millisecond // slow motion so the browser can see each state

	mux := http.NewServeMux()
	mux.Handle("/api/", api.New(e.svc).Handler())
	mux.HandleFunc("/__live/stub", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Conf    float64  `json:"conf"`
			Improve *float64 `json:"improve"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		e.stub.set(body.Conf, "团战画面", "这是一段精彩的团战画面")
		e.stub.mu.Lock()
		e.stub.confOnFeedback = body.Improve
		e.stub.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/__live/delay", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		delay = time.Duration(ms) * time.Millisecond
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/__live/stop", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		default:
			close(stop)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f := filepath.Join(dist, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if st, err := os.Stat(f); err != nil || st.IsDir() {
			f = filepath.Join(dist, "index.html")
		}
		http.ServeFile(w, r, f)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	if ready := os.Getenv("VAC_AUTO_UI_READY"); ready != "" {
		if err := os.WriteFile(ready, []byte("http://"+ln.Addr().String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The driver and the workers run here, on the test goroutine, because the
	// harness reports failures with t.Fatal.
	for {
		select {
		case <-stop:
			return
		default:
		}
		registerValidCapability(t, e.svc, "agent-exp-1")
		e.reportCapabilities()
		if err := e.svc.AutoTick(t.Context()); err != nil {
			t.Fatalf("AutoTick: %v", err)
		}
		q := e.queued()
		if len(q) == 0 {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		var runID string
		if err := e.st.DB().QueryRowContext(t.Context(), `SELECT run_id FROM workflow_runs WHERE asset_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, e.assetID).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		e.runID = runID
		e.runTask(q[0])
		time.Sleep(delay)
	}
}
