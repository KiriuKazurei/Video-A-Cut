package project

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

func timelineFixture() AvpDocument {
	return AvpDocument{Format: "avp", SchemaVersion: 2, Project: Identity{ID: "p", Name: "test"}, Resources: []Resource{{ID: "r", Name: "source", Kind: "video", Out: 10, Metadata: map[string]any{"duration": 10, "width": 1920, "height": 1080, "avg_frame_rate": "30000/1001", "streams": []map[string]any{{"index": 0, "codec_type": "video"}, {"index": 2, "codec_type": "audio"}, {"index": 4, "codec_type": "audio"}, {"index": 5, "codec_type": "audio"}, {"index": 7, "codec_type": "audio"}}}}}, Sequences: []Sequence{NewSequence("main")}}
}

func TestTimelineOpenNormalizesEmptyCollectionsAndUndoStates(t *testing.T) {
	d := timelineFixture()
	d.Sequences[0].Markers = nil
	d.Sequences[0].Groups = nil
	d.Sequences[0].Tracks[0].Clips = nil
	d.Sequences[0].History = []EditRecord{{Before: EditSnapshot{}, After: EditSnapshot{}}}
	d.Upgrade()
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"markers":null`) || strings.Contains(string(data), `"groups":null`) || strings.Contains(string(data), `"clips":null`) || strings.Contains(string(data), `"tracks":null`) {
		t.Fatalf("empty editor collections must remain iterable after open and undo: %s", data)
	}
}
func TestTimelineStreamsLinksAndSourceAnchors(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "r"}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	if len(s.Tracks) != 7 || s.FPS != (Rate{30000, 1001}) {
		t.Fatalf("auto-expand/configuration: %+v", s)
	}
	streams := []int{}
	for _, tr := range s.Tracks {
		for _, c := range tr.Clips {
			if c.LinkID != s.Tracks[0].Clips[0].LinkID {
				t.Fatal("missing A/V link")
			}
			streams = append(streams, c.StreamIndex)
		}
	}
	if got := strings.Trim(strings.Join(strings.Fields(strings.TrimSpace(string(mustJSON(streams)))), ""), " "); got != "[0,2,4,5,7]" {
		t.Fatalf("stream mapping %s", got)
	}
	source := MediaTime(2)
	d.Resources[0].Markers = []Marker{{ID: "mark", Notes: "开场", Kind: "point", SourceTime: &source}}
	var candidate ReferenceCandidate
	for _, r := range ReferenceDirectory(&d, s.ID) {
		if r.ClipID != "" && r.Kind == "source_marker" {
			candidate = r
			break
		}
	}
	if candidate.Frame == nil || *candidate.Frame != s.FPS.Frames(2) {
		t.Fatalf("bad source mapping %+v", candidate)
	}
	clip := s.Tracks[0].Clips[0]
	if err := s.Apply(&d, Operation{Type: "clip.move", ClipIDs: []string{clip.ID}, At: 90}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	mapped, err := ResolveReference(&d, candidate.Reference)
	if err != nil || *mapped.Frame != 90+s.FPS.Frames(2) {
		t.Fatalf("moved anchor %+v %v", mapped, err)
	}
	if err = s.Apply(&d, Operation{Type: "clip.update", ClipIDs: []string{clip.ID}, Speed: ptr(2.0)}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	mapped, err = ResolveReference(&d, candidate.Reference)
	if err != nil || *mapped.Frame != 90+s.FPS.Frames(1) {
		t.Fatalf("speed mapping %+v %v", mapped, err)
	}
	if err = d.Validate(); err != nil {
		t.Fatal(err)
	}
}
func ptr[T any](v T) *T     { return &v }
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func TestTimelineRippleMarkerLocksAndTrimInvalidation(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Configured = true
	s.FPS = Rate{30, 1}
	for _, at := range []int64{0, 300} {
		if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "r", At: at}, Origin{}); err != nil {
			t.Fatal(err)
		}
	}
	s.Markers = []Marker{{ID: "follow", Notes: "follow", Kind: "point", Frame: 330}, {ID: "fixed", Notes: "fixed", Kind: "point", Frame: 330, Locked: true}}
	if err := s.Apply(&d, Operation{Type: "clip.delete", ClipIDs: []string{s.Tracks[0].Clips[0].ID}, Mode: "ripple"}, Origin{}); err != nil {
		t.Fatal(err)
	}
	if s.Markers[0].Frame != 30 || s.Markers[1].Frame != 330 || s.Tracks[0].Clips[0].At != 0 {
		t.Fatalf("ripple markers %+v", s.Markers)
	}
	at := MediaTime(2)
	d.Resources[0].Markers = []Marker{{ID: "source", Notes: "source", Kind: "point", SourceTime: &at}}
	var ref Reference
	for _, r := range ReferenceDirectory(&d, s.ID) {
		if r.ClipID == s.Tracks[0].Clips[0].ID && r.Kind == "source_marker" {
			ref = r.Reference
		}
	}
	if err := s.Apply(&d, Operation{Type: "clip.trim", ClipIDs: []string{ref.ClipID}, At: 90, End: ptr(int64(300))}, Origin{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveReference(&d, ref); err == nil {
		t.Fatal("trimmed-away anchor silently rebound")
	}
}
func TestReferenceDuplicateSelectionAndSelectiveUTF16Rename(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Markers = []Marker{{ID: "a", Kind: "point", Notes: "重名", Frame: 10}, {ID: "b", Kind: "point", Notes: "重名", Frame: 20}}
	body := "😀 {@重名} 正文重名 {@重名}"
	tokens := ReferenceTokens(body)
	if tokens[0].Start != 3 {
		t.Fatalf("not UTF16: %+v", tokens)
	}
	refs, err := BindReferences(&d, body, nil, s.ID, false)
	if err != nil || !refs[0].Unresolved {
		t.Fatal("ambiguous name bound implicitly")
	}
	if _, err = BindReferences(&d, body, nil, s.ID, true); err == nil {
		t.Fatal("ambiguous name accepted")
	}
	candidates := ReferenceDirectory(&d, s.ID)
	for i := range refs {
		refs[i].Unresolved = false
		refs[i].Kind = "sequence_marker"
		refs[i].SequenceID = s.ID
		refs[i].TargetID = []string{"a", "b"}[i]
	}
	_ = candidates
	d.Scripts = []AvsDocument{{Format: "avs", SchemaVersion: 2, ID: "script", Body: body, Segments: json.RawMessage(`[]`), References: refs}}
	RenameMarkerReferences(&d, "a", "新的备注")
	if got := d.Scripts[0].Body; got != "😀 {@新的备注} 正文重名 {@重名}" {
		t.Fatal(got)
	}
	if err = ValidateReferences(d.Scripts[0].Body, d.Scripts[0].References); err != nil {
		t.Fatal(err)
	}
	if d.Scripts[0].References[1].End != len(utf16.Encode([]rune(d.Scripts[0].Body))) {
		t.Fatal("later occurrence offset not updated")
	}
}
func TestAVPVersionOneUpgradeAndTwoRoundTrip(t *testing.T) {
	raw := []byte(`{"format":"avp","schema_version":1,"project":{"id":"p","name":"old"},"resources":[],"scripts":[],"sequences":[{"timeline":{"fps":30},"video":[]}],"settings":{},"workspace":{}}`)
	var d AvpDocument
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.Upgrade()
	if len(d.Sequences) != 1 || len(d.LegacySequences) != 1 || d.SchemaVersion != SchemaVersion {
		t.Fatalf("migration %+v", d)
	}
	var round AvpDocument
	if err := json.Unmarshal(mustJSON(d), &round); err != nil {
		t.Fatal(err)
	}
	if err := round.Validate(); err != nil {
		t.Fatal(err)
	}
	if round.Sequences[0].ID != d.Sequences[0].ID || len(round.LegacySequences) != 1 {
		t.Fatal("unstable roundtrip")
	}
}

func TestTimelineInsertPreservesSourceSamplesAndScopedEffects(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Configured = true
	s.FPS = Rate{30, 1}
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "r"}, Origin{}); err != nil {
		t.Fatal(err)
	}
	c := &s.Tracks[0].Clips[0]
	c.Effects.GainDB = -6
	c.Effects.FromFrame = ptr(int64(60))
	c.Effects.ToFrame = ptr(int64(180))
	originalID := c.ID
	if err := s.Apply(&d, Operation{Type: "gap.insert", TrackID: s.Tracks[0].ID, At: 90, Duration: ptr(int64(30))}, Origin{}); err != nil {
		t.Fatal(err)
	}
	clips := s.Tracks[0].Clips
	if len(clips) != 2 || clips[0].ID != originalID || clips[1].ID == originalID || clips[1].At != 120 {
		t.Fatalf("gap lost content: %+v", clips)
	}
	if *clips[0].Effects.FromFrame != 60 || *clips[0].Effects.ToFrame != 90 || *clips[1].Effects.FromFrame != 0 || *clips[1].Effects.ToFrame != 90 {
		t.Fatal("scoped effects no longer follow their original frames")
	}
	if Seconds(clips[0].SourceOut) != 3 || Seconds(clips[1].SourceIn) != 3 {
		t.Fatal("gap cut shifted source frames")
	}
	for _, tr := range s.Tracks {
		if len(tr.Clips) == 2 && tr.Clips[1].LinkID != clips[1].LinkID {
			t.Fatal("split link groups diverged")
		}
	}
	r := Resource{Metadata: map[string]any{"streams": []map[string]any{{"index": 3, "codec_type": "audio", "sample_rate": "44100"}}}}
	time := SourceTime(r, 3, 12345.0/44100)
	if time.Scale != 44100 || time.Value != 12345 {
		t.Fatalf("sample precision %+v", time)
	}
}

func TestGapCloseRipplesEmptySpanAndRefusesOverlap(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Configured = true
	s.FPS = Rate{30, 1}
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "r"}, Origin{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(&d, Operation{Type: "gap.insert", TrackID: s.Tracks[0].ID, At: 90, Duration: ptr(int64(30))}, Origin{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(&d, Operation{Type: "gap.close", TrackID: s.Tracks[0].ID, At: 0, Duration: ptr(int64(10))}, Origin{}); err == nil {
		t.Fatal("overlap must be refused")
	}
	s.Markers = []Marker{
		{ID: "inside", Notes: "空", Kind: "point", Frame: 100},
		{ID: "after", Notes: "后", Kind: "point", Frame: 200},
		{ID: "lock", Notes: "锁", Kind: "point", Frame: 200, Locked: true},
	}
	if err := s.Apply(&d, Operation{Type: "gap.close", TrackID: s.Tracks[0].ID, At: 90, Duration: ptr(int64(30))}, Origin{}); err != nil {
		t.Fatal(err)
	}
	clips := s.Tracks[0].Clips
	if len(clips) != 2 || clips[1].At != 90 {
		t.Fatalf("gap not closed: %+v", clips)
	}
	got := map[string]int64{}
	for _, m := range s.Markers {
		got[m.ID] = m.Frame
	}
	if got["inside"] != 90 || got["after"] != 170 || got["lock"] != 200 {
		t.Fatalf("markers %+v", got)
	}
}

func TestReferenceLiteralSubtitleAndIndependentInstances(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Configured = true
	s.FPS = Rate{30, 1}
	for _, at := range []int64{0, 300} {
		if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "r", At: at}, Origin{}); err != nil {
			t.Fatal(err)
		}
	}
	time := MediaTime(1)
	d.Resources[0].Markers = []Marker{{ID: "source", Notes: "frame", Kind: "point", SourceTime: &time}}
	var refs []Reference
	for _, candidate := range ReferenceDirectory(&d, s.ID) {
		if candidate.Kind == "source_marker" && candidate.ClipID != "" {
			refs = append(refs, candidate.Reference)
		}
	}
	if len(refs) < 2 {
		t.Fatal("missing concrete instances")
	}
	one := refs[0]
	var other Reference
	for _, ref := range refs {
		if ref.ClipID != one.ClipID {
			other = ref
			break
		}
	}
	before, _ := ResolveReference(&d, other)
	if err := s.Apply(&d, Operation{Type: "clip.move", ClipIDs: []string{one.ClipID}, At: 700, Unlinked: true}, Origin{}); err != nil {
		t.Fatal(err)
	}
	after, err := ResolveReference(&d, other)
	if err != nil || *before.Frame != *after.Frame {
		t.Fatal("another instance followed the move")
	}
	tokens := ReferenceTokens(`{"literal {@not-a-reference}"} {@frame}`)
	if len(tokens) != 1 || tokens[0].Name != "frame" {
		t.Fatalf("literal subtitle bound as reference: %+v", tokens)
	}
}

func TestGainKeyframesRoundTripAndSliceRemap(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "r"}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	audio := s.Tracks[3]
	clip := audio.Clips[0]
	keys := []GainKeyframe{{T: 0, DB: 0}, {T: 0.5, DB: 6}, {T: 1, DB: -12}}
	fx := clip.Effects
	fx.GainDB = 0
	fx.GainKeyframes = keys
	if err := s.Apply(&d, Operation{Type: "clip.update", ClipIDs: []string{clip.ID}, Effects: &fx}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s.Tracks[3].Clips[0].Effects)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"gain_keyframes"`) || !strings.Contains(string(raw), `"db":6`) {
		t.Fatalf("keyframes missing from JSON: %s", raw)
	}
	var back Effects
	if err = json.Unmarshal(raw, &back); err != nil || len(back.GainKeyframes) != 3 || back.GainKeyframes[1].DB != 6 {
		t.Fatalf("round-trip %+v %v", back, err)
	}
	// Split at midpoint: left half should end at +6 dB, right half start at +6 dB.
	mid := clip.At + clip.Duration/2
	if err = s.Apply(&d, Operation{Type: "clip.split", ClipIDs: []string{clip.ID}, At: mid}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	left, right := s.Tracks[3].Clips[0], s.Tracks[3].Clips[1]
	if len(left.Effects.GainKeyframes) < 2 || left.Effects.GainKeyframes[len(left.Effects.GainKeyframes)-1].DB != 6 {
		t.Fatalf("left remap %+v", left.Effects.GainKeyframes)
	}
	if len(right.Effects.GainKeyframes) < 2 || right.Effects.GainKeyframes[0].DB != 6 {
		t.Fatalf("right remap %+v", right.Effects.GainKeyframes)
	}
	if err = d.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := left.Effects
	bad.GainKeyframes = []GainKeyframe{{T: 0.5, DB: 0}, {T: 0.2, DB: 1}}
	if err = s.Apply(&d, Operation{Type: "clip.update", ClipIDs: []string{left.ID}, Effects: &bad}, Origin{Actor: "human"}); err != nil {
		t.Fatal(err)
	}
	if err = d.Validate(); err == nil {
		t.Fatal("unsorted keyframes must be rejected by Validate")
	}
}

