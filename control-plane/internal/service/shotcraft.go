package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/shotcraft"
)

func (s *Service) ListShotCards(ctx context.Context, query string) ([]shotcraft.Card, error) {
	cards := shotcraft.List(query)
	root, err := s.shotTemplateRoot(ctx)
	if err != nil {
		return nil, err
	}
	var entries []shotcraft.UserTemplate
	if root != nil {
		defer root.Close()
		entries, err = listShotTemplateFiles(root)
		if err != nil {
			return nil, err
		}
	} else {
		rows, err := s.st.ListShotTemplates(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			var entry shotcraft.UserTemplate
			if err := json.Unmarshal([]byte(row), &entry); err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.ID+" "+entry.Category+" "+entry.Summary), query) {
			cards = append(cards, entry.Card())
		}
	}
	return cards, nil
}

func (s *Service) UserShotTemplate(ctx context.Context, id string) (shotcraft.UserTemplate, error) {
	var entry shotcraft.UserTemplate
	if !shotcraft.ValidUserID(id) {
		return entry, model.ErrNotFound
	}
	root, err := s.shotTemplateRoot(ctx)
	if err != nil {
		return entry, err
	}
	if root != nil {
		defer root.Close()
		return readShotTemplateFile(root, id)
	}
	body, err := s.st.ShotTemplate(ctx, id)
	if err != nil {
		return entry, err
	}
	err = json.Unmarshal([]byte(body), &entry)
	return entry, err
}

func validateShotTemplate(entry *shotcraft.UserTemplate) error {
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Category = strings.TrimSpace(entry.Category)
	if entry.Version != 1 || !shotcraft.ValidUserID(entry.ID) || entry.Summary == "" ||
		len(entry.Summary) > 256 || entry.Category == "" || len(entry.Category) > 96 || strings.TrimSpace(entry.Source) == "" {
		return fmt.Errorf("模板需要 version=1、user- 开头的小写卡号、说明、分类和源码: %w", model.ErrArgument)
	}
	if _, _, ok := shotcraft.Lookup(entry.ID); ok {
		return model.ErrConflict
	}
	if err := userShotComposition(*entry).Validate(); err != nil {
		return fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	return nil
}

func userShotComposition(entry shotcraft.UserTemplate) project.DrawComposition {
	c := project.DrawComposition{ID: entry.ID, Name: entry.Summary}
	setShotComposition(&c, shotcraft.Template{Source: entry.Source, DurationFrames: entry.DurationFrames})
	return c
}

func (s *Service) ImportShotTemplate(ctx context.Context, entry shotcraft.UserTemplate) (shotcraft.Card, error) {
	if err := validateShotTemplate(&entry); err != nil {
		return shotcraft.Card{}, err
	}
	root, err := s.shotTemplateRoot(ctx)
	if err != nil {
		return shotcraft.Card{}, err
	}
	if root != nil {
		defer root.Close()
	}
	c := userShotComposition(entry)
	if _, err := s.scriptProcess(ctx, true, "draw-check", map[string]any{"composition": c, "deterministic": true}); err != nil {
		return shotcraft.Card{}, fmt.Errorf("模板源码检查失败: %v: %w", err, model.ErrArgument)
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return shotcraft.Card{}, err
	}
	if root != nil {
		return entry.Card(), writeShotTemplateFile(root, entry.ID, body)
	}
	return entry.Card(), s.st.InsertShotTemplate(ctx, entry.ID, string(body))
}

func (s *Service) DeleteShotTemplate(ctx context.Context, id string) error {
	if _, _, ok := shotcraft.Lookup(id); ok {
		return model.ErrForbidden
	}
	if !shotcraft.ValidUserID(id) {
		return model.ErrNotFound
	}
	root, err := s.shotTemplateRoot(ctx)
	if err != nil {
		return err
	}
	if root != nil {
		defer root.Close()
		if _, err := readShotTemplateFile(root, id); err != nil {
			return err
		}
		return root.Remove(id + ".json")
	}
	return s.st.DeleteShotTemplate(ctx, id)
}

func (s *Service) lookupShot(ctx context.Context, reference string) (shotcraft.Card, shotcraft.Variant, shotcraft.Template, error) {
	if card, style, ok := shotcraft.Lookup(reference); ok {
		return card, style, shotcraft.Compiled(card, style), nil
	}
	if !shotcraft.UserReference(reference) {
		return shotcraft.Card{}, shotcraft.Variant{}, shotcraft.Template{}, model.ErrNotFound
	}
	id, _, _ := strings.Cut(reference, "/")
	entry, err := s.UserShotTemplate(ctx, id)
	if err != nil {
		return shotcraft.Card{}, shotcraft.Variant{}, shotcraft.Template{}, err
	}
	card := entry.Card()
	return card, card.Styles[0], shotcraft.Template{Source: entry.Source, DurationFrames: entry.DurationFrames}, nil
}

func (s *Service) PreviewShotTemplate(ctx context.Context, reference string, position int) ([]byte, error) {
	if position < 0 || position > 100 {
		return nil, model.ErrArgument
	}
	card, variant, tpl, err := s.lookupShot(ctx, reference)
	if err != nil {
		return nil, err
	}
	if len(tpl.ImageAliases) > 0 {
		return nil, fmt.Errorf("此变体需要工程图片（%s），可在绑定图片后从绘制工作区预览: %w", strings.Join(tpl.ImageAliases, "、"), model.ErrArgument)
	}
	c := project.DrawComposition{ID: card.ID}
	if err := applyShotTemplate(&c, reference, variant, tpl); err != nil {
		return nil, err
	}
	frame := int64(position) * (c.DurationFrames - 1) / 100
	const scale = 0.25
	doc := c.UnifiedDocument()
	key := skillHash([]any{doc, frame, scale, drawingEnvironmentVersion()})
	cached := s.visualCachePath("shotcraft-v1", key+".png")
	read := func() []byte {
		raw := readVisualCache(cached)
		if bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) {
			return raw
		}
		return nil
	}
	if raw := read(); raw != nil {
		return raw, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := s.shotPreview.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer s.shotPreview.Release(1)
	if raw := read(); raw != nil {
		return raw, nil
	}
	dir, err := os.MkdirTemp("", "vac-shotcraft-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(dir)
	defer os.Remove(filepath.Join(dir, "image-cache"))
	defer os.Remove(filepath.Join(dir, "scene.js"))
	path := filepath.Join(dir, "frame.png")
	defer os.Remove(path)
	_, err = s.scriptProcess(ctx, true, "draw-source-frame", map[string]any{
		"document": doc, "output_dir": dir, "frame": frame, "scale": scale,
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("生成模板预览超时: %w", model.ErrInvalidState)
		}
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err == nil && ctx.Err() == nil {
		s.writeVisualCache(cached, raw)
	}
	return raw, err
}

func shotLookupError(id string, err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return fmt.Errorf("unknown shot card or variant %q: %w", id, model.ErrArgument)
	}
	return err
}
