package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func syncGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func syncRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	r, git := repo(t)
	remote := t.TempDir()
	syncGit(t, remote, "init", "-q", "--bare", "-b", "main")
	git("remote", "add", "origin", remote)
	git("push", "-q", "-u", "origin", "main")
	return r, remote
}

func TestSyncPushesAhead(t *testing.T) {
	r, remote := syncRepo(t)
	syncGit(t, r.Root, "commit", "-q", "--allow-empty", "-m", "local")
	if err := r.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := syncGit(t, remote, "rev-parse", "main"); got != r.Head() {
		t.Fatalf("remote HEAD %s, want %s", got, r.Head())
	}
	st, err := r.Status()
	if err != nil || st.Ahead != 0 || st.Behind != 0 {
		t.Fatalf("status after sync: %+v, %v", st, err)
	}
}

func TestSyncPullsUnknownRemoteChanges(t *testing.T) {
	r, remote := syncRepo(t)
	other := t.TempDir()
	syncGit(t, other, "clone", "-q", remote, ".")
	syncGit(t, other, "commit", "-q", "--allow-empty", "-m", "remote")
	syncGit(t, other, "push", "-q")
	st, err := r.Status()
	if err != nil || st.Behind != 0 {
		t.Fatalf("remote update should be unknown: %+v, %v", st, err)
	}
	// No local commits means no push is needed, even to check it is current.
	write(t, r.Dir, "hooks/pre-push", "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(r.Dir, "hooks/pre-push"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.Sync(); err != nil {
		t.Fatal(err)
	}
	if got, want := r.Head(), syncGit(t, other, "rev-parse", "HEAD"); got != want {
		t.Fatalf("local HEAD %s, want %s", got, want)
	}
}

func TestSyncDoesNotPushAfterDivergence(t *testing.T) {
	r, remote := syncRepo(t)
	other := t.TempDir()
	syncGit(t, other, "clone", "-q", remote, ".")
	syncGit(t, other, "commit", "-q", "--allow-empty", "-m", "remote")
	syncGit(t, other, "push", "-q")
	syncGit(t, r.Root, "commit", "-q", "--allow-empty", "-m", "local")
	before := r.Head()
	write(t, r.Dir, "hooks/pre-push", "#!/bin/sh\ntouch sync-pushed\n")
	if err := os.Chmod(filepath.Join(r.Dir, "hooks/pre-push"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.Sync(); err == nil {
		t.Fatal("diverged sync succeeded")
	}
	if got := r.Head(); got != before {
		t.Fatalf("failed pull changed HEAD from %s to %s", before, got)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "sync-pushed")); !os.IsNotExist(err) {
		t.Fatalf("push ran after failed pull: %v", err)
	}
	if got, want := syncGit(t, remote, "rev-parse", "main"), syncGit(t, other, "rev-parse", "HEAD"); got != want {
		t.Fatalf("remote changed to %s, want %s", got, want)
	}
}

func TestSyncTargetsUpstreamRegardlessOfPushDefaults(t *testing.T) {
	for _, mode := range []string{"simple", "matching", "custom"} {
		t.Run(mode, func(t *testing.T) {
			r, remote := syncRepo(t)
			base := r.Head()
			// Keep the upstream name while renaming the local branch.
			syncGit(t, r.Root, "branch", "-m", "topic")
			syncGit(t, r.Root, "branch", "unrelated")
			syncGit(t, r.Root, "push", "-q", "origin", "unrelated")
			other := t.TempDir()
			syncGit(t, other, "init", "-q", "--bare", "-b", "main")
			syncGit(t, r.Root, "remote", "add", "fork", other)
			syncGit(t, r.Root, "push", "-q", "fork", "topic", "unrelated")
			syncGit(t, r.Root, "config", "remote.pushDefault", "fork")
			syncGit(t, r.Root, "config", "branch.topic.pushRemote", "fork")
			if mode == "custom" {
				syncGit(t, r.Root, "config", "remote.origin.push", "refs/heads/unrelated:refs/heads/unrelated")
				syncGit(t, r.Root, "config", "remote.fork.push", "refs/heads/unrelated:refs/heads/unrelated")
			} else {
				syncGit(t, r.Root, "config", "push.default", mode)
			}
			syncGit(t, r.Root, "switch", "-q", "unrelated")
			syncGit(t, r.Root, "commit", "-q", "--allow-empty", "-m", "unrelated local")
			syncGit(t, r.Root, "switch", "-q", "topic")
			syncGit(t, r.Root, "commit", "-q", "--allow-empty", "-m", "topic local")
			if err := r.Sync(); err != nil {
				t.Fatal(err)
			}
			if got := syncGit(t, remote, "rev-parse", "main"); got != r.Head() {
				t.Fatalf("upstream %s, want %s", got, r.Head())
			}
			for _, target := range []struct{ dir, ref string }{{remote, "unrelated"}, {other, "topic"}, {other, "unrelated"}} {
				if got := syncGit(t, target.dir, "rev-parse", target.ref); got != base {
					t.Fatalf("unrelated %s in %s changed from %s to %s", target.ref, target.dir, base, got)
				}
			}
		})
	}
}

func TestSyncRequiresCommittedBranchAndUpstream(t *testing.T) {
	for _, state := range []string{"no upstream", "detached", "unborn"} {
		t.Run(state, func(t *testing.T) {
			r, remote := syncRepo(t)
			before := syncGit(t, remote, "rev-parse", "main")
			switch state {
			case "no upstream":
				syncGit(t, r.Root, "branch", "--unset-upstream")
			case "detached":
				syncGit(t, r.Root, "switch", "-q", "--detach")
			case "unborn":
				syncGit(t, r.Root, "switch", "-q", "--orphan", "unborn")
			}
			if err := r.Sync(); err == nil {
				t.Fatalf("sync succeeded with %s", state)
			}
			if got := syncGit(t, remote, "rev-parse", "main"); got != before {
				t.Fatalf("remote changed from %s to %s", before, got)
			}
		})
	}
}