func TestTimelineCrossGroupMoveMixesDrawingAndVideo(t *testing.T) {
	for _, drawing := range []bool{false, true} {
		t.Run(fmt.Sprint(drawing), func(t *testing.T) {
			d := timelineFixture()
			s := &d.Sequences[0]
			s.Configured = true
			s.FPS = Rate{30, 1}
			r := Resource{ID: "visual", Kind: "video", Out: 2, Metadata: map[string]any{"duration": 2, "streams": []map[string]any{{"index": 0, "codec_type": "video"}}}}
			if drawing {
				r.Draw = &DrawSourceReference{CompositionID: "scene", Mode: "snapshot"}
			}
			d.Resources = append(d.Resources, r)
			s.Groups = append(s.Groups, TrackGroup{ID: "agent-group"})
			s.Tracks = append(s.Tracks, newTrack("video", "agent-group"))
			src := s.Tracks[len(s.Tracks)-1].ID
			dst := s.Tracks[0].ID
			if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: src, ResourceID: r.ID}, Origin{Actor: "agent:run"}); err != nil {
				t.Fatal(err)
			}
			clip := s.Tracks[len(s.Tracks)-1].Clips[0]
			if err := s.Apply(&d, Operation{Type: "clip.move", ClipIDs: []string{clip.ID}, TargetTrackID: dst, At: 90}, Origin{Actor: "human:window"}); err != nil {
				t.Fatal(err)
			}
			if len(s.Tracks[0].Clips) != 1 || s.Tracks[0].Clips[0].ResourceID != r.ID || s.Tracks[0].Clips[0].At != 90 {
				t.Fatal("drawing/video was not moved intact")
			}
		})
	}
}
func TestTimelineCrossGroupMoveKeepsLinkedAudioInDestinationGroup(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Groups = append(s.Groups, TrackGroup{ID: "g"})
	s.Tracks = append(s.Tracks, newTrack("video", "g"))
	src := s.Tracks[len(s.Tracks)-1].ID
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: src, ResourceID: "r"}, Origin{}); err != nil {
		t.Fatal(err)
	}
	var id, link string
	for _, tr := range s.Tracks {
		if tr.ID == src {
			id = tr.Clips[0].ID
			link = tr.Clips[0].LinkID
		}
	}
	if err := s.Apply(&d, Operation{Type: "clip.move", ClipIDs: []string{id}, TargetTrackID: s.Tracks[0].ID, At: 120}, Origin{Actor: "human:window"}); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tr := range s.Tracks {
		for _, c := range tr.Clips {
			if c.LinkID == link {
				count++
				if tr.GroupID != "" || c.At != 120 {
					t.Fatalf("linked clip left behind: %+v", tr)
				}
			}
		}
	}
	if count != 5 {
		t.Fatalf("linked stream count %d", count)
	}
}
func TestTimelineAgentReservationUsesSharedLock(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	trackID := s.Tracks[0].ID
	agent := Origin{Actor: "agent:render-task"}
	human := Origin{Actor: "human:window"}
	if err := s.Apply(&d, Operation{Type: "track.update", TrackID: trackID, Locked: ptr(true)}, agent); err != nil {
		t.Fatal(err)
	}
	if !s.Tracks[0].Locked {
		t.Fatal("Agent lock must be the displayed track lock")
	}
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: trackID, ResourceID: "r"}, Origin{Actor: "agent:other-task"}); err == nil {
		t.Fatal("other task entered reservation")
	}
	if err := s.Apply(&d, Operation{Type: "track.update", TrackID: trackID, Locked: ptr(false)}, Origin{Actor: "agent:other-task"}); err == nil {
		t.Fatal("other task released reservation")
	}

	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: trackID, ResourceID: "r"}, human); err == nil {
		t.Fatal("human edit entered reserved track")
	}
	if err := s.Apply(&d, Operation{Type: "clip.insert", TrackID: trackID, ResourceID: "r"}, agent); err != nil {
		t.Fatalf("lock owner must insert before unlocking: %v", err)
	}
	if !s.Tracks[0].Locked {
		t.Fatal("insertion prematurely unlocked track")
	}
	if err := s.Apply(&d, Operation{Type: "track.update", TrackID: trackID, Locked: ptr(false)}, agent); err != nil {
		t.Fatal(err)
	}
	if s.Tracks[0].Locked {
		t.Fatal("Agent did not release displayed lock")
	}
	if err := s.Apply(&d, Operation{Type: "track.update", TrackID: trackID, Locked: ptr(true)}, human); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(&d, Operation{Type: "track.update", TrackID: trackID, Locked: ptr(false)}, agent); err == nil {
		t.Fatal("Agent released manual lock")
	}
}

