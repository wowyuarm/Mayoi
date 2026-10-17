//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// writeSpecialFiles makes the files of dir that are not regular ones,
// which a Markdown preview may name — a FIFO, and a device that never
// ends — writes the Markdown that names them, and returns their paths and
// the path of the Markdown. Reading either must not block or allocate, as
// a document names what it names.
func writeSpecialFiles(dir string) (files []string, markdown string) {
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err == nil {
		files = append(files, fifo)
	}
	files = append(files, "/dev/zero")
	md := ""
	for _, f := range files {
		md += "![x](" + f + ") "
	}
	markdown = filepath.Join(dir, "README.md")
	os.WriteFile(markdown, []byte(md), 0o644)
	return files, markdown
}
