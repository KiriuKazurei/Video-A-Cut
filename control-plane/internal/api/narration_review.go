package api

import (
	"net/http"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// narrationReviewRequest is the body of POST /api/assets/{id}/narration-reviews.
//
// The handler does not accept a client-supplied hash. The service hashes the
// text, window and source, so an approval cannot be pointed at a digest the
// plane never saw.
type narrationReviewRequest struct {
	Text   string  `json:"text"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Source string  `json:"source"`
}

type narrationReviewResponse struct {
	AssetID   string `json:"asset_id"`
	DraftHash string `json:"draft_hash"`
}

func (s *Server) approveNarration(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var req narrationReviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	hash, err := s.svc.ApproveNarration(r.Context(), humanActor, id, service.NarrationDraft{
		Text: req.Text, Start: req.Start, End: req.End, Source: req.Source,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, narrationReviewResponse{AssetID: id, DraftHash: hash})
}

func (s *Server) revokeNarration(w http.ResponseWriter, r *http.Request) {
	id, ok := assetID(w, r.PathValue("id"))
	if !ok {
		return
	}
	hash := strings.ToLower(r.PathValue("hash"))
	if err := s.svc.RevokeNarration(r.Context(), humanActor, id, hash); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, narrationReviewResponse{AssetID: id, DraftHash: hash})
}

func assetID(w http.ResponseWriter, id string) (string, bool) {
	if id == "" {
		writeError(w, http.StatusBadRequest, codeArgument, "asset id is required")
		return "", false
	}
	if strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusBadRequest, codeArgument, "asset id must not contain a path separator")
		return "", false
	}
	return id, true
}