func TestTimelineCrossGroupInsertRipplesDestinationOnly(t *testing.T) {
	d := timelineFixture()
	s := &d.Sequences[0]
	s.Configured = true
	s.FPS = Rate{30, 1}
	d.Resources = append(d.Resources, Resource{ID: "silent", Kind: "video", Out: 1, Metadata: map[string]any{"duration": 1, "streams": []map[string]any{{"index": 0, "codec_type": "video"}}}})
	s.Groups = append(s.Groups, TrackGroup{ID: "g"})
	s.Tracks = append(s.Tracks, newTrack("video", "g"))
	source := s.Tracks[len(s.Tracks)-1].ID
	for _, op := range []Operation{{Type: "clip.insert", TrackID: source, ResourceID: "silent"}, {Type: "clip.insert", TrackID: source, ResourceID: "silent", At: 90}, {Type: "clip.insert", TrackID: s.Tracks[0].ID, ResourceID: "silent", At: 90}} {
		if e := s.Apply(&d, op, Origin{}); e != nil {
			t.Fatal(e)
		}
	}
	src, _ := track(s, source)
	id := src.Clips[0].ID
	if e := s.Apply(&d, Operation{Type: "clip.move", ClipIDs: []string{id}, TargetTrackID: s.Tracks[0].ID, At: 30, Mode: "insert"}, Origin{Actor: "human:window"}); e != nil {
		t.Fatal(e)
	}
	src, _ = track(s, source)
	if src.Clips[0].At != 90 {
		t.Fatal("insert rippled the source comparison group")
	}
	found := false
	for _, c := range s.Tracks[0].Clips {
		if c.ID != id && c.At == 120 {
			found = true
		}
	}
	if !found {
		t.Fatal("destination did not ripple")
	}
}

