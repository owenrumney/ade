package web

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestASnapshotChecksThePullRequestOut(t *testing.T) {
	s := newTestServer(t)
	s.ws.pr = &PRInfo{Number: 42, ResolveBlocked: "not checked out"}
	s.ws.snapshot = true

	asked := 0
	s.checkOutPR = func(_ context.Context, main string, number int) (string, error) {
		asked++
		assert.Equal(t, s.ws.homeRoot, main)
		assert.Equal(t, 42, number)
		return main + "-pr-42", nil
	}
	opened := ""
	s.openIn = func(_ context.Context, dir, _ string) (string, error) {
		opened = dir
		return "http://127.0.0.1:1/", nil
	}
	handed := ""
	s.ask = func(_ context.Context, _, _, path, body string) error {
		handed = body
		assert.Equal(t, "/api/chat/new", path)
		return nil
	}

	// Chatting is the checkout: no refusal, and the reply is where to carry on.
	rec := do(t, s, "POST", "/api/chat", `{"message":"fix the thing"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, asked)
	assert.Contains(t, opened, "-pr-42")
	assert.Contains(t, handed, "fix the thing")
	assert.Contains(t, rec.Body.String(), "http://127.0.0.1:1/")

	// The button does the same without a message to carry.
	rec = do(t, s, "POST", "/api/pr/checkout", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, asked)
}

func TestCheckingOutWithNothingTypedJustOpensTheWorktree(t *testing.T) {
	s := newTestServer(t)
	s.ws.pr = &PRInfo{Number: 9}
	s.ws.snapshot = true
	s.checkOutPR = func(_ context.Context, main string, _ int) (string, error) { return main + "-pr-9", nil }
	s.openIn = func(context.Context, string, string) (string, error) { return "http://127.0.0.1:1/", nil }
	s.ask = func(context.Context, string, string, string, string) error {
		t.Fatal("nothing was typed, so there is no prompt to hand over")
		return nil
	}

	rec := do(t, s, "POST", "/api/pr/checkout", `{"message":"  "}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestCheckoutIsRefusedWithoutASnapshotToLeave(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/pr/checkout", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "already checked out")

	// A snapshot with no pull request behind it cannot be one, but chat must
	// still say so rather than reach for a number that is not there.
	s.ws.snapshot = true
	rec = do(t, s, "POST", "/api/chat", `{"message":"hello"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request is open")
}

func TestAFailedCheckoutSaysSoRatherThanOpeningNothing(t *testing.T) {
	s := newTestServer(t)
	s.ws.pr = &PRInfo{Number: 5}
	s.ws.snapshot = true
	s.checkOutPR = func(context.Context, string, int) (string, error) {
		return "", errors.New("gh pr checkout 5: no such pull request")
	}
	s.openIn = func(context.Context, string, string) (string, error) {
		t.Fatal("nothing was checked out, so there is nothing to open")
		return "", nil
	}

	rec := do(t, s, "POST", "/api/pr/checkout", `{}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "no such pull request")
}
