package main

import (
	"os"
	"testing"
)

func TestCompareLocalFilesAcceptsEitherSide(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AutoSave = false
	left, right := hs.path("left.txt"), hs.path("right.txt")
	if err := os.WriteFile(left, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(right, []byte("one\nchanged\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := newLocalCompareView(parseDiffLayout(a.settings.DiffLayout), left, right)
	col := a.column()
	at := min(col.active+1, len(col.tabs))
	a.addTab(col, at, &colTab{diff: v})
	a.activate(col, at, true)
	hs.settleDiff(v)
	if len(v.files) != 1 || len(v.files[0].hunks) != 1 {
		t.Fatalf("local diff hunks: %#v", v.files)
	}
	f := v.files[0]
	ars := hunkArrows(f)
	if len(ars) != 2 || ars[0].dir != 1 || ars[1].dir != -1 {
		t.Fatalf("local arrows: %#v", ars)
	}
	if err := os.Chmod(left, 0o444); err != nil {
		t.Fatal(err)
	}
	if ro := hunkArrows(f); len(ro) != 1 || ro[0].dir != 1 {
		t.Fatalf("read-only left arrows: %#v", ro)
	}
	if err := os.Chmod(left, 0o644); err != nil {
		t.Fatal(err)
	}
	h := f.hunks[0]

	// Accept Left opens and edits the right document, and the diff keyboard
	// can undo and redo that edit as one history step.
	a.applyArrow(v, f, h, ars[0])
	rightDoc := a.docs[a.docIndex(right)]
	if got := rightDoc.buf.String(); got != "one\ntwo\nthree\n" {
		t.Fatalf("right after Accept Left: %q", got)
	}
	a.undoDiff(v, false)
	hs.settle()
	if got := rightDoc.buf.String(); got != "one\nchanged\nthree\n" {
		t.Fatalf("right after undo: %q", got)
	}
	a.undoDiff(v, true)
	hs.settle()
	if got := rightDoc.buf.String(); got != "one\ntwo\nthree\n" {
		t.Fatalf("right after redo: %q", got)
	}

	// Undo the right edit, then Accept Right writes the right side into left.
	a.undoDiff(v, false)
	hs.settle()
	a.applyArrow(v, f, h, ars[1])
	leftDoc := a.docs[a.docIndex(left)]
	if got := leftDoc.buf.String(); got != "one\nchanged\nthree\n" {
		t.Fatalf("left after Accept Right: %q", got)
	}
	a.saveDoc(leftDoc, nil)
	if data, err := os.ReadFile(left); err != nil || string(data) != "one\nchanged\nthree\n" {
		t.Fatalf("saved left: %q, %v", data, err)
	}
}
