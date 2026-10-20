package main

import (
	"fmt"
	"os"
	"testing"
)

// benchScm has n files changed, the source control view shown and the
// diff of every file open, for the cost of a frame of them:
// go test -short -run XXX -bench Scm
func benchScm(b *testing.B, n int) *harness {
	hs := newHarness(b, true)
	git := hs.gitRepo()
	os.MkdirAll(hs.path("many"), 0o755)
	for i := range n {
		os.WriteFile(hs.path(fmt.Sprintf("many/f%04d.go", i)), []byte("package many\n\nfunc f() {\n\tprintln(1)\n}\n"), 0o644)
	}
	git("add", ".")
	git("commit", "-qm", "many")
	for i := range n {
		os.WriteFile(hs.path(fmt.Sprintf("many/f%04d.go", i)), []byte(fmt.Sprintf("package many\n\nfunc f() {\n\tprintln(%d)\n}\n", i)), 0o644)
	}
	hs.a.run("view.scm")
	hs.settleGit()
	hs.a.openChangesTab(false)
	hs.settleGit()
	return hs
}

func BenchmarkScmIdleFrame(b *testing.B) {
	hs := benchScm(b, 2000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		step(hs.tt)
	}
}

func BenchmarkScmScrollFrame(b *testing.B) {
	hs := benchScm(b, 2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(60)
		if i/200%2 == 1 {
			dy = -60
		}
		hs.tt.Scroll(700, 400, 0, dy)
		step(hs.tt)
	}
}

// BenchmarkScmStatusFrame reads the status again, as every few seconds,
// though nothing changed.
func BenchmarkScmStatusFrame(b *testing.B) {
	hs := benchScm(b, 2000)
	st := *hs.a.git.status
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cp := st
		hs.a.setStatus(&cp)
		step(hs.tt)
	}
}

// BenchmarkReviewScrollFrame scrolls the diff of 2000 files with a
// review comment on each of 200 of them.
func BenchmarkReviewScrollFrame(b *testing.B) {
	hs := benchScm(b, 2000)
	for i := range 200 {
		line := fmt.Sprintf("\tprintln(%d)", i*10)
		hs.a.review.notes = append(hs.a.review.notes, &reviewNote{
			path: hs.path(fmt.Sprintf("many/f%04d.go", i*10)), from: 3, to: 4, diff: true, version: unfound,
			snippet: []string{"+" + line}, lines: []string{line}, text: fmt.Sprintf("Comment %d, which says a little more than a word", i),
		})
	}
	hs.a.review.gen++
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(60)
		if i/200%2 == 1 {
			dy = -60
		}
		hs.tt.Scroll(700, 400, 0, dy)
		step(hs.tt)
	}
}

// BenchmarkReviewLayout lays out the diff of 2000 files again, with a
// review comment on each of 200 of them, as a comment is kept.
func BenchmarkReviewLayout(b *testing.B) {
	hs := benchScm(b, 2000)
	for i := range 200 {
		line := fmt.Sprintf("\tprintln(%d)", i*10)
		hs.a.review.notes = append(hs.a.review.notes, &reviewNote{
			path: hs.path(fmt.Sprintf("many/f%04d.go", i*10)), from: 3, to: 4, diff: true, version: unfound,
			snippet: []string{"+" + line}, lines: []string{line}, text: fmt.Sprintf("Comment %d, which says a little more than a word", i),
		})
	}
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hs.a.review.gen++
		step(hs.tt)
	}
	b.StopTimer()
	if v := hs.a.activeTab().diff; v == nil || v.built.review != hs.a.review.gen || len(v.cards) == 0 {
		b.Fatal("the diff was not laid out with the comments")
	}
}

// BenchmarkSplitScrollFrame scrolls the diff of 2000 files side by side,
// with the bands and arrows of its changes in the gutter between them.
func BenchmarkSplitScrollFrame(b *testing.B) {
	hs := benchScm(b, 2000)
	hs.a.setDiffLayout(hs.a.activeTab().diff, layoutSplit)
	hs.settle()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(60)
		if i/200%2 == 1 {
			dy = -60
		}
		hs.tt.Scroll(700, 400, 0, dy)
		step(hs.tt)
	}
}
