package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// idTokenSkew treats a token about to expire as expired; a turn outlives it.
const idTokenSkew = 2 * time.Minute

// refreshFloor stops a provider that sends no id_token refreshing every run.
const refreshFloor = 10 * time.Minute

// Freshen refreshes the login at path when its id_token has expired. That
// token lives an hour and the CLI will not refresh a file ade stamped fresh.
func Freshen(ctx context.Context, path string) error {
	return freshenWith(ctx, path, Opts{})
}

func freshenWith(ctx context.Context, path string, opts Opts) error {
	creds, err := readAuth(path)
	if err != nil {
		return err
	}
	if !staleIDToken(creds.IDToken) || refreshedWithin(path, refreshFloor) {
		return nil
	}
	fill(&opts)
	refreshed, err := refresh(ctx, opts.Client, opts.TokenURL, creds)
	if err != nil {
		return err
	}
	return saveTo(path, refreshed)
}

// staleIDToken is whether the CLI would send a dead account claim.
// Unreadable counts as stale: a refresh is the corrective either way.
func staleIDToken(token string) bool {
	exp, err := jwtExpiry(token)
	if err != nil {
		return true
	}
	return time.Now().Add(idTokenSkew).After(exp)
}

// refreshedWithin reports a file refreshed in the last d.
func refreshedWithin(path string, d time.Duration) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own login file
	if err != nil {
		return false
	}
	var auth struct {
		LastRefresh string `json:"last_refresh"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return false
	}
	at, err := time.Parse(time.RFC3339, auth.LastRefresh)
	if err != nil {
		return false
	}
	return time.Since(at) < d
}

// jwtExpiry reads exp out of a JWT payload; the signature is not checked.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode the JWT payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("read the JWT claims: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, errors.New("the JWT has no exp")
	}
	return time.Unix(claims.Exp, 0), nil
}
