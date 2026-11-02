package service

import (
	"fmt"
	"testing"
	"time"
)

func TestExternalAgentStatusLifecycle(t *testing.T) {
	s := New(nil)
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	if len(s.ExternalAgentStatus().Agents) != 0 {
		t.Fatal("invented a connected agent")
	}
	identity := ExternalAgentIdentity{ID: "local-test", Name: "test-client", Version: "1", Transport: "desktop_mcp"}
	s.ObserveExternalAgent(identity, "initialize", "")(false)
	state := func(want string, active int) {
		t.Helper()
		agents := s.ExternalAgentStatus().Agents
		if len(agents) != 1 || agents[0].State != want || agents[0].ActiveCalls != active {
			t.Fatalf("want %s/%d, got %+v", want, active, agents)
		}
	}
	state("connected", 0)
	first := s.ObserveExternalAgent(identity, "tools/call", "skills.get")
	second := s.ObserveExternalAgent(identity, "tools/call", "projects.list")
	state("busy", 2)
	first(true)
	first(true)
	state("busy", 1)
	second(false)
	state("connected", 0)
	s.ObserveExternalAgent(identity, "tools/call", "unknown.tool")(true)
	state("error", 0)
	s.ObserveExternalAgent(identity, "ping", "")(false)
	state("connected", 0)
	now = now.Add(91 * time.Second)
	state("stale", 0)
	now = now.Add(30 * time.Minute)
	state("expired", 0)
	pending := s.ObserveExternalAgent(identity, "tools/call", "skills.get")
	s.DisconnectExternalAgent(identity.ID)
	state("disconnected", 1)
	pending(false)
	state("disconnected", 0)
	expired := s.ObserveExternalAgent(identity, "tools/call", "skills.get")
	s.ExpireExternalAgent(identity.ID)
	expired(false)
	state("expired", 0)
}

func TestExternalAgentStatusBoundedAndConcurrent(t *testing.T) {
	s := New(nil)
	for i := 0; i < 300; i++ {
		s.ObserveExternalAgent(ExternalAgentIdentity{ID: fmt.Sprint(i), Name: "client\nname", Transport: "desktop_mcp"}, "ping", "")(false)
	}
	report := s.ExternalAgentStatus()
	if len(report.Agents) > 256 || len(report.Agents) == 0 {
		t.Fatalf("unbounded registry: %d", len(report.Agents))
	}
	if report.Agents[0].Name != "clientname" {
		t.Fatal("untrusted control characters retained")
	}
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			finish := s.ObserveExternalAgent(ExternalAgentIdentity{ID: "parallel", Name: "parallel"}, "tools/call", "projects.list")
			_ = s.ExternalAgentStatus()
			finish(false)
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
	for _, agent := range s.ExternalAgentStatus().Agents {
		if agent.ID == "parallel" && (agent.State != "connected" || agent.ActiveCalls != 0) {
			t.Fatalf("concurrent finish: %+v", agent)
		}
	}
}
