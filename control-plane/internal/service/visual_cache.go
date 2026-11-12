package service

import (
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

const maxVisualCacheItem = 8 << 20

func readVisualCache(path string) []byte {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxVisualCacheItem {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxVisualCacheItem+1))
	if err != nil || len(raw) > maxVisualCacheItem {
		return nil
	}
	return raw
}

func (s *Service) visualCachePath(kind, file string) string {
	s.visualCacheMu.Lock()
	defer s.visualCacheMu.Unlock()
	dir, err := s.harnessDirectory(filepath.Join(".visual-cache", kind))
	if err != nil {
		return ""
	}
	return filepath.Join(dir, file)
}

func (s *Service) writeVisualCache(path string, raw []byte) {
	if path == "" || len(raw) > maxVisualCacheItem {
		return
	}
	s.visualCacheMu.Lock()
	defer s.visualCacheMu.Unlock()
	if err := project.WriteBytesAtomic(path, raw); err != nil {
		return
	}
	dir, ext := filepath.Dir(path), filepath.Ext(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 64+len(ext) || filepath.Ext(name) != ext || name == filepath.Base(path) {
			continue
		}
		if _, err := hex.DecodeString(name[:64]); err != nil {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			files = append(files, info)
		}
	}
	slices.SortFunc(files, func(a, b os.FileInfo) int { return b.ModTime().Compare(a.ModTime()) })
	size := int64(len(raw))
	for i, info := range files {
		size += info.Size()
		if i >= 255 || size > 96<<20 {
			_ = os.Remove(filepath.Join(dir, info.Name()))
		}
	}
}

// Cache hits bypass the queue. Identical requests wait for one producer.
func (s *Service) renderThumbnail(ctx context.Context, path string, render func() error) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		release, err := s.acquireScript("thumbnail:" + path)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Millisecond):
				continue
			}
		}
		defer release()
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if err := s.thumbnailSlots.Acquire(ctx, 1); err != nil {
			return err
		}
		defer s.thumbnailSlots.Release(1)
		return render()
	}
}
