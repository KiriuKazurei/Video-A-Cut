package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// IngestTaskView pairs a step task with its binding.
type IngestTaskView struct {
	Task    model.Task              `json:"task"`
	Binding model.IngestTaskBinding `json:"binding"`
}

// IngestPlanView exposes one verified plan revision.
type IngestPlanView struct {
	Revision  int                  `json:"revision"`
	Kind      string               `json:"kind"`
	SHA256    string               `json:"sha256"`
	Actor     string               `json:"actor"`
	Analysis  *ingest.AnalysisPlan `json:"analysis,omitempty"`
	Selection *ingest.Selection    `json:"selection,omitempty"`
	CreatedAt string               `json:"created_at"`
}

// IngestExecutionView is the de-identified execution snapshot for the UI.
// It has no token, absolute directory, or process id.
type IngestExecutionView struct {
	ExecutionID     string `json:"execution_id,omitempty"`
	Generation      int    `json:"generation,omitempty"`
	Status          string `json:"status,omitempty"`
	WaitReason      string `json:"wait_reason,omitempty"`
	Connection      string `json:"connection,omitempty"`
	CheckpointsDone int    `json:"checkpoints_done"`
	RecoveryFrom    string `json:"recovery_from,omitempty"`
	Cleanup         string `json:"cleanup,omitempty"`
	ControlVersion  int    `json:"control_version,omitempty"`
}

// IngestRunView is GET /api/ingest-runs/{run}.
type IngestRunView struct {
	Run       model.IngestRun       `json:"run"`
	Source    model.RecordingSource `json:"source"`
	Probe     *ingest.Probe         `json:"probe,omitempty"`
	Plans     []IngestPlanView      `json:"plans"`
	Tasks     []IngestTaskView      `json:"tasks"`
	Segments  *SegmentSummary       `json:"segments,omitempty"`
	Files     []string              `json:"files"`
	Asset     model.Asset           `json:"asset"`
	Execution *IngestExecutionView  `json:"execution,omitempty"`
}

// SegmentSummary describes the registered candidate list.
type SegmentSummary struct {
	Count         int                 `json:"count"`
	CutCount      int                 `json:"cut_count"`
	Method        string              `json:"method"`
	MethodVersion string              `json:"method_version"`
	RangeUs       [2]int64            `json:"range_us"`
	Config        ingest.DetectConfig `json:"config"`
}

func (s *Service) GetIngestRunView(ctx context.Context, runID string) (IngestRunView, error) {
	run, err := s.st.GetIngestRun(ctx, runID)
	if err != nil {
		return IngestRunView{}, err
	}
	v := IngestRunView{Run: run, Plans: []IngestPlanView{}, Tasks: []IngestTaskView{}, Files: []string{}}
	if v.Source, err = s.st.GetSource(ctx, run.SourceID); err != nil {
		return v, err
	}
	if v.Asset, err = s.st.GetAsset(ctx, run.AssetID); err != nil {
		return v, err
	}
	if run.ProbeJSON != "" {
		pr, err := s.runProbe(run)
		if err != nil {
			return v, err
		}
		v.Probe = &pr
	}
	plans, err := s.st.ListPlanRevisions(ctx, runID)
	if err != nil {
		return v, err
	}
	for _, p := range plans {
		pv := IngestPlanView{Revision: p.Revision, Kind: p.Kind, SHA256: p.SHA256, Actor: p.Actor, CreatedAt: p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
		if p.Kind == "analysis" {
			plan, _, err := s.analysisRevision(ctx, s.st, run, p.Revision)
			if err != nil {
				return v, err
			}
			pv.Analysis = &plan
		} else {
			sel, _, err := s.selectionRevision(ctx, s.st, run, p.Revision)
			if err != nil {
				return v, err
			}
			pv.Selection = &sel
		}
		v.Plans = append(v.Plans, pv)
	}
	bindings, err := s.st.ListIngestBindings(ctx, runID)
	if err != nil {
		return v, err
	}
	for _, b := range bindings {
		tk, err := s.st.GetTask(ctx, b.TaskID)
		if err != nil {
			return v, err
		}
		v.Tasks = append(v.Tasks, IngestTaskView{Task: tk, Binding: b})
		if b.TaskID == run.CurrentTaskID && b.ExecutionID != "" {
			if ex, err := s.st.GetIngestExecution(ctx, b.ExecutionID); err == nil {
				view := IngestExecutionView{ExecutionID: ex.ExecutionID, Generation: ex.Generation, Status: ex.Status, CheckpointsDone: 0}
				switch ex.Status {
				case model.ExecRunning:
					view.Connection = "connected"
				case model.ExecSuspended:
					view.Connection = "suspended"
					view.WaitReason = "connection_interrupted"
				case model.ExecWaitingResource, model.ExecStopRequested, model.ExecDraining:
					view.WaitReason = "waiting_for_previous_execution"
				case model.ExecCleanupBlocked:
					view.Cleanup = "blocked"
					view.WaitReason = "cleanup_blocked"
				case model.ExecStopped:
					view.Cleanup = "drained"
				}
				if cps, err := s.st.ListCheckpoints(ctx, b.TaskID); err == nil {
					for _, cp := range cps {
						if cp.ExecutionID == ex.ExecutionID {
							view.CheckpointsDone++
						}
					}
				}
				if ctrl, err := s.st.GetExecutionControl(ctx, ex.ExecutionID); err == nil {
					view.ControlVersion = ctrl.ControlVersion
				}
				v.Execution = &view
			}
		}
	}
	if run.SegmentsRef != "" {
		doc, err := s.runSegments(ctx, run)
		if err != nil {
			return v, err
		}
		v.Segments = &SegmentSummary{Count: len(doc.Candidates), CutCount: len(doc.Cuts), Method: doc.Method, MethodVersion: doc.MethodVersion, RangeUs: doc.RangeUs, Config: doc.Config}
	}
	for k := range run.Files {
		v.Files = append(v.Files, k)
	}
	sort.Strings(v.Files)
	return v, nil
}

func (s *Service) ListIngestRuns(ctx context.Context, assetID string, limit, offset int) ([]model.IngestRun, error) {
	if limit < 1 || limit > 50 || offset < 0 || offset > 100000 {
		return nil, fmt.Errorf("limit must be 1..50 and offset 0..100000: %w", model.ErrArgument)
	}
	if _, err := s.st.GetAsset(ctx, assetID); err != nil {
		return nil, err
	}
	return s.st.ListIngestRuns(ctx, assetID, limit, offset)
}

// runSegments re-reads segments.json and checks its registered hash.
func (s *Service) runSegments(ctx context.Context, run model.IngestRun) (ingest.SegmentsDoc, error) {
	policy, err := runPolicy(run)
	if err != nil {
		return ingest.SegmentsDoc{}, err
	}
	raw, sum, err := s.readSmall(run.SegmentsRef, policy.MaxDetectionJSONBytes)
	if err != nil {
		return ingest.SegmentsDoc{}, err
	}
	if sum != run.SegmentsSHA256 {
		return ingest.SegmentsDoc{}, fmt.Errorf("segments.json no longer matches its registered hash: %w", model.ErrInvalidState)
	}
	var doc ingest.SegmentsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return doc, fmt.Errorf("segments.json is unreadable: %w", model.ErrInvalidState)
	}
	return doc, nil
}