func TestTimelineAudioMixingAndSharedReservation(t *testing.T) {
	for _, kind := range []string{"music", "voice", "linked-video-audio"} {
		t.Run(kind, func(t *testing.T) {
			d := timelineFixture()
			s := &d.Sequences[0]
			s.Configured = true
			s.FPS = Rate{30, 1}
			resourceID := "r"
			if kind != "linked-video-audio" {
				resourceID = kind
				d.Resources = append(d.Resources, Resource{ID: kind, Name: kind, Kind: "audio", Out: 2, Metadata: map[string]any{"duration": 2, "streams": []map[string]any{{"index": 0, "codec_type": "audio"}}}})
			}
			s.Groups = append(s.Groups, TrackGroup{ID: "agent-audio"})
			s.Tracks = append(s.Tracks, newTrack("audio", "agent-audio"))
			sourceID := s.Tracks[len(s.Tracks)-1].ID
			targetID := s.Tracks[3].ID
			agent := Origin{Actor: "agent:audio-render"}
			human := Origin{Actor: "human:window"}
			if e := s.Apply(&d, Operation{Type: "track.update", TrackID: sourceID, Locked: ptr(true)}, agent); e != nil {
				t.Fatal(e)
			}
			if kind == "linked-video-audio" {
				s.Tracks = append(s.Tracks, newTrack("video", "agent-audio"))
				videoID := s.Tracks[len(s.Tracks)-1].ID
				if e := s.Apply(&d, Operation{Type: "clip.insert", TrackID: videoID, ResourceID: resourceID}, agent); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := s.Apply(&d, Operation{Type: "clip.insert", TrackID: sourceID, ResourceID: resourceID}, human); e == nil {
					t.Fatal("audio reservation accepted human insert")
				}
				if e := s.Apply(&d, Operation{Type: "clip.insert", TrackID: sourceID, ResourceID: resourceID}, agent); e != nil {
					t.Fatal(e)
				}
			}
			src, _ := track(s, sourceID)
			if !src.Locked {
				t.Fatal("audio insertion cleared lock")
			}
			clip := src.Clips[0]
			if e := s.Apply(&d, Operation{Type: "track.update", TrackID: sourceID, Locked: ptr(false)}, agent); e != nil {
				t.Fatal(e)
			}
			if e := s.Apply(&d, Operation{Type: "clip.move", ClipIDs: []string{clip.ID}, TargetTrackID: targetID, At: 120}, human); e != nil {
				t.Fatal(e)
			}
			audio, picture := 0, 0
			for _, tr := range s.Tracks {
				for _, c := range tr.Clips {
					if c.LinkID == clip.LinkID {
						if tr.Kind == "audio" {
							audio++
						} else if tr.Kind == "video" {
							picture++
						}
						if tr.GroupID != "" || c.At != 120 || c.ResourceID != resourceID {
							t.Fatal("linked mapping changed group/time/resource")
						}
					}
				}
			}
			expectedAudio, expectedVideo := 1, 0
			if kind == "linked-video-audio" {
				expectedAudio, expectedVideo = 4, 1
			}
			if audio != expectedAudio || picture != expectedVideo {
				t.Fatalf("audio/video count %d/%d, want %d/%d", audio, picture, expectedAudio, expectedVideo)
			}
		})
	}
}
