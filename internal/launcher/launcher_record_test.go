package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A record nobody could write is how a whole worker went quiet: every session
// read the last launcher's token, announced with it, and was refused.
func TestARecordThatCannotBeWrittenStopsTheLauncher(t *testing.T) {
	home := t.TempDir()
	s := &Server{home: home}

	at := filepath.Join(home, "server.json")
	require.NoError(t, s.writeRecord(at, "http://127.0.0.1:1/?t=live"))
	body, err := os.ReadFile(at)
	require.NoError(t, err)
	assert.Contains(t, string(body), "t=live")

	if os.Geteuid() == 0 {
		t.Skip("root writes into a directory with no write bit, so there is nothing to refuse")
	}
	shut := filepath.Join(home, "shut")
	require.NoError(t, os.Mkdir(shut, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	err = s.writeRecord(filepath.Join(shut, "server.json"), "http://127.0.0.1:1/?t=live")
	require.Error(t, err, "the launcher refuses to serve unaddressably")
	assert.Contains(t, err.Error(), "record this launcher")
}
