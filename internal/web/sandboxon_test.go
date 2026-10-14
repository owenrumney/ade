package web

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/sandbox"
)

func TestAWorkerSessionCannotLeaveItsContainer(t *testing.T) {
	// The worker image carries ade and no agent, so "off" is a session that
	// cannot run at all rather than one that runs on the host.
	s := newTestServer(t)
	assert.Empty(t, s.sandboxView(sandbox.Workspace{}).Fixed)

	t.Setenv("ADE_WORKER_NAME", "ade-worker")
	assert.Contains(t, s.sandboxView(sandbox.Workspace{}).Fixed, "worker")

	rec := do(t, s, http.MethodPut, "/api/sandbox/on", `{"on":false}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "worker")
}

func TestOffAWorkerTheSandboxStaysASwitch(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("ADE_WORKER_NAME", "")
	assert.Empty(t, s.sandboxView(sandbox.Workspace{}).Fixed)
}
