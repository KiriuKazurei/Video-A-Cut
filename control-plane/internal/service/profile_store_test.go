package service_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func builtinProfile(revision int) preparation.Profile {
	return preparation.Profile{
		SchemaVersion: 1, ProfileID: "local_regression", Revision: revision, Name: "本地回归",
		ContentMode: "builtin", Vision: preparation.Provider{Adapter: "builtin"},
		Narration: preparation.Provider{Adapter: "builtin"},
		Sampling:  preparation.Sampling{MaxFrames: 12, MaxBytes: 12 * 1024 * 1024, TimeoutSeconds: 60},
		TTSVoice:  "Microsoft Huihui Desktop", ExportTarget: "premiere",
	}
}

func TestProcessingProfileRevisionsAreImmutable(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	first, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem-1", 0, builtinProfile(1))
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem-1", 0, builtinProfile(1))
	if err != nil || again.Revision != first.Revision {
		t.Fatalf("repeat = %+v, %v", again, err)
	}
	if _, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem-2", 0, builtinProfile(1)); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same key new content or duplicate create err = %v", err)
	}
	second := builtinProfile(2)
	second.Name = "本地回归二"
	saved, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem-3", 1, second)
	if err != nil {
		t.Fatal(err)
	}
	old, oldSum, err := svc.GetProcessingProfile(ctx, "local_regression", 1)
	if err != nil || old.Name != "本地回归" {
		t.Fatalf("old revision changed: %+v, %v", old, err)
	}
	current, currentSum, err := svc.GetProcessingProfile(ctx, "local_regression", 0)
	if err != nil || current.Revision != saved.Revision || current.Name != "本地回归二" || oldSum == currentSum {
		t.Fatalf("current = %+v sums %s %s err %v", current, oldSum, currentSum, err)
	}
	rows, err := svc.ListAudit(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, row := range rows {
		if row.Action == "profile.revise" && row.Target == "local_regression" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("profile audit rows = %d, want 2", found)
	}
	other := builtinProfile(1)
	other.Name = "其他请求"
	if _, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem-1", 0, other); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same idempotency key with different content err = %v", err)
	}
}

func TestProfileSaveRollsBackWhenReceiptOrAuditFails(t *testing.T) {
	for _, table := range []string{"idempotency_keys", "audit_logs"} {
		t.Run(table, func(t *testing.T) {
			svc := newService(t)
			db := storeOf(t, svc).DB()
			trigger := "fail_" + table
			if _, err := db.Exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if _, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem", 0, builtinProfile(1)); err == nil {
				t.Fatal("injected failure was ignored")
			}
			if _, _, err := svc.GetProcessingProfile(ctx, "local_regression", 1); !errors.Is(err, model.ErrNotFound) {
				t.Fatalf("rolled back profile err = %v", err)
			}
			if _, err := db.Exec(`DROP TRIGGER ` + trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem", 0, builtinProfile(1)); err != nil {
				t.Fatal(err)
			}
			again, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem", 0, builtinProfile(1))
			if err != nil || again.Revision != 1 || again.Name != "本地回归" {
				t.Fatalf("retry = %+v, %v", again, err)
			}
		})
	}
}

func TestGetProcessingProfileRejectsTamperedRows(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	if _, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem", 0, builtinProfile(1)); err != nil {
		t.Fatal(err)
	}
	db := storeOf(t, svc).DB()
	if _, err := db.Exec(`UPDATE processing_profile_revisions SET canonical_json = replace(canonical_json, '本地回归', '已篡改') WHERE profile_id = 'local_regression'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.GetProcessingProfile(ctx, "local_regression", 1); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("tampered json err = %v", err)
	}
	bad := `{"schema_version":1,"profile_id":"other","revision":1}`
	sum := sha256.Sum256([]byte(bad))
	if _, err := db.Exec(`UPDATE processing_profile_revisions SET canonical_json = ?, sha256 = ? WHERE profile_id = 'local_regression'`, bad, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.GetProcessingProfile(ctx, "local_regression", 1); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("identity mismatch err = %v", err)
	}
	if _, err := db.Exec(`UPDATE idempotency_keys SET response_json = ? WHERE idem_key = 'idem'`, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveProcessingProfile(ctx, "human:webui", "idem", 0, builtinProfile(1)); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("tampered idempotency cache err = %v", err)
	}
}

func TestConcurrentProfileSaveWithSameKeyReturnsOneRevision(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	got := make([]preparation.Profile, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = svc.SaveProcessingProfile(ctx, "human:webui", "same", 0, builtinProfile(1))
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil || got[i].Revision != 1 {
			t.Fatalf("save %d = %+v, %v", i, got[i], errs[i])
		}
	}
	current, _, err := svc.GetProcessingProfile(ctx, "local_regression", 0)
	if err != nil || current.Revision != 1 {
		t.Fatalf("current = %+v, %v", current, err)
	}
}

func storeOf(t *testing.T, svc *service.Service) *store.Store {
	t.Helper()
	value, ok := storesByService.Load(svc)
	if !ok {
		t.Fatal("store was not registered")
	}
	return value.(*store.Store)
}
