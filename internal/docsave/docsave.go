// Package docsave writes a design document through the running ade, because
// .git is a path an agent's harness refuses to write to.
package docsave

import (
	"context"
	"errors"
	"strings"

	"github.com/owenrumney/ade/internal/delegate"
)

// Path is the route Send posts to, and the one the server mounts.
const Path = "/api/internal/delegate/doc"

// Answer is where the document landed.
type Answer struct {
	File string `json:"file"`
	Path string `json:"path"`
}

// Send replaces the named document's contents. The document must already
// exist: this replaces what an agent was given, it does not create documents.
func Send(ctx context.Context, name, body string) (Answer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Answer{}, errors.New("which document?")
	}
	if strings.TrimSpace(body) == "" {
		return Answer{}, errors.New("refusing to write an empty document")
	}
	var out Answer
	err := delegate.Post(ctx, "to save "+name, Path,
		map[string]string{"name": name, "body": body}, &out)
	return out, err
}
