package settings

import "strings"

// muted is the pull requests you have put aside.
//
// Review requests are read fresh from GitHub every few minutes, so there is
// nowhere on them to keep a flag. This is that flag: a list of "owner/repo#12"
// keys, machine-wide because the requests are too — they are not a checkout's.
//
// Nothing prunes it. A muted pull request that is merged never comes back in
// the search, so its key is a dead line in a file of a few hundred at worst.
var muted = &flags{store{name: "muted-reviews.json"}}

// MutedReviewsPath is that file.
func MutedReviewsPath() (string, error) { return muted.path() }

// MutedReviews is the set of muted pull request keys.
func MutedReviews() map[string]bool { return muted.all() }

// SetMutedReview puts a pull request aside, or brings it back.
func SetMutedReview(key string, isMuted bool) error {
	return muted.set(strings.TrimSpace(key), isMuted)
}
