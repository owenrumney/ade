// Package lsp answers language questions about a checkout by driving a
// language server.

// It sits beside the checkout, not in the window: a session on another
// machine then answers through the proxy that carries its chat.
package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/owenrumney/go-lsp/client"
	"github.com/owenrumney/go-lsp/lsp"
)

// ErrNoServer means nothing is installed to answer for that language.
var ErrNoServer = errors.New("no language server for that file")

// Servers is the argv for each language ade knows, by the language id
// LSP uses. One entry: a second earns its place when something asks.
var Servers = map[string][]string{ //nolint:gochecknoglobals // a table, not state
	"go": {"gopls", "serve"},
}

// Language is the LSP language id for a path, or empty when ade has no
// server for it.
func Language(path string) string {
	if strings.HasSuffix(path, ".go") {
		return "go"
	}
	return ""
}

// Pool is one checkout's language servers, started when first asked and
// closed with the session. Nothing is spawned by opening the window.
type Pool struct {
	root string

	mu   sync.Mutex
	held map[string]*held
	// find is the argv for a language, so a test can put its own server
	// in place of the real one.
	find func(string) []string
}

// held is one server, dialled once however many requests arrive at once.
type held struct {
	once sync.Once
	cli  *client.Client
	err  error
	// open is what this server has been told about. The editor is
	// read-only, so a file is opened at disk and stays there.
	openMu sync.Mutex
	open   map[string]bool
}

// New is a pool for a checkout. The root is what the server indexes.
func New(root string) *Pool {
	return &Pool{root: root, held: map[string]*held{}, find: func(id string) []string {
		return Servers[id]
	}}
}

// Close stops every server this pool started.
func (p *Pool) Close() {
	p.mu.Lock()
	all := p.held
	p.held = map[string]*held{}
	p.mu.Unlock()
	for _, h := range all {
		if h.cli != nil {
			_ = h.cli.Close()
		}
	}
}

// Hover is what the server says about the symbol at a position, as
// markdown. Empty is a real answer: not everything has something to say.
func (p *Pool) Hover(ctx context.Context, file string, line, col int) (string, error) {
	cli, uri, err := p.at(ctx, file)
	if err != nil {
		return "", err
	}
	// LSP counts from zero and the window counts from one.
	got, err := cli.Hover(ctx, uri, lsp.Position{Line: max(line-1, 0), Character: max(col-1, 0)})
	if err != nil || got == nil {
		return "", err
	}
	return got.Contents.Value(), nil
}

// at is the server for a file, dialled and ready, with the file opened.
func (p *Pool) at(ctx context.Context, file string) (*client.Client, lsp.DocumentURI, error) {
	id := Language(file)
	if id == "" {
		return nil, "", fmt.Errorf("%s: %w", filepath.Base(file), ErrNoServer)
	}
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.root, file)
	}
	h, err := p.server(ctx, id)
	if err != nil {
		return nil, "", err
	}
	// A server accepts initialize long before it has indexed, and a
	// request in that window answers empty rather than failing.
	if err := h.cli.Ready(ctx); err != nil {
		return nil, "", err
	}
	uri := lsp.DocumentURI("file://" + path)
	if err := h.opened(ctx, uri, id, path); err != nil {
		return nil, "", err
	}
	return h.cli, uri, nil
}

// server dials the one for a language, or hands back the one already up.
func (p *Pool) server(ctx context.Context, id string) (*held, error) {
	p.mu.Lock()
	h, ok := p.held[id]
	if !ok {
		h = &held{open: map[string]bool{}}
		p.held[id] = h
	}
	argv := p.find(id)
	p.mu.Unlock()

	h.once.Do(func() {
		if len(argv) == 0 {
			h.err = fmt.Errorf("%s: %w", id, ErrNoServer)
			return
		}
		if _, err := exec.LookPath(argv[0]); err != nil {
			h.err = fmt.Errorf("%s is not installed: %w", argv[0], ErrNoServer)
			return
		}
		h.cli, h.err = client.Dial(ctx, client.Config{
			Command:    argv,
			Dir:        p.root,
			RootURI:    lsp.DocumentURI("file://" + p.root),
			ClientInfo: &lsp.ClientInfo{Name: "ade"},
		})
	})
	return h, h.err
}

// opened tells the server about a file once. The editor is read-only,
// so what was sent is what is on disk and stays that way.
func (h *held) opened(ctx context.Context, uri lsp.DocumentURI, id, path string) error {
	h.openMu.Lock()
	defer h.openMu.Unlock()
	if h.open[string(uri)] {
		return nil
	}
	body, err := os.ReadFile(path) // #nosec G304 -- a file in the checkout ade serves
	if err != nil {
		return err
	}
	if err := h.cli.DidOpen(ctx, uri, id, 1, string(body)); err != nil {
		return err
	}
	h.open[string(uri)] = true
	return nil
}

// Spot is a place in the checkout, as the window opens one: a path
// relative to the root, and a line counted from one.
type Spot struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// Definition is where the symbol under the cursor is defined. Nothing
// found is not a failure: plenty of positions define nothing.
func (p *Pool) Definition(ctx context.Context, file string, line, col int) (*Spot, error) {
	cli, uri, err := p.at(ctx, file)
	if err != nil {
		return nil, err
	}
	found, err := cli.Definition(ctx, uri, lsp.Position{Line: max(line-1, 0), Character: max(col-1, 0)})
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return p.spot(found[0]), nil
}

// References is every use of the symbol, the definition among them.
func (p *Pool) References(ctx context.Context, file string, line, col int) ([]Spot, error) {
	cli, uri, err := p.at(ctx, file)
	if err != nil {
		return nil, err
	}
	found, err := cli.References(ctx, uri, lsp.Position{Line: max(line-1, 0), Character: max(col-1, 0)}, true)
	if err != nil {
		return nil, err
	}
	out := make([]Spot, 0, len(found))
	for _, at := range found {
		if spot := p.spot(at); spot != nil {
			out = append(out, *spot)
		}
	}
	return out, nil
}

// spot turns a server's location into the window's. One outside the
// checkout is dropped: the window cannot show a file it does not serve.
func (p *Pool) spot(at lsp.Location) *Spot {
	path := strings.TrimPrefix(string(at.URI), "file://")
	rel, err := filepath.Rel(p.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	return &Spot{Path: rel, Line: at.Range.Start.Line + 1}
}
