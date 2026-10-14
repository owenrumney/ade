// Package whisper turns a short recording into text with a local whisper-cli.
// The design is .git/ade/docs/add-speech-to-text-to.md.
package whisper

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Two minutes of 16 kHz 16-bit mono, base64 in JSON, with room to spare.
	maxBody  = 8 << 20
	runLimit = 90 * time.Second
	modelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"
)

// Handler is POST /api/transcribe: {"wav": base64} in, {"text": "..."} out.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WAV string `json:"wav"`
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "that recording is too long")
			return
		}
		if err := json.Unmarshal(data, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		wav, err := base64.StdEncoding.DecodeString(req.WAV)
		if err != nil || len(wav) <= 44 {
			writeError(w, http.StatusBadRequest, "no audio")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), runLimit)
		defer cancel()
		text, err := Transcribe(ctx, wav)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
	}
}

// Transcribe runs whisper-cli over one WAV and returns what it heard.
func Transcribe(ctx context.Context, wav []byte) (string, error) {
	model, err := Model()
	if err != nil {
		return "", err
	}
	bin, err := binary()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "ade-whisper-")
	if err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "speech.wav")
	if err := os.WriteFile(file, wav, 0o600); err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin, "-m", model, "-l", "en", "-nt", "-oj", "-f", file)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("whisper-cli: %w: %s", err, tail(out))
	}
	raw, err := os.ReadFile(file + ".json")
	if err != nil {
		return "", fmt.Errorf("whisper-cli wrote no transcript: %w", err)
	}
	return parse(raw)
}

// Model is the model file whisper-cli reads. ade never downloads it.
func Model() (string, error) {
	path := os.Getenv("ADE_WHISPER_MODEL")
	if path == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("no cache directory: %w", err)
		}
		path = filepath.Join(cache, "ade", "whisper", "ggml-base.en.bin")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("no whisper model at %s. Fetch it once: mkdir -p %s && curl -L -o %s %s",
			path, filepath.Dir(path), path, modelURL)
	}
	return path, nil
}

// binary falls back to Homebrew: a process the editor starts may not have a shell's PATH.
func binary() (string, error) {
	if path, err := exec.LookPath("whisper-cli"); err == nil {
		return path, nil
	}
	const brew = "/opt/homebrew/bin/whisper-cli"
	if _, err := os.Stat(brew); err == nil {
		return brew, nil
	}
	return "", errors.New("whisper-cli is not installed: brew install whisper.cpp")
}

func parse(raw []byte) (string, error) {
	var got struct {
		Transcription []struct {
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return "", fmt.Errorf("whisper-cli transcript: %w", err)
	}
	var parts []string
	for _, seg := range got.Transcription {
		text := strings.TrimSpace(seg.Text)
		// Silence comes back as a marker such as [BLANK_AUDIO], not as nothing.
		if text == "" || strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " "), nil
}

func tail(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > 400 {
		text = text[len(text)-400:]
	}
	return text
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
