package preview

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestASSPreviewFingerprintCrossLanguage(t *testing.T) {
	in := fixtureInput(t)
	original, _ := Fingerprint(in)
	in.SchemaVersion = 2
	in.SubtitleVariant = &SubtitleVariantInput{VariantID: "dv_test", PackageRef: "delivery-variants/asset/dv_test", ASS: InputFile{Kind: "ass", Path: "subtitles.ass", Size: 256, SHA256: strings.Repeat("a", 64)}, StyleSHA256: strings.Repeat("b", 64), FontFamilies: []string{"Microsoft YaHei", "Arial"}}
	hash, e := Fingerprint(in)
	if e != nil || hash == original {
		t.Fatal(hash, e)
	}
	raw, _ := json.Marshal(in)
	repo, _ := filepath.Abs("../../..")
	cmd := exec.Command("node", "--input-type=module", "-e", `import{fingerprintPreviewInput}from'./workers/node/timeline-cli/src/preview-protocol.mjs';console.log(fingerprintPreviewInput(JSON.parse(process.argv[1])));`, string(raw))
	cmd.Dir = repo
	b, e := cmd.CombinedOutput()
	if e != nil || strings.TrimSpace(string(b)) != hash {
		t.Fatal("Go/Node ASS preview fingerprint differs", string(b), hash, e)
	}
	in.SubtitleVariant.ASS.SHA256 = strings.Repeat("c", 64)
	changed, e := Fingerprint(in)
	if e != nil || changed == hash {
		t.Fatal("ASS change reused fingerprint")
	}
	in.SubtitleVariant.PackageRef = "../escape"
	if _, e = Fingerprint(in); e == nil {
		t.Fatal("escaped subtitle package accepted")
	}
	in.SubtitleVariant.PackageRef = "delivery-variants/asset/dv_test"
	in.SchemaVersion = 1
	if _, e = Fingerprint(in); e == nil {
		t.Fatal("ASS accepted in legacy input")
	}
}
