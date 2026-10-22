package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

type ScriptMedia struct {
	Kind    string  `json:"kind,omitempty"`
	ID      string  `json:"id"`
	AssetID string  `json:"asset_id"`
	Name    string  `json:"name"`
	Source  string  `json:"-"`
	Path    string  `json:"-"`
	In      float64 `json:"in"`
	Out     float64 `json:"out"`
}
type ScriptSource struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt,omitempty"`
}
type ScriptSegment struct {
	Index            int          `json:"index"`
	Narration        string       `json:"narration"`
	MediaID          string       `json:"media_id"`
	Start            float64      `json:"start"`
	End              float64      `json:"end"`
	SourceURLs       []string     `json:"source_urls,omitempty"`
	TTSEnabled       *bool        `json:"tts_enabled,omitempty"`
	SubtitlesEnabled *bool        `json:"subtitles_enabled,omitempty"`
	Voice            *ScriptVoice `json:"voice,omitempty"`
}
type ScriptVoice struct {
	Settings       tts.Settings `json:"settings"`
	OffsetSeconds  float64      `json:"offset_seconds"`
	AcceptExternal bool         `json:"accept_external"`
}

func scriptOption(override *bool, fallback bool) bool {
	if override != nil {
		return *override
	}
	return fallback
}

type ScriptWorkbench struct {
	References        []project.Reference   `json:"references"`
	SourceFingerprint string                `json:"source_fingerprint,omitempty"`
	Script            model.Script          `json:"script"`
	Version           int                   `json:"version"`
	Document          scriptsyntax.Document `json:"document"`
	Segments          []ScriptSegment       `json:"segments"`
	Sources           []ScriptSource        `json:"sources"`
	Questions         []string              `json:"questions"`
	TTSEnabled        bool                  `json:"tts_enabled"`
	SubtitlesEnabled  bool                  `json:"subtitles_enabled"`
	AllowWebResearch  bool                  `json:"allow_web_research"`
	Rejected          bool                  `json:"rejected"`
	ProfileID         string                `json:"profile_id,omitempty"`
	ProfileRevision   int                   `json:"profile_revision,omitempty"`
	Delivery          *store.ScriptDelivery `json:"delivery,omitempty"`
}
type SaveScriptRequest struct {
	References       []project.Reference `json:"references,omitempty"`
	SequenceID       string              `json:"sequence_id,omitempty"`
	ExpectedVersion  int                 `json:"expected_version"`
	VisibleSetSHA    string              `json:"visible_set_sha"`
	Title            string              `json:"title"`
	Body             string              `json:"body"`
	Segments         []ScriptSegment     `json:"segments,omitempty"`
	TTSEnabled       bool                `json:"tts_enabled"`
	SubtitlesEnabled bool                `json:"subtitles_enabled"`
	AllowWebResearch bool                `json:"allow_web_research"`
}

func workbenchAt(ctx context.Context, st *store.Store, id string) (ScriptWorkbench, error) {
	sc, err := st.GetScript(ctx, id)
	if err != nil {
		return ScriptWorkbench{}, err
	}
	v, raw, err := st.GetScriptWorkbench(ctx, id)
	if err != nil {
		return ScriptWorkbench{}, err
	}
	w := ScriptWorkbench{Segments: []ScriptSegment{}, Sources: []ScriptSource{}, Questions: []string{}, AllowWebResearch: true}
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &w); err != nil {
			return w, err
		}
	}
	w.Script = sc
	w.Version = v
	w.Document = scriptsyntax.Parse(sc.Body)
	return w, nil
}
func (s *Service) GetScriptWorkbench(ctx context.Context, id string) (ScriptWorkbench, error) {
	var w ScriptWorkbench
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		var err error
		w, err = workbenchAt(ctx, tx, id)
		if err != nil {
			return err
		}
		w.Delivery = nil
		if w.Script.Status == model.ScriptFinalized {
			d, err := tx.GetScriptDelivery(ctx, id, w.Version)
			if err == nil {
				w.Delivery = &d
			} else if !isNotFound(err) {
				return err
			}
		}
		return nil
	})
	if err == nil && s.WorkspaceEnabled() && w.Script.Status == model.ScriptFinalized {
		if sourceErr := s.checkScriptSources(ctx, w); sourceErr != nil {
			w.Script.Status = model.ScriptNeedsRefinalize
			w.Script.StaleReason = "source_changed"
			w.Delivery = nil
		}
	}
	return w, err
}
func (s *Service) ScriptRevisions(ctx context.Context, id string) ([]store.ScriptRevision, error) {
	if _, err := s.st.GetScript(ctx, id); err != nil {
		return nil, err
	}
	return s.st.ListScriptRevisions(ctx, id)
}

