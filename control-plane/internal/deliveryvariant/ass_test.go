package deliveryvariant

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestASSPreservesLiteralTextAndQuantizes(t *testing.T) {
	raw := []byte("1\r\n00:00:00,005 --> 00:00:00,995\r\n中文, 😀 {\\an8} \\N\r\n第二行  \r\n\r\n")
	cues, err := ParseSRT(raw, 1000)
	if err != nil || len(cues) != 1 || cues[0].Start != 1 || cues[0].End != 100 || !strings.HasSuffix(cues[0].Text, "  ") {
		t.Fatalf("%+v %v", cues, err)
	}
	ass := ASS(cues)
	if !strings.Contains(ass, "\\{\\\u2060an8\\} \\\u2060N\\N第二行  \n") {
		t.Fatal(ass)
	}
	for _, bad := range []string{"1\n00:00:00,001 --> 00:00:00,004\nx", "1\n00:00:00,000 --> 00:00:02,000\nx", "1\n00:00:00,000 --> 00:00:00,100\na\x00"} {
		if _, err := ParseSRT([]byte(bad), 1000); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if cues, err := ParseSRT(nil, 1000); err != nil || len(cues) != 0 {
		t.Fatal(cues, err)
	}
}
func TestVariantFingerprintGolden(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/delivery-variant/v1/fixtures/input.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Input    Input  `json:"input"`
		Expected string `json:"expected_sha256"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	sha, err := Fingerprint(golden.Input)
	if err != nil || sha != golden.Expected {
		t.Fatal(sha, golden.Expected, err)
	}
	golden.Input.TaskID = "other"
	golden.Input.VariantID = "other"
	sha, _ = Fingerprint(golden.Input)
	if sha != golden.Expected {
		t.Fatal("attempt IDs enter fingerprint")
	}
	golden.Input.SourceFiles = append(golden.Input.SourceFiles, golden.Input.SourceFiles[0])
	if _, err := Fingerprint(golden.Input); err == nil {
		t.Fatal("duplicate accepted")
	}
	contract, err := os.ReadFile("../../../contracts/delivery-variant/v1/specs/ass-basic-v1.json")
	if err != nil || string(contract) != string(SpecBytes) {
		t.Fatal("embedded spec drift", err)
	}
}

func TestFixedASSWrapping(t *testing.T) {
	text := strings.Repeat("长句中文字幕", 12)
	wrapped := wrapASS(text)
	if strings.ReplaceAll(wrapped, "\n", "") != text {
		t.Fatal("wrapping changed text")
	}
	for _, line := range strings.Split(wrapped, "\n") {
		if len([]rune(line)) > 28 {
			t.Fatal("unsafe wrapped line")
		}
	}
	if wrapASS(strings.Repeat("a", 28)+"\u0301\n中文") != strings.Repeat("a", 28)+"\u0301\n中文" {
		t.Fatal("combining mark detached")
	}
	if _, err := ParseSRT([]byte("1\n00:00:00,000 --> 00:00:00,990\n"+strings.Repeat("长", 28*9)), 1000); err == nil {
		t.Fatal("excessive subtitle rows accepted")
	}
}
