package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"sort"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

// Removed media with editing references remains reachable as a missing
// placeholder. Compute this view flag from the current workbenches and run
// timelines, rather than making an import's own bridge EDL count as a use.
func (s *Service) markRemovedResourceReferences(ctx context.Context, doc *project.AvpDocument) error {
	if doc == nil {
		return nil
	}
	removed := map[int][]byte{}
	for i, resource := range doc.Resources {
		if resource.Removed {
			raw, _ := json.Marshal(resource.ID)
			removed[i] = raw
		}
	}
	if len(removed) == 0 {
		return nil
	}
	mark := func(raw []byte) {
		for i, id := range removed {
			if bytes.Contains(raw, id) {
				doc.Resources[i].Referenced = true
			}
		}
	}
	for _, script := range doc.Scripts {
		raw, _ := json.Marshal(script)
		mark(raw)
	}
	rows, err := s.st.DB().QueryContext(ctx, `SELECT document_json, 1 FROM script_workbenches UNION ALL SELECT body, 0 FROM vac_skill_runs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var script int
		if err = rows.Scan(&raw, &script); err != nil {
			return err
		}
		if script == 1 {
			var workbench struct {
				Segments json.RawMessage `json:"segments"`
			}
			if err = json.Unmarshal(raw, &workbench); err != nil {
				return err
			}
			raw = workbench.Segments
		}
		mark(raw)
	}
	return rows.Err()
}

func (s *Service) scriptSourceFingerprint(ctx context.Context, w ScriptWorkbench) (string, error) {
	ids := map[string]bool{}
	for _, segment := range w.Segments {
		if segment.MediaID != "" {
			ids[segment.MediaID] = true
		}
	}
	keys := []string{}
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	deps := []map[string]string{}
	for _, id := range keys {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r, err := s.ProjectResource(s.CurrentProjectID(), id)
		if err != nil {
			return "", err
		}
		if r.Removed || r.Missing {
			return "", model.ErrConflict
		}
		file, err := os.Open(r.Source.OriginalPath)
		if err != nil {
			return "", err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return "", err
		}
		deps = append(deps, map[string]string{"id": id, "sha256": hex.EncodeToString(hash.Sum(nil))})
	}
	return skillHash(deps), nil
}

func (s *Service) checkScriptSources(ctx context.Context, w ScriptWorkbench) error {
	if !s.WorkspaceEnabled() || w.SourceFingerprint == "" {
		return nil
	}
	fingerprint, err := s.scriptSourceFingerprint(ctx, w)
	if err != nil {
		return err
	}
	if fingerprint != w.SourceFingerprint {
		return model.ErrConflict
	}
	return nil
}

func (s *Service) skillRunUsesResource(ctx context.Context, run skill.Run, id string) bool {
	var snap harnessSnapshot
	if s.st.SkillSnapshot(ctx, run.ID, &snap) != nil {
		return false
	}
	for _, media := range snap.Media {
		if media.ID == id {
			return true
		}
	}
	return false
}

func scriptContentFingerprint(w ScriptWorkbench) string {
	return skillHash(map[string]any{"body": w.Script.Body, "segments": w.Segments, "tts": w.TTSEnabled, "subtitles": w.SubtitlesEnabled, "research": w.AllowWebResearch})
}
