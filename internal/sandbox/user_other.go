//go:build !linux

package sandbox

// Off Linux a VM maps ownership. Design: .git/ade/docs/sandbox-file-ownership.md.
func hostUser(string) (string, []string) { return "", nil }

func handToGuest(...string) {}
