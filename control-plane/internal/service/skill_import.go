package service

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type ProjectImportRequest struct {
	RootID          string `json:"root_id"`
	RelativePath    string `json:"relative_path"`
	ExpectedVersion int    `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

// ImportProjectMedia copies a single explicitly selected file from configured
// ingest roots and registers its full window; no ASR/segmentation is required.
func (s *Service) ImportProjectMedia(ctx context.Context, actor, id string, req ProjectImportRequest) (skill.Project, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return p, err
	}
	if err = requireHumanConfirm(actor, "import project media"); err != nil {
		return p, err
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 {
		return p, model.ErrArgument
	}
	hash := skillHash(req)
	scope := "project-import:" + id
	old, _, e := s.st.SkillRequest(ctx, scope, req.IdempotencyKey)
	if e == nil {
		if old != hash {
			return p, model.ErrConflict
		}
		return p, nil
	}
	if p.Version != req.ExpectedVersion {
		return p, model.ErrConflict
	}
	root, err := ingest.FindRoot(s.ingest.roots, req.RootID)
	if err != nil {
		return p, err
	}
	source, err := ingest.ResolveSource(root, req.RelativePath)
	if err != nil {
		return p, err
	}
	if source.Size <= 0 || source.Size > s.ingest.policy.MaxSourceBytes {
		return p, model.ErrArgument
	}
	dir, err := s.harnessDirectory(filepath.Join("projects", id, "media", newID("import")))
	if err != nil {
		return p, err
	}
	name := "source" + filepath.Ext(source.Path)
	dest := filepath.Join(dir, name)
	from, err := os.Open(source.Path)
	if err != nil {
		return p, err
	}
	defer from.Close()
	to, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return p, err
	}
	size, copyErr := io.Copy(to, io.LimitReader(from, s.ingest.policy.MaxSourceBytes+1))
	closeErr := to.Close()
	if copyErr != nil {
		return p, copyErr
	}
	if closeErr != nil {
		return p, closeErr
	}
	if size != source.Size {
		return p, model.ErrConflict
	}
	after, err := ingest.ResolveSource(root, req.RelativePath)
	if err != nil || after.Size != source.Size || after.MTimeNs != source.MTimeNs {
		return p, model.ErrConflict
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "ffprobe", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpegvideo,flv,ogg", "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", dest)
	hideTTSCommand(cmd)
	var out scriptOutput
	cmd.Stdout = &out
	if err = cmd.Run(); err != nil {
		return p, model.ErrInvalidState
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type string `json:"codec_type"`
		} `json:"streams"`
	}
	if json.Unmarshal(out.Bytes(), &probe) != nil {
		return p, model.ErrInvalidState
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || !finiteScriptTime(duration) || duration <= 0 {
		return p, model.ErrArgument
	}
	video := false
	for _, stream := range probe.Streams {
		if stream.Type == "video" {
			video = true
		}
	}
	if !video {
		return p, model.ErrArgument
	}
	edl := map[string]any{"version": 1, "timeline": map[string]any{"fps": 30, "sample_rate": 48000}, "video": []any{map[string]any{"id": "v1", "src": name, "in": 0, "out": duration, "start": 0}}, "voice": []any{}, "music": []any{}, "narration": []any{}, "subtitle": []any{}}
	raw, _ := json.Marshal(edl)
	if err = os.WriteFile(filepath.Join(dir, "edl.json"), raw, 0600); err != nil {
		return p, err
	}
	rel, _ := filepath.Rel(s.deliveryRoot, filepath.Join(dir, "edl.json"))
	assetID := newID("media")
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		var cur skill.Project
		if e := tx.SkillGet(ctx, "project", id, &cur); e != nil {
			return e
		}
		if cur.Version != req.ExpectedVersion {
			return model.ErrConflict
		}
		if e := tx.CreateAsset(ctx, model.Asset{AssetID: assetID, Status: model.AssetStatusIngested, InputKind: model.InputKindEDLPackage, AgentVisible: true, AllowedAgents: []string{}, Artifacts: map[string]string{"edl": filepath.ToSlash(rel)}}); e != nil {
			return e
		}
		cur.Assets = append(cur.Assets, assetID)
		cur.Version++
		cur.ConfirmedSHA = ""
		cur.ConfirmedBy = ""
		cur.UpdatedAt = s.autoNow()
		p = cur
		if e := tx.SkillPut(ctx, "project", id, req.ExpectedVersion, cur); e != nil {
			return e
		}
		if e := tx.SkillRemember(ctx, scope, req.IdempotencyKey, hash, assetID); e != nil {
			return e
		}
		return auditTx(ctx, tx, actor, "project.media.import", id, assetID)
	})
	if err == nil {
		s.publish("project.changed", map[string]any{"project_id": id})
		err = s.mirrorProject(p)
	}
	return p, err
}
