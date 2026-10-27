package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

type DrawPreviewRequest struct {
	Session        string                `json:"session"`
	ContentVersion string                `json:"content_version"`
	RequestID      string                `json:"request_id"`
	Frame          int64                 `json:"frame"`
	Document       *project.DrawDocument `json:"document,omitempty"`
	Scale          float64               `json:"scale,omitempty"`
}
type DrawPreviewFrame struct {
	OK             bool             `json:"ok"`
	Session        string           `json:"session"`
	ContentVersion string           `json:"content_version"`
	Revision       string           `json:"revision"`
	RequestID      string           `json:"request_id"`
	Frame          int64            `json:"frame"`
	PNG            string           `json:"png"`
	Geometry       []DrawNodeBounds `json:"geometry"`
	WorkerPID      int              `json:"worker_pid"`
	Message        string           `json:"message,omitempty"`
}
type DrawNodeBounds struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type drawFrameProcess struct {
	environment string
	mu          sync.Mutex
	stop        context.CancelFunc
	input       io.WriteCloser
	responses   chan []byte
	done        chan struct{}
}

func (s *Service) CloseDrawPreviews() {
	s.drawMu.Lock()
	processes := s.drawFrames
	s.drawFrames = nil
	s.drawMu.Unlock()
	for _, p := range processes {
		p.stop()
		<-p.done
	}
}
func (s *Service) CloseDrawPreview(projectID, session, id string) error {
	if s.CurrentProjectID() != projectID || s.ProjectSessionID() != session {
		return model.ErrConflict
	}
	key := session + ":" + id
	s.drawMu.Lock()
	p := s.drawFrames[key]
	delete(s.drawFrames, key)
	s.drawMu.Unlock()
	if p != nil {
		p.stop()
		<-p.done
	}
	return nil
}
func (s *Service) startDrawPreview(key, environment string) (*drawFrameProcess, error) {
	s.drawMu.Lock()
	defer s.drawMu.Unlock()
	if p := s.drawFrames[key]; p != nil {
		if p.environment != environment {
			p.stop()
			<-p.done
		}
		select {
		case <-p.done:
			delete(s.drawFrames, key)
		default:
			return p, nil
		}
	}
	root, _, e := s.scriptRoot()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "workers", "node", "timeline-cli", "src", "draw-preview.mjs"))
	cmd.Dir = filepath.Join(root, "workers", "node", "timeline-cli")
	cmd.Env = toolEnvironment()
	configureMediaCommand(cmd)
	in, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		cancel()
		in.Close()
		return nil, e
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		cancel()
		in.Close()
		return nil, e
	}
	p := &drawFrameProcess{stop: cancel, input: in, responses: make(chan []byte, 1), done: make(chan struct{}), environment: environment}
	if s.drawFrames == nil {
		s.drawFrames = map[string]*drawFrameProcess{}
	}
	s.drawFrames[key] = p
	go func() {
		defer close(p.done)
		defer close(p.responses)
		defer in.Close()
		defer cancel()
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 65536), 32<<20)
		for scan.Scan() {
			line := append([]byte{}, scan.Bytes()...)
			select {
			case p.responses <- line:
			case <-ctx.Done():
			}
		}
		_ = cmd.Wait()
	}()
	return p, nil
}

