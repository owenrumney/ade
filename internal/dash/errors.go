package dash

import "errors"

// ErrNotOffered is a pull request the caller named that the search did not
// return; ErrGone is one that has since dropped off the list.
var (
	ErrNotOffered = errors.New("that is not one of the pull requests waiting on you")
	ErrGone       = errors.New("that review request is not on the list any more")
	ErrNotInInbox = errors.New("that is not one of the notifications in your inbox")
)
