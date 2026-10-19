//go:build !windows

package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

// specialFiles makes the files in dir that are not regular ones, which a
// Markdown preview may name, and returns them: a FIFO, and a device that
// never ends.
func specialFiles(t *testing.T, dir string) []string {
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{fifo, "/dev/zero"}
}
