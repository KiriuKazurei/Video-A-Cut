// Package mcpserver exposes the control plane's agent-only MCP projection.
// Business rules remain in service; this package owns transport and identity.
package mcpserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Agent struct {
	ID          string `json:"agent_id"`
	Role        string `json:"role"`
	TokenSHA256 string `json:"token_sha256"`
}

type credentials struct {
	Agents []Agent `json:"agents"`
}
type identityKey struct{}

// Load rejects ambiguous credentials at startup. The source file is intended
// for local administration; bearer tokens are never persisted by this server.
func Load(path string, svc *service.Service, lease time.Duration) (http.Handler, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if len(c.Agents) == 0 {
		return nil, errors.New("mcp: at least one agent is required")
	}
	seenIDs, seenHashes := map[string]bool{}, map[string]bool{}
	for _, a := range c.Agents {
		if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Role) == "" {
			return nil, errors.New("mcp: agent_id and role are required")
		}
		decoded, err := hex.DecodeString(a.TokenSHA256)
		if err != nil || len(decoded) != sha256.Size || a.TokenSHA256 != strings.ToLower(a.TokenSHA256) {
			return nil, fmt.Errorf("mcp: invalid token_sha256 for %q", a.ID)
		}
		if seenIDs[a.ID] || seenHashes[a.TokenSHA256] {
			return nil, errors.New("mcp: duplicate agent identity or token hash")
		}
		seenIDs[a.ID], seenHashes[a.TokenSHA256] = true, true
	}
	return New(c.Agents, svc, lease), nil
}

// maxRequestBytes caps one JSON-RPC request body.
const maxRequestBytes = 256 << 10

// DefaultLease is used when the caller passes a non-positive lease window.
const DefaultLease = 30 * time.Second

// New creates a stateless 2026-07-28 MCP endpoint. Authentication precedes
// the SDK so unauthenticated callers cannot discover tools or call service.
// lease is the window granted by claim_task and renewed by heartbeat; it comes
// from the control plane configuration, never from the agent.
func New(agents []Agent, svc *service.Service, lease time.Duration) http.Handler {
	if lease <= 0 {
		lease = DefaultLease
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "video-auto-cut", Version: "0.1.0"}, &mcp.ServerOptions{
		SupportedProtocolVersions: []string{"2026-07-28"},
	})
	registerTools(server, svc)
	registerTaskTools(server, svc, lease)
	base := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origin is not allowed", http.StatusForbidden)
			return
		}
		// Tool arguments are small; a large body is either a bug or abuse.
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authorization, "Bearer ")
		if len(token) < 32 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		hash := sha256.Sum256([]byte(token))
		for _, a := range agents {
			stored, _ := hex.DecodeString(a.TokenSHA256)
			if len(stored) == sha256.Size && subtle.ConstantTimeCompare(stored, hash[:]) == 1 {
				base.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, a)))
				return
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

type assetArgs struct {
	AssetID string `json:"asset_id" jsonschema:"asset identifier"`
}
type submitArgs struct {
	TaskID    string            `json:"task_id" jsonschema:"task identifier"`
	Artifacts map[string]string `json:"artifacts" jsonschema:"result artifact references"`
}
type assetsResult struct {
	Assets []model.Asset `json:"assets"`
}
type assetResult struct {
	Asset model.Asset `json:"asset"`
}
type submitResult struct {
	Submitted bool `json:"submitted"`
}

func registerTools(server *mcp.Server, svc *service.Service) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_assets", Description: "List assets visible to this agent role"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, assetsResult, error) {
		a := ctx.Value(identityKey{}).(Agent)
		assets, err := svc.ListVisibleAssets(ctx, a.Role)
		return nil, assetsResult{Assets: assets}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "get_asset", Description: "Get one asset visible to this agent role"}, func(ctx context.Context, _ *mcp.CallToolRequest, args assetArgs) (*mcp.CallToolResult, assetResult, error) {
		a := ctx.Value(identityKey{}).(Agent)
		assets, err := svc.ListVisibleAssets(ctx, a.Role)
		if err != nil {
			return nil, assetResult{}, err
		}
		for _, asset := range assets {
			if asset.AssetID == args.AssetID {
				return nil, assetResult{Asset: asset}, nil
			}
		}
		return nil, assetResult{}, model.ErrNotFound
	})
	mcp.AddTool(server, &mcp.Tool{Name: "submit_result", Description: "Submit artifacts for a task leased to this agent"}, func(ctx context.Context, _ *mcp.CallToolRequest, args submitArgs) (*mcp.CallToolResult, submitResult, error) {
		a := ctx.Value(identityKey{}).(Agent)
		if err := requireTaskID(args.TaskID); err != nil {
			return nil, submitResult{}, err
		}
		if err := svc.SubmitResult(ctx, a.ID, args.TaskID, args.Artifacts); err != nil {
			return nil, submitResult{}, toolError(err)
		}
		return nil, submitResult{Submitted: true}, nil
	})
}
