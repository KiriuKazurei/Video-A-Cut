// Package api is the HTTP transport of the control plane: it owns the URLs,
// the request decoding and the response encoding, and nothing else.
//
// Every rule — visibility, state machines, ownership, audit — lives in
// internal/service, and this package calls it and reports the outcome. A
// handler that decides a rule would create a second copy of it, which is the
// failure mode docs/项目开发文档.md §11.2 warns about: two filters drift apart
// the first time the rules change.
//
// Only the standard library net/http and encoding/json are used. There is no
// framework, so the handler tree is plain Go and inspectable in one read.
package api

import (
	"bytes"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Server is the HTTP surface of the control plane.
//
// It holds only its collaborators: the service holds the rules and the store,
// the bus holds the SSE fan-out. A Server is safe for concurrent use because
// net/http.ServeMux and service.Service are.
type Server struct {
	// svc is the business-logic facade every handler delegates to. It is
	// never nil in practice — New requires it — but a nil one is safe to
	// construct for tests that only exercise routing.
	svc *service.Service

	// mux carries the routing table. It is kept rather than turned into an
	// anonymous http.HandlerFunc so ServeMux's per-method Allow reporting,
	// pattern matching and ServeHTTP implementation are reused instead of
	// reimplemented: those edges (trailing slashes, method routing, 404 vs
	// 405) are the ones a hand-rolled router gets wrong.
	mux *http.ServeMux
}

// New returns a Server whose handlers delegate to svc.
//
// The routing table is registered here, once, instead of inside Handler, so
// the URL surface is fixed at construction time and the http.Handler is a
// cheap accessor a server (or a test) can request repeatedly.
func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Error codes. These are the stable machine-readable values a client may
// branch on; the human message is allowed to change and to carry detail a
// client must not parse.
const (
	codeNotFound             = "not_found"
	codeArgument             = "invalid_argument"
	codeForbidden            = "forbidden"
	codeConflict             = "conflict"
	codeInvalidState         = "invalid_state"
	codeLeaseExpired         = "lease_expired"
	codeLeaseHeld            = "lease_held"
	codeResourceBusy         = "resource_busy"
	codeStaleExecution       = "stale_execution"
	codeBodyTooLarge         = "payload_too_large"
	codeInternal             = "internal_error"
	codeMethodNotAllowed     = "method_not_allowed"
	codeUnsupportedMediaType = "unsupported_media_type"
)

// routes registers every documented URL (docs/项目开发文档.md §9.1).
//
// The whole surface hangs off /api/ so a static frontend can be served from
// the same origin with a single prefix carve-out rather than per-route CORS
// rules. Each entry is one method on one path: PATCH and GET on
// /api/assets/{id} are separate registrations because ServeMux matches on
// method, and splitting them is what produces a correct Allow header on a
// method mismatch.
//
// Until a handler exists it answers 501 not_implemented through the same
// envelope as every other failure, so a client integrating against this
// server sees one error shape from day one and a later task only replaces the
// response behind the status.
func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/providers/{operation}", s.diagnoseProvider)
	s.mux.HandleFunc("GET /api/processing-profiles", s.listProfiles)
	s.mux.HandleFunc("POST /api/processing-profiles", s.saveProfile)
	s.mux.HandleFunc("GET /api/processing-profiles/{id}/revisions/{revision}", s.getProfile)
	s.mux.HandleFunc("POST /api/processing-profiles/{id}/revisions", s.saveProfile)
	s.mux.HandleFunc("POST /api/processing-profiles/{id}/revisions/{revision}/external-consent", s.profileConsent)
	s.mux.HandleFunc("DELETE /api/processing-profiles/{id}/revisions/{revision}/external-consent", s.profileConsent)
	s.mux.HandleFunc("POST /api/assets/{id}/prepared-preflight", s.preparedPreflight)
	s.mux.HandleFunc("POST /api/assets/{id}/prepared-workflows", s.startPrepared)
	s.mux.HandleFunc("POST /api/assets/{id}/workflow-preflight", s.preflightWorkflow)
	s.mux.HandleFunc("GET /api/workflows/{run}/evidence/{key}", s.workflowEvidence)
	s.mux.HandleFunc("GET /api/workflows/{run}/delivery.zip", s.workflowZIP)
	s.mux.HandleFunc("GET /api/ingest-roots", s.listIngestRoots)
	s.mux.HandleFunc("POST /api/recordings", s.registerRecording)
	s.mux.HandleFunc("GET /api/assets/{id}/recordings", s.listRecordings)
	s.mux.HandleFunc("POST /api/assets/{id}/ingest-runs", s.startIngest)
	s.mux.HandleFunc("GET /api/assets/{id}/ingest-runs", s.listIngestRuns)
	s.mux.HandleFunc("GET /api/ingest-runs/{run}", s.getIngestRun)
	s.mux.HandleFunc("GET /api/ingest-runs/{run}/segments", s.listIngestSegments)
	s.mux.HandleFunc("POST /api/ingest-runs/{run}/analysis-plans", s.postAnalysisPlan)
	s.mux.HandleFunc("POST /api/ingest-runs/{run}/selection-revisions", s.postSelection)
	s.mux.HandleFunc("POST /api/ingest-runs/{run}/prepare", s.postPrepare)
	s.mux.HandleFunc("POST /api/ingest-runs/{run}/cancel", s.cancelIngest)
	s.mux.HandleFunc("POST /api/ingest-runs/{run}/retry", s.retryIngest)
	s.mux.HandleFunc("GET /api/ingest-runs/{run}/files/{key}", s.getIngestFile)
	s.mux.HandleFunc("GET /api/assets", s.listAssets)
	s.mux.HandleFunc("GET /api/assets/{id}", s.getAsset)
	s.mux.HandleFunc("PATCH /api/assets/{id}", s.patchAsset)
	s.mux.HandleFunc("POST /api/assets/{id}/narration-reviews", s.approveNarration)
	s.mux.HandleFunc("DELETE /api/assets/{id}/narration-reviews/{hash}", s.revokeNarration)
	s.mux.HandleFunc("POST /api/assets/{id}/workflows", s.startWorkflow)
	s.mux.HandleFunc("GET /api/assets/{id}/workflows", s.listWorkflows)
	s.mux.HandleFunc("GET /api/workflows/{run}", s.getWorkflow)
	s.mux.HandleFunc("POST /api/workflows/{run}/cancel", s.cancelWorkflow)
	s.mux.HandleFunc("POST /api/workflows/{run}/retry", s.retryWorkflow)
	s.mux.HandleFunc("GET /api/workflows/{run}/review", s.reviewWorkflow)
	s.mux.HandleFunc("POST /api/workflows/{run}/revisions", s.editWorkflow)
	s.mux.HandleFunc("POST /api/workflows/{run}/scene-reviews", s.reviewScene)
	s.mux.HandleFunc("POST /api/workflows/{run}/narration-reviews", s.reviewNarration)
	s.mux.HandleFunc("DELETE /api/workflows/{run}/narration-reviews/{narration}", s.revokeWorkflowNarration)
	s.mux.HandleFunc("GET /api/workflows/{run}/acceptance", s.getAcceptance)
	s.mux.HandleFunc("POST /api/workflows/{run}/acceptance", s.postAcceptance)
	s.mux.HandleFunc("POST /api/tasks", s.createTask)
	s.mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	s.mux.HandleFunc("GET /api/events", s.streamEvents)
	s.mux.HandleFunc("GET /api/audit", s.listAudit)
	s.mux.HandleFunc("POST /api/deliveries/import", s.importDelivery)
	s.mux.HandleFunc("POST /api/assets/{id}/reopen", s.reopenAsset)
	s.mux.HandleFunc("GET /api/assets/{id}/files", s.listDeliveryFiles)
	s.mux.HandleFunc("GET /api/assets/{id}/files/{key}", s.getDeliveryFile)
	s.mux.HandleFunc("GET /api/assets/{id}/delivery.zip", s.downloadDelivery)
}

