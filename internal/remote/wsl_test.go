package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
	"unicode/utf16"
)

func TestWSLHost(t *testing.T) {
	for host, want := range map[string]string{
		"wsl:Ubuntu":        "Ubuntu",
		"wsl:Debian 12":     "Debian 12",
		"wsl:ubuntu-22.04":  "ubuntu-22.04",
		"wsl:":              "",
		"wsl":               "",
		"box":               "",
		"me@box":            "",
		"[wsl:Ubuntu]":      "",
		"wsl:Ubuntu:2222":   "Ubuntu:2222",
		"/ssh:wsl:Ubuntu/x": "",
	} {
		if got := WSLDistribution(host); got != want {
			t.Errorf("WSLDistribution(%q) = %q", host, got)
		}
	}
	ok := map[string]bool{"wsl:Ubuntu": true, "wsl:openSUSE": true}
	for host, want := range ok {
		if IsWSLHost(host) != want {
			t.Errorf("IsWSLHost(%q) = %v", host, !want)
		}
	}
	for _, bad := range []string{"wsl:", "wsl", "box", "me@box:22", "[wsl:Ubuntu]", ""} {
		if IsWSLHost(bad) {
			t.Errorf("IsWSLHost(%q) is true", bad)
		}
	}
	if err := CheckHost("wsl:Ubuntu"); err != nil {
		t.Errorf("CheckHost(wsl:Ubuntu) = %v", err)
	}
	for _, bad := range []string{"wsl:ubu ntu", "wsl:ubu`ntu"} {
		if err := CheckHost(bad); err == nil {
			t.Errorf("CheckHost(%q) passed", bad)
		}
	}
}

// utf16le is s as wsl.exe writes it, with its BOM.
func utf16le(s string) []byte {
	b := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func TestDecodeWSL(t *testing.T) {
	if got := decodeWSL(utf16le("Ubuntu\r\nDebian\r\n")); got != "Ubuntu\r\nDebian\r\n" {
		t.Fatalf("decodeWSL of UTF-16 = %q", got)
	}
	if got := decodeWSL([]byte("plain UTF-8")); got != "plain UTF-8" {
		t.Fatalf("decodeWSL of UTF-8 = %q", got)
	}
	if got := decodeWSL(utf16le("list is empty")); got != "list is empty" {
		t.Fatalf("decodeWSL of a message = %q", got)
	}
}

// fakeWSL is a wsl.exe that lists the distributions in the encoding of
// the real one — UTF-16LE with a BOM, or UTF-8 when WSL_UTF8 is set —
// and runs commands in a distribution it answers, by this machine's sh.
const fakeWSL = `#!/bin/sh
case "$*" in
  *--list*)
    if [ -n "$WSL_FAIL" ]; then
      printf '%s' "$WSL_FAIL" >&2
      exit 1
    fi
    if [ -n "$WSL_UTF8" ]; then
      printf 'Ubuntu\nDebian\n'
    else
      # "Ubuntu\r\nDebian\r\n" as wsl.exe writes it: UTF-16LE with a BOM.
      printf '\377\376\125\000\142\000\165\000\156\000\164\000\165\000\015\000\012\000\104\000\145\000\142\000\151\000\141\000\156\000\015\000\012\000'
    fi
    exit 0
    ;;
esac
distro=
prev=
for a in "$@"; do
  if [ "$prev" = "--distribution" ]; then distro=$a; fi
  prev=$a
done
if [ "$distro" != Ubuntu ] && [ "$distro" != Debian ]; then
  # As the real wsl.exe does: the message on stdout, in UTF-16LE, naming
  # the code of the failure; exit 255.
  printf '\346\214\207\344\270\215\345\255\230\345\234\250\345\205\267\346\234\211\346\211\200\346\217\220\344\276\233\345\220\215\347\247\260\347\232\204\345\210\206\345\217\221\343\200\202\015\000\012\000\351\224\231\350\257\257\344\273\243\347\240\201\072\040\127\163\154\057\123\145\162\166\151\143\145\057\127\123\114\137\105\137\104\111\123\124\122\117\137\116\117\124\137\106\117\125\116\104' | iconv -f UTF-8 -t UTF-16LE
  printf '\r\n'
  exit 255
fi
found=
for a in "$@"; do
  if [ "$a" = "--exec" ]; then found=1; fi
done
if [ -z "$found" ]; then
  echo "--exec is missing" >&2
  exit 1
fi
# Hand the rest to sh: --distribution D --exec sh -c <launcher>.
exec sh -c "$(echo "$@" | sed 's/.*--exec //; s/^sh -c //')"
`

// TestWSLHosts lists the distributions through a fake wsl.exe, in its
// UTF-16 and in UTF-8.
func TestWSLHosts(t *testing.T) {
	fakeWSLDir(t, "")
	hosts, err := WSLHosts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"wsl:Ubuntu", "wsl:Debian"}; !slices.Equal(hosts, want) {
		t.Fatalf("hosts = %q", hosts)
	}
}

