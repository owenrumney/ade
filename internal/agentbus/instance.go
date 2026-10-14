package agentbus

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// InstanceID is this checkout's id, made once and kept in dir. A request that
// arrived while ade was down finds it again under the same name.
func InstanceID(dir string) string {
	path := filepath.Join(dir, "agentbus-instance")
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}
	id := "ade-" + randomHex(4)
	if os.MkdirAll(dir, 0o700) == nil {
		_ = os.WriteFile(path, []byte(id+"\n"), 0o600)
	}
	return id
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
