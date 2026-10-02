package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// msgInternal is the message an unclassified failure is reported with. The
// real error goes to the server log only.
const msgInternal = "internal error"

// maxBodyBytes caps a request body.
//
// The plane has no upload endpoint: the largest legitimate payload is a
// governance PATCH carrying three booleans. The cap exists so a misdirected
// multipart upload cannot be buffered whole, and it is read via
// http.MaxBytesReader so an oversized request is cut off by the reader rather
// than accumulated and rejected after the fact.
const maxBodyBytes = 1 << 20 // 1 MiB

// errBodyTooLarge is this package's own sentinel for a rejected oversize
// body. It exists because the cap is a transport rule and the service has no
// vocabulary for it, yet it must map to 413 rather than 400 or 500.
var errBodyTooLarge = errors.New("request body too large")

// apiErrorEnvelope is the top-level shape of every error response: one object
// under one "error" key.
//
// The wrapper is what makes the body extensible — a later version can add a
// sibling key ("request_id", "hint") without changing the meaning of an error
// response a client already parses, which a bare {code,message} object cannot
// offer.
type apiErrorEnvelope struct {
	Error apiErrorBody `json:"error"`
}

// apiErrorBody is the error object inside the response envelope.
//
// Code is the stable, machine-readable value; Message is for humans and may
// carry server-side detail, so clients must never parse it.
type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON writes v as the response with JSON Content-Type.
//
// The value is encoded into a buffer before the header is written: a partial
// write would otherwise leave a 200 status and a half-written body, which no
// client can parse and no log can explain. Encoding to memory first turns an
// impossible payload into a clean 500 through the same envelope.
//
// X-Content-Type-Options: nosniff is set because a JSON body served without
// it can be content-sniffed into something script-bearing by an old browser,
// which is a real hazard on a surface that holds a WebUI on the same origin.
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiErrorEnvelope{
			Error: apiErrorBody{Code: codeInternal, Message: msgInternal},
		})
		return
	}

	// json.Encoder terminates every value with a newline, which is a
	// delimiter for a stream of JSON documents and wrong for a single
	// response body: a client comparing bytes (and some strict parsers)
	// sees trailing whitespace as part of the value.
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))

	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// decodeJSON reads one JSON object from r into v.
//
// It is the readers' gatekeeper, shared by every handler rather than each
// writing its own json.NewDecoder(r.Body).Decode(v) loop, for three reasons
// that matter more here than in a typical CRUD app:
//
//   - DisallowUnknownFields rejects a field a later version of the client
//     invented. Silently ignoring it would report success for a request whose
//     payload was largely not understood.
//   - A trailing second value is refused. json.Decoder stops after the first
//     value, so a body like {"a":1}{"b":2} would decode and look valid.
//   - The size cap is enforced by MaxBytesReader, so the decoder — not the
//     handler — is the thing that notices.
//
// An empty body is model.ErrArgument, not a silent no-op: a PATCH or POST
// with nothing in it is a client bug, and answering 400 is what tells the
// client so. The writer is taken so the handler does not have to, and so the
// status is written in one place.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(body)
	var raw json.RawMessage

	if err := dec.Decode(&raw); err != nil {
		if isBodyTooLarge(err) {
			return fmt.Errorf("api: body exceeds %d bytes: %w", maxBodyBytes, errBodyTooLarge)
		}
		if isEOF(err) {
			return fmt.Errorf("api: empty request body: %w", model.ErrArgument)
		}
		return fmt.Errorf("api: malformed JSON body: %w", model.ErrArgument)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("api: request body must be a JSON object: %w", model.ErrArgument)
	}
	strict := json.NewDecoder(bytes.NewReader(trimmed))
	strict.DisallowUnknownFields()
	if err := strict.Decode(v); err != nil {
		return fmt.Errorf("api: malformed JSON body: %w", model.ErrArgument)
	}

	// A second value must not exist: refuse it rather than let the first
	// object stand in for the whole request. It is a client bug, so it maps
	// to 400 exactly like a malformed one does.
	var trailing json.RawMessage
	err := dec.Decode(&trailing)
	if isBodyTooLarge(err) {
		return fmt.Errorf("api: body exceeds %d bytes: %w", maxBodyBytes, errBodyTooLarge)
	}
	if !isEOF(err) {
		return fmt.Errorf("api: body must hold exactly one JSON object: %w", model.ErrArgument)
	}
	return nil
}

// statusFor maps one error onto an HTTP status.
//
// The mapping is total: an error no sentinel claims is an internal failure,
// because the transport cannot tell a bug in the service from a dead database
// and must not answer 400 for either and mislead a client into retrying.
func statusFor(err error) int {
	switch {
	case errors.Is(err, model.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, model.ErrArgument):
		return http.StatusBadRequest
	case errors.Is(err, model.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, model.ErrResourceBusy),
		errors.Is(err, model.ErrStaleExecution),
		errors.Is(err, model.ErrConflict),
		errors.Is(err, model.ErrInvalidState),
		errors.Is(err, model.ErrLeaseExpired),
		errors.Is(err, model.ErrLeaseHeld):
		return http.StatusConflict
	case errors.Is(err, errBodyTooLarge):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

// codeFor maps one error onto the stable error code of the envelope.
func codeFor(err error) string {
	switch {
	case errors.Is(err, model.ErrNotFound):
		return codeNotFound
	case errors.Is(err, model.ErrArgument):
		return codeArgument
	case errors.Is(err, model.ErrForbidden):
		return codeForbidden
	case errors.Is(err, model.ErrResourceBusy):
		return codeResourceBusy
	case errors.Is(err, model.ErrStaleExecution):
		return codeStaleExecution
	case errors.Is(err, model.ErrConflict):
		return codeConflict
	case errors.Is(err, model.ErrInvalidState):
		return codeInvalidState
	case errors.Is(err, model.ErrLeaseExpired):
		return codeLeaseExpired
	case errors.Is(err, model.ErrLeaseHeld):
		return codeLeaseHeld
	case errors.Is(err, errBodyTooLarge):
		return codeBodyTooLarge
	default:
		return codeInternal
	}
}

// messageFor decides whether the error's own message is safe to echo.
//
// A sentinel error carries a wrapped sentence someone wrote about this exact
// failure — "service: approve asset a_1: not found" — and echoing it tells the
// caller which entity failed without telling them anything about the server.
// An unclassified error can name file paths, SQL and hostnames, so it is
// replaced with msgInternal and its detail is left for the server log.
func messageFor(err error) string {
	if errors.Is(err, errBodyTooLarge) {
		return fmt.Sprintf("request body exceeds %d bytes", maxBodyBytes)
	}
	if isClassified(err) {
		return err.Error()
	}
	return msgInternal
}

// isClassified reports whether statusFor would claim err as a known failure.
func isClassified(err error) bool {
	return statusFor(err) != http.StatusInternalServerError ||
		errors.Is(err, errBodyTooLarge)
}

// isBodyTooLarge reports the http.MaxBytesReader reason for oversize bodies.
//
// The standard answer is a *http.MaxBytesError, but the check falls back to
// the documented string so a wrapped or older presentation of the same
// failure still maps to 413 instead of a 400.
func isBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return true
	}
	return err.Error() == "http: request body too large"
}

// isEOF reports a clean end of input, which for a request body means the
// client sent an empty one.
func isEOF(err error) bool { return errors.Is(err, io.EOF) }
