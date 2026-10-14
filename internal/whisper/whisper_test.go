package whisper

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fake puts a whisper-cli on PATH that records its argv and writes transcript as its JSON.
func fake(t *testing.T, transcript string) (argv string) {
	t.Helper()
	dir := t.TempDir()
	argv = filepath.Join(dir, "argv")
	reply := filepath.Join(dir, "reply.json")
	require.NoError(t, os.WriteFile(reply, []byte(transcript), 0o600))
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argv + "\n" +
		"while [ $# -gt 0 ]; do [ \"$1\" = -f ] && out=\"$2.json\"; shift; done\n" +
		"cp " + reply + " \"$out\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "whisper-cli"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	model := filepath.Join(dir, "model.bin")
	require.NoError(t, os.WriteFile(model, []byte("m"), 0o600))
	t.Setenv("ADE_WHISPER_MODEL", model)
	return argv
}

func post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/transcribe", strings.NewReader(body))
	Handler()(rec, req)
	return rec
}

func wavBody() string {
	wav := make([]byte, 44+320)
	copy(wav, "RIFF")
	b, _ := json.Marshal(map[string]string{"wav": base64.StdEncoding.EncodeToString(wav)})
	return string(b)
}

func TestHandlerRunsWhisperAndReturnsTheText(t *testing.T) {
	argv := fake(t, `{"transcription":[{"text":" Hello from"},{"text":" [BLANK_AUDIO]"},{"text":" the composer."}]}`)

	rec := post(t, wavBody())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "Hello from the composer.", got["text"])

	raw, err := os.ReadFile(argv)
	require.NoError(t, err)
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, args, 8)
	assert.Equal(t, []string{"-m", os.Getenv("ADE_WHISPER_MODEL"), "-l", "en", "-nt", "-oj", "-f"}, args[:7])
	assert.Equal(t, "speech.wav", filepath.Base(args[7]))
}

func TestHandlerNamesTheCurlWhenTheModelIsMissing(t *testing.T) {
	fake(t, `{}`)
	t.Setenv("ADE_WHISPER_MODEL", filepath.Join(t.TempDir(), "absent.bin"))

	rec := post(t, wavBody())

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "curl -L -o")
	assert.Contains(t, rec.Body.String(), "absent.bin")
}

func TestHandlerRefusesABodyWithNoAudio(t *testing.T) {
	fake(t, `{}`)
	tests := []struct {
		name string
		body string
	}{
		{"not json", "nope"},
		{"not base64", `{"wav":"%%%"}`},
		{"header only", `{"wav":"` + base64.StdEncoding.EncodeToString(make([]byte, 44)) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, post(t, tt.body).Code)
		})
	}
}
