package preparation

import (
	"context"
	"testing"
	"time"
)

func TestLocalProbeDoesNotAuthorizeLaunch(t *testing.T) {
	probe := NewLocalProbe()
	checks, err := probe.Probe(t.Context(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Code != "ffmpeg" || checks[1].Code != "ffprobe" {
		t.Fatalf("checks = %+v", checks)
	}
	for _, check := range checks {
		if check.Status != "passed" && check.Status != "blocked" {
			t.Fatalf("status = %s", check.Status)
		}
		if check.Message == "" || len(check.Message) > 80 {
			t.Fatalf("message leaked or empty: %q", check.Message)
		}
	}
	report, err := (Engine{Probe: probe}).Check(t.Context(), fixture())
	if err != nil || report.CanStart || report.Status != "blocked" {
		t.Fatalf("report = %+v, %v", report, err)
	}
}

func TestLocalProbeTimesOutWithoutRunningAModel(t *testing.T) {
	probe := LocalProbe{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Timeout: time.Nanosecond}
	checks, err := probe.Probe(t.Context(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Status != "blocked" {
			t.Fatalf("%s was not blocked on a tiny timeout: %+v", check.Code, check)
		}
	}
}

func TestLocalProbeMissingToolIsBlocked(t *testing.T) {
	probe := LocalProbe{FFmpeg: "vac-missing-ffmpeg", FFprobe: "vac-missing-ffprobe", Timeout: time.Second}
	checks, err := probe.Probe(context.Background(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Status != "blocked" || check.Message == "" {
			t.Fatalf("%+v", check)
		}
	}
}
