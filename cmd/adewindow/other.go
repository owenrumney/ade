//go:build !darwin

package main

import "log"

// The spike is about the Mac's own web view; elsewhere there is nothing
// to measure, and the package still has to build.
func main() {
	log.Fatal("the webview spike is macOS only")
}
