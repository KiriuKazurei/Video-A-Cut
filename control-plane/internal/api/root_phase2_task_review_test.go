package api

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestReviewPhase2TaskHTTPStoreSSEAudit(t *testing.T) {
	srv := newBusServer(t)
	stream, _ := connectSSE(t, srv)
	defer stream.stop()
	base := stream.ts.URL
	client := stream.ts.Client()

	const taskID = "phase2_task_review"
	requestBody := `{"task_id":"phase2_task_review","asset_id":"a_1","type":"recognize","agent_role":"recognizer"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/api/tasks", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("POST /api/tasks = %d: %s", resp.StatusCode, body)
	}
	var created model.Task
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if created.TaskID != taskID || created.Status != model.TaskStatusQueued || created.UpdatedAt.IsZero() {
		t.Fatalf("created task = %+v", created)
	}

	getResp, err := client.Get(base + "/api/tasks/" + taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET task = %d", getResp.StatusCode)
	}
	var persisted model.Task
	if err := json.NewDecoder(getResp.Body).Decode(&persisted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, created) {
		t.Fatalf("GET task differs from POST result: GET=%+v POST=%+v", persisted, created)
	}

	frame := stream.waitFrameName(t, "task_created")
	var eventTask model.Task
	if err := json.Unmarshal([]byte(frame.data), &eventTask); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(eventTask, persisted) {
		t.Fatalf("SSE task differs from SQLite-backed GET: SSE=%+v GET=%+v", eventTask, persisted)
	}

	auditResp, err := client.Get(base + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer auditResp.Body.Close()
	if auditResp.StatusCode != http.StatusOK {
		t.Fatalf("GET audit = %d", auditResp.StatusCode)
	}
	var audit []model.AuditLog
	if err := json.NewDecoder(auditResp.Body).Decode(&audit); err != nil {
		t.Fatal(err)
	}
	for _, row := range audit {
		if row.Action == "task.create" && row.Target == taskID && row.Actor == "system" {
			return
		}
	}
	t.Fatalf("task creation missing from audit: %+v", audit)
}
