package remote

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ConfigHosts returns the hosts ~/.ssh/config names, in its order, with
// those of the files it includes: each name of a Host line without
// wildcards or negation.
func ConfigHosts() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".ssh")
	var hosts []string
	seen := map[string]bool{}
	readConfig(filepath.Join(dir, "config"), dir, home, 0, func(name string) {
		if !seen[name] {
			seen[name] = true
			hosts = append(hosts, name)
		}
	})
	return hosts
}

// readConfig reads a file of ssh's config, and those it includes, depth
// deep in includes, and gives each name of a host to add.
func readConfig(path, sshDir, home string, depth int, add func(string)) {
	if depth > 8 {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, args := configLine(sc.Text())
		switch key {
		case "host":
			for _, name := range args {
				if !strings.ContainsAny(name, "*?!") && CheckHost(name) == nil {
					add(name)
				}
			}
		case "include":
			for _, pat := range args {
				switch {
				case strings.HasPrefix(pat, "~/"):
					pat = filepath.Join(home, pat[2:])
				case !filepath.IsAbs(pat):
					// Relative to ~/.ssh, as ssh reads them in a user's
					// config.
					pat = filepath.Join(sshDir, pat)
				}
				matches, _ := filepath.Glob(pat)
				slices.Sort(matches)
				for _, m := range matches {
					readConfig(m, sshDir, home, depth+1, add)
				}
			}
		}
	}
}

// configLine parts a line of ssh's config into its keyword, in lower
// case, and its arguments: "" for a comment or a blank line. A keyword
// may end with "=", and arguments may be quoted.
func configLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), nil
	}
	key := strings.ToLower(line[:i])
	rest := strings.TrimLeft(line[i:], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	var args []string
	for rest != "" {
		var arg string
		if rest[0] == '"' {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				arg, rest = rest[1:], ""
			} else {
				arg, rest = rest[1:end+1], rest[end+2:]
			}
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				arg, rest = rest, ""
			} else {
				arg, rest = rest[:end], rest[end:]
			}
		}
		if strings.HasPrefix(arg, "#") {
			break
		}
		args = append(args, arg)
		rest = strings.TrimLeft(rest, " \t")
	}
	return key, args
}

var (
	resolvedMu sync.Mutex
	// resolved are the answers of Resolve, until ~/.ssh/config changes,
	// as configMod says it last did.
	resolved  = map[string]string{}
	configMod time.Time
)

// configStamp is when ~/.ssh/config last changed.
func configStamp() time.Time {
	home, err := os.UserHomeDir()
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Resolved returns what Resolve said of host, if it did since ssh's
// config last changed, without running ssh.
func Resolved(host string) (string, bool) {
	mod := configStamp()
	resolvedMu.Lock()
	defer resolvedMu.Unlock()
	if !mod.Equal(configMod) {
		clear(resolved)
		configMod = mod
	}
	s, ok := resolved[host]
	return s, ok
}

// Resolve returns who and where ssh signs in as for host, by its config:
// "user@hostname", or "" when ssh cannot say.
func Resolve(host string) string {
	if s, ok := Resolved(host); ok {
		return s
	}
	s := resolve(host)
	resolvedMu.Lock()
	resolved[host] = s
	resolvedMu.Unlock()
	return s
}

func resolve(host string) string {
	if IsWSLHost(host) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", append([]string{"-G"}, hostArgs(host)...)...).Output()
	if err != nil {
		return ""
	}
	var user, name, port string
	for line := range strings.SplitSeq(string(out), "\n") {
		k, v, _ := strings.Cut(line, " ")
		switch k {
		case "user":
			user = v
		case "hostname":
			name = v
		case "port":
			port = v
		}
	}
	if name == "" {
		return ""
	}
	s := name
	if user != "" {
		s = user + "@" + s
	}
	if port != "" && port != "22" {
		s += ":" + port
	}
	return s
}
