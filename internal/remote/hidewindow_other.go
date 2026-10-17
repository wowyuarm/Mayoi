//go:build !windows

package remote

import "os/exec"

// hideWindow does nothing: only Windows gives a program started without a
// console one of its own.
func hideWindow(*exec.Cmd) {}