func TestWSLHostsFailed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wsl.exe"), []byte("#!/bin/sh\necho 'Windows Subsystem for Linux has no installed distributions.' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := WSLHosts(); err == nil {
		t.Fatal("a failing wsl passed")
	}
}

// TestWSLConnection runs the commands of a connection through a fake
// wsl.exe: every command a wsl.exe with --exec starts, the launcher
// protocol of the connection over its stdin.
func TestWSLConnection(t *testing.T) {
	c := fakeWSLConn(t, "")
	defer Shutdown()
	exerciseHost(t, c.Host)
}

// TestWSLConnectionGone proves the errors of a connection whose
// distribution is not there: connecting says so, and is refused, not
// lost.
func TestWSLConnectionGone(t *testing.T) {
	fakeWSLDir(t, "")
	c := For("wsl:Nope")
	defer Shutdown()
	c.Connect()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s, err := c.State()
		if s == Offline {
			if err == nil || !Refused(err) {
				t.Fatalf("the missing distribution is %v, not refused", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out connecting")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := c.Run(nil, "true"); !errors.Is(err, ErrOffline) {
		t.Fatalf("run while offline: %v", err)
	}
}

// TestWSLForward proves a WSL connection does not forward ports through
// ssh.
func TestWSLForward(t *testing.T) {
	c := fakeWSLConn(t, "")
	if f, err := c.Forward(8080); err == nil || f != nil {
		t.Fatalf("Forward = %v, %v", f, err)
	}
}

// fakeWSLConn writes the fake wsl.exe and returns a connection to it,
// which the caller Connects. fail, when not "", is what wsl fails with.
func fakeWSLConn(t *testing.T, fail string) *Conn {
	t.Helper()
	fakeWSLDir(t, fail)
	host := "wsl:Ubuntu"
	c := For(host)
	t.Cleanup(func() { c.Disconnect() })
	return c
}

// fakeWSLDir writes the fake wsl.exe on the PATH, of fail, when not "",
// and returns its folder.
func fakeWSLDir(t *testing.T, fail string) string {
	t.Helper()
	// The fake is a shell script, which only a Linux runs as wsl.exe
	// answers a Windows; wsl_live_test.go exercises the real one here.
	if runtime.GOOS != "linux" {
		t.Skip("the fake wsl.exe is a shell script for a Linux")
	}
	dir := t.TempDir()
	name := "wsl.exe"
	script := fakeWSL
	if fail != "" {
		script = "#!/bin/sh\nprintf '%s' " + quote(fail) + " >&2\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// TestWSLProbe proves the probe reads what the distribution has, script
// among it: a terminal of a WSL host needs it to make its PTY there.
func TestWSLProbe(t *testing.T) {
	fakeWSLDir(t, "")
	info, err := wslProbe(context.Background(), "wsl:Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if info.Home == "" || info.OS == "" || !info.gnu {
		t.Fatalf("info = %+v", info)
	}
	// The fake wsl runs the commands on this machine, so what it has is
	// what this machine has.
	has := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}
	for _, c := range []struct {
		name string
		got  bool
	}{
		{"git", info.Git}, {"rg", info.Rg}, {"inotifywait", info.Inotify}, {"script", info.Script},
	} {
		if c.got != has(c.name) {
			t.Errorf("%s: probe %v, this machine %v", c.name, c.got, !c.got)
		}
	}
}
