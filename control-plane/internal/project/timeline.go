package project

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Time stores original media time independently of the sequence's frame grid.
type Time struct {
	Value int64 `json:"value"`
	Scale int64 `json:"scale"`
}

func Seconds(t Time) float64 {
	if t.Scale <= 0 {
		return math.NaN()
	}
	return float64(t.Value) / float64(t.Scale)
}
func MediaTime(seconds float64) Time { return Time{int64(math.Round(seconds * 1e9)), 1e9} }
func timeAt(seconds float64, scale int64) Time {
	if scale <= 0 {
		scale = 1e9
	}
	return Time{int64(math.Round(seconds * float64(scale))), scale}
}
func SourceTime(r Resource, index int, seconds float64) Time {
	raw, _ := json.Marshal(r.Metadata["streams"])
	var streams []map[string]any
	_ = json.Unmarshal(raw, &streams)
	for _, stream := range streams {
		if int(number(stream["index"])) != index {
			continue
		}
		if stream["codec_type"] == "audio" {
			scale := int64(number(stream["sample_rate"]))
			if scale > 0 {
				return timeAt(seconds, scale)
			}
		}
		var n, den int64
		if value, ok := stream["time_base"].(string); ok {
			_, _ = fmt.Sscanf(value, "%d/%d", &n, &den)
		}
		if n > 0 && den > 0 {
			return Time{int64(math.Round(seconds*float64(den)/float64(n))) * n, den}
		}
	}
	return MediaTime(seconds)
}

type Rate struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

func (r Rate) Value() float64               { return float64(r.Num) / float64(r.Den) }
func (r Rate) Frames(seconds float64) int64 { return int64(math.Round(seconds * r.Value())) }
func (r Rate) Seconds(frame int64) float64  { return float64(frame) * float64(r.Den) / float64(r.Num) }

type Marker struct {
	ID         string `json:"id"`
	Notes      string `json:"notes"`
	Kind       string `json:"kind"` // point, in, out; a point never requires an out point
	Frame      int64  `json:"frame"`
	SourceTime *Time  `json:"source_time,omitempty"`
	Locked     bool   `json:"locked"`
}
type Origin struct {
	Actor       string `json:"actor"`
	RunID       string `json:"run_id,omitempty"`
	ArtifactID  string `json:"artifact_id,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	PlanVersion int    `json:"plan_version,omitempty"`
}
type Effects struct {
	GainDB     float64 `json:"gain_db"`
	FadeIn     int64   `json:"fade_in"`
	FadeOut    int64   `json:"fade_out"`
	FadeColor  string  `json:"fade_color,omitempty"`
	Scale      float64 `json:"scale"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	CropLeft   float64 `json:"crop_left"`
	CropRight  float64 `json:"crop_right"`
	CropTop    float64 `json:"crop_top"`
	CropBottom float64 `json:"crop_bottom"`
	Transition int64   `json:"transition"`
	FromFrame  *int64  `json:"from_frame,omitempty"`
	ToFrame    *int64  `json:"to_frame,omitempty"`
}
type Clip struct {
	ID          string  `json:"id"`
	ResourceID  string  `json:"resource_id"`
	Name        string  `json:"name"`
	StreamIndex int     `json:"stream_index"`
	At          int64   `json:"at"`
	Duration    int64   `json:"duration"`
	SourceIn    Time    `json:"source_in"`
	SourceOut   Time    `json:"source_out"`
	Speed       float64 `json:"speed"`
	LinkID      string  `json:"link_id,omitempty"`
	Origin      Origin  `json:"origin"`
	Effects     Effects `json:"effects"`
	Text        string  `json:"text,omitempty"`
	Font        string  `json:"font,omitempty"`
	FontSize    int     `json:"font_size,omitempty"`
}
type Track struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Name    string  `json:"name"`
	Height  int     `json:"height"`
	Locked  bool    `json:"locked"`
	Muted   bool    `json:"muted"`
	Hidden  bool    `json:"hidden"`
	Solo    bool    `json:"solo"`
	GroupID string  `json:"group_id,omitempty"`
	GainDB  float64 `json:"gain_db"`
	Clips   []Clip  `json:"clips"`
}
type TrackGroup struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Origin Origin `json:"origin"`
}
type SequenceState struct {
	Name       string       `json:"name"`
	Width      int          `json:"width"`
	Height     int          `json:"height"`
	FPS        Rate         `json:"fps"`
	SampleRate int          `json:"sample_rate"`
	Tracks     []Track      `json:"tracks"`
	Markers    []Marker     `json:"markers"`
	Groups     []TrackGroup `json:"groups"`
	ScopeGroup string       `json:"scope_group,omitempty"`
	Configured bool         `json:"configured"`
}
type EditSnapshot struct {
	State         SequenceState       `json:"state"`
	SourceMarkers map[string][]Marker `json:"source_markers"`
}
type EditRecord struct {
	ID     string       `json:"id"`
	Label  string       `json:"label"`
	Actor  string       `json:"actor"`
	Before EditSnapshot `json:"before"`
	After  EditSnapshot `json:"after"`
}
type Sequence struct {
	ImportedResults map[string]bool `json:"imported_results,omitempty"`
	ID              string          `json:"id"`
	Revision        uint64          `json:"revision"`
	SequenceState
	History       []EditRecord `json:"history,omitempty"`
	HistoryCursor int          `json:"history_cursor"`
}

