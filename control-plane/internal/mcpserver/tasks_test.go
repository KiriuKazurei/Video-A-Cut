package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type loopFixture struct {
	t      *testing.T
	svc    *service.Service
	server *httptest.Server
	tokens map[string]string
}

func newLoopFixture(t *testing.T, lease time.Duration, agents ...Agent) *loopFixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "loop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)
	f := &loopFixture{t: t, svc: svc, tokens: map[string]string{}}
	for i := range agents {
		token := strings.Repeat(string(rune('a'+i)), 40)
		h := sha256.Sum256([]byte(token))
		agents[i].TokenSHA256 = hex.EncodeToString(h[:])
		f.tokens[agents[i].ID] = token
	}
	f.server = httptest.NewServer(New(agents, svc, lease))
	t.Cleanup(f.server.Close)
	return f
}

// call invokes one tool and returns (structuredContent, isError, raw).
func (f *loopFixture) call(agentID, tool string, args any) (map[string]any, bool, string) {
	f.t.Helper()
	argJSON, _ := json.Marshal(args)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + string(argJSON) +
		`,"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	req, _ := http.NewRequest(http.MethodPost, f.server.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", tool)
	req.Header.Set("Authorization", "Bearer "+f.tokens[agentID])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if resp.StatusCode != 200 {
		f.t.Fatalf("%s status %d: %s", tool, resp.StatusCode, buf.String())
	}
	var env struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		f.t.Fatalf("decode %s: %v %s", tool, err, buf.String())
	}
	return env.Result.StructuredContent, env.Result.IsError, buf.String()
}

func (f *loopFixture) seed(assets []model.Asset, tasks []model.Task) {
	f.t.Helper()
	ctx := context.Background()
	for _, a := range assets {
		if err := f.svc.CreateAsset(ctx, a); err != nil {
			f.t.Fatal(err)
		}
	}
	for _, tk := range tasks {
		if err := f.svc.CreateTask(ctx, tk); err != nil {
			f.t.Fatal(err)
		}
	}
}

func visible(id string, roles ...string) model.Asset {
	return model.Asset{AssetID: id, Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: roles}
}

func TestTaskLoopFullCycle(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "n1", Role: "narrator"})
	// Empty queue is a normal result, not an error.
	if out, isErr, raw := f.call("n1", "claim_task", struct{}{}); isErr || out["claimed"] != false {
		t.Fatalf("empty queue: %s", raw)
	}
	f.seed([]model.Asset{visible("clip", "narrator")},
		[]model.Task{{TaskID: "t1", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"}})

	out, isErr, raw := f.call("n1", "claim_task", struct{}{})
	if isErr || out["claimed"] != true || out["task"].(map[string]any)["task_id"] != "t1" {
		t.Fatalf("claim: %s", raw)
	}
	// Re-claim while holding a live lease returns the same task.
	if out, _, raw := f.call("n1", "claim_task", struct{}{}); out["task"].(map[string]any)["task_id"] != "t1" {
		t.Fatalf("idempotent claim: %s", raw)
	}
	if _, isErr, raw := f.call("n1", "heartbeat", map[string]string{"task_id": "t1"}); isErr || !strings.Contains(raw, "lease_expires_at") {
		t.Fatalf("heartbeat: %s", raw)
	}
	if _, isErr, raw := f.call("n1", "report_progress", map[string]any{"task_id": "t1", "progress": 0.5, "message": "half"}); isErr {
		t.Fatalf("progress: %s", raw)
	}
	out, isErr, raw = f.call("n1", "get_task_status", map[string]string{"task_id": "t1"})
	task := out["task"].(map[string]any)
	if isErr || task["status"] != "running" || task["progress"] != 0.5 || task["message"] != "half" {
		t.Fatalf("status: %s", raw)
	}
	if _, isErr, raw := f.call("n1", "submit_result", map[string]any{"task_id": "t1", "artifacts": map[string]string{"voice": "voice.wav"}}); isErr {
		t.Fatalf("submit: %s", raw)
	}
	// Retried submit is idempotent.
	if _, isErr, raw := f.call("n1", "submit_result", map[string]any{"task_id": "t1", "artifacts": map[string]string{"voice": "voice.wav"}}); isErr {
		t.Fatalf("resubmit: %s", raw)
	}
	// Late progress on a terminal task is refused with a stable code.
	if _, isErr, raw := f.call("n1", "report_progress", map[string]any{"task_id": "t1", "progress": 0.9}); !isErr || !strings.Contains(raw, "invalid_state") {
		t.Fatalf("late progress: %s", raw)
	}
	logs, err := f.svc.ListAudit(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, l := range logs {
		if l.Target == "t1" {
			seen[l.Action]++
		}
	}
	if seen["task.claim"] != 1 || seen["task.submit"] != 1 {
		t.Fatalf("audit trail: %v", seen)
	}
}

func TestTaskLoopIsolation(t *testing.T) {
	f := newLoopFixture(t, time.Minute,
		Agent{ID: "n1", Role: "narrator"}, Agent{ID: "n2", Role: "narrator"}, Agent{ID: "e1", Role: "exporter"})
	f.seed([]model.Asset{
		visible("clip", "narrator"),
		{AssetID: "locked", Status: model.AssetStatusIngested, AgentVisible: true, Locked: true, AllowedAgents: []string{"narrator"}},
	}, []model.Task{
		{TaskID: "hidden", AssetID: "locked", Type: model.TaskTypeTTS, AgentRole: "narrator"},
		{TaskID: "t1", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"},
	})
	// Locked asset's task is skipped; the visible one is claimed.
	out, _, raw := f.call("n1", "claim_task", struct{}{})
	if out["task"].(map[string]any)["task_id"] != "t1" {
		t.Fatalf("claim skipped locked: %s", raw)
	}
	// Other role sees nothing.
	if out, _, raw := f.call("e1", "claim_task", struct{}{}); out["claimed"] != false {
		t.Fatalf("exporter claim: %s", raw)
	}
	// Second narrator finds nothing visible left.
	if out, _, raw := f.call("n2", "claim_task", struct{}{}); out["claimed"] != false {
		t.Fatalf("n2 claim: %s", raw)
	}
	for tool, args := range map[string]any{
		"get_task_status": map[string]any{"task_id": "t1"},
		"heartbeat":       map[string]any{"task_id": "t1"},
		"report_progress": map[string]any{"task_id": "t1", "progress": 0.1},
		"fail_task":       map[string]any{"task_id": "t1", "reason": "x"},
		"submit_result":   map[string]any{"task_id": "t1", "artifacts": map[string]string{}},
	} {
		_, isErr, raw := f.call("n2", tool, args)
		if !isErr || !strings.Contains(raw, "not_found") || strings.Contains(raw, "n1") {
			t.Fatalf("n2 %s on foreign task: %s", tool, raw)
		}
	}
	// Unknown task yields the same code as a foreign one.
	if _, isErr, raw := f.call("n1", "get_task_status", map[string]string{"task_id": "nope"}); !isErr || !strings.Contains(raw, "not_found") {
		t.Fatalf("unknown task: %s", raw)
	}
	if tk, _ := f.svc.GetTask(context.Background(), "t1"); tk.Status != model.TaskStatusClaimed || tk.AgentID != "n1" {
		t.Fatalf("foreign calls mutated task: %+v", tk)
	}
	// Governance tools are never exposed.
	req, _ := http.NewRequest(http.MethodPost, f.server.URL, strings.NewReader(
		`{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/list")
	req.Header.Set("Authorization", "Bearer "+f.tokens["n1"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	resp.Body.Close()
	var list struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(buf.Bytes(), &list)
	var names []string
	for _, tl := range list.Result.Tools {
		names = append(names, tl.Name)
	}
	want := "ack_execution_stopped,begin_execution,claim_task,fail_task,get_asset,get_asset_edl,get_execution_control,get_execution_result_status,get_task_input,get_task_status,heartbeat,list_assets,list_recovery_candidates,reconcile_execution_drain,report_progress,save_ingest_checkpoint,submit_delivery,submit_ingest_result,submit_result"
	sortStrings(names)
	if strings.Join(names, ",") != want {
		t.Fatalf("tools = %v", names)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestTaskLoopConcurrentClaim(t *testing.T) {
	agents := []Agent{}
	for i := 0; i < 6; i++ {
		agents = append(agents, Agent{ID: "w" + string(rune('0'+i)), Role: "narrator"})
	}
	f := newLoopFixture(t, time.Minute, agents...)
	f.seed([]model.Asset{visible("clip", "narrator")}, []model.Task{
		{TaskID: "t1", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"},
		{TaskID: "t2", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"},
	})
	var mu sync.Mutex
	got := map[string]string{}
	var wg sync.WaitGroup
	for _, a := range agents {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			out, isErr, raw := f.call(id, "claim_task", struct{}{})
			if isErr {
				t.Errorf("%s claim error: %s", id, raw)
				return
			}
			if out["claimed"] == true {
				mu.Lock()
				got[out["task"].(map[string]any)["task_id"].(string)] += id + " "
				mu.Unlock()
			}
		}(a.ID)
	}
	wg.Wait()
	if len(got) != 2 || strings.Count(got["t1"], " ") != 1 || strings.Count(got["t2"], " ") != 1 {
		t.Fatalf("each task must go to exactly one agent: %v", got)
	}
}

func TestTaskLoopExpiredLease(t *testing.T) {
	f := newLoopFixture(t, 150*time.Millisecond, Agent{ID: "n1", Role: "narrator"}, Agent{ID: "n2", Role: "narrator"})
	f.seed([]model.Asset{visible("clip", "narrator")},
		[]model.Task{{TaskID: "t1", AssetID: "clip", Type: model.TaskTypeTTS, AgentRole: "narrator"}})
	f.call("n1", "claim_task", struct{}{})
	time.Sleep(250 * time.Millisecond)
	if _, isErr, raw := f.call("n1", "report_progress", map[string]any{"task_id": "t1", "progress": 0.3}); !isErr || !strings.Contains(raw, "lease_expired") {
		t.Fatalf("expired progress: %s", raw)
	}
	if _, isErr, raw := f.call("n1", "submit_result", map[string]any{"task_id": "t1", "artifacts": map[string]string{}}); !isErr || !strings.Contains(raw, "lease_expired") {
		t.Fatalf("expired submit: %s", raw)
	}
	if n, err := f.svc.RequeueExpiredLeases(context.Background()); err != nil || n != 1 {
		t.Fatalf("requeue: %d %v", n, err)
	}
	// After requeue, the old holder loses visibility and another agent can take it.
	if _, isErr, raw := f.call("n1", "get_task_status", map[string]string{"task_id": "t1"}); !isErr || !strings.Contains(raw, "not_found") {
		t.Fatalf("stale holder status: %s", raw)
	}
	if out, _, raw := f.call("n2", "claim_task", struct{}{}); out["claimed"] != true {
		t.Fatalf("reclaim: %s", raw)
	}
}

func TestTaskLoopArgumentValidation(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "n1", Role: "narrator"})
	for tool, args := range map[string]any{
		"get_task_status": map[string]string{"task_id": " "},
		"report_progress": map[string]any{"task_id": "", "progress": 0.1},
		"fail_task":       map[string]any{"task_id": "t", "reason": ""},
		"submit_result":   map[string]any{"task_id": "", "artifacts": map[string]string{}},
	} {
		if _, isErr, raw := f.call("n1", tool, args); !isErr || !strings.Contains(raw, "invalid_argument") {
			t.Fatalf("%s: %s", tool, raw)
		}
	}
	// Liveness-only heartbeat registers the agent without a task.
	if _, isErr, raw := f.call("n1", "heartbeat", struct{}{}); isErr {
		t.Fatalf("liveness heartbeat: %s", raw)
	}
	if ag, err := f.svc.GetAgent(context.Background(), "n1"); err != nil || ag.Role != "narrator" {
		t.Fatalf("agent row: %+v %v", ag, err)
	}
}
