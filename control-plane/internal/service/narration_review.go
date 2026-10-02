package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// NarrationDraft is the reviewable text the human approved. The hash covers
// the text, the time window and the source label. A later edit of any of
// those fields produces a different hash, so the old approval no longer matches.
type NarrationDraft struct {
	Text   string
	Start  float64
	End    float64
	Source string
}

// NarrationDraftHash is the canonical fingerprint shared with the Python worker.
//
// The bytes are UTF-8 of text, start, end and source, each separated by a
// newline. Times are formatted with six digits after the decimal point.
func NarrationDraftHash(text string, start, end float64, source string) string {
	canonical := fmt.Sprintf("%s\n%.6f\n%.6f\n%s", text, start, end, source)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// ApproveNarration records a human approval of one draft. It is a governance
// write: REST only, attributed to the actor, and audited. Repeating the same
// draft is a no-op and does not write a second audit row.
func (s *Service) ApproveNarration(ctx context.Context, actor, assetID string, draft NarrationDraft) (string, error) {
	if actor == "" {
		return "", fmt.Errorf("service: approve narration %s: actor is required: %w", assetID, model.ErrArgument)
	}
	if err := validateNarrationDraft(draft); err != nil {
		return "", fmt.Errorf("service: approve narration %s: %w", assetID, err)
	}
	if hash, bound, err := s.BindLegacyNarrationApproval(ctx, actor, assetID, draft); bound || err != nil {
		return hash, err
	}
	if _, err := s.st.GetAsset(ctx, assetID); err != nil {
		return "", fmt.Errorf("service: approve narration %s: %w", assetID, err)
	}
	hash := NarrationDraftHash(draft.Text, draft.Start, draft.End, draft.Source)
	created, err := s.st.PutNarrationApproval(ctx, store.NarrationApproval{
		AssetID: assetID, DraftHash: hash, Text: draft.Text,
		Start: draft.Start, End: draft.End, Source: draft.Source,
	})
	if err != nil {
		return "", err
	}
	if created {
		s.audit(ctx, actor, "narration.approve", assetID, "draft_hash="+hash)
	}
	return hash, nil
}

// RevokeNarration removes one previously recorded draft approval.
func (s *Service) RevokeNarration(ctx context.Context, actor, assetID, draftHash string) error {
	if actor == "" {
		return fmt.Errorf("service: revoke narration %s: actor is required: %w", assetID, model.ErrArgument)
	}
	if len(draftHash) != 64 || !isHex(draftHash) {
		return fmt.Errorf("service: revoke narration %s: draft hash is invalid: %w", assetID, model.ErrArgument)
	}
	if _, err := s.st.GetAsset(ctx, assetID); err != nil {
		return fmt.Errorf("service: revoke narration %s: %w", assetID, err)
	}
	run, workflowErr := s.st.ActiveWorkflow(ctx, assetID)
	if workflowErr == nil {
		rev, e := s.st.GetRevision(ctx, run.CurrentRevisionID)
		if e != nil {
			return e
		}
		edl, e := s.readRevision(rev)
		if e != nil {
			return e
		}
		for _, line := range asMapSlice(edl["narration"]) {
			start, _ := number(line["start"])
			end, _ := number(line["end"])
			text, _ := line["text"].(string)
			if NarrationDraftHash(text, start, end, narrationSource(line)) == draftHash {
				id, _ := line["id"].(string)
				_, e = s.RevokeWorkflowNarration(ctx, actor, run.RunID, id, run.Version)
				return e
			}
		}
		return fmt.Errorf("draft does not match workflow: %w", model.ErrConflict)
	}
	if !errors.Is(workflowErr, model.ErrNotFound) {
		return workflowErr
	}
	history, err := s.st.ListWorkflows(ctx, assetID, 1)
	if err != nil {
		return err
	}
	if len(history) != 0 {
		return fmt.Errorf("use versioned workflow review for this asset: %w", model.ErrConflict)
	}
	deleted, err := s.st.DeleteNarrationApproval(ctx, assetID, draftHash)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("service: revoke narration %s: %w", assetID, model.ErrNotFound)
	}
	s.audit(ctx, actor, "narration.revoke", assetID, "draft_hash="+draftHash)
	return nil
}

// ListNarrationApprovals returns the draft hashes a worker may treat as
// reviewed. The list is a read of governance state, not a permission to
// change it.
func (s *Service) ListNarrationApprovals(ctx context.Context, assetID string) ([]string, error) {
	hashes, err := s.st.ListNarrationApprovals(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if hashes == nil {
		return []string{}, nil
	}
	return hashes, nil
}

func validateNarrationDraft(draft NarrationDraft) error {
	if !printableLimited(draft.Text, 1, 160) {
		return fmt.Errorf("narration text is empty or too long: %w", model.ErrArgument)
	}
	if utf8.RuneCountInString(draft.Source) > 100 || (draft.Source != "" && !printableLimited(draft.Source, 1, 100)) {
		return fmt.Errorf("narration source is invalid: %w", model.ErrArgument)
	}
	if math.IsNaN(draft.Start) || math.IsInf(draft.Start, 0) || math.IsNaN(draft.End) || math.IsInf(draft.End, 0) || !(draft.Start < draft.End) {
		return fmt.Errorf("narration window is invalid: %w", model.ErrArgument)
	}
	return nil
}

func printableLimited(text string, minRunes, maxRunes int) bool {
	n := utf8.RuneCountInString(text)
	if n < minRunes || n > maxRunes {
		return false
	}
	for _, r := range text {
		if r < 32 || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