// A reference identifies this occurrence in script text, not just its spelling.
type Reference struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	TargetID   string `json:"target_id"`
	ResourceID string `json:"resource_id,omitempty"`
	SequenceID string `json:"sequence_id,omitempty"`
	ClipID     string `json:"clip_id,omitempty"`
	Start      int    `json:"start"`
	End        int    `json:"end"` // UTF-16 offsets, matching the text editor
	Unresolved bool   `json:"unresolved"`
}
type ReferenceCandidate struct {
	Reference
	MediaID    string `json:"media_id,omitempty"`
	AnchorTime *Time  `json:"anchor_time,omitempty"`
	Owner      string `json:"owner"`
	Frame      *int64 `json:"frame,omitempty"`
	SourceTime *Time  `json:"source_time,omitempty"`
	FPS        Rate   `json:"fps"`
	In         *int64 `json:"in,omitempty"`
	Out        *int64 `json:"out,omitempty"`
}

func TimelineID(prefix string) string {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
func NewSequence(name string) Sequence {
	s := Sequence{ID: TimelineID("seq"), Revision: 1, SequenceState: SequenceState{Name: name, Width: 1920, Height: 1080, FPS: Rate{30, 1}, SampleRate: 48000, Tracks: []Track{}, Markers: []Marker{}, Groups: []TrackGroup{}}}
	for _, kind := range []string{"video", "audio"} {
		for i := 0; i < 3; i++ {
			s.Tracks = append(s.Tracks, newTrack(kind, ""))
		}
	}
	return s
}
func newTrack(kind, group string) Track {
	return Track{ID: TimelineID("trk"), Kind: kind, Name: map[string]string{"video": "视频", "audio": "音频", "subtitle": "字幕"}[kind], Height: 64, GroupID: group, Clips: []Clip{}}
}
func clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}

// v1 stored EDL projections in `sequences`; preserve those projections while
// decoding v2's authoritative, editable sequences. Never reinterpret an MP4.
func (d *AvpDocument) UnmarshalJSON(raw []byte) error {
	type plain AvpDocument
	var v struct {
		*plain
		Sequences []json.RawMessage `json:"sequences"`
	}
	v.plain = (*plain)(d)
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	d.Sequences = []Sequence{}
	for _, r := range v.Sequences {
		var header struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(r, &header); err != nil {
			return err
		}
		if header.ID == "" && d.SchemaVersion == 1 {
			d.LegacySequences = append(d.LegacySequences, r)
			continue
		}
		var s Sequence
		if err := json.Unmarshal(r, &s); err != nil {
			return err
		}
		d.Sequences = append(d.Sequences, s)
	}
	return nil
}
func (d *AvpDocument) Upgrade() {
	d.SchemaVersion = SchemaVersion
	for i := range d.Scripts {
		d.Scripts[i].SchemaVersion = SchemaVersion
	}
	if len(d.Sequences) == 0 {
		d.Sequences = []Sequence{NewSequence("主序列")}
	}
	for i := range d.Sequences {
		normalizeSequenceState(&d.Sequences[i].SequenceState)
		for j := range d.Sequences[i].History {
			normalizeSequenceState(&d.Sequences[i].History[j].Before.State)
			normalizeSequenceState(&d.Sequences[i].History[j].After.State)
		}
	}
}

