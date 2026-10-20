package main

import "testing"

// A line modified below one added has a number on each side; its words
// that changed are those against its own peer, on either side.
func TestLineWordsPairsOldWithNew(t *testing.T) {
	f := &diffFile{}
	f.old.lines = []string{"a", "f(x)", "z"}
	f.new.lines = []string{"a", "added", "f(x, y)", "z"}

	if got := f.lineWords(false, 1, 2); len(got) != 0 {
		t.Errorf("old side %v, want none", got)
	}
	if got := f.lineWords(true, 2, 1); len(got) != 1 || f.new.lines[2][got[0].Start:got[0].End] != ", y" {
		t.Errorf("new side %v, want the run of \", y\"", got)
	}
}

// A diff whose files went down to fewer than those near its view, as when
// changes are committed while it is scrolled to the last of them, reads
// what is left rather than slicing past it.
func TestRefreshDiffHandlesShrunkFileList(t *testing.T) {
	a := &app{}
	v := newDiffView(layoutInline)
	v.commit = &commitDiff{loaded: true}
	v.files = []*diffFile{{}, {}, {}, {}}
	v.near = [2]int{10, 18}

	a.refreshDiff(v)
}

// A row laid out for a file no longer in the diff anchors nothing.
func TestDiffAnchorIgnoresStaleFileRow(t *testing.T) {
	v := newDiffView(layoutInline)
	v.files = []*diffFile{{}, {}, {}, {}}
	v.rows = []diffRow{{kind: rowFile, file: 4}}

	if got := v.anchorOf(0); got != (diffAnchor{}) {
		t.Errorf("stale row has anchor %+v", got)
	}
}