// SegmentItem is one candidate row with its thumbnail key.
type SegmentItem struct {
	ingest.Candidate
	ThumbnailKey string `json:"thumbnail_key,omitempty"`
}

// SegmentPage is GET /api/ingest-runs/{run}/segments.
type SegmentPage struct {
	Total         int           `json:"total"`
	Limit         int           `json:"limit"`
	Offset        int           `json:"offset"`
	AnalysisRev   int           `json:"analysis_revision"`
	MethodVersion string        `json:"method_version"`
	Items         []SegmentItem `json:"items"`
}

func (s *Service) ListIngestSegments(ctx context.Context, runID string, limit, offset int) (SegmentPage, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 100000 {
		return SegmentPage{}, fmt.Errorf("limit must be 1..200 and offset 0..100000: %w", model.ErrArgument)
	}
	run, err := s.st.GetIngestRun(ctx, runID)
	if err != nil {
		return SegmentPage{}, err
	}
	if run.SegmentsRef == "" {
		return SegmentPage{}, fmt.Errorf("no candidate segments are registered for this run yet: %w", model.ErrInvalidState)
	}
	doc, err := s.runSegments(ctx, run)
	if err != nil {
		return SegmentPage{}, err
	}
	page := SegmentPage{Total: len(doc.Candidates), Limit: limit, Offset: offset, AnalysisRev: run.AnalysisRevision, MethodVersion: doc.MethodVersion, Items: []SegmentItem{}}
	for i := offset; i < len(doc.Candidates) && i < offset+limit; i++ {
		c := doc.Candidates[i]
		item := SegmentItem{Candidate: c}
		if c.Thumbnail != "" {
			item.ThumbnailKey = "thumb_" + c.SegmentID
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

// IngestProbeJSON returns the stored, verified probe document.
func (s *Service) IngestProbeJSON(ctx context.Context, runID string) (*ingest.Probe, error) {
	run, err := s.st.GetIngestRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	pr, err := s.runProbe(run)
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

// OpenIngestFile opens a registered thumbnail or prepared clip by key.
// Keys never encode paths, and the original recording is not reachable.
func (s *Service) OpenIngestFile(ctx context.Context, runID, key string) (*os.File, DeliveryFile, error) {
	if !deliveryKey.MatchString(key) {
		return nil, DeliveryFile{}, fmt.Errorf("invalid file key: %w", model.ErrArgument)
	}
	run, err := s.st.GetIngestRun(ctx, runID)
	if err != nil {
		return nil, DeliveryFile{}, err
	}
	rel, ok := run.Files[key]
	if !ok {
		return nil, DeliveryFile{}, fmt.Errorf("file key is not registered on this run: %w", model.ErrNotFound)
	}
	full, info, err := s.deliveryPath(filepath.FromSlash(rel), false)
	if err != nil {
		return nil, DeliveryFile{}, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, DeliveryFile{}, err
	}
	mime, playable := fileMime(full)
	return f, DeliveryFile{Key: key, Name: info.Name(), PackagePath: filepath.Base(full), Mime: mime, Size: info.Size(), Playable: playable}, nil
}
