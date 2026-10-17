package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A WSL terminal is a login shell of a distribution, in a Linux PTY that
// script makes there, over the plain pipes of a wsl.exe: what the
// terminal reads from it shows, what is typed is written to it, as a
// terminal of an ssh host is over its connection. The shell writes the
// path of its PTY to a file of the distribution as it starts, which the
// terminal reads through a wsl.exe of its own to tell the PTY its size,
// the one it started with and every one after.

// WSLTerminal is the terminal connection of a WSL host: the wsl.exe whose
// stdin and stdout are the terminal's.
type WSLTerminal struct {
	host string
	cmd  *exec.Cmd
	// in is what the shell reads, out what it writes. Closing in ends
	// script and its shell, as a hung-up terminal does.
	in  io.WriteCloser
	out io.ReadCloser
	// tty is the file of the distribution that names the shell's PTY;
	// cols and rows the size last told, told while sizing says a goroutine
	// waits to tell it, which it does until the shell's file is there.
	tty    string
	cols   int
	rows   int
	sizing bool
	// closed is set by Close, unplanned by an end that is not Close's.
	closed, unplanned bool
	done              chan struct{}
	mu                sync.Mutex
}

// wslTermNext tells the files of the terminals of this run apart.
var wslTermNext atomic.Int64

// NewWSLTerminal starts a login shell of the host's distribution in dir,
// of cols by rows, in a Linux PTY that script makes there. It fails when
// the distribution cannot run script, which the message says.
func NewWSLTerminal(ctx context.Context, host, dir string, cols, rows int) (*WSLTerminal, error) {
	tty := fmt.Sprintf("/tmp/.mayoi-term-%d-%d", os.Getpid(), wslTermNext.Add(1))
	// What the shell under script runs: it writes the path of its PTY,
	// takes the size, and becomes the login shell. It goes through sh, as
	// script has the login shell run its command, whose words may not be
	// POSIX ones.
	inner := `tty >` + quote(tty) + ` 2>/dev/null
stty rows ` + strconv.Itoa(rows) + ` cols ` + strconv.Itoa(cols) + ` 2>/dev/null
export TERM="${TERM:-xterm-256color}" LANG="${LANG:-C.UTF-8}"
exec "${SHELL:-sh}" -l`
	// The driver the distribution's sh runs, once the launcher has read
	// it: the folder, then the shell under script, whose PTY the
	// terminal sees through the wsl.exe's pipes.
	driver := `cd -- "$1" 2>/dev/null || cd
command -v script >/dev/null 2>&1 || { echo ` + quote("script is missing: install util-linux in the distribution") + ` >&2; exit 127; }
exec script -qef -c ` + quote(`exec sh -c `+quote(inner)) + ` /dev/null`
	full := "set -- " + quote(dir) + "\n" + driver
	cmd := exec.CommandContext(ctx, "wsl.exe", wslArgs(host, launcher)...)
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// The shell's output and its errors are the one stream the terminal
	// reads, as a terminal's are.
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, outW
	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		return nil, wslError(err, "", "")
	}
	if _, err := io.Copy(stdin, strings.NewReader(strconv.Itoa(len(full))+"\n"+full)); err != nil {
		stdin.Close()
		outR.Close()
		outW.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}
	// The pipe's write end is the child's now; closing this one lets the
	// reads end once the child's ends.
	outW.Close()
	c := &WSLTerminal{host: host, cmd: cmd, in: stdin, out: outR,
		tty: tty, cols: cols, rows: rows, done: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(c.done)
	}()
	go func() {
		<-c.done
		c.mu.Lock()
		if !c.closed {
			c.unplanned = true
		}
		c.mu.Unlock()
	}()
	return c, nil
}

// Read reads what the shell wrote.
func (c *WSLTerminal) Read(p []byte) (int, error) {
	return c.out.Read(p)
}

// Write writes what the terminal's user typed to the shell.
func (c *WSLTerminal) Write(p []byte) (int, error) {
	return c.in.Write(p)
}

// Resize tells the shell's PTY the size of the terminal. The shell takes
// a moment to write the file that names its PTY, so the first sizes wait
// for it in the background, and are told once it is there.
func (c *WSLTerminal) Resize(cols, rows int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrOffline
	}
	c.cols, c.rows = cols, rows
	if c.sizing {
		return nil
	}
	c.sizing = true
	go c.sizeWhenReady()
	return nil
}

// sizeWhenReady tells the shell's PTY the size last told, as soon as the
// shell's file names it, and every time it changes while the telling
// catches up. It gives up once the terminal ends, or the shell never
// wrote its file.
func (c *WSLTerminal) sizeWhenReady() {
	defer func() {
		c.mu.Lock()
		c.sizing = false
		c.mu.Unlock()
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		c.mu.Lock()
		cols, rows, closed := c.cols, c.rows, c.closed
		c.mu.Unlock()
		if closed || time.Now().After(deadline) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		const script = `p=$(cat "$1" 2>/dev/null) || exit 1
[ -c "$p" ] || exit 1
stty rows "$2" cols "$3" <"$p"`
		err := wslRun(ctx, c.host, script, c.tty, strconv.Itoa(rows), strconv.Itoa(cols))
		cancel()
		if err == nil {
			c.mu.Lock()
			if c.cols == cols && c.rows == rows {
				c.sizing = false
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Close ends the terminal: the shell's input first, so that script ends
// it as a hung-up terminal does, then wsl.exe itself, which the pipes'
// closing alone does not end, then its output and the file of the
// distribution that named the shell's PTY.
func (c *WSLTerminal) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.in.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
		<-c.done
	}
	c.out.Close()
	// The file the shell wrote of its PTY, which nothing needs now; it is
	// of the distribution's /tmp, which empties on a boot anyway.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		wslRun(ctx, c.host, `rm -f "$1"`, c.tty)
	}()
	return nil
}

// Unplanned reports whether the terminal ended without Close, as when the
// distribution goes: a terminal of the app closes as it is told to, one
// of a host that went does not.
func (c *WSLTerminal) Unplanned() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unplanned
}

// wslRun runs script in the host's distribution with the launcher, its
// arguments args, waiting for it to end.
func wslRun(ctx context.Context, host, script string, args ...string) error {
	var b strings.Builder
	b.WriteString("set --")
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(quote(a))
	}
	b.WriteByte('\n')
	b.WriteString(preamble)
	b.WriteString(script)
	full := b.String()
	cmd := wslCommand(ctx, host, launcher)
	cmd.Stdin = strings.NewReader(strconv.Itoa(len(full)) + "\n" + full)
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return wslError(err, errb.String(), "")
	}
	return nil
}
