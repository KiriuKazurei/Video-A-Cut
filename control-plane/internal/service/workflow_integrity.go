package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"math"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) readRevision(rev model.Revision) (map[string]any, error) {
	edl, sum, err := s.readPackageEDL(rev.PackageRef)
	if err != nil {
		return nil, err
	}
	ev, err := evidenceHash(s.deliveryRoot, rev.PackageRef)
	if err != nil {
		return nil, err
	}
	if sum != rev.EDLSHA256 || ev != rev.EvidenceManifestSHA256 {
		return nil, fmt.Errorf("revision content changed: %w", model.ErrConflict)
	}
	if len(asMapSlice(edl["scenes"])) > 0 {
		if _, err := s.evidenceFiles(rev.PackageRef, edl); err != nil {
			return nil, err
		}
	}
	return edl, nil
}

// EvidenceFiles validates bytes before exposing an immutable revision's images.
func (s *Service) evidenceFiles(pkg string, edl map[string]any) ([]map[string]string, error) {
	manifest := pkg + "/samples/evidence-manifest.json"
	file, _, err := s.deliveryPath(filepath.FromSlash(manifest), false)
	if err != nil {
		if len(asMapSlice(edl["scenes"])) == 0 {
			return []map[string]string{}, nil
		}
		return nil, fmt.Errorf("scene evidence manifest missing: %w", model.ErrInvalidState)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var data struct {
		Frames []struct {
			Path      string  `json:"path"`
			Hash      string  `json:"sha256"`
			Timestamp float64 `json:"timestamp"`
			Clip      int     `json:"clip_index"`
		}
	}
	if json.Unmarshal(raw, &data) != nil || len(data.Frames) == 0 || len(data.Frames) > 48 {
		return nil, fmt.Errorf("invalid evidence manifest: %w", model.ErrArgument)
	}
	known := map[string]map[string]string{}
	for _, f := range data.Frames {
		if !strings.HasPrefix(f.Path, "samples/") || len(f.Hash) != 64 {
			return nil, fmt.Errorf("invalid frame: %w", model.ErrArgument)
		}
		full, info, err := s.deliveryPath(filepath.FromSlash(pkg+"/"+f.Path), false)
		if err != nil {
			return nil, err
		}
		base, _, err := s.deliveryPath(filepath.FromSlash(pkg), true)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(base, full)
		if err != nil || !filepath.IsLocal(rel) || info.Size() > 10*1024*1024 {
			return nil, fmt.Errorf("frame escapes package or exceeds size: %w", model.ErrForbidden)
		}
		digest, err := s.hashUnderRoot(pkg + "/" + f.Path)
		if err != nil {
			return nil, err
		}
		if digest != f.Hash {
			return nil, fmt.Errorf("frame hash mismatch: %w", model.ErrConflict)
		}
		row := map[string]string{"sha256": f.Hash, "key": f.Hash, "path": f.Path, "timestamp": fmt.Sprint(f.Timestamp), "clip_index": fmt.Sprint(f.Clip)}
		known[f.Hash] = row
		known[f.Path] = row
	}
	out := []map[string]string{}
	for _, scene := range asMapSlice(edl["scenes"]) {
		if id, ok := scene["scene_id"].(string); !ok || id == "" {
			return nil, fmt.Errorf("scene identity missing: %w", model.ErrArgument)
		}
		refs := stringList(scene["evidence_frames"])
		if len(refs) == 0 {
			return nil, fmt.Errorf("scene has no evidence: %w", model.ErrInvalidState)
		}
		for _, ref := range refs {
			row, ok := known[ref]
			if !ok {
				return nil, fmt.Errorf("unknown scene evidence: %w", model.ErrArgument)
			}
			copy := map[string]string{}
			for k, v := range row {
				copy[k] = v
			}
			copy["scene_id"], _ = scene["scene_id"].(string)
			out = append(out, copy)
		}
	}
	return out, nil
}

func validateWorkflowEdit(edl map[string]any) error {
	scenes := asMapSlice(edl["scenes"])
	byID := map[string]map[string]any{}
	ranks := map[float64]bool{}
	ranked := 0
	for _, scene := range scenes {
		id, _ := scene["scene_id"].(string)
		label, _ := scene["label"].(string)
		if id == "" || byID[id] != nil || !printableLimited(label, 1, 100) {
			return fmt.Errorf("invalid scene identity or label: %w", model.ErrArgument)
		}
		byID[id] = scene
		if rank, ok := scene["sequence_rank"]; ok && rank != nil {
			n, valid := number(rank)
			if !valid {
				if i, ok := rank.(int); ok {
					n = float64(i)
					valid = true
				}
			}
			if !valid || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || math.Trunc(n) != n || ranks[n] {
				return fmt.Errorf("invalid or conflicting scene rank: %w", model.ErrArgument)
			}
			ranks[n] = true
			ranked++
		}
	}
	if ranked != 0 && ranked != len(scenes) {
		return fmt.Errorf("partial scene ranking: %w", model.ErrArgument)
	}
	video := asMapSlice(edl["video"])
	for _, line := range asMapSlice(edl["narration"]) {
		start, a := number(line["start"])
		end, b := number(line["end"])
		text, _ := line["text"].(string)
		if !a || !b {
			return fmt.Errorf("invalid narration time: %w", model.ErrArgument)
		}
		if err := validateNarrationDraft(NarrationDraft{Text: text, Start: start, End: end, Source: narrationSource(line)}); err != nil {
			return err
		}
		id, _ := line["source_scene_id"].(string)
		scene := byID[id]
		if scene == nil {
			return fmt.Errorf("narration source scene missing: %w", model.ErrArgument)
		}
		index, ok := number(scene["index"])
		if !ok || int(index) < 0 || int(index) >= len(video) {
			return fmt.Errorf("scene clip missing: %w", model.ErrArgument)
		}
		clip := video[int(index)]
		tin, a := number(clip["timeline_in"])
		cin, b := number(clip["in"])
		cout, c := number(clip["out"])
		if !a || !b || !c || start < tin || end > tin+cout-cin {
			return fmt.Errorf("narration escapes source clip: %w", model.ErrArgument)
		}
	}
	return nil
}

func (s *Service) publishTaskWorkflow(ctx context.Context, taskID string) {
	stg, err := s.st.StageByTask(ctx, taskID)
	if err != nil {
		return
	}
	run, err := s.st.GetWorkflow(ctx, stg.RunID)
	if err == nil {
		s.publish("workflow.changed", run)
	}
}

func auditTx(ctx context.Context, tx *store.Store, actor, action, target, detail string) error {
	return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: action, Target: target, Detail: detail})
}