// Unsaved visual parameters are a preview snapshot, never a project write.
// The saved base version is checked again after pixels arrive.
func (s *Service) PreviewDrawComposition(ctx context.Context, projectID, id string, req DrawPreviewRequest) (DrawPreviewFrame, error) {
	var result DrawPreviewFrame
	if s.workspace == nil {
		return result, model.ErrInvalidState
	}
	s.workspace.gate.RLock()
	defer s.workspace.gate.RUnlock()
	base, e := s.drawContext(projectID, id, DrawActionRequest{req.Session, req.ContentVersion})
	if e != nil {
		return result, e
	}
	if req.RequestID == "" || len(req.RequestID) > 128 || req.Scale < 0 || req.Scale > 1 {
		return result, model.ErrArgument
	}
	doc := base.UnifiedDocument()
	if req.Document != nil {
		raw, _ := json.Marshal(req.Document)
		_ = json.Unmarshal(raw, &doc)
	}
	for _, source := range doc.Sources {
		if e = project.ValidateDrawImageBindings(source.ImageBindings); e != nil {
			return result, fmt.Errorf("source %s image bindings: %v: %w", source.ID, e, model.ErrArgument)
		}
	}
	c := base
	if req.Document != nil {
		c.Images = nil
	}
	c.Document = &doc
	c.NormalizeDocument()
	doc = *c.Document
	for i := range doc.Nodes {
		n := &doc.Nodes[i]
		if n.Kind != "image" || n.ResourceSHA != "" {
			continue
		}
		r, e := s.ProjectResource(projectID, n.ResourceID)
		if e != nil {
			return result, e
		}
		n.ResourceSHA = project.ResourceContentVersion(r)
	}
	if e = doc.Validate(); e != nil {
		return result, fmt.Errorf("%v: %w", e, model.ErrArgument)
	}
	if req.Frame < 0 || req.Frame >= doc.DurationFrames {
		return result, model.ErrArgument
	}
	c.Document = &doc
	progress := func(phase string) {
		s.publish("workspace.draw.preview", map[string]any{"project_id": projectID, "session_id": req.Session, "composition_id": id, "request_id": req.RequestID, "frame": req.Frame, "phase": phase})
	}
	progress("loading")
	assets, e := s.drawImageAssets(ctx, projectID, c)
	if e != nil {
		return result, e
	}
	content := c.ContentHash()
	scale := req.Scale
	if scale == 0 {
		scale = 1
		if doc.Canvas.Width > 1280 {
			scale = 1280 / float64(doc.Canvas.Width)
		}
	}
	environment := drawingEnvironmentVersion()
	p, e := s.startDrawPreview(req.Session+":"+id, environment)
	if e != nil {
		return result, e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	payload := map[string]any{"document": doc, "assets": assets, "output_dir": filepath.Join(s.drawDir(projectID, id, "preview"), "worker"), "frame": req.Frame, "scale": scale, "request_id": req.RequestID, "revision": content, "session": req.Session, "content_version": content}
	raw, e := json.Marshal(payload)
	if e != nil {
		return result, e
	}
	if _, e = p.input.Write(append(raw, '\n')); e != nil {
		p.stop()
		return result, fmt.Errorf("预览进程已退出，请重试: %w", model.ErrInvalidState)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			p.stop()
			return result, waitCtx.Err()
		case raw, ok := <-p.responses:
			if !ok {
				return result, fmt.Errorf("预览进程退出: %w", model.ErrInvalidState)
			}
			var event struct {
				Event     string `json:"event"`
				Phase     string `json:"phase"`
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal(raw, &event) == nil && event.Event == "state" {
				if event.RequestID == req.RequestID {
					progress(event.Phase)
				}
				continue
			}
			if e = json.Unmarshal(raw, &result); e != nil {
				return result, e
			}
		}
		break
	}
	if !result.OK {
		return result, fmt.Errorf("%s: %w", result.Message, model.ErrInvalidState)
	}
	if result.RequestID != req.RequestID || result.Frame != req.Frame || result.Revision != content {
		return result, model.ErrConflict
	}
	if drawingEnvironmentVersion() != environment {
		return result, model.ErrConflict
	}
	if _, e = s.drawContext(projectID, id, DrawActionRequest{req.Session, req.ContentVersion}); e != nil {
		return result, e
	}
	if _, e = s.drawImageAssets(ctx, projectID, c); e != nil {
		return result, e
	}
	result.Session = req.Session
	result.ContentVersion = content
	return result, nil
}

func (s *Service) DrawCapabilities(projectID string) (map[string]any, error) {
	if _, e := s.DrawCompositions(projectID); e != nil {
		return nil, e
	}
	return map[string]any{"document_schema": 1, "avp_schema": 3, "policy_version": "vac-unified-1", "engines": map[string]string{"hyperframes": "0.8.193", "remotion": "4.0.538"}, "nodes": []string{"source", "image", "text", "group", "mask", "shape"}, "presets": []string{"auto", "interface", "data", "media"}, "live_preview": true, "alpha": "straight", "color_space": "srgb", "cross_document_backdrop_filter": false, "picture_generation": false, "no_runtime_downloads": true}, nil
}
