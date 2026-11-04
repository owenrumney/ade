package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/ledger"
	"github.com/owenrumney/ade/internal/ship"
)

func ledgerOf(t *testing.T, s *Server, method, path, body string) ledger.Report {
	t.Helper()
	rec := do(t, s, method, path, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got ledger.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

// seenAndTicked reports a dwell over the read band and the tick that claims it.
func seenAndTicked(file string, start, lines, ms int) string {
	return fmt.Sprintf(`{"seen":[{"file":%q,"start":%d,"lines":%d,"ms":%d}],`+
		`"tick":{"file":%q,"start":%d,"lines":%d},"on":true}`,
		file, start, lines, ms, file, start, lines)
}

func fileIn(t *testing.T, report ledger.Report, path string) ledger.FileCount {
	t.Helper()
	for _, file := range report.Files {
		if file.Path == path {
			return file
		}
	}
	require.Failf(t, "no count for a file in the diff", "%s", path)
	return ledger.FileCount{}
}

func TestTheLedgerCountsWhatWasReadOfWhatTheAgentWrote(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")

	before := ledgerOf(t, s, "GET", "/api/ledger", "")
	assert.Equal(t, 3, before.Agent)
	assert.Equal(t, 3, before.Unread, "nothing has been on screen yet")

	after := ledgerOf(t, s, "POST", "/api/ledger", seenAndTicked("b.go", 1, 3, ledger.Read))
	assert.Equal(t, 3, after.Read)
	assert.Equal(t, 0, after.Unread)
	assert.True(t, fileIn(t, after, "b.go").Viewed)
}

func TestASkimIsNotReading(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")

	got := ledgerOf(t, s, "POST", "/api/ledger", seenAndTicked("b.go", 1, 3, ledger.Skim-1))
	assert.Equal(t, 3, got.Unread)
	assert.True(t, fileIn(t, got, "b.go").Viewed, "the tick stands; it is the dwell that is missing")
}

func TestTheLedgerSurvivesTheBrowserAndStaysOnItsBranch(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")
	ledgerOf(t, s, "POST", "/api/ledger", seenAndTicked("b.go", 1, 3, ledger.Read))

	store, err := ledger.Open(s.space().gitDir, s.space().branch)
	require.NoError(t, err)
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	assert.Equal(t, 3, store.Count(diff, s.lineageStore().Marks(diff)).Read,
		"a fresh store reads the same count off disk")
}

func TestTooManyRangesInOneReportIsRefused(t *testing.T) {
	s := newTestServer(t)
	ranges := make([]string, 0, maxSeen+1)
	for range maxSeen + 1 {
		ranges = append(ranges, `{"file":"b.go","start":1,"lines":1,"ms":1000}`)
	}
	rec := do(t, s, "POST", "/api/ledger", `{"seen":[`+joinWith(ranges, ",")+`]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func joinWith(parts []string, sep string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += sep
		}
		out += part
	}
	return out
}

func TestUnreadAgentLinesShowOnTheRailAndInTheCommit(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")

	gates := s.shipState(context.Background()).Gates
	var gate ship.Gate
	for _, one := range gates {
		if one.Name == "Agent lines read" {
			gate = one
		}
	}
	assert.Equal(t, ship.Warn, gate.State)
	assert.Equal(t, "3 of 3 agent lines unread", gate.Note)

	rec := do(t, s, "POST", "/api/commit", `{"title":"feat(b): add y"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	msg, err := gitdiff.HeadMessage(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.Contains(t, msg, "Unreviewed-Agent-Lines: 3")
}

func TestTheTrailerCountsOnlyTheFilesTheCommitCarries(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "c.go"),
		[]byte("package main\n\nvar z = 2\n"), 0o600))
	recordTurn(t, s, "b.go", 1, 3, "add a y")
	recordTurn(t, s, "c.go", 1, 3, "add a z")
	ledgerOf(t, s, "POST", "/api/ledger", seenAndTicked("b.go", 1, 3, ledger.Read))

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/commit", `{"title":"feat(b): add y"}`).Code)
	msg, err := gitdiff.HeadMessage(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.NotContains(t, msg, "Unreviewed-Agent-Lines",
		"c.go is unread but not staged, so this commit does not carry it")
}

func TestReadWorkCommitsWithNoTrailer(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")
	ledgerOf(t, s, "POST", "/api/ledger", seenAndTicked("b.go", 1, 3, ledger.Read))

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/commit", `{"title":"feat(b): add y"}`).Code)
	msg, err := gitdiff.HeadMessage(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.NotContains(t, msg, "Unreviewed-Agent-Lines")
}
