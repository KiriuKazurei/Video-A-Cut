package service

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
)

type ExternalAgentIdentity struct{ ID, Name, Version, Transport string }

type ExternalAgentPresence struct {
	ID          string    `json:"agent_id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Transport   string    `json:"transport"`
	State       string    `json:"state"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	LastTool    string    `json:"last_tool,omitempty"`
	ActiveCalls int       `json:"active_calls"`
}

type ExternalAgentStatusReport struct {
	GeneratedAt       time.Time               `json:"generated_at"`
	StaleAfterSeconds int                     `json:"stale_after_seconds"`
	Agents            []ExternalAgentPresence `json:"agents"`
}

type externalAgentEntry struct {
	ExternalAgentPresence
	serial uint64
}

func agentStatusText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) > 128 {
		return string(runes[:128])
	}
	return value
}

// ObserveExternalAgent is a transport lifecycle hook, not a client-reported claim
// about model thinking or process liveness. The returned completion is idempotent.
func (s *Service) ObserveExternalAgent(identity ExternalAgentIdentity, method, tool string) func(bool) {
	now := s.autoNow()
	s.agentStatusMu.Lock()
	if s.agentStatus == nil {
		s.agentStatus = map[string]*externalAgentEntry{}
	}
	entry := s.agentStatus[identity.ID]
	if entry == nil {
		if len(s.agentStatus) >= 256 {
			oldest := ""
			for id, item := range s.agentStatus {
				if item.ActiveCalls == 0 && (oldest == "" || item.LastSeenAt.Before(s.agentStatus[oldest].LastSeenAt)) {
					oldest = id
				}
			}
			if oldest == "" {
				s.agentStatusMu.Unlock()
				return func(bool) {}
			}
			delete(s.agentStatus, oldest)
		}
		entry = &externalAgentEntry{ExternalAgentPresence: ExternalAgentPresence{ID: agentStatusText(identity.ID), Name: agentStatusText(identity.Name), Version: agentStatusText(identity.Version), Transport: identity.Transport, ConnectedAt: now}}
		s.agentStatus[identity.ID] = entry
	}
	entry.serial++
	serial := entry.serial
	entry.LastSeenAt = now
	entry.State = "connected"
	isTool := method == "tools/call"
	if isTool {
		entry.ActiveCalls++
		entry.LastTool = agentStatusText(tool)
	}
	s.agentStatusMu.Unlock()
	s.publishAgentStatus()
	var once sync.Once
	return func(failed bool) {
		once.Do(func() {
			finished := s.autoNow()
			s.agentStatusMu.Lock()
			if s.agentStatus[identity.ID] != entry {
				s.agentStatusMu.Unlock()
				return
			}
			if isTool {
				entry.ActiveCalls--
			}
			// A late response cannot revive an explicitly closed session, or replace
			// the outcome of a newer concurrent request.
			if entry.State != "disconnected" && entry.State != "expired" {
				entry.LastSeenAt = finished
				if entry.serial == serial {
					if failed {
						entry.State = "error"
					} else {
						entry.State = "connected"
					}
				}
			}
			s.agentStatusMu.Unlock()
			s.publishAgentStatus()
		})
	}
}

func (s *Service) DisconnectExternalAgent(id string) {
	s.closeExternalAgent(id, "disconnected")
}

func (s *Service) ExpireExternalAgent(id string) {
	s.closeExternalAgent(id, "expired")
}

func (s *Service) closeExternalAgent(id, state string) {
	s.agentStatusMu.Lock()
	entry := s.agentStatus[id]
	if entry != nil {
		entry.State = state
		entry.LastSeenAt = s.autoNow()
	}
	s.agentStatusMu.Unlock()
	if entry != nil {
		s.publishAgentStatus()
	}
}

// Snapshots age silence without a timer per connection. No credentials,
// protocol session headers, request arguments or filesystem paths are retained.
func (s *Service) ExternalAgentStatus() ExternalAgentStatusReport {
	now := s.autoNow()
	report := ExternalAgentStatusReport{GeneratedAt: now, StaleAfterSeconds: 90, Agents: []ExternalAgentPresence{}}
	s.agentStatusMu.Lock()
	for _, entry := range s.agentStatus {
		agent := entry.ExternalAgentPresence
		if agent.State != "disconnected" && agent.State != "expired" {
			switch {
			case agent.ActiveCalls > 0:
				agent.State = "busy"
			case now.Sub(agent.LastSeenAt) > 30*time.Minute:
				agent.State = "expired"
			case now.Sub(agent.LastSeenAt) > 90*time.Second:
				agent.State = "stale"
			}
		}
		report.Agents = append(report.Agents, agent)
	}
	s.agentStatusMu.Unlock()
	sort.Slice(report.Agents, func(i, j int) bool { return report.Agents[i].ID < report.Agents[j].ID })
	return report
}

func (s *Service) publishAgentStatus() {
	// Connection telemetry is process state. It must never dirty or persist AVP.
	if bus := s.Bus(); bus != nil {
		bus.Publish(events.Envelope{Name: "agent.status.changed", Payload: struct{}{}})
	}
}
