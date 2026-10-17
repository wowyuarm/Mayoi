package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is how a host's connection is.
type State int

const (
	Offline State = iota
	Connecting
	Connected
)

func (s State) String() string {
	switch s {
	case Connecting:
		return "connecting"
	case Connected:
		return "connected"
	}
	return "offline"
}

// ErrOffline is the error of what needs a connection that is not there.
var ErrOffline = errors.New("not connected")

// ErrLost is the error of a connection that ended without ssh saying why,
// as when the network goes.
var ErrLost = errors.New("the connection was lost")

// Info is what the connection found of the host as it connected.
type Info struct {
	Home string // the home folder
	OS   string // as uname -s says it: Linux, Darwin
	// gnu is set where stat takes GNU's -c rather than BSD's -f, and
	// nanos where it gives the time of a change to the nanosecond.
	gnu, nanos bool
	// The programs the host has.
	Git, Rg, Inotify, Script bool
}

// Conn is the connection to a host: an ssh master process whose socket
// every command goes through, so that only the first signs in.
type Conn struct {
	Host string

	mu    sync.Mutex
	state State
	err   error // why the connection failed or was lost
	info  Info
	dir   string // the folder of the master's socket
	// master is the ssh process that holds the connection, nil while
	// none does; ctx is canceled as it goes, which ends the commands that
	// went through it.
	master *exec.Cmd
	exited chan struct{} // closed as the master ends
	ctx    context.Context
	cancel context.CancelFunc
	// leaving is set while Disconnect ends the master, which then is not
	// lost.
	leaving bool
	users   int
	subs    map[int]func()
	nextSub int
	// forwards are the ports of the host forwarded to this machine.
	forwards []*Forward
}

var (
	connsMu sync.Mutex
	conns   = map[string]*Conn{}
)

// For returns the connection to host, which starts offline.
func For(host string) *Conn {
	connsMu.Lock()
	defer connsMu.Unlock()
	c := conns[host]
	if c == nil {
		c = &Conn{Host: host, subs: map[int]func(){}}
		conns[host] = c
	}
	return c
}

// Lookup returns the connection of a remote path, and the path on its
// host; false for a path of this machine.
func Lookup(p string) (*Conn, string, bool) {
	host, rpath, ok := Split(p)
	if !ok {
		return nil, "", false
	}
	return For(host), rpath, true
}

// Shutdown ends every connection, as the app quits.
func Shutdown() {
	connsMu.Lock()
	all := make([]*Conn, 0, len(conns))
	for _, c := range conns {
		all = append(all, c)
	}
	connsMu.Unlock()
	for _, c := range all {
		c.stopForwards()
		c.Disconnect()
		c.mu.Lock()
		if c.dir != "" {
			os.RemoveAll(c.dir)
			c.dir = ""
		}
		c.mu.Unlock()
	}
}

// State returns how the connection is, and why it failed or was lost.
func (c *Conn) State() (State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, c.err
}

// Connected reports whether commands can run on the host.
func (c *Conn) Connected() bool {
	s, _ := c.State()
	return s == Connected
}

// Info returns what the connection found of the host.
func (c *Conn) Info() Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// Subscribe has fn called, on any goroutine, whenever the state changes.
// It returns a function that stops it.
func (c *Conn) Subscribe(fn func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSub++
	id := c.nextSub
	c.subs[id] = fn
	return func() {
		c.mu.Lock()
		delete(c.subs, id)
		c.mu.Unlock()
	}
}

func (c *Conn) notify() {
	c.mu.Lock()
	fns := make([]func(), 0, len(c.subs))
	for _, fn := range c.subs {
		fns = append(fns, fn)
	}
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Acquire counts a project that uses the connection, and Release lets it
// go: the connection ends with the last.
func (c *Conn) Acquire() {
	c.mu.Lock()
	c.users++
	c.mu.Unlock()
}

func (c *Conn) Release() {
	c.mu.Lock()
	c.users--
	last := c.users <= 0
	c.mu.Unlock()
	if last {
		c.stopForwards()
		c.Disconnect()
	}
}

// socket is the path of the master's socket.
func (c *Conn) socket() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dir == "" {
		// Short, as a socket's path is at most about 100 bytes.
		dir, err := os.MkdirTemp("", "mayoi-ssh-")
		if err != nil {
			return "", err
		}
		c.dir = dir
	}
	return filepath.Join(c.dir, "s"), nil
}