// Handler returns the http.Handler to mount. It is the process's single entry
// point into this package; everything else is internals.
//
// The mux is wrapped in wrapJSONErrors so two ordinary net/http behaviours are
// replaced with JSON ones: a panicking handler and ServeMux's own plain-text
// 404 and 405 answers. Without the wrapper a client would receive a text/plain
// error body for every unmatched path and every wrong method, which is the
// case exactly when the caller cannot tell what went wrong.
func (s *Server) Handler() http.Handler { return localRequestGate(wrapJSONErrors(s.mux)) }

// localRequestGate protects this unauthenticated local control surface from
// DNS-rebinding and cross-origin browser writes. Requests without Origin are
// retained for command-line clients; browser requests must match the exact
// scheme and authority of the loopback request.
func localRequestGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackAuthority(r.Host) {
			writeError(w, http.StatusForbidden, codeForbidden, "request Host must be a loopback address")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !sameOriginRequest(origin, r) {
			writeError(w, http.StatusForbidden, codeForbidden, "cross-origin requests are not allowed")
			return
		}
		if requestHasJSONBody(r) {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !strings.EqualFold(mediaType, "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
					"request body must use application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func requestHasJSONBody(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		return false
	}
	return r.ContentLength != 0
}

func isLoopbackAuthority(authority string) bool {
	if authority == "" {
		return false
	}
	host := authority
	port := ""
	if strings.HasPrefix(authority, "[") {
		parsed, parsedPort, err := net.SplitHostPort(authority)
		if err == nil {
			host, port = parsed, parsedPort
		} else if strings.HasSuffix(authority, "]") {
			host = strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")
		} else {
			return false
		}
	} else if strings.Count(authority, ":") == 1 {
		parsed, parsedPort, err := net.SplitHostPort(authority)
		if err != nil {
			return false
		}
		host, port = parsed, parsedPort
	} else if strings.Contains(authority, ":") {
		return false
	}
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 0 || p > 65535 {
			return false
		}
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOriginRequest(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return false
	}
	wantScheme := "http"
	if r.TLS != nil {
		wantScheme = "https"
	}
	return strings.EqualFold(u.Scheme, wantScheme) && strings.EqualFold(u.Host, r.Host) && isLoopbackAuthority(u.Host)
}

// wrapJSONErrors translates net/http's own failure answers into the JSON
// envelope, and contains a handler panic.
//
// ServeMux answers 404 for an unmatched path and 405 for a matched path with
// the wrong method, both as plain text and both through
// http.Error(w, "404 page not found", 404). Neither can be intercepted by
// watching the writer's status: by the time ServeMux returns, the body has
// already reached the client.
//
// So the capture writer holds the whole answer back — its own header map, its
// status, its body — and the wrapper decides once ServeMux has returned. A
// 404 or 405 is rewritten as the envelope; anything else is forwarded exactly
// as it was written. For a 405 the Allow header ServeMux computed is copied
// across, because that header is the one thing the plain-text answer said that
// a client can act on.
//
// The panic recovery is what keeps one bad handler from taking the process
// down: net/http already recovers per connection, but it does so by logging
// and dropping the connection, which leaves the client with a truncated
// response and no status at all.
//
// Router failures are identified by ServeMux's exact default body, with the
// frozen Allow header required for 405. Handler-level JSON errors keep their
// own response even when their status is also 404.
func wrapJSONErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &captureWriter{ResponseWriter: w}
		defer rec.discard()
		defer func() {
			if p := recover(); p != nil {
				// A handler that already streamed bytes cannot be answered
				// again: a second header write is dropped by net/http and
				// leaves the client with a body it cannot parse. Writing the
				// envelope is only safe while nothing has gone out.
				if !rec.released {
					writeError(w, http.StatusInternalServerError, codeInternal, msgInternal)
				}
			}
		}()

		next.ServeHTTP(rec, r)

		// Nothing to translate once the handler produced a real answer:
		// rewriting it would duplicate a response already decided.
		//
		// The exact body emitted by ServeMux distinguishes an unmatched
		// route from a handler-level 404 for a missing row.
		switch {
		case isRouterAnswer(rec):
			// ServeMux's own plain-text answer is dropped. Allow survives:
			// it is the operational information a 405 exists to deliver,
			// and it was computed by the router, not by the error writer.
			for _, v := range rec.committedHeaders().Values("Allow") {
				w.Header().Add("Allow", v)
			}
			if rec.status == http.StatusNotFound {
				writeError(w, http.StatusNotFound, codeNotFound,
					fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
				return
			}
			writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed,
				fmt.Sprintf("method %s is not allowed for %s", r.Method, r.URL.Path))
		default:
			rec.release()
		}
	})
}

