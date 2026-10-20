package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAppendSourceLinesKeepsLocalChange(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig := "one\ntwo\nthree\n"
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	changed := strings.Replace(orig, "two\n", "local\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	f, hs0 := v.hunksAt(v.starts[0])
	if len(hs0) != 1 {
		t.Fatalf("hunks: %v", hs0)
	}
	a.appendHunks(v, f, hs0)
	hs.settle()
	got := a.docs[a.docIndex(hs.path("t.txt"))].buf.String()
	if want := "one\nlocal\ntwo\nthree\n"; got != want {
		t.Fatalf("append result %q, want %q", got, want)
	}
	a.docs[a.docIndex(hs.path("t.txt"))].views[0].undo()
	if got := a.docs[a.docIndex(hs.path("t.txt"))].buf.String(); got != changed {
		t.Fatalf("undo result %q, want %q", got, changed)
	}
}

// TestAppendRegistersTheUndoTargetOfTheDiff: after an append, the undo
// of the diff's keyboard takes the append back.
func TestAppendRegistersTheUndoTargetOfTheDiff(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig := "one\ntwo\nthree\n"
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	changed := strings.Replace(orig, "two\n", "local\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	f, hs0 := v.hunksAt(v.starts[0])
	if len(hs0) != 1 {
		t.Fatalf("hunks: %v", hs0)
	}
	a.appendHunks(v, f, hs0)
	hs.settle()
	if v.edited == nil {
		t.Fatal("the append did not register the undo target of the diff")
	}
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyZ, Mods: ui.Cmd}) {
		t.Fatal("the diff did not take the undo")
	}
	hs.settle()
	if got := a.docs[a.docIndex(hs.path("t.txt"))].buf.String(); got != changed {
		t.Fatalf("undo from the diff left %q, want %q", got, changed)
	}
}

// TestAppendAtCursor: the Append command appends the lines of the
// compared side of the change the diff has chosen.
func TestAppendAtCursor(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig := "one\ntwo\nthree\n"
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	changed := strings.Replace(orig, "two\n", "local\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	// The diff chooses the first change, as a press of ] does.
	v.stepHunk(1, crossAlways)
	a.post(func() { a.appendAtCursor() })
	hs.settle()
	if got := a.docs[a.docIndex(hs.path("t.txt"))].buf.String(); got != "one\nlocal\ntwo\nthree\n" {
		t.Fatalf("append at cursor: %q", got)
	}
}