// options are those of every ssh this connection runs. ssh never asks
// for a password, as there is nowhere to type one, and notices a host
// gone quiet within half a minute.
func options(sock string) []string {
	return []string{
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=10", "-o", "ServerAliveCountMax=3",
		"-o", "ControlPath=" + sock,
	}
}

// clientArgs are the arguments of an ssh that goes through the master
// and runs command, without a terminal unless tty.
func (c *Conn) clientArgs(sock string, tty bool, command string) []string {
	args := append(options(sock), "-o", "ControlMaster=no", "-o", "ClearAllForwardings=yes", "-o", "LogLevel=ERROR")
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	args = append(args, hostArgs(c.Host)...)
	return append(args, command)
}

// Connect connects to the host in the background, unless it is or is
// connecting. Subscribe tells how it goes.
func (c *Conn) Connect() {
	c.mu.Lock()
	if c.state != Offline {
		c.mu.Unlock()
		return
	}
	c.state, c.err = Connecting, nil
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx, c.cancel = ctx, cancel
	c.mu.Unlock()
	c.notify()
	if IsWSLHost(c.Host) {
		go c.wslDial(ctx)
		return
	}
	go c.dial(ctx)
}

// wslDial connects to a WSL distribution: no process to keep, as an ssh
// master is — the distribution answers or it does not — so asking it is
// the whole of connecting.
func (c *Conn) wslDial(ctx context.Context) {
	fail := func(err error) {
		c.mu.Lock()
		if c.ctx == ctx {
			c.state, c.err, c.cancel = Offline, err, nil
		}
		c.mu.Unlock()
		c.notify()
	}
	info, err := wslProbe(ctx, c.Host)
	if err != nil {
		fail(err)
		return
	}
	c.mu.Lock()
	if c.ctx != ctx {
		c.mu.Unlock()
		return
	}
	c.state, c.info = Connected, info
	c.mu.Unlock()
	c.notify()
}

func (c *Conn) dial(ctx context.Context) {
	fail := func(err error) {
		c.mu.Lock()
		if c.ctx == ctx {
			c.state, c.err, c.master = Offline, err, nil
			c.cancel()
		}
		c.mu.Unlock()
		c.notify()
	}
	sock, err := c.socket()
	if err != nil {
		fail(err)
		return
	}
	os.Remove(sock)
	args := append(options(sock), "-o", "ControlMaster=yes", "-o", "ControlPersist=no", "-N")
	cmd := exec.Command("ssh", append(args, hostArgs(c.Host)...)...)
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		fail(fmt.Errorf("could not run ssh: %w", err))
		return
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	// The master listens on its socket once it has signed in.
	deadline := time.After(45 * time.Second)
	for ready := false; !ready; {
		select {
		case <-exited:
			fail(sshError(stderr.String(), errors.New("ssh exited")))
			return
		case <-ctx.Done():
			cmd.Process.Kill()
			<-exited
			return
		case <-deadline:
			cmd.Process.Kill()
			<-exited
			fail(errors.New("timed out connecting"))
			return
		case <-time.After(40 * time.Millisecond):
			_, err := os.Stat(sock)
			ready = err == nil
		}
	}
	c.mu.Lock()
	if c.ctx != ctx {
		c.mu.Unlock()
		cmd.Process.Kill()
		<-exited
		return
	}
	c.master, c.exited = cmd, exited
	c.mu.Unlock()
	info, err := c.probe(ctx, sock)
	if err != nil {
		cmd.Process.Kill()
		<-exited
		fail(err)
		return
	}
	c.mu.Lock()
	if c.ctx != ctx {
		c.mu.Unlock()
		return
	}
	c.state, c.info = Connected, info
	c.mu.Unlock()
	c.notify()
	// Lost, unless Disconnect ends it.
	go func() {
		<-exited
		c.mu.Lock()
		if c.master == cmd {
			c.master, c.exited = nil, nil
			c.state = Offline
			if !c.leaving {
				c.err = sshError(stderr.String(), ErrLost)
			}
			c.cancel()
		}
		c.mu.Unlock()
		c.notify()
	}()
}

