package main

import (
	"os"
	"path/filepath"
	"testing"
)

// benchMarkdown is the changelog and the docs, some thousands of lines
// of Markdown, for the cost of a parse and of a frame of the preview:
// go test -short -run XXX -bench Markdown
func benchMarkdown(b *testing.B) []byte {
	var src []byte
	for _, name := range []string{"../../CHANGELOG.md", "../../docs/FEATURES.md", "../../docs/ARCHITECTURE.md", "../../docs/SETTINGS.md", "../../README.md"} {
		data, err := os.ReadFile(name)
		if err != nil {
			b.Fatal(err)
		}
		src = append(append(src, data...), '\n')
	}
	return src
}

func BenchmarkMarkdownParse(b *testing.B) {
	src := benchMarkdown(b)
	path := filepath.Join(b.TempDir(), "doc.md")
	cache := &markdownCache{}
	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for range b.N {
		parseMarkdown(src, path, "", nil, cache)
	}
}

func benchMarkdownPreview(b *testing.B) (*harness, *editor) {
	hs := newHarness(b, true)
	os.WriteFile(hs.path("big.md"), benchMarkdown(b), 0o644)
	hs.a.openFile(hs.path("big.md"), true)
	hs.settle()
	d := hs.a.activeEditor()
	hs.a.setPreview(d, true)
	hs.until("preview", func() bool { return d.preview.loaded })
	hs.settle()
	return hs, d
}

func BenchmarkMarkdownIdleFrame(b *testing.B) {
	hs, _ := benchMarkdownPreview(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		step(hs.tt)
	}
}

func BenchmarkMarkdownScrollFrame(b *testing.B) {
	hs, _ := benchMarkdownPreview(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		dy := float32(80)
		if i/200%2 == 1 {
			dy = -80
		}
		hs.tt.Scroll(600, 400, 0, dy)
		step(hs.tt)
	}
}
