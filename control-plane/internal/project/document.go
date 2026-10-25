// Package project defines the portable VAC document formats. It does not open
// databases, start jobs, or call providers while reading a document.
package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const SchemaVersion = 2

type Source struct {
	RelativePath string `json:"relative_path,omitempty"`
	OriginalPath string `json:"original_path,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Size         int64  `json:"size_bytes"`
	ModifiedNS   int64  `json:"modified_ns,omitempty"`
}

type Resource struct {
	RawDevelop *RawDevelop    `json:"raw_develop,omitempty"`
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Source     Source         `json:"source"`
	Metadata   map[string]any `json:"metadata"`
	Notes      string         `json:"notes"`
	In         float64        `json:"in"`
	Out        float64        `json:"out"`
	Missing    bool           `json:"missing"`
	Removed    bool           `json:"removed,omitempty"`
	Changed    bool           `json:"changed,omitempty"`
	Referenced bool           `json:"referenced,omitempty"`
	AssetID    string         `json:"asset_id,omitempty"`
	ScriptID   string         `json:"script_id,omitempty"`
	Markers    []Marker       `json:"markers,omitempty"`
}

type Binding struct {
	SegmentIndexes []int  `json:"segment_indexes,omitempty"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	ResourceID     string `json:"resource_id,omitempty"`
	Unresolved     bool   `json:"unresolved"`
}

type AvsDocument struct {
	Format           string          `json:"format"`
	SchemaVersion    int             `json:"schema_version"`
	ID               string          `json:"id"`
	Title            string          `json:"title"`
	Body             string          `json:"body"`
	Segments         json.RawMessage `json:"segments"`
	Bindings         []Binding       `json:"bindings"`
	TTSEnabled       bool            `json:"tts_enabled"`
	SubtitlesEnabled bool            `json:"subtitles_enabled"`
	AllowWebResearch bool            `json:"allow_web_research"`
	Sources          json.RawMessage `json:"sources,omitempty"`
	Revisions        json.RawMessage `json:"revisions,omitempty"`
	References       []Reference     `json:"references,omitempty"`
}

type Identity struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AvpDocument struct {
	DrawCompositions []DrawComposition `json:"draw_compositions,omitempty"`
	FileReferences   map[string]string `json:"file_references,omitempty"`
	Format           string            `json:"format"`
	SchemaVersion    int               `json:"schema_version"`
	Project          Identity          `json:"project"`
	Resources        []Resource        `json:"resources"`
	Scripts          []AvsDocument     `json:"scripts"`
	Sequences        []Sequence        `json:"sequences"`
	LegacySequences  []json.RawMessage `json:"legacy_sequences,omitempty"`
	Settings         map[string]any    `json:"settings"`
	Workspace        map[string]any    `json:"workspace"`
	// Runtime is a versioned, allowlisted projection of this project's editable
	// database records. Global configuration and credentials are never included.
	Runtime json.RawMessage `json:"runtime,omitempty"`
}

type Recent struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	OpenedAt time.Time `json:"opened_at"`
}

