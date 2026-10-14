package web

import "github.com/owenrumney/ade/internal/limits"

// LimitsState is what the page shows. The cache behind it is shared with the
// launcher, which serves the same reading with no checkout under it.
type LimitsState = limits.State
