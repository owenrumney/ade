package docs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/designs"
	"github.com/owenrumney/ade/internal/docs"
)

// The guard in docs spells the store's filename itself rather than importing
// the store, so this is what keeps the two from drifting apart.
func TestTheStoreAndTheDocsAgreeOnTheName(t *testing.T) {
	assert.Equal(t, designs.File, docs.StoreDoc)
}
