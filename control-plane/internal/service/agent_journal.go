package service

import (
	"context"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"strings"
	"time"
)

func journalPayload(raw json.RawMessage) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return map[string]string{"sha256": skillHash(string(raw))}
	}
	var clean func(any) any
	clean = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, item := range x {
				lower := strings.ToLower(k)
				if strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "password") || lower == "authorization" || lower == "base64" {
					out[k] = "[redacted]"
				} else {
					out[k] = clean(item)
				}
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = clean(item)
			}
			return out
		case string:
			if len(x) > 8192 {
				return "[large text sha256=" + skillHash(x) + "]"
			}
		}
		return v
	}
	return clean(value)
}
func (s *Service) journalTx(ctx context.Context, tx *store.Store, actor, run, action string, detail any) error {
	body := map[string]any{"actor": actor, "action": action, "at": s.autoNow(), "detail": detail}
	if s.WorkspaceEnabled() {
		id, _ := ctx.Value(projectContextKey{}).(string)
		if id == "" {
			id = s.CurrentProjectID()
		}
		body["project_id"] = id
	}
	return tx.AppendAgentJournal(ctx, run, body)
}

// RecordAgentOperation is a write-only host broker. No transport exposes it to an Agent.
func (s *Service) RecordAgentOperation(ctx context.Context, actor, run, action string, raw json.RawMessage) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.st.Transaction(ctx, func(tx *store.Store) error { return s.journalTx(ctx, tx, actor, run, action, journalPayload(raw)) })
}
func (s *Service) AgentOperations(ctx context.Context, actor, run string, after int64, limit int) ([]store.JournalEntry, error) {
	if err := requireHumanConfirm(actor, "read Agent operation journal"); err != nil {
		return nil, err
	}
	if s.WorkspaceEnabled() {
		if run != "" {
			if _, err := s.GetSkillRun(ctx, actor, run); err != nil {
				return nil, err
			}
		}
		current := s.CurrentProjectID()
		out := []store.JournalEntry{}
		if current == "" {
			return out, nil
		}
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		for len(out) < limit {
			rows, err := s.st.AgentJournal(ctx, run, after, 500)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				after = row.Sequence
				var body struct {
					ProjectID string `json:"project_id"`
				}
				if json.Unmarshal(row.Body, &body) == nil && body.ProjectID == current {
					out = append(out, row)
					if len(out) == limit {
						return out, nil
					}
				}
			}
		}
		return out, nil
	}
	return s.st.AgentJournal(ctx, run, after, limit)
}
func (s *Service) VerifyAgentOperations(ctx context.Context, actor string) (int64, error) {
	if err := requireHumanConfirm(actor, "verify Agent operation journal"); err != nil {
		return 0, err
	}
	return s.st.VerifyAgentJournal(ctx)
}
