package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// SaveProcessingProfile validates and stores one immutable revision.
// expectedRevision 0 creates revision 1. Revision, receipt and audit commit together.
func (s *Service) SaveProcessingProfile(ctx context.Context, actor, idemKey string, expectedRevision int, profile preparation.Profile) (preparation.Profile, error) {
	if actor == "" || idemKey == "" {
		return preparation.Profile{}, fmt.Errorf("service: save profile: actor and idempotency_key are required: %w", model.ErrArgument)
	}
	next := expectedRevision + 1
	if profile.Revision != next {
		return preparation.Profile{}, fmt.Errorf("service: save profile: revision must be the next server revision: %w", model.ErrArgument)
	}
	sum, err := profile.Fingerprint()
	if err != nil {
		return preparation.Profile{}, fmt.Errorf("service: save profile: %v: %w", err, model.ErrArgument)
	}
	body, err := json.Marshal(profile)
	if err != nil {
		return preparation.Profile{}, err
	}
	scope := "profile.save:" + profile.ProfileID
	var saved preparation.Profile
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		cachedHash, cachedBody, err := tx.GetIdempotency(ctx, scope, idemKey)
		if err == nil {
			if cachedHash != sum {
				return fmt.Errorf("service: save profile: idempotency key reused: %w", model.ErrConflict)
			}
			parsed, err := verifiedProfile(profile.ProfileID, cachedBody, cachedHash)
			if err != nil {
				return err
			}
			if parsed.Revision != profile.Revision {
				return model.ErrInvalidState
			}
			saved = parsed
			return nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		current := 0
		if expectedRevision > 0 {
			row, err := tx.GetProfileRevision(ctx, profile.ProfileID, 0)
			if err != nil {
				return err
			}
			if row.Revision != expectedRevision {
				return fmt.Errorf("service: save profile: %w", model.ErrConflict)
			}
			current = row.Revision
		}
		if err := tx.InsertProfileRevision(ctx, store.ProfileRevisionRow{
			ProfileID: profile.ProfileID, Revision: profile.Revision, SchemaVersion: profile.SchemaVersion,
			CanonicalJSON: string(body), SHA256: sum, Actor: actor, CreatedAt: time.Now().UTC(),
		}, current); err != nil {
			return err
		}
		if err := tx.PutIdempotency(ctx, scope, idemKey, sum, string(body)); err != nil {
			return err
		}
		if err := tx.WriteAudit(ctx, model.AuditLog{
			Actor: actor, Action: "profile.revise", Target: profile.ProfileID,
			Detail: "revision=" + itoa(profile.Revision) + " sha256=" + sum,
		}); err != nil {
			return err
		}
		saved = profile
		return nil
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return preparation.Profile{}, fmt.Errorf("service: save profile: %w", model.ErrConflict)
		}
		return preparation.Profile{}, err
	}
	s.publish("profile.changed", map[string]any{"profile_id": saved.ProfileID})
	return saved, nil
}

func (s *Service) GetProcessingProfile(ctx context.Context, profileID string, revision int) (preparation.Profile, string, error) {
	row, err := s.st.GetProfileRevision(ctx, profileID, revision)
	if err != nil {
		return preparation.Profile{}, "", err
	}
	profile, err := verifiedProfile(profileID, row.CanonicalJSON, row.SHA256)
	if err != nil {
		return preparation.Profile{}, "", err
	}
	if profile.Revision != row.Revision || profile.SchemaVersion != row.SchemaVersion || (revision > 0 && profile.Revision != revision) {
		return preparation.Profile{}, "", fmt.Errorf("service: profile revision does not match the stored row: %w", model.ErrInvalidState)
	}
	return profile, row.SHA256, nil
}

// verifiedProfile checks that stored JSON is the profile it claims to be.
// The saved sha256 must be the server fingerprint of that JSON, and the
// identity fields must match the row that pointed at it.
func verifiedProfile(profileID, canonical, savedSum string) (preparation.Profile, error) {
	raw := sha256.Sum256([]byte(canonical))
	if hex.EncodeToString(raw[:]) != savedSum {
		return preparation.Profile{}, fmt.Errorf("service: stored profile hash does not match its content: %w", model.ErrInvalidState)
	}
	var profile preparation.Profile
	if err := json.Unmarshal([]byte(canonical), &profile); err != nil {
		return preparation.Profile{}, fmt.Errorf("service: stored profile is not valid JSON: %w", model.ErrInvalidState)
	}
	if profile.ProfileID != profileID {
		return preparation.Profile{}, fmt.Errorf("service: stored profile id does not match its row: %w", model.ErrInvalidState)
	}
	sum, err := profile.Fingerprint()
	if err != nil {
		return preparation.Profile{}, fmt.Errorf("service: stored profile failed validation: %v: %w", err, model.ErrInvalidState)
	}
	if sum != savedSum {
		return preparation.Profile{}, fmt.Errorf("service: stored profile fingerprint does not match its row: %w", model.ErrInvalidState)
	}
	return profile, nil
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
