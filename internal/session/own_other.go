//go:build !linux

package session

// Off Linux a VM maps ownership, as it does for a sandbox's own files.
func HandToHost(string) {}