// ScriptMediaCatalogue is derived only from confirmed, currently visible EDLs.
// Absolute paths stay inside the worker request, never in this public read model.
func (s *Service) ScriptMediaCatalogue(ctx context.Context) ([]ScriptMedia, error) {
	var assets []model.Asset
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		view, err := s.visibleSetAt(ctx, tx)
		if err != nil {
			return err
		}
		if !view.Confirmed {
			return model.ErrForbidden
		}
		set, err := loadVisibleSet(ctx, tx)
		if err != nil {
			return err
		}
		for _, id := range set.IDs {
			a, err := tx.GetAsset(ctx, id)
			if err != nil {
				return err
			}
			assets = append(assets, a)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	media, err := s.scriptMediaFromAssets(assets)
	if err != nil {
		return nil, err
	}
	if s.WorkspaceEnabled() {
		filtered := []ScriptMedia{}
		for _, item := range media {
			if item.Kind != "audio" {
				filtered = append(filtered, item)
			}
		}
		media = filtered
	}
	return media, nil
}

func (s *Service) scriptMediaFromAssets(assets []model.Asset) ([]ScriptMedia, error) {
	out := []ScriptMedia{}
	seen := map[string]bool{}
	for _, a := range assets {
		if resource, ok := s.registeredResource(a.AssetID); ok {
			if (resource.Kind == "video" || resource.Kind == "audio") && !resource.Removed && !resource.Missing {
				duration, _ := resource.Metadata["duration"].(float64)
				out = append(out, ScriptMedia{Kind: resource.Kind, ID: resource.ID, AssetID: a.AssetID, Name: resource.Name, Source: "@resource/" + resource.ID, Path: resource.Source.OriginalPath, In: 0, Out: duration})
			}
			continue
		}
		if a.Locked || a.Artifacts["edl"] == "" {
			continue
		}
		edlPath, info, err := s.deliveryPath(filepath.FromSlash(a.Artifacts["edl"]), false)
		if err != nil || info.Size() > maxAgentEDLBytes {
			continue
		}
		raw, err := os.ReadFile(edlPath)
		if err != nil {
			return nil, err
		}
		var edl struct {
			Video []struct {
				Src string  `json:"src"`
				In  float64 `json:"in"`
				Out float64 `json:"out"`
			} `json:"video"`
		}
		if json.Unmarshal(raw, &edl) != nil {
			continue
		}
		for _, v := range edl.Video {
			if v.Src == "" || !finiteScriptTime(v.In) || !finiteScriptTime(v.Out) || v.In < 0 || v.Out <= v.In {
				continue
			}
			abs := filepath.Join(filepath.Dir(edlPath), filepath.FromSlash(v.Src))
			if filepath.IsAbs(v.Src) {
				abs = v.Src
			}
			rel, err := filepath.Rel(s.deliveryRoot, abs)
			if err != nil || !filepath.IsLocal(rel) {
				continue
			}
			path, _, err := s.deliveryPath(rel, false)
			if err != nil {
				continue
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%.17g\n%.17g", a.AssetID, filepath.ToSlash(rel), v.In, v.Out)))
			id := hex.EncodeToString(sum[:12])
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, ScriptMedia{ID: id, AssetID: a.AssetID, Name: filepath.Base(v.Src), Source: filepath.ToSlash(rel), Path: path, In: v.In, Out: v.Out})
			if len(out) > 200 {
				return nil, fmt.Errorf("请将可见素材收窄到 200 个视频以内: %w", model.ErrArgument)
			}
		}
	}
	names := map[string]int{}
	for _, m := range out {
		names[m.Name]++
	}
	for i := range out {
		if names[out[i].Name] > 1 {
			out[i].Name = fmt.Sprintf("%s [%s]", out[i].Name, out[i].ID)
		}
	}
	return out, nil
}
func finiteScriptTime(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func validateScriptPlan(w ScriptWorkbench, media []ScriptMedia, requireGenerated bool) error {
	if len(w.Document.Diagnostics) > 0 || len(w.Document.Paragraphs) == 0 {
		return fmt.Errorf("请先修正脚本语法: %w", model.ErrArgument)
	}
	byID := map[string]ScriptMedia{}
	byName := map[string][]ScriptMedia{}
	for _, m := range media {
		byID[m.ID] = m
		byName[m.Name] = append(byName[m.Name], m)
	}
	for _, p := range w.Document.Paragraphs {
		if len(w.References) == 0 && p.Video != "" && len(byName[p.Video]) != 1 {
			return fmt.Errorf("视频 %q 不在可见素材中或名称重复: %w", p.Video, model.ErrArgument)
		}
	}
	if len(w.Segments) == 0 && !requireGenerated {
		return nil
	}
	if (requireGenerated && (w.Rejected || len(w.Questions) > 0)) || len(w.Segments) != len(w.Document.Paragraphs) {
		return fmt.Errorf("文稿尚未生成完整，或仍有待澄清内容: %w", model.ErrInvalidState)
	}
	urls := map[string]bool{}
	for _, src := range w.Sources {
		u, err := url.Parse(src.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return model.ErrArgument
		}
		urls[src.URL] = true
	}
	for i, seg := range w.Segments {
		if seg.Voice != nil {
			if err := seg.Voice.Settings.Validate(); err != nil {
				return fmt.Errorf("第 %d 段配音：%v: %w", i+1, err, model.ErrArgument)
			}
			if !finiteScriptTime(seg.Voice.OffsetSeconds) || seg.Voice.OffsetSeconds < 0 || seg.Voice.OffsetSeconds >= seg.End-seg.Start {
				return fmt.Errorf("第 %d 段配音起点超出片段: %w", i+1, model.ErrArgument)
			}
		}
		if !requireGenerated && seg.MediaID == "" && seg.Index == i && utf8.RuneCountInString(seg.Narration) <= 2000 {
			continue // An unfinished manual draft is saveable, never finalizable.
		}
		m, ok := byID[seg.MediaID]
		if !ok || seg.Index != i || !finiteScriptTime(seg.Start) || !finiteScriptTime(seg.End) || seg.Start < m.In || seg.End > m.Out || seg.End <= seg.Start || utf8.RuneCountInString(seg.Narration) > 2000 || (requireGenerated && w.Document.Paragraphs[i].Instruction != "" && strings.TrimSpace(seg.Narration) == "") {
			return fmt.Errorf("第 %d 段的文稿或视频时间窗无效: %w", i+1, model.ErrArgument)
		}
		p := w.Document.Paragraphs[i]
		if p.VideoID != "" && seg.MediaID != p.VideoID {
			return fmt.Errorf("第 %d 段没有使用绑定的画面素材: %w", i+1, model.ErrForbidden)
		}
		for _, ref := range p.ResolvedReferences {
			if ref.AnchorTime != nil && ref.MediaID == seg.MediaID {
				at := project.Seconds(*ref.AnchorTime)
				if at < seg.Start || at >= seg.End {
					return fmt.Errorf("第 %d 段未包含引用的源画面: %w", i+1, model.ErrArgument)
				}
			}
		}
		if len(w.References) == 0 && p.Video != "" && m.Name != p.Video {
			return fmt.Errorf("第 %d 段没有使用强制视频: %w", i+1, model.ErrForbidden)
		}
		for _, u := range seg.SourceURLs {
			if !urls[u] {
				return fmt.Errorf("来源未经过查证: %w", model.ErrArgument)
			}
		}
	}
	return nil
}

func (s *Service) scriptWrite(ctx context.Context, actor, id string, expected int, sha, action string, edit func(*ScriptWorkbench) error, checks ...func(*store.Store) error) (ScriptWorkbench, error) {
	if ws := s.workspace; ws != nil && ctx.Value(projectMutationContextKey{}) != ws {
		if err := ws.lockMutation(ctx); err != nil {
			return ScriptWorkbench{}, err
		}
		defer ws.op.Unlock()
	}
	if err := requireHumanConfirm(actor, "script "+action); err != nil {
		return ScriptWorkbench{}, err
	}
	media, err := s.ScriptMediaCatalogue(ctx)
	if err != nil {
		return ScriptWorkbench{}, err
	}
	var saved ScriptWorkbench
	changedRecord := true
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		w, err := workbenchAt(ctx, tx, id)
		if err != nil {
			return err
		}
		if w.Version != expected {
			return model.ErrConflict
		}
		if err := s.requireVisibleConfirmation(ctx, tx, w.Script.AssetID); err != nil {
			return err
		}
		set, err := loadVisibleSet(ctx, tx)
		if err != nil {
			return err
		}
		if !s.WorkspaceEnabled() && set.SHA != sha {
			return model.ErrConflict
		}
		asset, err := tx.GetAsset(ctx, w.Script.AssetID)
		if err != nil {
			return err
		}
		if asset.Locked {
			return model.ErrForbidden
		}
		for _, check := range checks {
			if err := check(tx); err != nil {
				return err
			}
		}
		if action == "generate" {
			for _, m := range media {
				a, err := tx.GetAsset(ctx, m.AssetID)
				if err != nil {
					return err
				}
				if a.Locked || !a.AgentVisible || !containsString(a.AllowedAgents, "recognizer") || !containsString(a.AllowedAgents, "narrator") {
					return model.ErrForbidden
				}
			}
		}
		before := w
		if err := edit(&w); err != nil {
			return err
		}
		if s.WorkspaceEnabled() && action == "save" && before.Version > 0 && scriptContentFingerprint(before) == scriptContentFingerprint(w) {
			title := w.Script.Title
			w = before
			if title == before.Script.Title {
				changedRecord = false
				saved = w
				if d, e := tx.GetScriptDelivery(ctx, id, w.Version); e == nil {
					saved.Delivery = &d
				}
				return nil
			}
			w.Script.Title = title
			w.Script.UpdatedAt = s.autoNow()
			if err := tx.UpdateScript(ctx, w.Script); err != nil {
				return err
			}
			raw, err := json.Marshal(w)
			if err != nil {
				return err
			}
			if err = tx.PutScriptWorkbenchMetadata(ctx, id, w.Version, string(raw)); err != nil {
				return err
			}
			saved = w
			if d, e := tx.GetScriptDelivery(ctx, id, w.Version); e == nil {
				saved.Delivery = &d
			}
			return auditTx(ctx, tx, actor, "script.rename", id, title)
		}
		w.Document = scriptsyntax.Parse(w.Script.Body)
		if action != "save" && action != "reject" {
			resolved, err := s.resolveScriptDocument(w)
			if err != nil {
				return err
			}
			w.Document = resolved
		}
		if s.WorkspaceEnabled() && action != "save" && action != "reject" {
			ws := s.workspace
			ws.mu.Lock()
			_, err := project.BindReferences(ws.document, w.Script.Body, w.References, "", true)
			ws.mu.Unlock()
			if err != nil {
				return fmt.Errorf("%v: %w", err, model.ErrArgument)
			}
		}
		if action != "reject" && !(s.WorkspaceEnabled() && action == "save") {
			if err := validateScriptPlan(w, media, action == "finalize"); err != nil {
				return err
			}
		}
		if s.WorkspaceEnabled() {
			fingerprint, err := s.scriptSourceFingerprint(ctx, w)
			if err == nil {
				w.SourceFingerprint = fingerprint
			} else if action != "save" && action != "reject" {
				return err
			}
		}
		w.Script.VisibleSetSHA = sha
		w.Script.UpdatedAt = time.Now().UTC()
		w.Version++
		w.Delivery = nil
		if w.Script.Status != model.ScriptFinalized {
			w.Script.FinalizedBy = ""
			w.Script.FinalizedAt = nil
		}
		if err := tx.UpdateScript(ctx, w.Script); err != nil {
			return err
		}
		raw, err := json.Marshal(w)
		if err != nil {
			return err
		}
		if expected == 0 {
			old, _ := json.Marshal(before)
			if err := tx.InsertScriptRevision(ctx, id, store.ScriptRevision{Version: 0, Action: "enter", Actor: actor, Snapshot: string(old), CreatedAt: before.Script.CreatedAt}); err != nil {
				return err
			}
		}
		if err := tx.PutScriptWorkbench(ctx, id, expected, string(raw)); err != nil {
			return err
		}
		if err := tx.InsertScriptRevision(ctx, id, store.ScriptRevision{Version: w.Version, Action: action, Actor: actor, Snapshot: string(raw), CreatedAt: w.Script.UpdatedAt}); err != nil {
			return err
		}
		if action != "finalize" {
			if err := tx.UpsertDeliveryInvalidation(ctx, w.Script.AssetID, "script_changed", w.Script.UpdatedAt); err != nil {
				return err
			}
			if err := s.parkChangedScriptTx(ctx, tx, w); err != nil {
				return err
			}
		}
		if err := auditTx(ctx, tx, actor, "script."+action, id, fmt.Sprintf("version=%d visible_set=%s", w.Version, sha)); err != nil {
			return err
		}
		saved = w
		return nil
	})
	if err == nil && changedRecord {
		s.publish("script.changed", map[string]any{"script_id": id, "asset_id": saved.Script.AssetID})
		s.publish("auto.changed", map[string]any{})
		s.publish("workflow.changed", map[string]any{"asset_id": saved.Script.AssetID})
	}
	return saved, err
}

