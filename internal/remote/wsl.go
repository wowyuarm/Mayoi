package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
)

// A WSL host is a distribution of the Windows Subsystem for Linux, which
// wsl.exe runs commands in: each command starts a wsl.exe that enters the
// distribution and runs the launcher through its sh, from which the
// connection's scripts work as they do over SSH. There is no program to
// keep running, as an ssh master is: the host is connected while the
// distribution answers.

// wslPrefix is how a WSL host spells itself inside a host: wsl:Ubuntu.
const wslPrefix = "wsl:"

// ErrNoWSL is that WSL itself is not there, which every distribution
// fails for: not worth trying any of them again.
var ErrNoWSL = errors.New("WSL is not installed")

// IsWSLHost reports whether host names a WSL distribution. A WSL host
// has no port: [wsl:Ubuntu] would be an ssh spelling of one.
func IsWSLHost(host string) bool {
	host, _, _ = strings.Cut(host, "]")
	return strings.HasPrefix(host, wslPrefix) && len(host) > len(wslPrefix)
}

// WSLDistribution returns the distribution the host names, "" for a host
// that is not one.
func WSLDistribution(host string) string {
	if !IsWSLHost(host) {
		return ""
	}
	return host[len(wslPrefix):]
}

// DisplayHost is how a host shows in the editor's words: a WSL
// distribution as "WSL: Ubuntu", an ssh host as it is spelled.
func DisplayHost(host string) string {
	if d := WSLDistribution(host); d != "" {
		return "WSL: " + d
	}
	return host
}

// KindHost names a host with the kind of machine it is, for a title or a
// label beside a folder: "SSH: box", "WSL: Ubuntu".
func KindHost(host string) string {
	if d := WSLDistribution(host); d != "" {
		return "WSL: " + d
	}
	return "SSH: " + host
}

// SSHAllowed reports whether an ssh host can be connected to on this
// machine: Windows reaches its WSL distributions only, as it has no ssh
// of Mayoi's.
func SSHAllowed() bool { return !wslOnly }

// wslOnly is whether this machine reaches hosts only through WSL.
var wslOnly = runtime.GOOS == "windows"

// WSLHosts returns the WSL distributions there are, in wsl's order, as
// hosts to connect to.
func WSLHosts() ([]string, error) {
	out, err := wslOutput(exec.Command("wsl.exe", "--list", "--quiet"))
	if err != nil {
		return nil, err
	}
	var hosts []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimRight(line, "\r"); line == "" || strings.Contains(line, "\x00") {
			continue
		}
		d := strings.TrimSpace(line)
		if d == "" || seen[d] {
			continue
		}
		if strings.ContainsAny(d, "/\\\x00\r\n") {
			continue
		}
		seen[d] = true
		hosts = append(hosts, wslPrefix+d)
	}
	return hosts, nil
}

// wslOutput runs a wsl.exe command and returns what it wrote, as text:
// wsl writes UTF-16LE, or UTF-8 when WSL_UTF8 is set, and its own errors
// to its stdout, in the words of the Windows display language.
func wslOutput(cmd *exec.Cmd) (string, error) {
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	hideWindow(cmd)
	err := cmd.Run()
	text := decodeWSL([]byte(out.String()))
	if err != nil {
		return text, wslError(err, decodeWSL([]byte(errb.String())), text)
	}
	return text, nil
}

// decodeWSL reads what wsl.exe wrote. Its own words come as UTF-16LE,
// usually with a BOM and sometimes without; what a program in the
// distribution wrote, as when WSL_UTF8 is set, comes as UTF-8, which
// never holds a NUL that text of wsl.exe's own would not.
func decodeWSL(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	} else if !bytes.Contains(b, []byte{0}) {
		return string(b)
	}
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}

// wslError is the error of a wsl.exe that failed, by what it said, in
// the words of its message: its own go to stdout, in the Windows display
// language, and name their code as Wsl/Service/<CODE>.
func wslError(err error, stderr, stdout string) error {
	if errors.Is(err, exec.ErrNotFound) {
		return ErrNoWSL
	}
	msg := lastLine(stderr)
	if msg == "" {
		// wsl.exe says its own failures on stdout, where a program of the
		// distribution would say its output.
		msg = lastLine(stdout)
	}
	switch {
	case msg == "":
		return err
	case strings.Contains(msg, "Wsl/"):
		// A code of WSL's own, whatever the language it is told in: the
		// distribution could not be reached, and is not one to try again
		// by itself, as a lost connection is.
		return &refusedError{msg}
	}
	return &Error{Stderr: msg, Err: err}
}

// lastLine is the last line of s that is not blank: what a failure of
// wsl's own says last, which is its message.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return strings.TrimRight(l, ".")
		}
	}
	return ""
}

// wslArgs are the arguments of a wsl.exe that runs the launcher of the
// connection in the host's distribution: --exec hands the command to the
// distribution's own exec, not to a shell that would take the launcher's
// quoting apart.
func wslArgs(host, command string) []string {
	return []string{"--distribution", WSLDistribution(host), "--exec", "sh", "-c", command}
}

// wslCommand is the wsl.exe of a command of the connection.
func wslCommand(ctx context.Context, host, launcher string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "wsl.exe", wslArgs(host, launcher)...)
	hideWindow(cmd)
	return cmd
}

// wslProbe finds what the distribution is and has, one wsl.exe for it
// all.
func wslProbe(ctx context.Context, host string) (Info, error) {
	const script = `printf '%s\n' "$HOME"
uname -s
if stat -c %s / >/dev/null 2>&1; then echo gnu; stat -c %.9Y / 2>/dev/null; fi
for t in git rg inotifywait script; do
  if command -v $t >/dev/null 2>&1; then echo have:$t; fi
done
`
	full := strconv.Itoa(len(script)) + "\n" + script
	cmd := wslCommand(ctx, host, launcher)
	cmd.Stdin = strings.NewReader(full)
	out, err := wslOutput(cmd)
	if err != nil {
		return Info{}, err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		return Info{}, fmt.Errorf("%s did not answer as a POSIX shell does", host)
	}
	info := Info{Home: lines[0], OS: lines[1]}
	for i, l := range lines[2:] {
		switch {
		case l == "gnu":
			info.gnu = true
			if i+3 < len(lines) {
				t := lines[i+3]
				info.nanos = strings.Contains(t, ".") && !strings.Contains(t, "Y")
			}
		case l == "have:git":
			info.Git = true
		case l == "have:rg":
			info.Rg = true
		case l == "have:inotifywait":
			info.Inotify = true
		case l == "have:script":
			info.Script = true
		}
	}
	return info, nil
}
