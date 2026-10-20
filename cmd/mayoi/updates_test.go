package main

import (
	"testing"

	"mayoi/locales"
)

// TestUpdateIndicator: a new version shows in the status bar and on the
// welcome page, whose click opens the update window.
func TestUpdateIndicator(t *testing.T) {
	hs := newHarness(t, false)
	if hs.tt.HasText(locales.UpdateAvailable("0.12.0")) || hs.tt.HasText("0.12.0") {
		t.Fatal("up to date, an update shows")
	}
	hs.h.newVersion = "0.12.0"
	hs.a.invalidate()
	hs.settle()
	if !hs.tt.HasText("0.12.0") || !hs.tt.HasText(locales.UpdateAvailable("0.12.0")) {
		t.Fatal("the update does not show")
	}
	if err := hs.tt.Click(locales.UpdateAvailable("0.12.0")); err != nil {
		t.Fatal(err)
	}
	if hs.h.updateOpens != 1 {
		t.Errorf("the click opened %d update windows", hs.h.updateOpens)
	}
}