// Optional empty JSON arrays have one canonical representation in the live
// workspace, including history restored by undo. Opening never rewrites disk.
func normalizeSequenceState(s *SequenceState) {
	if s.Tracks == nil {
		s.Tracks = []Track{}
	}
	if s.Markers == nil {
		s.Markers = []Marker{}
	}
	if s.Groups == nil {
		s.Groups = []TrackGroup{}
	}
	for i := range s.Tracks {
		if s.Tracks[i].Clips == nil {
			s.Tracks[i].Clips = []Clip{}
		}
	}
}
func (s Sequence) Validate() error {
	if !validID(s.ID) || strings.TrimSpace(s.Name) == "" || s.Width <= 0 || s.Height <= 0 || s.Width > 8192 || s.Height > 8192 || s.FPS.Num <= 0 || s.FPS.Den <= 0 || s.FPS.Value() > 240 || s.SampleRate < 8000 || s.SampleRate > 192000 || len(s.Tracks) > 256 {
		return fmt.Errorf("无效的序列参数")
	}
	ids := map[string]bool{}
	groups := map[string]bool{}
	for _, g := range s.Groups {
		if !validID(g.ID) || g.ID == "original" || groups[g.ID] {
			return fmt.Errorf("无效或重复的轨道组")
		}
		groups[g.ID] = true
	}
	if s.ScopeGroup != "" && s.ScopeGroup != "original" && !groups[s.ScopeGroup] {
		return fmt.Errorf("对照轨道组不存在")
	}
	for _, t := range s.Tracks {
		if !validID(t.ID) || ids[t.ID] || (t.Kind != "video" && t.Kind != "audio" && t.Kind != "subtitle") || !finite(t.GainDB) || t.Height < 40 || t.Height > 300 {
			return fmt.Errorf("无效的轨道")
		}
		ids[t.ID] = true
		if t.GroupID != "" && !groups[t.GroupID] {
			return fmt.Errorf("轨道组不存在")
		}
		for _, c := range t.Clips {
			if !validID(c.ID) || ids[c.ID] || c.At < 0 || c.Duration < 1 || !finite(c.Speed) || c.Speed <= 0 || !finite(Seconds(c.SourceIn)) || !finite(Seconds(c.SourceOut)) || Seconds(c.SourceIn) < 0 || Seconds(c.SourceOut) <= Seconds(c.SourceIn) || c.StreamIndex < 0 {
				return fmt.Errorf("无效的片段 %s", c.ID)
			}
			ids[c.ID] = true
			if c.SourceIn.Scale <= 0 || c.SourceOut.Scale <= 0 {
				return fmt.Errorf("源时间精度必须为正数")
			}
			e := c.Effects
			for _, v := range []float64{e.GainDB, e.Scale, e.X, e.Y, e.CropLeft, e.CropRight, e.CropTop, e.CropBottom} {
				if !finite(v) {
					return fmt.Errorf("无效的片段效果")
				}
			}
			if e.Scale <= 0 || e.FadeIn < 0 || e.FadeOut < 0 || e.FadeIn > c.Duration || e.FadeOut > c.Duration || e.Transition < 0 || e.Transition > c.Duration || e.CropLeft < 0 || e.CropRight < 0 || e.CropTop < 0 || e.CropBottom < 0 || e.CropLeft+e.CropRight >= 1 || e.CropTop+e.CropBottom >= 1 || (e.FadeColor != "" && e.FadeColor != "black" && e.FadeColor != "white") {
				return fmt.Errorf("效果范围超出片段")
			}
			if (e.FromFrame != nil && (*e.FromFrame < 0 || *e.FromFrame >= c.Duration)) || (e.ToFrame != nil && (*e.ToFrame <= 0 || *e.ToFrame > c.Duration)) || (e.FromFrame != nil && e.ToFrame != nil && *e.FromFrame >= *e.ToFrame) {
				return fmt.Errorf("效果帧范围无效")
			}
		}
	}
	for _, m := range s.Markers {
		if err := validateMarker(m); err != nil {
			return err
		}
		if ids[m.ID] {
			return fmt.Errorf("重复标记 ID")
		}
		ids[m.ID] = true
	}
	if s.HistoryCursor < 0 || s.HistoryCursor > len(s.History) {
		return fmt.Errorf("编辑历史损坏")
	}
	return nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validateMarker(m Marker) error {
	if !validID(m.ID) || strings.TrimSpace(m.Notes) == "" || strings.ContainsAny(m.Notes, "{}\r\n") || len(m.Notes) > 512 || m.Frame < 0 || (m.Kind != "point" && m.Kind != "in" && m.Kind != "out") {
		return fmt.Errorf("无效的标记备注或位置")
	}
	if m.SourceTime != nil && (m.SourceTime.Scale <= 0 || !finite(Seconds(*m.SourceTime)) || Seconds(*m.SourceTime) < 0) {
		return fmt.Errorf("无效的素材标记时间")
	}
	return nil
}
func (s Sequence) Duration() int64 {
	var end int64
	for _, t := range s.Tracks {
		for _, c := range t.Clips {
			end = max(end, c.At+c.Duration)
		}
	}
	return end
}
func (s Sequence) Snapshot(d *AvpDocument) EditSnapshot {
	m := map[string][]Marker{}
	for _, r := range d.Resources {
		m[r.ID] = clone(r.Markers)
	}
	return EditSnapshot{State: clone(s.SequenceState), SourceMarkers: m}
}

type Operation struct {
	Type          string   `json:"type"`
	TrackID       string   `json:"track_id,omitempty"`
	TargetTrackID string   `json:"target_track_id,omitempty"`
	ClipIDs       []string `json:"clip_ids,omitempty"`
	ResourceID    string   `json:"resource_id,omitempty"`
	At            int64    `json:"at,omitempty"`
	End           *int64   `json:"end,omitempty"`
	SourceIn      *Time    `json:"source_in,omitempty"`
	SourceOut     *Time    `json:"source_out,omitempty"`
	Duration      *int64   `json:"duration,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Name          string   `json:"name,omitempty"`
	Height        *int     `json:"height,omitempty"`
	Order         *int     `json:"order,omitempty"`
	Locked        *bool    `json:"locked,omitempty"`
	Muted         *bool    `json:"muted,omitempty"`
	Hidden        *bool    `json:"hidden,omitempty"`
	Solo          *bool    `json:"solo,omitempty"`
	GainDB        *float64 `json:"gain_db,omitempty"`
	Speed         *float64 `json:"speed,omitempty"`
	Effects       *Effects `json:"effects,omitempty"`
	Marker        *Marker  `json:"marker,omitempty"`
	MarkerID      string   `json:"marker_id,omitempty"`
	GroupID       string   `json:"group_id,omitempty"`
	Text          string   `json:"text,omitempty"`
	Font          string   `json:"font,omitempty"`
	FontSize      int      `json:"font_size,omitempty"`
	Clips         []Clip   `json:"clips,omitempty"`
	Unlinked      bool     `json:"unlinked,omitempty"`
}
type Command struct {
	Session          string      `json:"session"`
	ExpectedRevision uint64      `json:"expected_revision"`
	IdempotencyKey   string      `json:"idempotency_key"`
	Label            string      `json:"label"`
	Operations       []Operation `json:"operations"`
}

func resource(d *AvpDocument, id string) (*Resource, error) {
	for i := range d.Resources {
		if d.Resources[i].ID == id {
			r := &d.Resources[i]
			if r.Missing || r.Removed {
				return nil, fmt.Errorf("素材缺失")
			}
			return r, nil
		}
	}
	return nil, fmt.Errorf("资源不属于当前工程")
}
func track(s *Sequence, id string) (*Track, error) {
	for i := range s.Tracks {
		if s.Tracks[i].ID == id {
			return &s.Tracks[i], nil
		}
	}
	return nil, fmt.Errorf("轨道不存在")
}
func number(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}
func Streams(r Resource, kind string) []int {
	raw, _ := json.Marshal(r.Metadata["streams"])
	var list []map[string]any
	_ = json.Unmarshal(raw, &list)
	out := []int{}
	for _, s := range list {
		if s["codec_type"] == kind {
			out = append(out, int(number(s["index"])))
		}
	}
	return out
}
func configure(s *Sequence, r Resource) {
	if s.Configured || r.Kind != "video" {
		return
	}
	raw, _ := json.Marshal(r.Metadata)
	var meta map[string]any
	_ = json.Unmarshal(raw, &meta)
	width, height := int(number(meta["width"])), int(number(meta["height"]))
	if width > 0 && height > 0 {
		s.Width, s.Height = width, height
	}
	var n, den int64
	if text, ok := meta["avg_frame_rate"].(string); ok {
		_, _ = fmt.Sscanf(text, "%d/%d", &n, &den)
	}
	if n > 0 && den > 0 && float64(n)/float64(den) <= 240 {
		old := s.FPS
		s.FPS = Rate{n, den}
		convert := func(v int64) int64 { return s.FPS.Frames(old.Seconds(v)) }
		for i := range s.Tracks {
			for j := range s.Tracks[i].Clips {
				c := &s.Tracks[i].Clips[j]
				c.At = convert(c.At)
				c.Duration = max(1, convert(c.Duration))
				c.Effects.FadeIn = convert(c.Effects.FadeIn)
				c.Effects.FadeOut = convert(c.Effects.FadeOut)
				c.Effects.Transition = convert(c.Effects.Transition)
				if c.Effects.FromFrame != nil {
					v := convert(*c.Effects.FromFrame)
					c.Effects.FromFrame = &v
				}
				if c.Effects.ToFrame != nil {
					v := convert(*c.Effects.ToFrame)
					c.Effects.ToFrame = &v
				}
			}
		}
		for i := range s.Markers {
			s.Markers[i].Frame = convert(s.Markers[i].Frame)
		}
	}
	s.Configured = true
}
func (s *Sequence) ConfigureResource(r Resource) { configure(s, r) }
func RebaseOperation(op Operation, from, to Rate) Operation {
	if from == to {
		return op
	}
	convert := func(v int64) int64 { return to.Frames(from.Seconds(v)) }
	op.At = convert(op.At)
	if op.End != nil {
		v := convert(*op.End)
		op.End = &v
	}
	if op.Duration != nil {
		v := max(int64(1), convert(*op.Duration))
		op.Duration = &v
	}
	if op.Marker != nil {
		m := *op.Marker
		m.Frame = convert(m.Frame)
		op.Marker = &m
	}
	fx := func(e Effects) Effects {
		e.FadeIn = convert(e.FadeIn)
		e.FadeOut = convert(e.FadeOut)
		e.Transition = convert(e.Transition)
		if e.FromFrame != nil {
			v := convert(*e.FromFrame)
			e.FromFrame = &v
		}
		if e.ToFrame != nil {
			v := convert(*e.ToFrame)
			e.ToFrame = &v
		}
		return e
	}
	if op.Effects != nil {
		e := fx(*op.Effects)
		op.Effects = &e
	}
	op.Clips = clone(op.Clips)
	for i := range op.Clips {
		c := &op.Clips[i]
		c.At = convert(c.At)
		c.Duration = max(1, convert(c.Duration))
		c.Effects = fx(c.Effects)
	}
	return op
}
func shiftMarkers(s *Sequence, at, delta int64) {
	for i := range s.Markers {
		m := &s.Markers[i]
		if !m.Locked && m.Frame >= at {
			m.Frame = max(0, m.Frame+delta)
		}
	}
}
func ripple(s *Sequence, at, delta int64, group string) error {
	for i := range s.Tracks {
		t := &s.Tracks[i]
		if t.GroupID != group {
			continue
		}
		for j := range t.Clips {
			c := &t.Clips[j]
			if c.At >= at {
				if t.Locked {
					return fmt.Errorf("波纹编辑涉及锁定轨道")
				}
				c.At = max(0, c.At+delta)
			}
		}
	}
	shiftMarkers(s, at, delta)
	return nil
}
func insertGap(s *Sequence, at, duration int64, group string) error {
	links := map[string]string{}
	for i := range s.Tracks {
		t := &s.Tracks[i]
		if t.GroupID != group {
			continue
		}
		out := []Clip{}
		for _, c := range t.Clips {
			if c.At < at && c.At+c.Duration > at {
				if t.Locked {
					return fmt.Errorf("插入涉及锁定轨道")
				}
				left, right := sliceClip(c, c.At, at, s.FPS), sliceClip(c, at, c.At+c.Duration, s.FPS)
				right.ID = TimelineID("clip")
				if c.LinkID != "" {
					if links[c.LinkID] == "" {
						links[c.LinkID] = TimelineID("link")
					}
					right.LinkID = links[c.LinkID]
				}
				out = append(out, left, right)
			} else {
				out = append(out, c)
			}
		}
		t.Clips = out
	}
	return ripple(s, at, duration, group)
}
func sliceClip(c Clip, from, to int64, fps Rate) Clip {
	original := c.At
	c.At = from
	c.Duration = to - from
	in := Seconds(c.SourceIn)
	c.SourceIn = timeAt(in+fps.Seconds(from-original)*c.Speed, c.SourceIn.Scale)
	c.SourceOut = timeAt(in+fps.Seconds(to-original)*c.Speed, c.SourceOut.Scale)
	c.Effects.FadeIn = min(c.Effects.FadeIn, c.Duration)
	c.Effects.FadeOut = min(c.Effects.FadeOut, c.Duration)
	c.Effects.Transition = min(c.Effects.Transition, c.Duration)
	if from > original {
		c.Effects.Transition = 0
	}
	if c.Effects.FromFrame != nil || c.Effects.ToFrame != nil {
		start, end := int64(0), to-original
		if c.Effects.FromFrame != nil {
			start = *c.Effects.FromFrame
		}
		if c.Effects.ToFrame != nil {
			end = *c.Effects.ToFrame
		}
		start = max(int64(0), start-(from-original))
		end = min(c.Duration, end-(from-original))
		if end <= start {
			c.Effects = Effects{Scale: 1}
		} else {
			c.Effects.FromFrame = &start
			c.Effects.ToFrame = &end
		}
	}
	return c
}
func cutTrack(t *Track, at, end int64, fps Rate, except map[string]bool) error {
	out := []Clip{}
	for _, c := range t.Clips {
		if except[c.ID] || c.At >= end || c.At+c.Duration <= at {
			out = append(out, c)
			continue
		}
		if t.Locked {
			return fmt.Errorf("目标轨道已锁定")
		}
		if c.At < at {
			out = append(out, sliceClip(c, c.At, at, fps))
		}
		if c.At+c.Duration > end {
			right := sliceClip(c, end, c.At+c.Duration, fps)
			if c.At < at {
				right.ID = TimelineID("clip")
			}
			out = append(out, right)
		}
	}
	t.Clips = out
	return nil
}
func expandSelection(s Sequence, ids []string, unlinked bool) map[string]bool {
	selected := map[string]bool{}
	links := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	for _, t := range s.Tracks {
		for _, c := range t.Clips {
			if selected[c.ID] && c.LinkID != "" && !unlinked {
				links[c.LinkID] = true
			}
		}
	}
	for _, t := range s.Tracks {
		for _, c := range t.Clips {
			if links[c.LinkID] {
				selected[c.ID] = true
			}
		}
	}
	return selected
}
func (s *Sequence) Apply(d *AvpDocument, op Operation, origin Origin) error {
	selected := expandSelection(*s, op.ClipIDs, op.Unlinked)
	for _, t := range s.Tracks {
		for _, c := range t.Clips {
			if selected[c.ID] && t.Locked {
				return fmt.Errorf("所选片段在锁定轨道上")
			}
		}
	}
	switch op.Type {
	case "gap.insert":
		if op.At < 0 || op.Duration == nil || *op.Duration <= 0 {
			return fmt.Errorf("插入空隙需要合法的起点和时长")
		}
		t, e := track(s, op.TrackID)
		if e != nil {
			return e
		}
		if t.Locked {
			return fmt.Errorf("目标轨道已锁定")
		}
		return insertGap(s, op.At, *op.Duration, t.GroupID)
	case "sequence.rename":
		if strings.TrimSpace(op.Name) == "" {
			return fmt.Errorf("名称不能为空")
		}
		s.Name = strings.TrimSpace(op.Name)
	case "scope.set":
		if op.GroupID != "" && op.GroupID != "original" && !slices.ContainsFunc(s.Groups, func(g TrackGroup) bool { return g.ID == op.GroupID }) {
			return fmt.Errorf("轨组不存在")
		}
		s.ScopeGroup = op.GroupID
	case "track.add":
		if op.Kind != "video" && op.Kind != "audio" && op.Kind != "subtitle" {
			return fmt.Errorf("轨道类型无效")
		}
		t := newTrack(op.Kind, op.GroupID)
		if op.Name != "" {
			t.Name = op.Name
		}
		s.Tracks = append(s.Tracks, t)
	case "track.update", "track.remove":
		t, e := track(s, op.TrackID)
		if e != nil {
			return e
		}
		if op.Type == "track.remove" {
			if t.Locked {
				return fmt.Errorf("轨道已锁定")
			}
			s.Tracks = slices.DeleteFunc(s.Tracks, func(t Track) bool { return t.ID == op.TrackID })
			break
		}
		if op.Name != "" {
			t.Name = op.Name
		}
		if op.Height != nil {
			if *op.Height < 40 || *op.Height > 300 {
				return fmt.Errorf("轨道高度为 40～300")
			}
			t.Height = *op.Height
		}
		if op.Locked != nil {
			t.Locked = *op.Locked
		}
		if op.Muted != nil {
			t.Muted = *op.Muted
		}
		if op.Hidden != nil {
			t.Hidden = *op.Hidden
		}
		if op.Solo != nil {
			t.Solo = *op.Solo
		}
		if op.GainDB != nil {
			t.GainDB = *op.GainDB
		}
		if op.Order != nil {
			old := slices.IndexFunc(s.Tracks, func(t Track) bool { return t.ID == op.TrackID })
			v := s.Tracks[old]
			s.Tracks = slices.Delete(s.Tracks, old, old+1)
			at := max(0, min(len(s.Tracks), *op.Order))
			s.Tracks = slices.Insert(s.Tracks, at, v)
		}
	case "clip.insert":
		return s.insert(d, op, origin)
	case "clip.paste":
		t, e := track(s, op.TrackID)
		if e != nil {
			return e
		}
		if t.Locked {
			return fmt.Errorf("轨道已锁定")
		}
		for _, original := range op.Clips {
			r, err := resource(d, original.ResourceID)
			if err != nil {
				return err
			}
			if t.Kind == "audio" && len(Streams(*r, "audio")) == 0 || t.Kind == "video" && r.Kind != "video" && r.Kind != "image" {
				return fmt.Errorf("粘贴轨道类型不匹配")
			}
			if (t.Kind == "audio" || t.Kind == "video") && r.Kind != "image" && !slices.Contains(Streams(*r, t.Kind), original.StreamIndex) {
				return fmt.Errorf("粘贴片段引用了不存在的源流")
			}
			c := clone(original)
			c.ID = TimelineID("clip")
			c.At += op.At
			c.Origin = editedOrigin(c.Origin, origin)
			if e = cutTrack(t, c.At, c.At+c.Duration, s.FPS, nil); e != nil {
				return e
			}
			t.Clips = append(t.Clips, c)
		}
	case "clip.delete":
		var start int64 = math.MaxInt64
		var end int64
		groups := map[string]bool{}
		for i := range s.Tracks {
			t := &s.Tracks[i]
			t.Clips = slices.DeleteFunc(t.Clips, func(c Clip) bool {
				if selected[c.ID] {
					groups[t.GroupID] = true
					start = min(start, c.At)
					end = max(end, c.At+c.Duration)
					return true
				}
				return false
			})
		}
		if end == 0 {
			return fmt.Errorf("未选择片段")
		}
		if op.Mode == "ripple" {
			if len(groups) != 1 {
				return fmt.Errorf("请在同一对照轨组内波纹删除")
			}
			group := ""
			for id := range groups {
				group = id
			}
			for _, t := range s.Tracks {
				if t.GroupID != group {
					continue
				}
				for _, c := range t.Clips {
					if c.At < end && c.At+c.Duration > start {
						return fmt.Errorf("波纹范围仍有未选择内容，请一并选择或使用普通删除")
					}
				}
			}
			for i := range s.Markers {
				m := &s.Markers[i]
				if !m.Locked && m.Frame >= start && m.Frame < end {
					m.Frame = start
				}
			}
			if e := ripple(s, end, start-end, group); e != nil {
				return e
			}
		}
	case "clip.split":
		count := 0
		linkRight := map[string]string{}
		for i := range s.Tracks {
			t := &s.Tracks[i]
			out := []Clip{}
			for _, c := range t.Clips {
				if selected[c.ID] && op.At > c.At && op.At < c.At+c.Duration {
					left := sliceClip(c, c.At, op.At, s.FPS)
					right := sliceClip(c, op.At, c.At+c.Duration, s.FPS)
					right.ID = TimelineID("clip")
					if c.LinkID != "" {
						if linkRight[c.LinkID] == "" {
							linkRight[c.LinkID] = TimelineID("link")
						}
						right.LinkID = linkRight[c.LinkID]
					}
					left.Origin, right.Origin = editedOrigin(c.Origin, origin), editedOrigin(c.Origin, origin)
					out = append(out, left, right)
					count++
				} else {
					out = append(out, c)
				}
			}
			t.Clips = out
		}
		if count == 0 {
			return fmt.Errorf("播放头不在所选片段内部")
		}
	case "clip.move":
		var first *Clip
		var originalTrack string
		for _, t := range s.Tracks {
			for _, c := range t.Clips {
				if len(op.ClipIDs) > 0 && c.ID == op.ClipIDs[0] {
					x := c
					first = &x
					originalTrack = t.ID
				}
			}
		}
		if first == nil {
			return fmt.Errorf("未选择片段")
		}
		delta := op.At - first.At
		moving := []struct {
			track string
			clip  Clip
		}{}
		for i := range s.Tracks {
			t := &s.Tracks[i]
			t.Clips = slices.DeleteFunc(t.Clips, func(c Clip) bool {
				if !selected[c.ID] {
					return false
				}
				moving = append(moving, struct {
					track string
					clip  Clip
				}{t.ID, c})
				return true
			})
		}
		if op.Mode == "insert" {
			sourceTrack, _ := track(s, originalTrack)
			span := int64(0)
			for _, item := range moving {
				span = max(span, item.clip.At+item.clip.Duration-first.At)
			}
			if err := insertGap(s, op.At, span, sourceTrack.GroupID); err != nil {
				return err
			}
		}
		for _, m := range moving {
			target := m.track
			if m.track == originalTrack && op.TargetTrackID != "" {
				target = op.TargetTrackID
			}
			t, e := track(s, target)
			if e != nil {
				return e
			}
			src, _ := track(s, m.track)
			if t.Locked || t.Kind != src.Kind || t.GroupID != src.GroupID {
				return fmt.Errorf("目标轨道类型不匹配或已锁定")
			}
			m.clip.At += delta
			if m.clip.At < 0 {
				return fmt.Errorf("片段不能移到序列起点之前")
			}
			if e = cutTrack(t, m.clip.At, m.clip.At+m.clip.Duration, s.FPS, nil); e != nil {
				return e
			}
			m.clip.Origin = editedOrigin(m.clip.Origin, origin)
			t.Clips = append(t.Clips, m.clip)
		}
	case "clip.trim", "clip.update", "clip.link", "clip.unlink":
		count := 0
		linkID := TimelineID("link")
		for i := range s.Tracks {
			t := &s.Tracks[i]
			for j := range t.Clips {
				c := &t.Clips[j]
				if !selected[c.ID] {
					continue
				}
				count++
				switch op.Type {
				case "clip.link":
					c.LinkID = linkID
				case "clip.unlink":
					c.LinkID = ""
				case "clip.trim":
					end := c.At + c.Duration
					if op.End != nil {
						end = *op.End
					}
					start := op.At
					if start < 0 || start >= end {
						return fmt.Errorf("修剪范围无效")
					}
					original := *c
					*c = sliceClip(original, start, end, s.FPS)
				case "clip.update":
					if op.Speed != nil {
						if !finite(*op.Speed) || *op.Speed <= 0 {
							return fmt.Errorf("速度必须为正数")
						}
						c.Speed = *op.Speed
						c.Duration = max(1, s.FPS.Frames((Seconds(c.SourceOut)-Seconds(c.SourceIn))/c.Speed))
					}
					if op.SourceIn != nil {
						c.SourceIn = *op.SourceIn
					}
					if op.SourceOut != nil {
						c.SourceOut = *op.SourceOut
					}
					if op.SourceIn != nil || op.SourceOut != nil {
						c.Duration = max(1, s.FPS.Frames((Seconds(c.SourceOut)-Seconds(c.SourceIn))/c.Speed))
					}
					if op.Duration != nil {
						c.Duration = *op.Duration
						c.SourceOut = MediaTime(Seconds(c.SourceIn) + s.FPS.Seconds(c.Duration)*c.Speed)
					}
					if op.Effects != nil {
						c.Effects = *op.Effects
					}
					if op.Name != "" {
						c.Name = op.Name
					}
					if op.Text != "" {
						c.Text = op.Text
					}
					if op.Font != "" {
						c.Font = op.Font
					}
					if op.FontSize > 0 {
						c.FontSize = op.FontSize
					}
				}
				c.Origin = editedOrigin(c.Origin, origin)
				if c.Text == "" {
					r, e := resource(d, c.ResourceID)
					if e != nil {
						return e
					}
					if Seconds(c.SourceIn) < -1e-8 || (r.Kind != "image" && Seconds(c.SourceOut) > number(r.Metadata["duration"])+1e-6) {
						return fmt.Errorf("修剪或变速超出源素材")
					}
				}
			}
		}
		if count == 0 {
			return fmt.Errorf("未选择片段")
		}
		if op.Type == "clip.trim" || op.Type == "clip.update" {
			for _, t := range s.Tracks {
				for _, c := range t.Clips {
					if !selected[c.ID] {
						continue
					}
					for _, other := range t.Clips {
						if c.ID != other.ID && c.At < other.At+other.Duration && c.At+c.Duration > other.At {
							return fmt.Errorf("调整后片段重叠，请先腾出空间")
						}
					}
				}
			}
		}
	case "marker.add", "marker.update", "marker.remove":
		return s.editMarker(d, op)
	default:
		return fmt.Errorf("未知编辑命令 %s", op.Type)
	}
	return nil
}
func editedOrigin(old, next Origin) Origin {
	if strings.HasPrefix(next.Actor, "agent") {
		return next
	}
	return old
}
func (s *Sequence) insert(d *AvpDocument, op Operation, origin Origin) error {
	if op.At < 0 {
		return fmt.Errorf("落点不能为负")
	}
	r, e := resource(d, op.ResourceID)
	if e != nil {
		return e
	}
	if r.Kind != "video" && r.Kind != "audio" && r.Kind != "image" && r.Kind != "subtitle" {
		return fmt.Errorf("此类资源请在脚本工作台打开")
	}
	oldRate := s.FPS
	configure(s, *r)
	op = RebaseOperation(op, oldRate, s.FPS)
	start, end := r.In, r.Out
	if r.Kind == "image" {
		start, end = 0, 5
	}
	if op.SourceIn != nil {
		start = Seconds(*op.SourceIn)
	}
	if op.SourceOut != nil {
		end = Seconds(*op.SourceOut)
	}
	if r.Kind == "subtitle" {
		if op.Duration == nil || op.Text == "" {
			return fmt.Errorf("字幕需要正文和时长")
		}
		start, end = 0, s.FPS.Seconds(*op.Duration)
	}
	if end <= start || start < 0 || (r.Kind != "image" && r.Kind != "subtitle" && end > number(r.Metadata["duration"])+1e-6) {
		return fmt.Errorf("入出点超出素材范围")
	}
	duration := max(int64(1), s.FPS.Frames(end-start))
	if op.Duration != nil && r.Kind == "image" {
		duration = *op.Duration
		end = s.FPS.Seconds(duration)
	}
	target, e := track(s, op.TrackID)
	if e != nil {
		return e
	}
	if target.Locked {
		return fmt.Errorf("目标轨道已锁定")
	}
	expected := map[string]string{"video": "video", "image": "video", "audio": "audio", "subtitle": "subtitle"}[r.Kind]
	if target.Kind != expected {
		return fmt.Errorf("素材与轨道类型不匹配")
	}
	if op.Mode == "insert" { // Split crossing clips before shifting so no source content is lost.
		if e = insertGap(s, op.At, duration, target.GroupID); e != nil {
			return e
		}
	}
	target, _ = track(s, op.TrackID)
	targetID := target.ID
	group := target.GroupID
	makeClip := func(stream int) Clip {
		return Clip{ID: TimelineID("clip"), ResourceID: r.ID, Name: r.Name, StreamIndex: stream, At: op.At, Duration: duration, SourceIn: SourceTime(*r, stream, start), SourceOut: SourceTime(*r, stream, end), Speed: 1, Origin: origin, Effects: Effects{Scale: 1}, Text: op.Text, Font: op.Font, FontSize: op.FontSize}
	}
	link := TimelineID("link")
	appendTo := func(id string, c Clip) error {
		t, e := track(s, id)
		if e != nil {
			return e
		}
		if t.Locked {
			return fmt.Errorf("轨道已锁定")
		}
		if e = cutTrack(t, c.At, c.At+c.Duration, s.FPS, nil); e != nil {
			return e
		}
		c.LinkID = link
		t.Clips = append(t.Clips, c)
		return nil
	}
	if r.Kind == "audio" {
		streams := Streams(*r, "audio")
		if len(streams) == 0 {
			return fmt.Errorf("没有可用音轨")
		}
		return s.appendAudio(streams, targetID, group, makeClip, appendTo)
	}
	stream := 0
	if streams := Streams(*r, "video"); len(streams) > 0 {
		stream = streams[0]
	}
	if e = appendTo(targetID, makeClip(stream)); e != nil {
		return e
	}
	if r.Kind == "video" {
		streams := Streams(*r, "audio")
		if len(streams) > 0 {
			return s.appendAudio(streams, "", group, makeClip, appendTo)
		}
	}
	return nil
}
func (s *Sequence) appendAudio(streams []int, target, group string, makeClip func(int) Clip, appendTo func(string, Clip) error) error {
	ids := []string{}
	for _, t := range s.Tracks {
		if t.Kind == "audio" && t.GroupID == group {
			ids = append(ids, t.ID)
		}
	}
	start := 0
	if target != "" {
		start = slices.Index(ids, target)
		if start < 0 {
			return fmt.Errorf("目标音轨不属于当前轨组")
		}
	}
	for len(ids) < start+len(streams) {
		t := newTrack("audio", group)
		s.Tracks = append(s.Tracks, t)
		ids = append(ids, t.ID)
	}
	for i, stream := range streams {
		if e := appendTo(ids[start+i], makeClip(stream)); e != nil {
			return e
		}
	}
	return nil
}
func (s *Sequence) editMarker(d *AvpDocument, op Operation) error {
	list := &s.Markers
	var source *Resource
	if op.ResourceID != "" {
		r, e := resource(d, op.ResourceID)
		if e != nil {
			return e
		}
		list = &r.Markers
		source = r
	}
	prepare := func(m *Marker) error {
		if source != nil {
			if source.Kind != "video" && source.Kind != "audio" && source.Kind != "image" {
				return fmt.Errorf("此类资源没有素材帧标记")
			}
			if m.SourceTime == nil {
				return fmt.Errorf("素材标记需要源时间")
			}
			value := Seconds(*m.SourceTime)
			if source.Kind != "image" && (value < 0 || value >= number(source.Metadata["duration"])) {
				return fmt.Errorf("素材标记超出源范围")
			}
			streams := Streams(*source, "video")
			if source.Kind == "audio" {
				streams = Streams(*source, "audio")
			}
			index := 0
			if len(streams) > 0 {
				index = streams[0]
			}
			time := SourceTime(*source, index, value)
			m.SourceTime = &time
		} else if m.SourceTime != nil {
			return fmt.Errorf("时间线标记不应携带素材时间")
		}
		return validateMarker(*m)
	}
	if op.Type == "marker.add" {
		if op.Marker == nil {
			return fmt.Errorf("缺少标记")
		}
		m := *op.Marker
		m.ID = TimelineID("mark")
		if m.Notes == "" {
			m.Notes = fmt.Sprintf("标记%d", len(*list)+1)
		}
		if m.Kind == "" {
			m.Kind = "point"
		}
		if e := prepare(&m); e != nil {
			return e
		}
		*list = append(*list, m)
		return nil
	}
	i := slices.IndexFunc(*list, func(m Marker) bool { return m.ID == op.MarkerID })
	if i < 0 {
		return fmt.Errorf("标记不存在")
	}
	if op.Type == "marker.remove" {
		*list = slices.Delete(*list, i, i+1)
		return nil
	}
	if op.Marker == nil {
		return fmt.Errorf("缺少标记内容")
	}
	m := *op.Marker
	m.ID = op.MarkerID
	if e := prepare(&m); e != nil {
		return e
	}
	old := (*list)[i]
	(*list)[i] = m
	if old.Notes != m.Notes {
		RenameMarkerReferences(d, m.ID, m.Notes)
	}
	return nil
}
