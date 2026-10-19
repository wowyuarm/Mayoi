package main

import "testing"

// specialFiles makes the files in dir that are not regular ones, which a
// Markdown preview may name, and returns them: none on Windows, which has
// neither a FIFO a test can make nor a device that never ends.
func specialFiles(*testing.T, string) []string { return nil }
