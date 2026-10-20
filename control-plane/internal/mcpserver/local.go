package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const localSessionTTL = 30 * time.Minute
const maxLocalSessions = 256

type localSession struct {
	agent   Agent
	touched time.Time
}
type localConnections struct {
	mu       sync.Mutex
	sessions map[string]localSession
}

// Client names are reported metadata, never human identity or permissions.
// Session identifiers are automatically managed by the MCP transport; users
// neither register an Agent nor provision a token to connect to desktop MCP.
func (l *localConnections) serve(base http.Handler, svc *service.Service, w http.ResponseWriter, r *http.Request) {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !loopbackHost(peer) || !loopbackHost(r.Host) || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Host") != "" {
		http.Error(w, "local MCP requires a direct loopback connection", http.StatusForbidden)
		return
	}
	sessionID := r.Header.Get("Mcp-Session-Id")
	var raw []byte
	var method string
	var client struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	source := r.Header.Get("X-VAC-Agent-Source")
	if r.Method == http.MethodPost {
		raw, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid MCP body", http.StatusBadRequest)
			return
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				ClientInfo json.RawMessage            `json:"clientInfo"`
				Meta       map[string]json.RawMessage `json:"_meta"`
			} `json:"params"`
		}
		if json.Unmarshal(raw, &request) != nil {
			http.Error(w, "invalid MCP request", http.StatusBadRequest)
			return
		}
		method = request.Method
		info := request.Params.ClientInfo
		if len(info) == 0 {
			info = request.Params.Meta[mcp.MetaKeyClientInfo]
		}
		if len(info) != 0 && json.Unmarshal(info, &client) != nil {
			http.Error(w, "invalid clientInfo", http.StatusBadRequest)
			return
		}
		if source == "" {
			_ = json.Unmarshal(request.Params.Meta["vac/agentSource"], &source)
		}
	}
	l.mu.Lock()
	now := time.Now()
	for id, old := range l.sessions {
		if now.Sub(old.touched) > localSessionTTL {
			delete(l.sessions, id)
		}
	}
	session, exists := l.sessions[sessionID]
	if sessionID != "" && !exists {
		l.mu.Unlock()
		http.Error(w, "MCP session expired; reconnect and claim the run when its lease permits", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodDelete {
		if !exists {
			l.mu.Unlock()
			http.Error(w, "Mcp-Session-Id required", http.StatusBadRequest)
			return
		}
		delete(l.sessions, sessionID)
		l.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !exists {
		if method != "initialize" && method != "server/discover" {
			l.mu.Unlock()
			http.Error(w, "connect using MCP initialize or server/discover first", http.StatusBadRequest)
			return
		}
		if len(l.sessions) >= maxLocalSessions {
			l.mu.Unlock()
			http.Error(w, "too many MCP connections", http.StatusServiceUnavailable)
			return
		}
		var nonce [32]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			l.mu.Unlock()
			http.Error(w, "connection unavailable", http.StatusServiceUnavailable)
			return
		}
		sessionID = hex.EncodeToString(nonce[:])
		name := cleanClientText(client.Name, 128)
		if name == "" {
			name = "unknown-mcp-client"
		}
		if source == "" {
			source = r.UserAgent()
		}
		if source == "" {
			source = "streamable-http"
		}
		session.agent = Agent{ID: "local-" + hexDigest(nonce[:])[:24], Role: "narrator", Name: name, Version: cleanClientText(client.Version, 64), Source: cleanClientText(source, 256), Peer: peer, IdentityKind: "client_reported"}
	} else if (client.Name != "" && cleanClientText(client.Name, 128) != session.agent.Name) || (client.Version != "" && cleanClientText(client.Version, 64) != session.agent.Version) || (source != "" && cleanClientText(source, 256) != session.agent.Source) {
		l.mu.Unlock()
		http.Error(w, "MCP connection identity changed; reconnect", http.StatusBadRequest)
		return
	}
	session.touched = now
	l.sessions[sessionID] = session
	l.mu.Unlock()
	// Both modern and legacy SDKs propagate this header automatically. Never
	// log the header value or interpret a client-selected name as authentication.
	w.Header().Set("Mcp-Session-Id", sessionID)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	a := session.agent
	serveJournaledMCP(base, svc, a, w, r.WithContext(context.WithValue(r.Context(), identityKey{}, a)))
}

func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return net.ParseIP(strings.Trim(host, "[]")).IsLoopback()
}
func cleanClientText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}
