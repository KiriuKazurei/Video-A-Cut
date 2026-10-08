package deliveryvariant

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditableASSCrossLanguage(t *testing.T) {
	cues := []Cue{{Start: 10, End: 90, Text: "中文 {\\an8} \\N\n第二行"}, {Start: 90, End: 100, Text: "结束"}}
	d := StyleDocument{SourceSRTSHA256: strings.Repeat("a", 64), PlayResX: 1280, PlayResY: 720, Style: DefaultStyle(), Overrides: []SubtitleCueOverride{{CueID: 2, Style: map[string]json.RawMessage{"primary_color": json.RawMessage(`"#FF0000"`), "alignment": json.RawMessage(`8`)}}}}
	d.Style.Bold = true
	d.Style.FadeInMS = 40
	d.Style.FadeOutMS = 50
	if e := ValidateStyleDocument(d, cues, 1000); e != nil {
		t.Fatal(e)
	}
	expected := StyledASS(cues, d)
	if !strings.Contains(expected, "{\\fad(40,50)}") || !strings.Contains(expected, "&H000000FF") || !strings.Contains(expected, "\\{\\\u2060an8\\}") {
		t.Fatal(expected)
	}
	raw, _ := json.Marshal(map[string]any{"document": d, "cues": cues})
	_ = raw
	nodeCues := []map[string]any{}
	for _, c := range cues {
		nodeCues = append(nodeCues, map[string]any{"start": c.Start, "end": c.End, "text": c.Text})
	}
	payload, _ := json.Marshal(map[string]any{"document": d, "cues": nodeCues})
	repo, _ := filepath.Abs("../../..")
	script := `import {serializeStyledAss} from './workers/node/timeline-cli/adapters/ass.mjs'; import {canonicalJSON} from './contracts/delivery-variant/v1/style.mjs';import{createHash}from'node:crypto';const v=JSON.parse(process.argv[1]);console.log(JSON.stringify({ass:serializeStyledAss(v.cues,v.document,1000),hash:createHash('sha256').update(canonicalJSON(v.document)).digest('hex')}));`
	cmd := exec.Command("node", "--input-type=module", "-e", script, string(payload))
	cmd.Dir = repo
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("node %v %s", e, b)
	}
	var result struct {
		ASS  string `json:"ass"`
		Hash string `json:"hash"`
	}
	if e = json.Unmarshal(b, &result); e != nil {
		t.Fatal(e, string(b))
	}
	if result.ASS != expected || result.Hash != StyleHash(d) {
		t.Fatal("Go/Node style serialization or hash differs", result, expected)
	}
	d.Style.FadeInMS = 60
	d.Style.FadeOutMS = 50
	if ValidateStyleDocument(d, cues, 1000) == nil {
		t.Fatal("short cue accepted overlapping fades")
	}
}
