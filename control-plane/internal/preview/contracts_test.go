package preview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func fixtureInput(t *testing.T) Input {
	t.Helper()
	raw, err := os.ReadFile("../../../contracts/mp4-preview/v1/fixtures/input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Description string `json:"description"`
		Input       Input  `json:"input"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Input
}

func TestSharedFingerprintAndPreset(t *testing.T) {
	in := fixtureInput(t)
	got, err := Fingerprint(in)
	if err != nil || got != in.InputSHA256 {
		t.Fatalf("fingerprint %s, error %v", got, err)
	}
	presetBytes, err := os.ReadFile("../../../contracts/mp4-preview/v1/presets/mp4-720p-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(presetBytes)
	if hex.EncodeToString(sum[:]) != in.PresetSHA256 {
		t.Fatal("shared preset bytes changed without updating the fixture")
	}
	var want, actual any
	if err := json.Unmarshal(presetBytes, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(in.Preset, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, actual) {
		t.Fatal("preset snapshot differs from registry")
	}
}

func TestFingerprintIdentityAndWireGuards(t *testing.T) {
	in := fixtureInput(t)
	in.PreviewID = "retry"
	in.TaskID = "new-task"
	for i, j := 0, len(in.Files)-1; i < j; i, j = i+1, j-1 {
		in.Files[i], in.Files[j] = in.Files[j], in.Files[i]
	}
	if got, err := Fingerprint(in); err != nil || got != in.InputSHA256 {
		t.Fatal("ordering or attempt ID changed input identity")
	}
	in.RevisionID += "-changed"
	if got, err := Fingerprint(in); err != nil || got == in.InputSHA256 {
		t.Fatal("revision change did not change fingerprint")
	}
	for _, path := range []string{"/outside", "../outside", "a/../b", "a//b", "C:/outside", "a\\b", "a\x00b", "a/"} {
		bad := fixtureInput(t)
		bad.Files[0].Path = path
		if IsRelativePath(path) {
			t.Fatalf("unsafe path accepted: %q", path)
		}
		if _, err := Fingerprint(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	bad := fixtureInput(t)
	bad.Files = append(bad.Files, bad.Files[0])
	if _, err := Fingerprint(bad); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("duplicate file accepted")
	}
	for _, size := range []int64{-1, 9007199254740992} {
		bad := fixtureInput(t)
		bad.Files[0].Size = size
		if _, err := Fingerprint(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid size accepted")
		}
	}
}

func TestFrameworkPortNeverReportsSuccess(t *testing.T) {
	ctx := context.Background()
	port := Unimplemented{}
	_, start := port.Start(ctx, "human", "run", StartRequest{})
	_, list := port.List(ctx, "run")
	_, get := port.Get(ctx, "preview")
	_, cancel := port.Cancel(ctx, "human", "preview")
	_, input := port.TaskInput(ctx, "agent", "task")
	submit := port.Submit(ctx, "agent", "task", "dir")
	_, file := port.ResolveFile(ctx, "preview", "mp4")
	for _, err := range []error{start, list, get, cancel, input, submit, file} {
		if !errors.Is(err, ErrNotImplemented) {
			t.Fatalf("scaffold reported success: %v", err)
		}
	}
}
