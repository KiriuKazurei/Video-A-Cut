package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// defaultAuditLimit bounds how many audit rows GET /api/audit returns when the
// caller does not ask for a specific page.
//
// The number has to be chosen against two facts that pull in opposite
// directions. The log is a governance view an operator scrolls through, so a
// page of 20 would hide recent history behind a 「load more」 click nobody
// documents; and the endpoint has no access control of its own yet — the
// surface listens on localhost until Phase 3 adds auth — so the bounded page
// is what keeps an unbounded scan of the whole audit table off this URL.
//
// 200 is the point where both lose something acceptable: a governance screen
// renders it in one list without pagination, and the response stays a size a
// browser handles without streaming.
//
// Passing this value down rather than a non-positive one is deliberate. The
// store normalises a non-positive limit to its own default of 100, which is a
// page size chosen for internal callers; if this endpoint forwarded a zero,
// the size of its response would become a detail of the storage layer instead
// of a decision of the surface that has to render it.
const defaultAuditLimit = 200

// maxAuditLimit caps an explicitly requested page.
//
// Without it a single request could name a limit larger than the table, which
// is not a read the plane owes any one client: an operator reading a week of
// history through a paging UI asks for 50 at a time, and a value that empty the
// store into one response is how a governance screen becomes a denial of
// service against itself.
const maxAuditLimit = 1000

// listAudit answers GET /api/audit with the audit log, newest first.
//
// This is the human governance view of what happened to the plane, so it calls
// service.ListAudit and applies no rule of its own — no visibility filter, no
// actor filter, no row is dropped. §11.2 makes any filtering decision
// belong to the service layer: a second filter here would drift from it the
// first time the rules change, and a governance screen that hid agent-side
// rows would hide precisely the rows an operator opens this endpoint to audit.
// The rows report their own actor ("system", "human:webui", "agent:<id>"), and
// the prefix is left intact for the same reason the retention sweep reads it:
// it is what tells a machine action from a person's.
//
// The optional limit query parameter is paging, and a limit that is not a
// positive integer is refused with invalid_argument rather than silently
// replaced by the default. A client that typed ?limit=abc, ?limit=0 or
// ?limit=-5 has a bug, and answering 200 with a page it did not ask for
// teaches it that the parameter does nothing.
//
// The array is encoded straight through as the response body: the store always
// builds a non-nil slice, which is what makes an empty log encode as []
// rather than null, and an envelope would put the list behind a key §9.1
// documents nowhere.
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	// The key is looked up rather than defaulted on absence, because a
	// present-but-empty parameter (?limit=) is a client bug, not an omitted
	// one: the caller named the parameter and then sent nothing for it.
	// Trimmed rather than parsed raw, because a space-padded "50" is still
	// the number 50.
	limit := defaultAuditLimit
	if raw, ok := r.URL.Query()["limit"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(raw[0]))
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeArgument,
				fmt.Sprintf("limit must be a positive integer, got %q", raw[0]))
			return
		}
		if n > maxAuditLimit {
			n = maxAuditLimit
		}
		limit = n
	}

	rows, err := s.svc.ListAudit(r.Context(), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// The store returns a non-nil empty slice for an empty log, so nothing
	// here normalises a nil: encoding a nil slice would produce JSON null,
	// and every client would have to nil-check before it could range over
	// the response.
	if rows == nil {
		rows = []model.AuditLog{}
	}
	writeJSON(w, http.StatusOK, rows)
}