type Recovery struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	RecoveryPath string    `json:"recovery_path"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID          string       `json:"id"`
	Path        string       `json:"path"`
	Dirty       bool         `json:"dirty"`
	Revision    uint64       `json:"revision"`
	ActiveTasks int          `json:"active_tasks"`
	Document    *AvpDocument `json:"document"`
	Recent      []Recent     `json:"recent"`
	Recoveries  []Recovery   `json:"recoveries"`
}

func validID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\x00")
}

func (d AvpDocument) Validate() error {
	if d.Format != "avp" || (d.SchemaVersion != 1 && d.SchemaVersion != SchemaVersion) || !validID(d.Project.ID) || strings.TrimSpace(d.Project.Name) == "" {
		return fmt.Errorf("不支持的 AVP 工程或格式版本")
	}
	if len(d.DrawCompositions) > 16 {
		return fmt.Errorf("too many draw compositions")
	}
	seen := map[string]bool{}
	for _, c := range d.DrawCompositions {
		if err := c.Validate(); err != nil {
			return err
		}
		if c.Revision == 0 || c.ContentVersion != c.ContentHash() {
			return fmt.Errorf("draw source content version mismatch")
		}
		if seen["draw:"+c.ID] {
			return fmt.Errorf("duplicate draw id")
		}
		seen["draw:"+c.ID] = true
	}

	resources := map[string]Resource{}
	for _, r := range d.Resources {
		if !validID(r.ID) || seen[r.ID] || !validKind(r.Kind) || math.IsNaN(r.In) || math.IsNaN(r.Out) || math.IsInf(r.In, 0) || math.IsInf(r.Out, 0) || r.In < 0 || r.Out < r.In {
			return fmt.Errorf("无效或重复的工程资源 %q", r.ID)
		}
		if r.RawDevelop != nil {
			if r.Kind != "image" || !IsCameraRaw(r.Source.OriginalPath) {
				return fmt.Errorf("RAW 参数仅适用于相机原片")
			}
			if err := r.RawDevelop.Validate(); err != nil {
				return err
			}
		}
		seen[r.ID] = true
		resources[r.ID] = r
		for _, marker := range r.Markers {
			if err := validateMarker(marker); err != nil {
				return err
			}
			if marker.SourceTime == nil || seen["marker:"+marker.ID] {
				return fmt.Errorf("无效或重复的素材标记")
			}
			seen["marker:"+marker.ID] = true
		}
	}
	for _, composition := range d.DrawCompositions {
		for _, image := range composition.Images {
			resource, ok := resources[image.ResourceID]
			if !ok || resource.Kind != "image" || len(image.ResourceSHA) != 64 {
				return fmt.Errorf("draw image resource is not in this project")
			}
		}
	}
	for _, sc := range d.Scripts {
		if seen["script:"+sc.ID] {
			return fmt.Errorf("重复的脚本 ID")
		}
		seen["script:"+sc.ID] = true
		if err := sc.Validate(); err != nil {
			return err
		}
	}
	for _, sequence := range d.Sequences {
		if seen["sequence:"+sequence.ID] {
			return fmt.Errorf("重复的序列 ID")
		}
		seen["sequence:"+sequence.ID] = true
		if err := sequence.Validate(); err != nil {
			return err
		}
		for _, track := range sequence.Tracks {
			for _, clip := range track.Clips {
				if !seen[clip.ResourceID] {
					return fmt.Errorf("片段引用工程外资源")
				}
				r := resources[clip.ResourceID]
				if (track.Kind == "video" && r.Kind != "video" && r.Kind != "image") || (track.Kind == "audio" && r.Kind != "audio" && r.Kind != "video") || (track.Kind == "subtitle" && r.Kind != "subtitle") {
					return fmt.Errorf("片段资源与轨道类型不匹配")
				}
				if (track.Kind == "audio" || track.Kind == "video") && r.Kind != "image" && r.Metadata["streams"] != nil && !slices.Contains(Streams(r, track.Kind), clip.StreamIndex) {
					return fmt.Errorf("片段引用了错误的源流索引")
				}
				if clip.Effects.Transition > 0 {
					var previous *Clip
					for i := range track.Clips {
						c := &track.Clips[i]
						if c.At+c.Duration == clip.At {
							previous = c
							break
						}
					}
					if previous == nil {
						return fmt.Errorf("交叉溶解需要同轨相邻片段")
					}
					source := resources[previous.ResourceID]
					end := number(source.Metadata["duration"])
					if source.Kind != "image" && end > 0 && Seconds(previous.SourceOut)+sequence.FPS.Seconds(clip.Effects.Transition)*previous.Speed > end+1e-6 {
						return fmt.Errorf("交叉溶解缺少源尾部，请先缩短前片段出点")
					}
				}
			}
		}
	}
	return nil
}

func validKind(k string) bool {
	switch k {
	case "video", "audio", "image", "subtitle", "document", "script":
		return true
	}
	return false
}

func (d AvsDocument) Validate() error {
	if d.Format != "avs" || (d.SchemaVersion != 1 && d.SchemaVersion != SchemaVersion) || !validID(d.ID) || !json.Valid(d.Segments) {
		return fmt.Errorf("不支持的 AVS 脚本或格式版本")
	}
	seen := map[string]bool{}
	for _, b := range d.Bindings {
		if !validID(b.ID) || seen[b.ID] || (b.Kind != "video" && b.Kind != "audio") {
			return fmt.Errorf("重复或无效的脚本绑定")
		}
		seen[b.ID] = true
	}
	return ValidateReferences(d.Body, d.References)
}

func Read(path, format string, out any) error {
	if !strings.EqualFold(filepath.Ext(path), "."+format) {
		return fmt.Errorf("请选择 .%s 文件", format)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return fmt.Errorf("请选择不超过 128 MiB 的文档文件")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (128<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 128<<20 || !utf8.Valid(raw) {
		return fmt.Errorf("文档必须是 UTF-8 JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(out); err != nil {
		return fmt.Errorf("读取工程文档: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("文档含多余或无效内容")
	}
	return nil
}

// WriteAtomic leaves the previous document intact until the complete new file
// is flushed and closed, then uses the platform atomic replacement primitive.
func WriteAtomic(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	f, err := os.CreateTemp(filepath.Dir(path), ".vac-save-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = io.Copy(f, bytes.NewReader(raw)); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceDocumentFile(temp, path)
}

// CopyAtomic publishes verified media without exposing a partly written file.
func CopyAtomic(target, source string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target), ".vac-export-*.tmp")
	if err != nil {
		return err
	}
	temp := out.Name()
	defer os.Remove(temp)
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceDocumentFile(temp, target)
}

func ResolveSource(documentPath string, source Source) string {
	if source.RelativePath != "" {
		path := filepath.Clean(filepath.Join(filepath.Dir(documentPath), filepath.FromSlash(source.RelativePath)))
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return filepath.Clean(source.OriginalPath)
}
