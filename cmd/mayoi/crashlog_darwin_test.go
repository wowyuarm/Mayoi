package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeepSystemCrashes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reports := filepath.Join(home, "Library", "Logs", "DiagnosticReports")
	os.MkdirAll(reports, 0o755)
	exe, _ := os.Executable()
	report := func(name string, at time.Time) {
		path := filepath.Join(reports, filepath.Base(exe)+"-"+name+".ips")
		os.WriteFile(path, []byte(`{"app_version":"0.11.5"}`+"\n"+`{"path":"`+home+`/x"}`), 0o644)
		os.Chtimes(path, at, at)
	}
	dir := filepath.Join(t.TempDir(), "crashes")
	now := time.Now().Truncate(time.Second)

	// The first launch takes none: they're from before.
	report("old", now.Add(-time.Hour))
	keepSystemCrashes(dir)
	if logs := listCrashes(dir); len(logs) != 0 {
		t.Fatalf("first launch kept %+v", logs)
	}

	// Then the new ones, but not one of a crash crash.log kept.
	report("new", now.Add(time.Minute))
	report("go", now.Add(2*time.Minute))
	os.WriteFile(filepath.Join(dir, now.Add(2*time.Minute+3*time.Second).Format(crashTime)+".log"), []byte("panic: x"), 0o644)
	os.WriteFile(filepath.Join(reports, "Other-x.ips"), nil, 0o644)
	keepSystemCrashes(dir)
	logs := listCrashes(dir)
	if len(logs) != 2 || logs[0].kind != crashSystem || logs[1].kind != crashFatal {
		t.Fatalf("logs = %+v", logs)
	}
	b, _ := os.ReadFile(logs[0].path)
	if strings.Contains(string(b), home) || !strings.Contains(string(b), `"~/x"`) {
		t.Errorf("not redacted: %s", b)
	}

	// Once seen, never again.
	os.Remove(logs[0].path)
	keepSystemCrashes(dir)
	if logs := listCrashes(dir); len(logs) != 1 {
		t.Errorf("seen again: %+v", logs)
	}
}
