package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"sync"
)

// A port of the host forwards to one of this machine, as ssh -L does, so
// that a server running there, as a web app in development, opens in a
// browser here. This machine listens itself, and each connection to it
// goes to the host by an ssh -W through the master: a forward outlives
// the master, and works again as soon as the host connects again.

// Forward is a port of the host that a port of this machine forwards to.
type Forward struct {
	// Port is the port on the host, and Local the one of this machine.
	Port, Local int

	c  *Conn
	ln net.Listener
	// open are the connections it carries, which Close ends.
	mu   sync.Mutex
	open map[net.Conn]bool
}

// URL is the address of the forward in a browser.
func (f *Forward) URL() string { return fmt.Sprintf("http://localhost:%d", f.Local) }

// Forward forwards port of the host, unless it is already, to the same
// port of this machine when it is free, or else to another.
func (c *Conn) Forward(port int) (*Forward, error) {
	if IsWSLHost(c.Host) {
		return nil, errors.New("WSL ports are on this machine's localhost already")
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("a port is a number from 1 to 65535")
	}
	c.mu.Lock()
	for _, f := range c.forwards {
		if f.Port == port {
			c.mu.Unlock()
			return f, nil
		}
	}
	c.mu.Unlock()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return nil, err
		}
	}
	f := &Forward{Port: port, Local: ln.Addr().(*net.TCPAddr).Port, c: c, ln: ln, open: map[net.Conn]bool{}}
	c.mu.Lock()
	c.forwards = append(c.forwards, f)
	c.mu.Unlock()
	go f.serve()
	c.notify()
	return f, nil
}

// Forwards returns the ports of the host forwarded, in the order they
// were.
func (c *Conn) Forwards() []*Forward {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.forwards)
}

// LocalPort is the port of this machine that a port of the host
// forwards to, 0 for one not forwarded.
func (c *Conn) LocalPort(port int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.forwards {
		if f.Port == port {
			return f.Local
		}
	}
	return 0
}

// Close stops forwarding the port.
func (f *Forward) Close() {
	c := f.c
	c.mu.Lock()
	i := slices.Index(c.forwards, f)
	if i >= 0 {
		c.forwards = slices.Delete(c.forwards, i, i+1)
	}
	c.mu.Unlock()
	f.ln.Close()
	f.mu.Lock()
	for conn := range f.open {
		conn.Close()
	}
	f.mu.Unlock()
	if i >= 0 {
		c.notify()
	}
}

// stopForwards stops every forward, as the last project of the host
// lets go of it.
func (c *Conn) stopForwards() {
	for _, f := range c.Forwards() {
		f.Close()
	}
}

func (f *Forward) serve() {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.open[conn] = true
		f.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.pipe(conn)
			conn.Close()
			f.mu.Lock()
			delete(f.open, conn)
			f.mu.Unlock()
		}()
	}
}

// pipe carries a connection to the port on the host, through the master.
func (f *Forward) pipe(conn net.Conn) {
	c := f.c
	sock, ctx, err := c.live()
	if err != nil {
		return
	}
	args := append(options(sock), "-o", "ControlMaster=no", "-o", "LogLevel=ERROR",
		"-W", net.JoinHostPort("localhost", strconv.Itoa(f.Port)))
	cmd := exec.CommandContext(ctx, "ssh", append(args, hostArgs(c.Host)...)...)
	cmd.Stdout = conn
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	go func() {
		io.Copy(stdin, conn)
		stdin.Close()
	}()
	cmd.Wait()
}
