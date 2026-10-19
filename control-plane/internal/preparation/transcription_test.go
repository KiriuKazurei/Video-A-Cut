package preparation

import "testing"

func TestTranscriptionIsOptionalVersionedProvider(t *testing.T) {
	p := fixture()
	before, err := p.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	p.Transcription = &TranscriptionProvider{APIFormat: "elevenlabs", Endpoint: "https://api.elevenlabs.io/v1", Model: "scribe_v1", TokenEnv: "VAC_ASR_TOKEN"}
	after, err := p.Fingerprint()
	if err != nil || after == before || !p.External() {
		t.Fatalf("transcription not bound to profile fingerprint: %v", err)
	}
	p.Transcription.Endpoint = "https://api.elevenlabs.io/v1?key=secret"
	if p.Validate() == nil {
		t.Fatal("credential in endpoint was accepted")
	}
}