// isRouterAnswer recognizes the exact default body and headers emitted by
// this ServeMux for an unmatched path or method.
func isRouterAnswer(rec *captureWriter) bool {
	switch rec.status {
	case http.StatusNotFound:
		return rec.buf.String() == "404 page not found\n"
	case http.StatusMethodNotAllowed:
		return rec.buf.String() == "Method Not Allowed\n" &&
			len(rec.committedHeaders().Values("Allow")) > 0
	default:
		return false
	}
}

// captureWriter holds back a response so the wrapper can decide whether to
// translate it, and forwards it unchanged when it should not.
//
// The header map is a copy made on first use, never the wrapped writer's:
// ServeMux's http.Error writes "Content-Type: text/plain" through this map
// before it calls WriteHeader, and if the map were the real one the client
// would already have a text/plain body committed by the time the wrapper runs.
//
// The body is buffered for the same reason. FlushError releases the captured
// response when ResponseController flushes, preserving the streaming behavior
// and the underlying transport's flush result.
type captureWriter struct {
	http.ResponseWriter

	// hdr is the handler's view of the response headers. A copy is taken so
	// nothing written through it reaches the client before release.
	hdr http.Header
	// committedHeader freezes the header values at the first status or body
	// write, matching net/http's commit semantics despite buffering.
	committedHeader http.Header

	// status is the code WriteHeader recorded; 0 means none was written.
	status int
	// buf holds the body while the response is held back.
	buf bytes.Buffer
	// released marks the response as already on the wire: the panic
	// recovery must not answer it a second time.
	released bool
}

