package web

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// avatarTTL is how long a fetched avatar is trusted before it is fetched
// again. A GitHub avatar changes rarely, so a day is plenty and keeps a busy
// review from hammering github.com on every reload.
const avatarTTL = 24 * time.Hour

// githubLogin is what a GitHub username is allowed to look like. It guards
// the request this package makes on the login's behalf: without it, whatever
// a comment's author field held would go straight into a URL ade fetches.
var githubLogin = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38})$`)

// avatarEntry is one login's last-fetched image, cached whole: small enough
// that keeping the bytes in memory beats re-fetching, and re-serving beats a
// disk cache neither restart nor branch switch needs to survive.
type avatarEntry struct {
	body        []byte
	contentType string
	status      int
	fetchedAt   time.Time
}

// avatarCache is the process's shared reading of github.com/<login>.png,
// refreshed lazily per login. get fetches the image for a caller-supplied
// login; a field so a test can stand one in without reaching the network.
type avatarCache struct {
	mu      sync.Mutex
	entries map[string]avatarEntry
	get     func(ctx context.Context, login string) (avatarEntry, error)
}

func newAvatarCache() *avatarCache {
	return &avatarCache{
		entries: map[string]avatarEntry{},
		get:     fetchAvatar,
	}
}

func fetchAvatar(ctx context.Context, login string) (avatarEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/"+login+".png", nil)
	if err != nil {
		return avatarEntry{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return avatarEntry{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return avatarEntry{}, err
	}
	return avatarEntry{
		body:        body,
		contentType: resp.Header.Get("Content-Type"),
		status:      resp.StatusCode,
		fetchedAt:   time.Now(),
	}, nil
}

func (c *avatarCache) fetch(ctx context.Context, login string) (avatarEntry, error) {
	c.mu.Lock()
	entry, ok := c.entries[login]
	c.mu.Unlock()
	if ok && time.Since(entry.fetchedAt) < avatarTTL {
		return entry, nil
	}

	entry, err := c.get(ctx, login)
	if err != nil {
		return avatarEntry{}, err
	}
	c.mu.Lock()
	c.entries[login] = entry
	c.mu.Unlock()
	return entry, nil
}

// handleAvatar proxies a GitHub user's avatar so the page never talks to
// github.com directly: an <img> tag on a private diff is otherwise a leak of
// what you are reviewing to a third party on every render, cache or not.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	login := r.PathValue("login")
	if !githubLogin.MatchString(login) {
		http.Error(w, "not a github login", http.StatusBadRequest)
		return
	}

	entry, err := s.avatars.fetch(r.Context(), login)
	if err != nil || entry.status != http.StatusOK {
		http.Error(w, "avatar unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", entry.contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.body)
}
