package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/shotcraft"
)

type ShotTemplateDirectory struct {
	Directory string `json:"directory"`
}

func (s *Service) ShotTemplateDirectory(ctx context.Context) (ShotTemplateDirectory, error) {
	directory, err := s.st.ShotTemplateDirectory(ctx)
	return ShotTemplateDirectory{Directory: directory}, err
}

func (s *Service) SetShotTemplateDirectory(ctx context.Context, value ShotTemplateDirectory) (ShotTemplateDirectory, error) {
	if value.Directory != "" {
		if !filepath.IsAbs(value.Directory) {
			return ShotTemplateDirectory{}, fmt.Errorf("请选择现有模板库目录的绝对路径: %w", model.ErrArgument)
		}
		value.Directory = filepath.Clean(value.Directory)
		root, err := os.OpenRoot(value.Directory)
		if err != nil {
			return ShotTemplateDirectory{}, fmt.Errorf("无法打开模板库目录: %v: %w", err, model.ErrArgument)
		}
		defer root.Close()
		if _, err := fs.ReadDir(root.FS(), "."); err != nil {
			return ShotTemplateDirectory{}, fmt.Errorf("无法读取模板库目录: %v: %w", err, model.ErrArgument)
		}
	}
	return value, s.st.SetShotTemplateDirectory(ctx, value.Directory)
}

// A nil root selects the original application database.
func (s *Service) shotTemplateRoot(ctx context.Context) (*os.Root, error) {
	directory, err := s.st.ShotTemplateDirectory(ctx)
	if err != nil || directory == "" {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("模板库目录不可用，请在设置中重新选择目录: %v: %w", err, model.ErrInvalidState)
	}
	return root, nil
}

const maxShotTemplateFile = 1 << 20

func readShotTemplateFile(root *os.Root, id string) (shotcraft.UserTemplate, error) {
	var entry shotcraft.UserTemplate
	name := id + ".json"
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return entry, model.ErrNotFound
	}
	if err != nil {
		return entry, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxShotTemplateFile {
		return entry, fmt.Errorf("模板必须为 1 MiB 以内的普通 JSON 文件: %w", model.ErrArgument)
	}
	f, err := root.Open(name)
	if err != nil {
		return entry, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxShotTemplateFile+1))
	if err != nil {
		return entry, err
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	dec.DisallowUnknownFields()
	var extra any
	if len(data) > maxShotTemplateFile || dec.Decode(&entry) != nil || dec.Decode(&extra) != io.EOF || entry.ID != id {
		return entry, fmt.Errorf("模板 JSON 无效，文件名必须为卡号加 .json: %w", model.ErrArgument)
	}
	err = validateShotTemplate(&entry)
	return entry, err
}

func listShotTemplateFiles(root *os.Root) ([]shotcraft.UserTemplate, error) {
	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	var entries []shotcraft.UserTemplate
	for _, file := range files {
		id, ok := strings.CutSuffix(file.Name(), ".json")
		if !ok || !shotcraft.ValidUserID(id) || !file.Type().IsRegular() {
			continue
		}
		entry, err := readShotTemplateFile(root, id)
		if errors.Is(err, model.ErrArgument) || errors.Is(err, model.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func writeShotTemplateFile(root *os.Root, id string, data []byte) error {
	// Publish the finished file without replacing an existing template.
	name := newID("shot-template") + ".tmp"
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	err = root.Link(name, id+".json")
	if errors.Is(err, fs.ErrExist) {
		return model.ErrConflict
	}
	return err
}
