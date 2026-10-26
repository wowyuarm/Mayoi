package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// keepSystemCrashes copies the reports macOS wrote of Mayoi's crashes
// since the last launch into dir, the crashes folder, with the home folder
// taken out: such as of an uncaught Objective-C exception, which crash.log
// misses. dir's .ips-seen file has the time of the latest one seen. At the
// first launch none are copied, being from before Mayoi kept them, and
// neither is one of a crash crash.log already kept.
func keepSystemCrashes(dir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	prefix := strings.ToLower(filepath.Base(exe)) + "-"
	seen := filepath.Join(dir, ".ips-seen")
	fi, err := os.Stat(seen)
	first := err != nil
	var since time.Time
	if !first {
		since = fi.ModTime()
	}
	latest := since
	reports := filepath.Join(home, "Library", "Logs", "DiagnosticReports")
	entries, _ := os.ReadDir(reports)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ips") || !strings.HasPrefix(strings.ToLower(name), prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().After(since) {
			continue
		}
		t := info.ModTime()
		if t.After(latest) {
			latest = t
		}
		if first || crashNear(dir, t) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(reports, name))
		if err != nil || os.MkdirAll(dir, 0o755) != nil {
			continue
		}
		path := filepath.Join(dir, t.Format(crashTime)+"-system.log")
		if os.WriteFile(path, []byte(redactHome(string(b))), 0o644) == nil {
			os.Chtimes(path, t, t)
		}
	}
	if first {
		latest = time.Now()
	}
	if first || latest.After(since) {
		if os.MkdirAll(dir, 0o755) == nil && os.WriteFile(seen, nil, 0o644) == nil {
			os.Chtimes(seen, latest, latest)
		}
	}
}

// crashNear reports whether dir keeps the log of a crash, not one the
// system reported, within seconds of t: the same crash.
func crashNear(dir string, t time.Time) bool {
	for _, c := range listCrashes(dir) {
		if c.kind != crashSystem && c.at.Sub(t).Abs() <= 10*time.Second {
			return true
		}
	}
	return false
}
