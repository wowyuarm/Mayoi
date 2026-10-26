package main

import (
	"os"
	"path/filepath"

	"mayoi/internal/highlight"
	"mayoi/internal/settings"
	"mayoi/locales"
)

// untitledDir is the folder beside the settings that keeps the text of
// the untitled documents as the app quits, "" without settings.
func (a *app) untitledDir() string {
	if a.settings.Path() == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(a.settings.Path()), "untitled")
}

// keepUntitled writes the text of the window's changed untitled
// documents beside the settings as the app quits, for the next start to
// open them again, rather than asking where to save them: a restart of
// the computer goes on. An empty one has nothing to keep. One that could
// not be written is asked about still.
func (a *app) keepUntitled() {
	dir := a.untitledDir()
	if dir == "" {
		return
	}
	for _, d := range a.docs {
		if d.path != "" || !d.dirty() {
			continue
		}
		if d.buf.Len() == 0 {
			d.markSaved()
			continue
		}
		if os.MkdirAll(dir, 0o755) != nil {
			return
		}
		if d.kept == "" {
			f, err := os.CreateTemp(dir, "*.txt")
			if err != nil {
				continue
			}
			f.Close()
			d.kept = filepath.Base(f.Name())
		}
		if os.WriteFile(filepath.Join(dir, d.kept), []byte(d.buf.String()), 0o644) == nil {
			d.keptVersion = d.buf.Version()
		}
	}
}

// keptNow reports whether d is an untitled document whose text is kept
// as it is now.
func (d *document) keptNow() bool {
	return d.path == "" && d.kept != "" && d.keptVersion == d.buf.Version()
}

// dropKept removes the text kept of an untitled document, saved to a
// file or its changes discarded.
func (a *app) dropKept(d *document) {
	if d.kept == "" {
		return
	}
	if dir := a.untitledDir(); dir != "" {
		os.Remove(filepath.Join(dir, d.kept))
	}
	d.kept = ""
}

// anyUnkept reports whether quitting loses changes: those of documents
// other than the untitled ones kept.
func (a *app) anyUnkept() bool {
	for _, d := range a.docs {
		if d.dirty() && !d.keptNow() {
			return true
		}
	}
	return false
}

// addUntitled adds a tab of an untitled document with the number n and
// text to col.
func (a *app) addUntitled(col *column, n int, text string) {
	a.nextID++
	doc := newDocument(a.nextID, "", "", highlight.New(nil, nil))
	doc.title, doc.number = locales.UntitledName(n), n
	if text != "" {
		// Changed, as it was when the app quit.
		doc.buf.Set(text)
		doc.indent = detectIndent(doc.buf.Snapshot(), "")
		doc.measureWidest()
	}
	a.docs = append(a.docs, doc)
	a.addTab(col, len(col.tabs), &colTab{ed: newEditor(doc)})
}

// restoreUntitled opens in col the untitled document of the last session
// that ts is the tab of, with the text kept of it, which it then no
// longer needs.
func (a *app) restoreUntitled(col *column, ts settings.Tab) {
	dir := a.untitledDir()
	if dir == "" || filepath.Base(ts.Untitled) != ts.Untitled {
		return
	}
	path := filepath.Join(dir, ts.Untitled)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	a.untitled = max(a.untitled, ts.Number)
	a.addUntitled(col, ts.Number, string(data))
	a.activate(col, len(col.tabs)-1, false)
	os.Remove(path)
}
