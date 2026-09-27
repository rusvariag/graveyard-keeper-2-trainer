//go:build !windows

package main

import "errors"

// The game only runs on Windows; this stub lets the UI build and run elsewhere
// (it can still talk to a helper that was injected by the Windows build).
func injectHelper(dllPath string) error {
	return errors.New("injection is only supported on Windows")
}

func gameRunning() bool { return false }
