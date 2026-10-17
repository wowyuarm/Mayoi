package git

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorktreeForBranch(t *testing.T) {
	// The name has a newline and a quote in it; Windows forbids both.
	if runtime.GOOS == "windows" {
		t.Skip("the worktree's name has characters Windows forbids")
	}
	r, git := repo(t)
	path := filepath.Join(t.TempDir(), "work tree\nquoted\"name")
	git("worktree", "add", "-b", "topic", path)
	// Git canonicalizes the temporary directory's symlink on macOS.
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.WorktreeForBranch("topic")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	got, err = r.WorktreeForBranch("missing")
	if err != nil || got != "" {
		t.Fatalf("missing: %q, %v", got, err)
	}
	var occupied *WorktreeInUse
	if switchErr := r.Switch("topic", false); !errors.As(switchErr, &occupied) || occupied.Path != want {
		t.Fatalf("switch occupied branch: %v", switchErr)
	}
	current, err := r.run(nil, "symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(current)) != "main" {
		t.Fatalf("original branch changed: %q, %v", current, err)
	}
}

func TestPruneMissingWorktree(t *testing.T) {
	r, git := repo(t)
	path := filepath.Join(t.TempDir(), "gone")
	git("worktree", "add", "-b", "topic", path)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	var occupied *WorktreeInUse
	if err := r.Switch("topic", false); !errors.As(err, &occupied) {
		t.Fatalf("switch to the branch of a missing worktree: %v", err)
	}
	if err := r.PruneWorktrees(); err != nil {
		t.Fatal(err)
	}
	if err := r.Switch("topic", false); err != nil {
		t.Fatalf("switch after pruning: %v", err)
	}
}

// Git before 2.36 lists worktrees a field a line, with no -z, and a path
// with a newline in it over two lines.
func TestNulRecords(t *testing.T) {
	out := "worktree /r\nHEAD 8d66\nbranch refs/heads/main\n\n" +
		"worktree /w/plain\nHEAD 8d66\nbranch refs/heads/plain\n\n" +
		"worktree /w/work tree\nquoted\"name\nHEAD 8d66\nbranch refs/heads/topic\n\n" +
		"worktree /bare\nbare\n\n"
	want := "worktree /r\x00HEAD 8d66\x00branch refs/heads/main\x00\x00" +
		"worktree /w/plain\x00HEAD 8d66\x00branch refs/heads/plain\x00\x00" +
		"worktree /w/work tree\nquoted\"name\x00HEAD 8d66\x00branch refs/heads/topic\x00\x00" +
		"worktree /bare\x00bare\x00\x00"
	got := nulRecords(out)
	if got != want {
		t.Fatalf("nulRecords:\n%q\nwant\n%q", got, want)
	}
	if p := worktreeForBranch("/r", got, "topic"); p != "/w/work tree\nquoted\"name" {
		t.Errorf("topic is in %q", p)
	}
	if p := worktreeForBranch("/r", got, "plain"); p != "/w/plain" {
		t.Errorf("plain is in %q", p)
	}
}
