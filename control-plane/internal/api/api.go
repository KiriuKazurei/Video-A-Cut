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
	"net/http"
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
	codeNotFound         = "not_found"
	codeArgument         = "invalid_argument"
	codeForbidden        = "forbidden"
	codeConflict         = "conflict"
	codeInvalidState     = "invalid_state"
	codeLeaseExpired     = "lease_expired"
	codeLeaseHeld        = "lease_held"
	codeBodyTooLarge     = "payload_too_large"
	codeInternal         = "internal_error"
	codeMethodNotAllowed = "method_not_allowed"
	codeNotImplemented   = "not_implemented"
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
	s.mux.HandleFunc("GET /api/assets", s.listAssets)
	s.mux.HandleFunc("GET /api/assets/{id}", s.getAsset)
	s.mux.HandleFunc("PATCH /api/assets/{id}", s.patchAsset)
	s.mux.HandleFunc("POST /api/tasks", s.notImplementedYet("create task"))
	s.mux.HandleFunc("GET /api/tasks/{id}", s.notImplementedYet("get task"))
	s.mux.HandleFunc("GET /api/events", s.notImplementedYet("event stream"))
	s.mux.HandleFunc("GET /api/audit", s.notImplementedYet("audit log"))
}

// Handler returns the http.Handler to mount. It is the process's single entry
// point into this package; everything else is internals.
//
// The mux is wrapped in wrapJSONErrors so two ordinary net/http behaviours are
// replaced with JSON ones: a panicking handler and ServeMux's own plain-text
// 404 and 405 answers. Without the wrapper a client would receive a text/plain
// error body for every unmatched path and every wrong method, which is the
// case exactly when the caller cannot tell what went wrong.
func (s *Server) Handler() http.Handler { return wrapJSONErrors(s.mux) }

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
// Two documented behaviours are relied on here, which is why they are worth
// stating: ServeMux sets "Content-Type: text/plain; charset=utf-8" for its own
// 404 and 405 and nothing else does, so the two statuses are identified by
// status alone, and the standard server's own error body is discarded rather
// than forwarded.
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
		switch rec.status {
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			// ServeMux's own plain-text answer is dropped. Allow survives:
			// it is the operational information a 405 exists to deliver,
			// and it was computed by the router, not by the error writer.
			for _, v := range rec.Header().Values("Allow") {
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

// captureWriter holds back a response so the wrapper can decide whether to
// translate it, and forwards it unchanged when it should not.
//
// The header map is a copy made on first use, never the wrapped writer's:
// ServeMux's http.Error writes "Content-Type: text/plain" through this map
// before it calls WriteHeader, and if the map were the real one the client
// would already have a text/plain body committed by the time the wrapper runs.
//
// The body is buffered for the same reason. Flush bypasses the buffer, so a
// streaming handler (the SSE endpoint of a later task) is not turned into a
// slow one: the first flush releases the header and commits the response.
type captureWriter struct {
	http.ResponseWriter

	// hdr is the handler's view of the response headers. A copy is taken so
	// nothing written through it reaches the client before release.
	hdr http.Header

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

// WriteHeader records the status and holds the response back.
func (c *captureWriter) WriteHeader(status int) { c.status = status }

// Write buffers the body while the response is held back.
func (c *captureWriter) Write(p []byte) (int, error) {
	if c.released {
		return c.ResponseWriter.Write(p)
	}
	return c.buf.Write(p)
}

// Flush releases the response and flushes the wrapped writer, so a streaming
// handler keeps working through this wrapper.
func (c *captureWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		c.release()
		f.Flush()
	}
}

// release writes the held response to the client exactly as it was captured.
//
// A status that was never written defaults to 200, which is what
// ResponseWriter does on the first Write with no WriteHeader.
func (c *captureWriter) release() {
	if c.released {
		return
	}
	c.released = true

	h := c.ResponseWriter.Header()
	for k, vs := range c.hdr {
		h[k] = vs
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	c.ResponseWriter.WriteHeader(c.status)
	_, _ = c.ResponseWriter.Write(c.buf.Bytes())
}

// discard abandons a held response. It is what a translated 404 or 405 leaves
// behind: the buffered body was ServeMux's plain text and is simply not
// forwarded.
func (c *captureWriter) discard() {
	c.released = true
}

// notImplementedYet returns a handler that reports the operation by
// name with codeNotImplemented and HTTP 501.
//
// A registered route answering 501 rather than a bare 404 matters to a
// WebUI: 404 means "this URL will never exist", while 501 means "this URL is
// the right one and the plane does not speak it yet", which is the case here
// — the skeleton deliberately registers the surface ahead of the handlers.
func (s *Server) notImplementedYet(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, codeNotImplemented,
			fmt.Sprintf("operation %q is not implemented yet", operation))
	}
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
