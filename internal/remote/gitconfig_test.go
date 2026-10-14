package remote

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const laptopConfig = `[user]
	email = owen@example.com
	signingkey = ~/.ssh/id_ed25519.pub
[includeIf "gitdir:~/code/owenrumney/"]
	path = ~/.gitconfig-owenrumney
[includeIf "gitdir:~/code/infracost/"]
	path = ~/.gitconfig-infracost
`

func TestIncludeIfIsRetargetedAtTheWorkersCheckouts(t *testing.T) {
	got := string(RetargetGitConfig([]byte(laptopConfig), "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Contains(t, got, `gitdir:/home/pi/code/owenrumney/`)
	assert.Contains(t, got, `gitdir:/home/pi/code/infracost/`)
	assert.NotContains(t, got, "gitdir:~/", "a ~ rule matches nothing on a worker")
}

func TestATrailingSlashOnTheRootDoesNotDouble(t *testing.T) {
	got := string(RetargetGitConfig([]byte(laptopConfig), "/Users/owen", "/Users/owen/code", "/home/pi/code/"))
	assert.Contains(t, got, `gitdir:/home/pi/code/owenrumney/`)
	assert.NotContains(t, got, "//owenrumney")
}

func TestEverySigningKeyBecomesTheOneThatIsThere(t *testing.T) {
	config := "[user]\n\tsigningkey = ~/.ssh/yubikey_piv.pub\n"
	got := string(RetargetGitConfig([]byte(config), "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Contains(t, got, "signingkey = ~/.ssh/ade_signing.pub")
	assert.NotContains(t, got, "yubikey", "a key that cannot be copied cannot sign over there")
}

func TestThePathsThatCarryIdentityAreAllSent(t *testing.T) {
	rec := &recorder{}
	files := []GitConfig{
		{Name: ".gitconfig", Data: []byte(laptopConfig)},
		{Name: ".gitconfig-infracost", Data: []byte("[user]\n\temail = owen@infracost.io\n")},
	}
	require.NoError(t, testHost(t, rec).PrepareGitConfig(context.Background(), files, "/Users/owen", "/Users/owen/code", "/home/pi/code"))

	all := said(rec)
	assert.Contains(t, all, ".gitconfig")
	assert.Contains(t, all, ".gitconfig-infracost")
	var bodies string
	for _, in := range rec.input {
		bodies += string(in)
	}
	assert.Contains(t, bodies, "owen@infracost.io", "the per-directory email is the whole point")
}

func TestAConfigNameIsNotAPath(t *testing.T) {
	rec := &recorder{}
	files := []GitConfig{{Name: "../../etc/passwd", Data: []byte("x")}}
	require.NoError(t, testHost(t, rec).PrepareGitConfig(context.Background(), files, "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Empty(t, rec.calls, "a name with a separator is not a file in HOME")
}

func TestAnEmptyConfigIsNotSent(t *testing.T) {
	rec := &recorder{}
	require.NoError(t, testHost(t, rec).PrepareGitConfig(context.Background(),
		[]GitConfig{{Name: ".gitconfig"}}, "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Empty(t, rec.calls, "writing an empty file would wipe what is there")
}

func TestRetargetingLeavesEverythingElseAlone(t *testing.T) {
	got := string(RetargetGitConfig([]byte(laptopConfig), "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Contains(t, got, "email = owen@example.com")
	assert.Equal(t, strings.Count(laptopConfig, "\n"), strings.Count(got, "\n"))
}

func TestAnSSHRewriteDoesNotFollowYouToTheWorker(t *testing.T) {
	config := "[url \"ssh://git@github.com/\"]\n\tinsteadOf = https://github.com/\n" +
		"[url \"git@github.com:\"]\n\tpushInsteadOf = https://github.com/\n" +
		"[url \"https://github.com/rust-lang/crates.io-index\"]\n\tinsteadOf = https://github.com/rust-lang/crates.io-index\n"
	got := string(RetargetGitConfig([]byte(config), "/Users/owen", "/Users/owen/code", "/home/pi/code"))

	assert.NotContains(t, got, "\n\tinsteadOf = https://github.com/\n",
		"an https remote rewritten to ssh has no key to push with over there")
	assert.NotContains(t, got, "\n\tpushInsteadOf =")
	assert.Contains(t, got, "\n\tinsteadOf = https://github.com/rust-lang/crates.io-index",
		"an https-to-https rewrite still works over there")
	assert.Equal(t, strings.Count(config, "\n"), strings.Count(got, "\n"))
}

func TestARewriteIsOnlyDroppedInsideItsOwnSection(t *testing.T) {
	config := "[url \"ssh://git@github.com/\"]\n\tinsteadOf = https://github.com/\n" +
		"[user]\n\tinsteadOf = not-a-url-rule\n"
	got := string(RetargetGitConfig([]byte(config), "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Contains(t, got, "insteadOf = not-a-url-rule", "the section ended at the next header")
}

const rewritingConfig = `[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
[url "git@bitbucket.org:"]
	pushInsteadOf = https://bitbucket.org/
[url "https://github.com/rust-lang/crates.io-index"]
	insteadOf = https://github.com/rust-lang/crates.io-index
`

func TestARewriteToSSHIsNotSent(t *testing.T) {
	got := string(RetargetGitConfig([]byte(rewritingConfig), "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.NotContains(t, got, "\tinsteadOf = https://github.com/\n", "https sent to ssh is a push the worker cannot make")
	assert.NotContains(t, got, "\tpushInsteadOf", "the scp-like spelling is ssh too")
	assert.Contains(t, got, "\tinsteadOf = https://github.com/rust-lang/crates.io-index",
		"a rewrite between https urls still works over there")
	assert.Equal(t, strings.Count(rewritingConfig, "\n"), strings.Count(got, "\n"))
}

func TestAValueContinuedOverTwoLinesGoesWhole(t *testing.T) {
	// Half a struck-out rule leaves a bare line git reads as a section of its
	// own, and every git command on the worker then fails.
	config := "[url \"ssh://git@github.com/\"]\n\tinsteadOf = https://\\\ngithub.com/\n[user]\n\temail = owen@example.com\n"
	got := string(RetargetGitConfig([]byte(config), "/Users/owen", "/Users/owen/code", "/home/pi/code"))

	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "github.com/") && !strings.Contains(line, "[url") {
			assert.True(t, strings.HasPrefix(line, "#"), "%q was left behind", line)
		}
	}
	assert.Contains(t, got, "email = owen@example.com")
	assert.Equal(t, strings.Count(config, "\n"), strings.Count(got, "\n"))
}

func TestOnlyTheRewritesInsideAURLSectionGo(t *testing.T) {
	config := "[url \"ssh://git@github.com/\"]\n\tinsteadOf = https://github.com/\n[user]\n\temail = owen@example.com\n"
	got := string(RetargetGitConfig([]byte(config), "/Users/owen", "/Users/owen/code", "/home/pi/code"))
	assert.Contains(t, got, "email = owen@example.com", "the section after it is untouched")
}
