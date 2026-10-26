//go:build !darwin

package main

// keyRepeat has a key held down repeat rather than open the accents of a
// letter, which only macOS does.
func keyRepeat() {}
