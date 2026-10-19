package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Separate from MCP bearer credentials. Never inherit this secret into workers.
// The UI can unlock an HttpOnly session; it must not persist the token in storage.
type humanSessions struct {
	mu     sync.Mutex
	tokens map[string]humanSession
	media  map[string]humanMediaTicket
}
type humanSession struct {
	Until   time.Time
	Binding [32]byte
}

type humanMediaTicket struct {
	Path    string
	Session humanSession
}

var skillHumanSessions = humanSessions{tokens: map[string]humanSession{}, media: map[string]humanMediaTicket{}}

func humanTokenOK(value string) bool {
	expected := os.Getenv("VAC_HUMAN_ACCESS_TOKEN")
	return len(expected) >= 32 && len(value) == len(expected) && subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}
func humanSkillRequest(r *http.Request) bool {
	if humanTokenOK(r.Header.Get("X-VAC-Human-Token")) {
		return true
	}
	cookie, e := r.Cookie("vac_human_session")
	if e != nil {
		return false
	}
	skillHumanSessions.mu.Lock()
	defer skillHumanSessions.mu.Unlock()
	session := skillHumanSessions.tokens[cookie.Value]
	expected := os.Getenv("VAC_HUMAN_ACCESS_TOKEN")
	return len(expected) >= 32 && time.Now().Before(session.Until) && session.Binding == sha256.Sum256([]byte(expected))
}
func (s *Server) humanSkillSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if e := decodeJSON(w, r, &req); e != nil {
		writeServiceError(w, e)
		return
	}
	if !humanTokenOK(req.Token) {
		writeError(w, http.StatusForbidden, "forbidden", "human access token required")
		return
	}
	var value [32]byte
	if _, e := rand.Read(value[:]); e != nil {
		writeError(w, 500, "internal_error", "session unavailable")
		return
	}
	token := hex.EncodeToString(value[:])
	skillHumanSessions.mu.Lock()
	for k, session := range skillHumanSessions.tokens {
		if time.Now().After(session.Until) {
			delete(skillHumanSessions.tokens, k)
		}
	}
	skillHumanSessions.tokens[token] = humanSession{Until: time.Now().Add(time.Hour), Binding: sha256.Sum256([]byte(req.Token))}
	skillHumanSessions.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "vac_human_session", Value: token, Path: "/api/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	writeJSON(w, 200, map[string]bool{"unlocked": true})
}
func protectHumanSkill(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		sensitive := strings.HasPrefix(path, "/api/agent-operations") || path == "/api/audit" || strings.HasPrefix(path, "/api/skill-runs") || strings.HasPrefix(path, "/api/projects") || (path == "/api/human-session" && r.Method == http.MethodDelete)
		mutation := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && (strings.HasPrefix(path, "/api/skill-runs") || strings.HasPrefix(path, "/api/projects") || strings.HasPrefix(path, "/api/skills"))
		if (sensitive || mutation) && !humanSkillRequest(r) && !humanMediaRequest(r) {
			writeError(w, http.StatusForbidden, "forbidden", "human session required; use /api/human-session")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) humanSkillLogout(w http.ResponseWriter, r *http.Request) {
	skillHumanSessions.mu.Lock()
	clear(skillHumanSessions.media)
	skillHumanSessions.mu.Unlock()
	if cookie, e := r.Cookie("vac_human_session"); e == nil {
		skillHumanSessions.mu.Lock()
		delete(skillHumanSessions.tokens, cookie.Value)
		skillHumanSessions.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "vac_human_session", Value: "", Path: "/api/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"locked": true})
}
func issueHumanMediaTicket(path string) (string, error) {
	var value [32]byte
	if _, e := rand.Read(value[:]); e != nil {
		return "", e
	}
	token := hex.EncodeToString(value[:])
	skillHumanSessions.mu.Lock()
	defer skillHumanSessions.mu.Unlock()
	for key, ticket := range skillHumanSessions.media {
		if time.Now().After(ticket.Session.Until) {
			delete(skillHumanSessions.media, key)
		}
	}
	if len(skillHumanSessions.media) >= 2048 {
		clear(skillHumanSessions.media)
	}
	skillHumanSessions.media[token] = humanMediaTicket{Path: path, Session: humanSession{Until: time.Now().Add(time.Hour), Binding: sha256.Sum256([]byte(os.Getenv("VAC_HUMAN_ACCESS_TOKEN")))}}
	return path + "?preview_ticket=" + token, nil
}
func humanMediaRequest(r *http.Request) bool {
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	token := r.URL.Query().Get("preview_ticket")
	if len(token) != 64 {
		return false
	}
	skillHumanSessions.mu.Lock()
	ticket, ok := skillHumanSessions.media[token]
	skillHumanSessions.mu.Unlock()
	expected := os.Getenv("VAC_HUMAN_ACCESS_TOKEN")
	return ok && ticket.Path == r.URL.Path && len(expected) >= 32 && time.Now().Before(ticket.Session.Until) && ticket.Session.Binding == sha256.Sum256([]byte(expected))
}
