package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Auto-run endpoints. The actor is always humanActor: an auto run is
// authorized, decided and revoked by the person at the UI, never by an agent.

// maxAutoIdempotencyKey bounds a client-supplied idempotency key. The key is
// stored and hashed per scope, so an unbounded one is storage the client
// controls.
const maxAutoIdempotencyKey = 128

func checkAutoIdempotencyKey(key string) error {
	if len(key) > maxAutoIdempotencyKey {
		return fmt.Errorf("api: idempotency_key is longer than %d bytes: %w", maxAutoIdempotencyKey, model.ErrArgument)
	}
	return nil
}

type autoRunRequest struct {
	ProfileID            string                   `json:"profile_id"`
	Revision             int                      `json:"revision"`
	RevisionID           string                   `json:"revision_id,omitempty"`
	ExpectedAssetVersion string                   `json:"expected_asset_version"`
	Brief                string                   `json:"brief"`
	Scope                service.AutoScopeRequest `json:"scope"`
	Authorization        struct {
		AcceptExternalEgress bool   `json:"accept_external_egress"`
		NoticeSHA256         string `json:"notice_sha256"`
	} `json:"authorization"`
	IdempotencyKey string `json:"idempotency_key"`
}

type autoDecisionRequest struct {
	Decision            string `json:"decision"`
	Feedback            string `json:"feedback"`
	ExpectedAutoVersion int    `json:"expected_auto_version"`
	IdempotencyKey      string `json:"idempotency_key"`
}

type autoRevokeRequest struct {
	Reason              string `json:"reason"`
	ExpectedAutoVersion int    `json:"expected_auto_version"`
	AlsoRevokeConsent   bool   `json:"also_revoke_consent"`
}

func (s *Server) autoNotice(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.Atoi(r.PathValue("revision"))
	if err != nil || revision < 1 {
		writeServiceError(w, model.ErrArgument)
		return
	}
	v, err := s.svc.AutoNoticeFor(r.Context(), r.PathValue("id"), revision)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) startAutoRun(w http.ResponseWriter, r *http.Request) {
	var req autoRunRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := checkAutoIdempotencyKey(req.IdempotencyKey); err != nil {
		writeServiceError(w, err)
		return
	}
	run, auto, err := s.svc.StartAutoRun(r.Context(), humanActor, service.AutoRunRequest{
		AssetID: r.PathValue("id"), RevisionID: req.RevisionID, ProfileID: req.ProfileID, ProfileRevision: req.Revision,
		ExpectedAssetVersion: req.ExpectedAssetVersion, Brief: req.Brief, Scope: req.Scope,
		AcceptExternalEgress: req.Authorization.AcceptExternalEgress, NoticeSHA256: req.Authorization.NoticeSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "auto": auto})
}

func (s *Server) getAuto(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.GetAuto(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) listAutoReviews(w http.ResponseWriter, r *http.Request) {
	rows, err := s.svc.ListAutoReviews(r.Context(), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) decideAuto(w http.ResponseWriter, r *http.Request) {
	var req autoDecisionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := checkAutoIdempotencyKey(req.IdempotencyKey); err != nil {
		writeServiceError(w, err)
		return
	}
	run, auto, err := s.svc.DecideAuto(r.Context(), humanActor, r.PathValue("run"), req.Decision, req.Feedback, req.ExpectedAutoVersion, req.IdempotencyKey)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto": auto, "run": run})
}

func (s *Server) revokeAuto(w http.ResponseWriter, r *http.Request) {
	var req autoRevokeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceError(w, err)
		return
	}
	auto, err := s.svc.RevokeAuto(r.Context(), humanActor, r.PathValue("run"), req.Reason, req.ExpectedAutoVersion, req.AlsoRevokeConsent)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto": auto})
}
