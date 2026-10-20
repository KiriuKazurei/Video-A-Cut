package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

func (p TranscriptionProvider) Local() bool { return p.APIFormat == "faster_whisper" }

func (p TranscriptionProvider) Validate() error {
	if p.Local() {
		if !filepath.IsAbs(p.ModelPath) || !textOK(p.ModelPath, 4096) || (p.PythonExecutable != "" && (!filepath.IsAbs(p.PythonExecutable) || !textOK(p.PythonExecutable, 4096))) {
			return fmt.Errorf("local transcription requires absolute model_path and optional python_executable")
		}
		if p.Endpoint != "" || p.TokenEnv != "" || p.Model != "" {
			return fmt.Errorf("local transcription cannot contain cloud credentials or endpoint")
		}
		if p.Device != "" && p.Device != "cpu" && p.Device != "cuda" {
			return fmt.Errorf("invalid transcription device")
		}
		if p.ComputeType != "" && p.ComputeType != "int8" && p.ComputeType != "float32" && !(p.Device == "cuda" && (p.ComputeType == "float16" || p.ComputeType == "int8_float16")) {
			return fmt.Errorf("invalid transcription compute_type for device")
		}
		if p.Language != "" && !regexp.MustCompile(`^[a-z]{2,3}$`).MatchString(p.Language) {
			return fmt.Errorf("invalid transcription language")
		}
		if p.CPUThreads < 0 || p.CPUThreads > 16 {
			return fmt.Errorf("transcription cpu_threads must be 1-16 or default 0")
		}
		return nil
	}
	if p.APIFormat != "elevenlabs" || p.Model != "scribe_v1" || p.ModelPath != "" || p.PythonExecutable != "" || p.Device != "" || p.ComputeType != "" || p.Language != "" || p.CPUThreads != 0 {
		return fmt.Errorf("unsupported transcription provider")
	}
	return (Provider{Adapter: "narration", APIFormat: "openai", Endpoint: p.Endpoint, Model: p.Model, TokenEnv: p.TokenEnv}).Validate(true)
}

// Bind caches to the actual local model bytes, in addition to the immutable profile.
// This reads only named model files, never scans media or downloads missing files.
func (p TranscriptionProvider) ModelFingerprint(ctx context.Context) (string, error) {
	if !p.Local() {
		return "", nil
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	h := sha256.New()
	for i, name := range []string{"config.json", "model.bin", "tokenizer.json", "vocabulary.txt", "vocabulary.json", "preprocessor_config.json"} {
		file, err := os.Open(filepath.Join(p.ModelPath, name))
		if os.IsNotExist(err) && i >= 3 {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("local ASR model file missing or unreadable: %s", name)
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8<<30 {
			file.Close()
			return "", fmt.Errorf("invalid local ASR model file: %s", name)
		}
		_, _ = fmt.Fprintf(h, "%s:%d:", name, info.Size())
		buffer := make([]byte, 256<<10)
		for {
			if err := ctx.Err(); err != nil {
				file.Close()
				return "", err
			}
			n, err := file.Read(buffer)
			if n > 0 {
				_, _ = h.Write(buffer[:n])
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				file.Close()
				return "", fmt.Errorf("cannot read local ASR model file: %s", name)
			}
		}
		after, err := file.Stat()
		file.Close()
		if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return "", fmt.Errorf("local ASR model changed during inspection")
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