func (s *Service) SaveScriptWorkbench(ctx context.Context, actor, id string, req SaveScriptRequest) (ScriptWorkbench, error) {
	if utf8.RuneCountInString(req.Title) > 200 {
		return ScriptWorkbench{}, model.ErrArgument
	}
	return s.scriptWrite(ctx, actor, id, req.ExpectedVersion, req.VisibleSetSHA, "save", func(w *ScriptWorkbench) error {
		previousSegments := w.Segments
		changed := w.Script.Body != req.Body
		w.Script.Title = strings.TrimSpace(req.Title)
		w.Script.Body = req.Body
		if ws := s.workspace; ws != nil {
			ws.mu.Lock()
			bindings := req.References
			if bindings == nil && !changed {
				bindings = w.References
			}
			refs, err := project.BindReferences(ws.document, req.Body, bindings, req.SequenceID, false)
			ws.mu.Unlock()
			if err != nil {
				return fmt.Errorf("%v: %w", err, model.ErrArgument)
			}
			w.References = refs
			temporary := &project.AvpDocument{Scripts: []project.AvsDocument{{Body: w.Script.Body, References: w.References}}}
			ws.mu.Lock()
			for _, ref := range refs {
				if ref.Kind == "source_marker" || ref.Kind == "sequence_marker" {
					if resolved, e := project.ResolveReference(ws.document, ref); e == nil && resolved.Name != ref.Name {
						project.RenameMarkerReferences(temporary, ref.TargetID, resolved.Name)
					}
				}
			}
			ws.mu.Unlock()
			w.Script.Body = temporary.Scripts[0].Body
			w.References = temporary.Scripts[0].References
		}
		w.Rejected = false
		w.TTSEnabled = req.TTSEnabled
		w.SubtitlesEnabled = req.SubtitlesEnabled
		w.AllowWebResearch = req.AllowWebResearch
		if changed {
			w.Segments = []ScriptSegment{}
			// Explicitly supplied manual segments retain their recorded source references.
			if req.Segments == nil {
				w.Sources = []ScriptSource{}
			}
			w.Questions = []string{}
		}
		if req.Segments != nil {
			w.Segments = req.Segments
			for i := range w.Segments {
				seg := &w.Segments[i]
				if seg.Voice == nil {
					continue
				}
				// Only the dedicated voice action can grant consent. Editing the
				// narration or voice settings invalidates the old authorization.
				voice := *seg.Voice
				voice.AcceptExternal = voice.AcceptExternal && i < len(previousSegments) && previousSegments[i].Voice != nil &&
					previousSegments[i].Voice.AcceptExternal && previousSegments[i].Narration == seg.Narration &&
					previousSegments[i].Voice.Settings == voice.Settings
				seg.Voice = &voice
			}
			w.Questions = []string{}
		}
		if w.Script.Status == model.ScriptFinalized || w.Script.Status == model.ScriptNeedsRefinalize {
			w.Script.Status = model.ScriptNeedsRefinalize
			w.Script.StaleReason = "script_changed"
		} else {
			w.Script.Status = model.ScriptDraft
			w.Script.StaleReason = ""
		}
		return nil
	})
}
func (s *Service) FinalizeScriptWorkbench(ctx context.Context, actor, id string, version int, sha string) (ScriptWorkbench, error) {
	return s.scriptWrite(ctx, actor, id, version, sha, "finalize", func(w *ScriptWorkbench) error {
		if w.Script.Status != model.ScriptDraft && w.Script.Status != model.ScriptNeedsRefinalize {
			return model.ErrInvalidState
		}
		if !s.WorkspaceEnabled() && w.Script.VisibleSetSHA != sha {
			return model.ErrConflict
		}
		now := time.Now().UTC()
		w.Script.Status = model.ScriptFinalized
		w.Script.StaleReason = ""
		w.Script.FinalizedBy = actor
		w.Script.FinalizedAt = &now
		return nil
	})
}
func (s *Service) RejectScriptWorkbench(ctx context.Context, actor, id string, version int, sha string) (ScriptWorkbench, error) {
	return s.scriptWrite(ctx, actor, id, version, sha, "reject", func(w *ScriptWorkbench) error {
		if w.Script.Status == model.ScriptFinalized || w.Script.Status == model.ScriptNeedsRefinalize {
			w.Script.Status = model.ScriptNeedsRefinalize
			w.Script.StaleReason = "script_changed"
		} else {
			w.Script.Status = model.ScriptDraft
		}
		w.Rejected = true
		w.Segments = []ScriptSegment{}
		return nil
	})
}

// A script edit parks live overlays in the same transaction as the new revision.
func (s *Service) parkChangedScriptTx(ctx context.Context, tx *store.Store, w ScriptWorkbench) error {
	ids, err := tx.ListLiveAutoRunIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		run, err := tx.GetWorkflow(ctx, id)
		if err != nil {
			return err
		}
		if run.AssetID != w.Script.AssetID {
			continue
		}
		a, err := tx.GetWorkflowAuto(ctx, id)
		if err != nil {
			return err
		}
		if a.HaltReason == model.AutoWaitVisibleSet {
			continue
		}
		if err := s.applyAutoTransition(ctx, tx, &a, autoTransition{state: model.AutoStateAwaitingDecision, reason: "script_changed", hold: boolPtr(true), wait: true, kind: model.AutoDecisionTakeover, target: w.Script.ScriptID, basis: map[string]any{"script_id": w.Script.ScriptID, "script_version": w.Version}, audit: "auto.script_changed"}, s.autoNow()); err != nil {
			return err
		}
	}
	return nil
}
