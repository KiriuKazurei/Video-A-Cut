package service_test

import (
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestNarrationDraftHashMatchesWorkerVector(t *testing.T) {
	got := service.NarrationDraftHash("已审批解说", 0.2, 1.0, "Boss")
	const want = "3c72592ba78722843b6d43d428e54940e47d7e82b3006344bf0b415c31a7a8e3"
	if got != want {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}

func TestApproveNarrationBindsTheDraftAndRejectsAChange(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusNarrated}); err != nil {
		t.Fatal(err)
	}
	draft := service.NarrationDraft{Text: "已审批解说", Start: 0.2, End: 1, Source: "Boss"}
	hash, err := svc.ApproveNarration(ctx, "human:webui", "clip", draft)
	if err != nil {
		t.Fatal(err)
	}
	if hash != service.NarrationDraftHash(draft.Text, draft.Start, draft.End, draft.Source) {
		t.Fatalf("hash = %s", hash)
	}
	again, err := svc.ApproveNarration(ctx, "human:webui", "clip", draft)
	if err != nil || again != hash {
		t.Fatalf("repeat approve = %s, %v", again, err)
	}
	rows, err := svc.ListAudit(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	approvals := 0
	for _, row := range rows {
		if row.Action == "narration.approve" && row.Target == "clip" {
			approvals++
			if row.Actor != "human:webui" || row.Detail != "draft_hash="+hash {
				t.Fatalf("audit row = %+v", row)
			}
		}
	}
	if approvals != 1 {
		t.Fatalf("approve audit rows = %d, want 1", approvals)
	}
	hashes, err := svc.ListNarrationApprovals(ctx, "clip")
	if err != nil || len(hashes) != 1 || hashes[0] != hash {
		t.Fatalf("approvals = %v, %v", hashes, err)
	}
	moved := draft
	moved.End = 1.5
	movedHash, err := svc.ApproveNarration(ctx, "human:webui", "clip", moved)
	if err != nil {
		t.Fatal(err)
	}
	if movedHash == hash {
		t.Fatal("a changed window reused the old hash")
	}
	if err := svc.RevokeNarration(ctx, "human:webui", "clip", hash); err != nil {
		t.Fatal(err)
	}
	left, err := svc.ListNarrationApprovals(ctx, "clip")
	if err != nil || len(left) != 1 || left[0] != movedHash {
		t.Fatalf("after revoke = %v, %v", left, err)
	}
}

func TestApproveNarrationRejectsEmptyActorAndMissingAsset(t *testing.T) {
	svc := newService(t)
	draft := service.NarrationDraft{Text: "解说", Start: 0, End: 1, Source: ""}
	if _, err := svc.ApproveNarration(t.Context(), "", "clip", draft); err == nil {
		t.Fatal("empty actor was accepted")
	}
	if _, err := svc.ApproveNarration(t.Context(), "human:webui", "missing", draft); err == nil {
		t.Fatal("missing asset was accepted")
	}
}
