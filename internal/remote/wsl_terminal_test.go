package remote

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestWSLTerminal runs a terminal of a WSL host through the fake wsl.exe:
// a login shell under this machine's script, its PTY over the wsl.exe's
// pipes. It proves the input, the output, the exit, and that Close ends
// the shell and wsl.exe both, which the pipes' closing alone would not.
func TestWSLTerminal(t *testing.T) {
	fakeWSLDir(t, "")
	host := "wsl:Ubuntu"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewWSLTerminal(ctx, host, t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// The shell answers, and its PTY echoes.
	if _, err := c.Write([]byte("echo wsl-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, c, time.Second*10, "wsl-42")
	if !strings.Contains(got, "wsl-42") {
		t.Fatalf("the shell's answer = %q", got)
	}
	// The shell tells the size it was started with, of its PTY.
	if _, err := c.Write([]byte("stty size\n")); err != nil {
		t.Fatal(err)
	}
	got = readUntil(t, c, time.Second*10, "24 80")
	if !strings.Contains(got, "24 80") {
		t.Fatalf("stty size = %q", got)
	}
	// Resize tells the PTY, once its file names it.
	if err := c.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	got = waitSize(t, c, 100, 30, 15*time.Second)
	if !strings.Contains(got, "30 100") {
		t.Fatalf("stty size after Resize = %q", got)
	}
	// A shell that exits ends the terminal.
	if _, err := c.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !c.Unplanned() {
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not end as the shell exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWSLTerminalClose proves Close ends the shell and its wsl.exe, a
// shell that did not exit.
func TestWSLTerminalClose(t *testing.T) {
	fakeWSLDir(t, "")
	host := "wsl:Ubuntu"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewWSLTerminal(ctx, host, t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("echo up\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, time.Second*10, "up")
	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("Close took %s, which a live shell's end should not", took)
	}
	if c.Unplanned() {
		t.Fatal("Close counted as an unplanned end")
	}
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("wsl.exe did not end with Close")
	}
}

// readUntil reads the terminal until want shows or the deadline passes,
// and returns what it read. A read that outlives the deadline does not
// hang the caller: the pipe is given a deadline of its own.
func readUntil(t *testing.T, c *WSLTerminal, d time.Duration, want string) string {
	t.Helper()
	var b strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(d)
	for !strings.Contains(b.String(), want) {
		if time.Now().After(deadline) {
			return b.String()
		}
		if f, ok := c.out.(interface{ SetReadDeadline(time.Time) error }); ok {
			f.SetReadDeadline(deadline)
		}
		n, err := c.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			return b.String()
		}
	}
	return b.String()
}

// waitSize writes stty size until the shell answers with the size given,
// which the terminal's resize tells it in the background: what a resize
// asks for is not what the shell answers before it lands.
func waitSize(t *testing.T, c *WSLTerminal, cols, rows int, d time.Duration) string {
	t.Helper()
	want := strconv.Itoa(rows) + " " + strconv.Itoa(cols)
	deadline := time.Now().Add(d)
	read := ""
	for time.Now().Before(deadline) {
		if _, err := c.Write([]byte("stty size\n")); err != nil {
			t.Fatal(err)
		}
		read += readUntil(t, c, 2*time.Second, want)
		if strings.Contains(read, want) {
			return read
		}
	}
	return read
}
