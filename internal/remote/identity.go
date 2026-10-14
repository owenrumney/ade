package remote

import (
	"context"
	"fmt"
	"strings"
)

// Identity is who commits over there, and what signs for them. The key is the
// private half: git signs with it, so a worker without it cannot sign at all.
type Identity struct {
	Name  string
	Email string

	// SigningKey and SigningPub are an ssh key pair, empty when this machine
	// does not sign or signs with gpg, which is not sent.
	SigningKey []byte
	SigningPub []byte

	// KnownHosts are this machine's github.com host keys. Sent rather than
	// scanned, so the worker trusts what you already trust.
	KnownHosts []byte
}

// Named reports whether there is a name and an email to send.
func (i Identity) Named() bool {
	return strings.TrimSpace(i.Name) != "" && strings.TrimSpace(i.Email) != ""
}

// Signs reports whether both halves of the signing key are here.
func (i Identity) Signs() bool {
	return len(i.SigningKey) > 0 && len(i.SigningPub) > 0
}

// writeSigningKey puts the pair in the worker's ~/.ssh. Read from stdin, not
// argv, so the private half never appears in a process list.
const writeSigningKey = `set -eu
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
cat > "$HOME/.ssh/ade_signing"
chmod 600 "$HOME/.ssh/ade_signing"`

const writeSigningPub = `set -eu
mkdir -p "$HOME/.ssh"
cat > "$HOME/.ssh/ade_signing.pub"
chmod 644 "$HOME/.ssh/ade_signing.pub"`

// useSigningKey scopes the key to github.com, and names both the config and the
// key absolutely: ssh expands ~ from the passwd file, not the worker's HOME.
const useSigningKey = `set -eu
mkdir -p "$HOME/.ssh"
cat > "$HOME/.ssh/config" <<EOF
Host github.com
  User git
  IdentityFile $HOME/.ssh/ade_signing
  IdentitiesOnly yes
  UserKnownHostsFile $HOME/.ssh/known_hosts
EOF
chmod 600 "$HOME/.ssh/config"
git config --global core.sshCommand "ssh -F $HOME/.ssh/config"`

// writeKnownHosts hands over the laptop's github.com keys. Without them a
// push stops to ask about an unknown host, with nobody there to answer.
const writeKnownHosts = `set -eu
mkdir -p "$HOME/.ssh"
cat > "$HOME/.ssh/known_hosts"
chmod 644 "$HOME/.ssh/known_hosts"`

// PrepareGitIdentity gives the worker your name, email and signing key, so a
// commit made over there is yours and passes a signed-commits policy.
func (h *Host) PrepareGitIdentity(ctx context.Context, who Identity) error {
	if !who.Named() {
		return nil
	}
	for _, pair := range [][2]string{{"user.name", who.Name}, {"user.email", who.Email}} {
		if err := h.gitConfig(ctx, pair[0], pair[1]); err != nil {
			return err
		}
	}
	if !who.Signs() {
		return nil
	}
	if _, err := h.RunInput(ctx, who.SigningKey, "docker", "exec", "-i", workerName, "sh", "-c", writeSigningKey); err != nil {
		return fmt.Errorf("send the signing key to %s: %w", h.Name, err)
	}
	if _, err := h.RunInput(ctx, who.SigningPub, "docker", "exec", "-i", workerName, "sh", "-c", writeSigningPub); err != nil {
		return fmt.Errorf("send the signing certificate to %s: %w", h.Name, err)
	}
	// The same key pushes, so ssh is pointed at it and told whom to trust.
	if _, err := h.RunInput(ctx, nil, "docker", "exec", "-i", workerName, "sh", "-c", useSigningKey); err != nil {
		return fmt.Errorf("point ssh at the signing key on %s: %w", h.Name, err)
	}
	if len(who.KnownHosts) > 0 {
		if _, err := h.RunInput(ctx, who.KnownHosts, "docker", "exec", "-i", workerName, "sh", "-c", writeKnownHosts); err != nil {
			return fmt.Errorf("send the known hosts to %s: %w", h.Name, err)
		}
	}
	// The public half names the key to git; the private half beside it signs.
	for _, pair := range [][2]string{
		{"gpg.format", "ssh"},
		{"user.signingkey", "~/.ssh/ade_signing.pub"},
		{"commit.gpgsign", "true"},
		{"tag.gpgsign", "true"},
	} {
		if err := h.gitConfig(ctx, pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

// PrepareGitCredentials points git at the worker's gh, which already holds the
// token. Without it a push to https asks for a login nobody is there to type.
func (h *Host) PrepareGitCredentials(ctx context.Context) error {
	if _, err := h.Run(ctx, "docker", "exec", workerName, "gh", "auth", "setup-git"); err != nil {
		return fmt.Errorf("set up git credentials on %s: %w", h.Name, err)
	}
	if _, err := h.Run(ctx, "docker", "exec", workerName, "sh", "-c", sendHTTPSRewrite); err != nil {
		return fmt.Errorf("send the https rewrite to %s: %w", h.Name, err)
	}
	return nil
}

const httpsRewrite = "url.https://github.com/.insteadOf"

// sendHTTPSRewrite sends an ssh remote over https: there is no key to offer
// over there, but gh's token logs in. One exec, so the two spellings land together.
const sendHTTPSRewrite = `set -eu
key='` + httpsRewrite + `'
git config --global --unset-all "$key" '^(git@github\.com:|ssh://git@github\.com/)$' || true
git config --global --add "$key" 'git@github.com:'
git config --global --add "$key" 'ssh://git@github.com/'`

func (h *Host) gitConfig(ctx context.Context, key, value string) error {
	if _, err := h.Run(ctx, "docker", "exec", workerName, "git", "config", "--global", key, value); err != nil {
		return fmt.Errorf("set %s on %s: %w", key, h.Name, err)
	}
	return nil
}
