//go:build windows

package main

import "os"

// writeSpecialFiles makes the files of dir that are not regular ones,
// which a Markdown preview may name, writes the Markdown that names them,
// and returns their paths and the path of the Markdown. Windows has
// neither a FIFO for a test to make nor a device that never ends, so
// there are none.
func writeSpecialFiles(dir string) (files []string, markdown string) {
	markdown = dir + "/README.md"
	os.WriteFile(markdown, []byte("no special files here"), 0o644)
	return nil, markdown
}
