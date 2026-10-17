package remote

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// These tests run against the real wsl.exe, which only a machine with WSL
// has: MAYOI_WSL_TEST_DISTRO names the distribution to use, and without
// it they are skipped, as they are where wsl.exe is not there. They are
// the ones that prove what the fakes cannot: the encoding wsl.exe really
// writes, the words it fails in, and the shells of a distribution.

// liveDistro is the distribution of MAYOI_WSL_TEST_DISTRO, or a default
// when there is one, "" to skip.
func liveDistro(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("wsl.exe is not there")
	}
	if d := os.Getenv("MAYOI_WSL_TEST_DISTRO"); d != "" {
		return d
	}
	hosts, err := WSLHosts()
	if err != nil || len(hosts) == 0 {
		t.Skip("no WSL distribution to test with")
	}
	return WSLDistribution(hosts[0])
}

// TestLiveWSLHosts lists the distributions of the machine, which is where
// the encoding of wsl.exe is really seen.
func TestLiveWSLHosts(t *testing.T) {
	liveDistro(t)
	hosts, err := WSLHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) == 0 {
		t.Fatal("no distributions listed")
	}
	for _, h := range hosts {
		if !IsWSLHost(h) || WSLDistribution(h) == "" {
			t.Fatalf("host %q", h)
		}
		if strings.ContainsAny(WSLDistribution(h), "\x00\r\n") {
			t.Fatalf("host %q kept the encoding", h)
		}
	}
	t.Logf("distributions: %v", hosts)
}

// TestLiveWSLMissing proves what wsl.exe says of a distribution that is
// not there, in whatever language, and that it is refused, not lost.
func TestLiveWSLMissing(t *testing.T) {
	liveDistro(t)
	c := For("wsl:mayoi-there-is-no-such-distribution")
	defer Shutdown()
	c.Connect()
	deadline := time.Now().Add(30 * time.Second)
	for {
		s, err := c.State()
		if s == Offline {
			if err == nil {
				t.Fatal("no error for a missing distribution")
			}
			if !Refused(err) {
				t.Fatalf("a missing distribution is %v, not refused", err)
			}
			t.Logf("said: %v", err)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestLiveWSLHost runs the whole command layer of the connection against a
// real distribution: the probe, files, folders, writes and a rename, and
// the paths of a program run through it.
func TestLiveWSLHost(t *testing.T) {
	d := liveDistro(t)
	c := For("wsl:" + d)
	defer Shutdown()
	exerciseHost(t, c.Host)
}

// TestLiveWSLTerminal runs a terminal of a real distribution: its shell
// answers, its PTY takes the size before and after a resize, and its exit
// ends the terminal.
func TestLiveWSLTerminal(t *testing.T) {
	d := liveDistro(t)
	host := "wsl:" + d
	home := wslHome(t, host)
	c, err := NewWSLTerminal(t.Context(), host, home, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("echo live-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	if got := readUntil(t, c, 30*time.Second, "live-42"); !strings.Contains(got, "live-42") {
		t.Fatalf("the shell's answer = %q", got)
	}
	if got := waitSize(t, c, 80, 24, 30*time.Second); !strings.Contains(got, "24 80") {
		t.Fatalf("stty size = %q", got)
	}
	if err := c.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if got := waitSize(t, c, 100, 30, 30*time.Second); !strings.Contains(got, "30 100") {
		t.Fatalf("stty size after Resize = %q", got)
	}
	if _, err := c.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for !c.Unplanned() {
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not end as the shell exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// wslHome is the home folder of the distribution, for the terminal to
// start in, as a shell would give it.
func wslHome(t *testing.T, host string) string {
	t.Helper()
	c := For(host)
	defer Shutdown()
	c.Connect()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if s, _ := c.State(); s == Connected {
			if h := c.Info().Home; h != "" {
				return h
			}
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "/"
}

// TestLiveWSLDescribe prints what the machine's wsl.exe says, for the
// record of a real run.
func TestLiveWSLDescribe(t *testing.T) {
	d := liveDistro(t)
	out, err := exec.Command("wsl.exe", "--version").Output()
	if err != nil {
		t.Logf("wsl --version: %v", err)
	}
	for _, l := range strings.Split(decodeWSL(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && !slices.Contains([]string{"\r"}, l) {
			t.Logf("%s", l)
		}
	}
	t.Logf("distro: %s", d)
}
