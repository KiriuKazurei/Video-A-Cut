package mcpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"hash"
	"io"
	"net/http"
)

type journalResponse struct {
	http.ResponseWriter
	status int
	digest hash.Hash
	prefix bytes.Buffer
}

func (w *journalResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *journalResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	w.digest.Write(data)
	if w.prefix.Len() < 4096 {
		n := 4096 - w.prefix.Len()
		if n > len(data) {
			n = len(data)
		}
		w.prefix.Write(data[:n])
	}
	return w.ResponseWriter.Write(data)
}
func (w *journalResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Authenticate first; then durably record each Agent tool attempt before the SDK
// can execute it. Neither journal contents nor hashes are returned to the Agent.
func serveJournaledMCP(base http.Handler, svc *service.Service, a Agent, w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid MCP body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var call struct {
		Method string `json:"method"`
		Params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &call) != nil || call.Method != "tools/call" {
		base.ServeHTTP(w, r)
		return
	}
	var target struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(call.Params.Arguments, &target)
	actor := "agent:" + a.ID
	identity := map[string]string{"name": a.Name, "version": a.Version, "source": a.Source, "peer": a.Peer, "identity_kind": a.IdentityKind}
	detail, _ := json.Marshal(map[string]any{"tool": call.Params.Name, "arguments": call.Params.Arguments, "request_sha256": hexDigest(raw), "agent": identity})
	if svc.RecordAgentOperation(r.Context(), actor, target.RunID, "mcp.requested", detail) != nil {
		http.Error(w, "operation journal unavailable", http.StatusServiceUnavailable)
		return
	}
	response := &journalResponse{ResponseWriter: w, digest: sha256.New()}
	base.ServeHTTP(response, r)
	result, _ := json.Marshal(map[string]any{"tool": call.Params.Name, "status": response.status, "is_error": bytes.Contains(response.prefix.Bytes(), []byte(`"isError":true`)), "response_sha256": hex.EncodeToString(response.digest.Sum(nil)), "agent": identity})
	// An incomplete terminal entry leaves a durable requested event. Future tool
	// requests also fail closed if the journal is unavailable.
	_ = svc.RecordAgentOperation(r.Context(), actor, target.RunID, "mcp.completed", result)
}
func hexDigest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