// probe finds what the host is and has.
func (c *Conn) probe(ctx context.Context, sock string) (Info, error) {
	const script = `printf '%s\n' "$HOME"
uname -s
if stat -c %s / >/dev/null 2>&1; then echo gnu; stat -c %.9Y / 2>/dev/null; fi
for t in git rg inotifywait; do
  if command -v $t >/dev/null 2>&1; then echo have:$t; fi
done
`
	out, err := c.run(ctx, sock, nil, script)
	if err != nil {
		return Info{}, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return Info{}, errors.New("the host did not answer as a POSIX shell does")
	}
	info := Info{Home: lines[0], OS: lines[1]}
	for i, l := range lines[2:] {
		switch {
		case l == "gnu":
			info.gnu = true
			// Without nanoseconds, an old stat gives the format back.
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

// Disconnect ends the connection, and the commands and terminals that use
// it.
func (c *Conn) Disconnect() {
	c.mu.Lock()
	master, exited, cancel := c.master, c.exited, c.cancel
	was := c.state
	c.leaving = true
	c.state, c.err, c.ctx = Offline, nil, nil
	c.mu.Unlock()
	if master != nil && master.Process != nil {
		master.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			master.Process.Kill()
			<-exited
		}
	}
	if cancel != nil {
		cancel()
	}
	c.mu.Lock()
	if c.master == master {
		c.master, c.exited = nil, nil
	}
	c.leaving = false
	c.mu.Unlock()
	if was != Offline {
		c.notify()
	}
}

// live returns the context of the connection and the socket of its ssh
// master, "" for a WSL one, or ErrOffline.
func (c *Conn) live() (string, context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != Connected {
		return "", nil, ErrOffline
	}
	if IsWSLHost(c.Host) {
		return "", c.ctx, nil
	}
	if c.dir == "" {
		return "", nil, ErrOffline
	}
	return filepath.Join(c.dir, "s"), c.ctx, nil
}

// launcher is the command ssh has the host's shell run: sh reads the
// length of the script, then the script, from what ssh sends it, and
// leaves the rest to the script. Nothing of the script goes through the
// login shell, which may not be a POSIX one.
const launcher = `sh -c 'IFS= read -r n; eval "$(dd bs=1 count=$n 2>/dev/null)"'`

// preamble starts every script: the folders programs are installed in,
// which a shell that does not log in may lack.
const preamble = `PATH="$PATH:$HOME/.local/bin:$HOME/bin:/usr/local/bin:/opt/homebrew/bin"; export PATH
LC_ALL=C; export LC_ALL
`

// Command returns the command that runs script with sh on the host, its
// arguments args, and stdin after it, nil for none. The command ends as
// the connection does.
func (c *Conn) Command(stdin io.Reader, script string, args ...string) (*exec.Cmd, error) {
	sock, ctx, err := c.live()
	if err != nil {
		return nil, err
	}
	return c.command(ctx, sock, stdin, script, args...), nil
}

func (c *Conn) command(ctx context.Context, sock string, stdin io.Reader, script string, args ...string) *exec.Cmd {
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
	head := strings.NewReader(strconv.Itoa(len(full)) + "\n" + full)
	if sock == "" {
		// A WSL host: wsl.exe runs the launcher in the distribution.
		cmd := wslCommand(ctx, c.Host, launcher)
		if stdin != nil {
			cmd.Stdin = io.MultiReader(head, stdin)
		} else {
			cmd.Stdin = head
		}
		return cmd
	}
	cmd := exec.CommandContext(ctx, "ssh", c.clientArgs(sock, false, launcher)...)
	if stdin != nil {
		cmd.Stdin = io.MultiReader(head, stdin)
	} else {
		cmd.Stdin = head
	}
	return cmd
}

// Run runs script as Command does, and returns what it wrote.
func (c *Conn) Run(stdin io.Reader, script string, args ...string) ([]byte, error) {
	sock, ctx, err := c.live()
	if err != nil {
		return nil, err
	}
	return c.run(ctx, sock, stdin, script, args...)
}

func (c *Conn) run(ctx context.Context, sock string, stdin io.Reader, script string, args ...string) ([]byte, error) {
	cmd := c.command(ctx, sock, stdin, script, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), c.failure(err, errb.String())
	}
	return out.Bytes(), nil
}

// failure is the error of a command that failed, by what it said: one
// that the connection's end stopped is ErrOffline.
func (c *Conn) failure(err error, stderr string) error {
	if !c.Connected() || errors.Is(err, context.Canceled) {
		return ErrOffline
	}
	return &Error{Stderr: strings.TrimSpace(stderr), Err: err}
}

// Error is a command on the host that failed, with what it said.
type Error struct {
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		// The last line, which is the error, of what ssh or the program
		// said.
		lines := strings.Split(e.Stderr, "\n")
		return lines[len(lines)-1]
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Is has errors.Is find fs.ErrNotExist in a command that failed for want
// of a file.
func (e *Error) Is(target error) bool {
	return target == fs.ErrNotExist && strings.Contains(e.Stderr, "No such file or directory")
}

// TerminalCommand returns the ssh command of a terminal in dir on the
// host, which signs in itself, in the terminal, when the connection is
// not there. A WSL host has none: its terminals run through a
// WSLTerminal of the connection instead.
func (c *Conn) TerminalCommand(dir string) []string {
	if IsWSLHost(c.Host) {
		return nil
	}
	sock, err := c.socket()
	if err != nil {
		return nil
	}
	// No BatchMode: a terminal can ask for a password.
	args := []string{"ssh", "-o", "ControlPath=" + sock, "-o", "ControlMaster=no", "-o", "LogLevel=ERROR",
		"-o", "ServerAliveInterval=10", "-o", "ServerAliveCountMax=3", "-t"}
	args = append(args, hostArgs(c.Host)...)
	// The login shell runs this: its quoting is that of most shells, and
	// sh does the rest.
	return append(args, "exec sh -c "+quote(`cd "$1" 2>/dev/null || cd; exec "${SHELL:-sh}" -l`)+" sh "+quote(dir))
}

// sshError is what to say of an ssh that failed, by what it said.
func sshError(stderr string, fallback error) error {
	var lines []string
	for l := range strings.SplitSeq(stderr, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "Warning:") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return fallback
	}
	msg := lines[len(lines)-1]
	switch {
	case strings.Contains(msg, "Host key verification failed"):
		return &refusedError{msg + " Connect once with ssh in a terminal to trust the host's key."}
	case strings.Contains(msg, "Permission denied"):
		return &refusedError{msg + " Mayoi signs in with keys and the SSH agent, not passwords."}
	}
	return errors.New(msg)
}

// refusedError is a host that would not let ssh sign in, or whose key is
// not trusted: connecting again fails the same way until something
// changes.
type refusedError struct{ msg string }

func (e *refusedError) Error() string { return e.msg }

// Refused reports whether err is a connection the host refused, which is
// not worth trying again by itself, unlike one lost or timed out.
func Refused(err error) bool {
	var r *refusedError
	return errors.As(err, &r)
}

// syncBuffer is a buffer that a process writes to while another goroutine
// reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// IsConnected reports whether the host of a remote path is connected.
func IsConnected(p string) bool {
	c, _, ok := Lookup(p)
	return ok && c.Connected()
}
