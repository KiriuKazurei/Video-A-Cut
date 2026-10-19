package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"time"
)

// RunBuiltinAgent is a bounded model/tool loop. It returns at every human gate.
// The request context cancels both generation and any tool process it spawned.
func (s *Service) RunBuiltinAgent(ctx context.Context, actor, id string, version int) (skill.Run, error) {
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return r, err
	}
	if err = requireHumanConfirm(actor, "execute builtin Agent"); err != nil {
		return r, err
	}
	if r.Mode != "builtin_api" || r.Version != version || r.State != "queued" {
		return r, model.ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.Budget.MaxSeconds)*time.Second)
	defer cancel()
	s.harnessMu.Lock()
	if s.harnessCancel == nil {
		s.harnessCancel = map[string]context.CancelFunc{}
	}
	s.harnessCancel["agent:"+id] = cancel
	s.harnessMu.Unlock()
	defer func() { s.harnessMu.Lock(); delete(s.harnessCancel, "agent:"+id); s.harnessMu.Unlock() }()
	release, err := s.acquireScript("agent:" + id)
	if err != nil {
		return r, err
	}
	defer release()
	history := []map[string]any{}
	for call := 0; call < r.Budget.MaxSteps; call++ {
		modelArgs, _ := json.Marshal(map[string]any{"history": history})
		next, raw, e := s.ExecuteSkillTool(ctx, actor, id, skill.ToolRequest{Name: "agent.model", Arguments: modelArgs, ExpectedVersion: r.Version, IdempotencyKey: newID("model")})
		r = next
		if e != nil {
			return r, e
		}
		var decision struct {
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
			Wait      string          `json:"wait"`
		}
		if json.Unmarshal(raw, &decision) != nil {
			return r, model.ErrInvalidState
		}
		if decision.Wait != "" {
			return r, nil
		}
		next, result, e := s.ExecuteSkillTool(ctx, actor, id, skill.ToolRequest{Name: decision.Tool, Arguments: decision.Arguments, ExpectedVersion: r.Version, IdempotencyKey: newID("agent")})
		r = next
		if e != nil {
			return r, e
		}
		var payload any
		_ = json.Unmarshal(result, &payload)
		history = append(history, map[string]any{"tool": decision.Tool, "arguments": decision.Arguments, "result": payload})
		if r.State == "waiting_human" || r.State == "succeeded" || r.State == "cancelled" {
			return r, nil
		}
	}
	return r, fmt.Errorf("Agent call budget exhausted: %w", model.ErrInvalidState)
}
