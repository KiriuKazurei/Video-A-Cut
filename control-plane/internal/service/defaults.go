package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

// EnsureDefaultProcessing creates the initial global preset and voice together.
// Existing IDs, including edited or archived defaults, belong to the user.
// No capability probe, model request or speech synthesis runs during startup.
func (s *Service) EnsureDefaultProcessing(ctx context.Context) error {
	return s.st.Transaction(ctx, func(tx *store.Store) error {
		const actor = "system:defaults"
		voice, err := tx.GetTTSTemplate(ctx, "vac_default_tts", 1)
		if errors.Is(err, model.ErrNotFound) {
			voice = tts.Template{
				TemplateID: "vac_default_tts", Revision: 1, Name: "默认中文配音（本地）",
				Settings:      tts.Settings{Engine: "sapi", Voice: "Microsoft Huihui Desktop", Language: "zh", Speed: 1, Volume: 1},
				ReferenceText: "你好，欢迎使用视频自动剪辑。这是一段默认中文配音试听。",
				CreatedAt:     time.Now().UTC(),
			}
			if err := voice.Settings.Validate(); err != nil {
				return err
			}
			voice.SHA256 = templateHash(voice)
			if err := tx.SaveTTSTemplate(ctx, voice, 0); err != nil {
				return err
			}
			if err := tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "tts.template.save", Target: voice.TemplateID, Detail: "revision=1 sha256=" + voice.SHA256}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.GetProfileRevision(ctx, "vac_default", 0); err == nil {
			return nil
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if voice.Settings.Validate() != nil || templateHash(voice) != voice.SHA256 {
			return model.ErrInvalidState
		}
		profile := preparation.Profile{
			SchemaVersion: 1, ProfileID: "vac_default", Revision: 1, Name: "默认处理预设（本地）",
			ContentMode: "builtin", Vision: preparation.Provider{Adapter: "builtin"}, Narration: preparation.Provider{Adapter: "builtin"},
			Sampling: preparation.Sampling{MaxFrames: 12, MaxBytes: 12 * 1024 * 1024, TimeoutSeconds: 60},
			TTSVoice: voice.Settings.Voice, ExportTarget: "premiere",
			TTSTemplate: &tts.Binding{TemplateID: voice.TemplateID, Revision: voice.Revision, SHA256: voice.SHA256, Settings: &voice.Settings},
		}
		sum, err := profile.Fingerprint()
		if err != nil {
			return err
		}
		body, err := json.Marshal(profile)
		if err != nil {
			return err
		}
		if err := tx.InsertProfileRevision(ctx, store.ProfileRevisionRow{
			ProfileID: profile.ProfileID, Revision: 1, SchemaVersion: 1, CanonicalJSON: string(body), SHA256: sum, Actor: actor, CreatedAt: time.Now().UTC(),
		}, 0); err != nil {
			return err
		}
		return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "profile.revise", Target: profile.ProfileID, Detail: "revision=1 sha256=" + sum})
	})
}
