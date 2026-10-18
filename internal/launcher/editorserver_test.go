package launcher

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/remote"
)

const template = "https://github.com/owenrumney/ade/releases/download/1.135.06783/ade-reh-${os}-${arch}-1.135.06783.tar.gz"

func TestTheDownloadUrlIsTheHostsOwnArch(t *testing.T) {
	url, err := editorServerURL(template, "linux", "arm64")
	require.NoError(t, err)
	assert.Equal(t,
		"https://github.com/owenrumney/ade/releases/download/1.135.06783/ade-reh-linux-arm64-1.135.06783.tar.gz",
		url)

	url, err = editorServerURL(template, "linux", "x64")
	require.NoError(t, err)
	assert.Contains(t, url, "linux-x64")
}

func TestAServerIsOnlyDownloadedFromAReleaseOverHttps(t *testing.T) {
	// The url ends up in a curl on another machine, and it arrives on a
	// request, so it is checked rather than passed on.
	for _, bad := range []string{
		"http://github.com/owenrumney/ade/x-${os}-${arch}.tar.gz",
		"https://example.invalid/x-${os}-${arch}.tar.gz",
		"file:///tmp/x-${os}-${arch}.tar.gz",
		"not a url at all",
		"https://github.com/owenrumney/ade/x-${os}-${missing}.tar.gz",
	} {
		_, err := editorServerURL(bad, "linux", "arm64")
		assert.Error(t, err, bad)
	}
}

func TestAnUnvalidatedTarballGetsItsOwnTreeAndKeepsIt(t *testing.T) {
	one := urlTree("https://github.com/VSCodium/vscodium/releases/download/1.135.06055/x.tar.gz")
	assert.Len(t, one, 40)
	assert.Equal(t, one, urlTree("https://github.com/VSCodium/vscodium/releases/download/1.135.06055/x.tar.gz"))
	assert.NotEqual(t, one, urlTree("https://github.com/VSCodium/vscodium/releases/download/1.135.06056/x.tar.gz"))
}

func TestTheEditorServerForwardIsWantedAlongsideEveryOtherPort(t *testing.T) {
	live := &conn{launcher: remote.Mapping{Local: 31000, Remote: 29233}}
	live.setPorts([]int{29233, 27431})
	assert.Equal(t,
		[]remote.Mapping{{Local: 31000, Remote: 29233}, remote.Same(27431)},
		live.want())

	// The host picks the server's port, and that number may be taken here.
	live.setEditor(remote.Mapping{Local: 41000, Remote: 39107}, "08d4889")
	assert.Equal(t,
		[]remote.Mapping{
			{Local: 31000, Remote: 29233},
			{Local: 41000, Remote: 39107},
			remote.Same(27431),
		},
		live.want())

	// A restarted server binds a fresh port, and the stale mapping goes with
	// the wanted set rather than being cancelled by hand.
	live.setEditor(remote.Mapping{Local: 41001, Remote: 39200}, "08d4889")
	assert.NotContains(t, live.want(), remote.Mapping{Local: 41000, Remote: 39107})
	mapping, commit := live.editorServer()
	assert.Equal(t, 39200, mapping.Remote)
	assert.Equal(t, "08d4889", commit)
}

func TestResolvingAnAuthorityNeedsABookmarkedMachine(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, http.MethodPost, "/api/remote/editor-server",
		`{"host":"pi@evil.example","commit":"08d4889","template":"`+template+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not one of your hosts")
}