// Header returns the copy this writer's handlers write into.
func (c *captureWriter) Header() http.Header {
	if c.hdr == nil {
		c.hdr = c.ResponseWriter.Header().Clone()
	}
	return c.hdr
}

// WriteHeader records only the first status, matching ResponseWriter.
func (c *captureWriter) WriteHeader(status int) {
	if c.released {
		c.ResponseWriter.WriteHeader(status)
		return
	}
	if c.status == 0 {
		c.commit(status)
	}
}

// Write buffers the body while the response is held back.
func (c *captureWriter) Write(p []byte) (int, error) {
	if c.released {
		return c.ResponseWriter.Write(p)
	}
	if c.status == 0 {
		c.commit(http.StatusOK)
	}
	return c.buf.Write(p)
}

// Unwrap lets ResponseController reach optional transport controls such as
// SetWriteDeadline on the underlying server writer.
func (c *captureWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// FlushError releases buffered headers/body and preserves a transport flush
// error. ResponseController uses this method when present.
func (c *captureWriter) FlushError() error {
	if err := c.release(); err != nil {
		return err
	}
	return http.NewResponseController(c.ResponseWriter).Flush()
}

func (c *captureWriter) commit(status int) {
	c.status = status
	c.committedHeader = c.Header().Clone()
}

func (c *captureWriter) committedHeaders() http.Header {
	if c.committedHeader != nil {
		return c.committedHeader
	}
	return c.Header()
}

// release writes the held response to the client exactly as it was captured.
//
// A status that was never written defaults to 200, which is what
// ResponseWriter does on the first Write with no WriteHeader.
func (c *captureWriter) release() error {
	if c.released {
		return nil
	}
	c.released = true

	h := c.ResponseWriter.Header()
	for k := range h {
		delete(h, k)
	}
	committed := c.committedHeader
	if committed == nil {
		committed = c.Header().Clone()
	}
	for k, vs := range committed {
		h[k] = append([]string(nil), vs...)
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	c.ResponseWriter.WriteHeader(c.status)
	_, err := c.ResponseWriter.Write(c.buf.Bytes())
	return err
}

// discard abandons a held response. It is what a translated 404 or 405 leaves
// behind: the buffered body was ServeMux's plain text and is simply not
// forwarded.
func (c *captureWriter) discard() {
	c.released = true
}

// listAssets answers GET /api/assets with every asset the plane holds.
//
// It is the human governance view, so it calls service.ListAllAssets and
// never ListVisibleAssets. §11.2 makes the latter the single decision point
// of "what may an agent see"; filtering here instead would create the second
// copy of that rule the docs warn about, and a governance screen that hid
// agent-invisible or locked rows would hide exactly the rows an operator
// opens this list to change.
//
// The array is encoded straight through as the response body: the service
// already guarantees a non-nil slice, which is what makes an empty store
// encode as [] rather than null.
func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.svc.ListAllAssets(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

// getAsset answers GET /api/assets/{id} with one asset, governance view: no
// visibility filtering, because the WebUI is already the human side of §11.3.
//
// The id is rejected before it reaches the store if it carries a path
// separator. r.PathValue returns the URL-decoded segment, so an escaped
// separator (a%2Fb) decodes to "a/b" and reaches this handler rather than
// being split into two segments by the router. The store would look such a
// value up as a literal and return not found, but answering 400 says the
// truth: this is a malformed id, not a missing one, and a caller that sees a
// 404 has no way to learn the difference. Keeping the separator out of the
// query also means no value derived from the URL can ever be assembled into
// a path.
func (s *Server) getAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, codeArgument, "asset id is required")
		return
	}
	// Both separators are checked, not just "/": "\\" separates path
	// components on Windows, and an id carrying either is equally malformed.
	if strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusBadRequest, codeArgument,
			"asset id must not contain a path separator")
		return
	}

	asset, err := s.svc.GetAsset(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

// writeError is the single place an error body is formatted, so the envelope
// cannot drift between handlers.
//
// The message is written verbatim: callers pass either a hand-written string
// or one wrapped by the service. Unclassified service failures are mapped by
// writeServiceError, which strips their detail; this function is the low-level
// writer and is not expected to make that judgement.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorEnvelope{
		Error: apiErrorBody{Code: code, Message: message},
	})
}

// writeServiceError maps one error from the service layer onto the HTTP
// response, and is the only writer a handler should use for them.
//
// The mapping is one sentinel to one status and one code, so a client can
// rely on the code while a server-side message changes.
//
// errors.Is against the sentinels is the matching primitive, because every
// layer between here and the returning function has wrapped its error with
// fmt.Errorf("%w"): the outermost string is for the log, the sentinel is for
// the protocol, and a client would get a 500 the day somebody added a wrap
// hop if the mapping compared strings or types instead.
//
// An unclassified error is answered with the generic body and no detail. Its
// message can name database paths, SQL and hostnames, and that is operational
// information for the log, not for the caller.
func writeServiceError(w http.ResponseWriter, err error) {
	writeError(w, statusFor(err), codeFor(err), messageFor(err))
}
