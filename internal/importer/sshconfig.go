package importer

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"finallyshell/internal/config"
)

// OpenSSH's client configuration, ~/.ssh/config (on Windows,
// %USERPROFILE%\.ssh\config), as ssh_config(5) documents it and
// OpenSSH's readconf.c reads it. The file is lines of a keyword and its
// arguments, "Key value" or "Key=value", keywords in any case, arguments
// in double or single quotes when they have spaces, "#" starting a
// comment at the start of a line or of an argument. Host lines start a
// block that applies to the names that match one of its patterns ("*"
// and "?" globs; a "!" pattern that matches keeps the block from
// applying), lines before any Host apply to every host, and ssh takes
// each setting from the first line that applies, in file order. Include
// reads other files in place, globs allowed; relative paths are in the
// folder of the file read, as ssh has them in ~/.ssh for this file.
//
// What is read:
//
//	Host            each name without "*", "?" or "!" is a host, named
//	                after it; patterns are not hosts, but their settings
//	                go to the names they match
//	HostName        the address ("%h" is the name); else the name
//	Port, User      the user is the local user's name when unset, as ssh
//	                has it
//	IdentityFile    the first: the host's key; %d, %u, %h, %r, %n and %p
//	                are expanded, others are left and noted
//	ProxyJump       the last hop, when it is one of the file's hosts: that
//	                host's the jump host; another is noted, and the host
//	                connects directly
//	ProxyCommand    noted as a proxy not imported
//	Include         at most sshIncludeDepth deep, sshIncludeFiles files
//	Match           not supported: its block is skipped, with a problem
//
// Other keywords are ignored. The file has no passwords: hosts with an
// IdentityFile log in with that key, the others with a password asked
// for at the first connection. Hosts are at the top level, with no
// notes.

// SSHConfig is the source of OpenSSH's client configuration.
const SSHConfig Kind = "ssh-config"

var sshConfigReader = &Reader{Kind: SSHConfig, Find: FindSSHConfig, Read: ReadSSHConfig}

// Reasons of the lines ReadSSHConfig does not take settings from.
const (
	// SSHMatch is a Match line, whose block is skipped.
	SSHMatch Reason = "ssh-match"
	// SSHPatternsOnly is a Host line with patterns only, as "Host *":
	// not a host, though its settings go to the hosts that match.
	SSHPatternsOnly Reason = "ssh-patterns"
	// SSHBadPort is a Port that is not a number from 1 to 65535: it is
	// ignored.
	SSHBadPort Reason = "ssh-port"
	// SSHBadLine is a line that does not parse, as a quote not closed.
	SSHBadLine Reason = "ssh-line"
	// SSHIncludeLimit is an Include past sshIncludeDepth, or
	// sshIncludeFiles: not read.
	SSHIncludeLimit Reason = "ssh-include-limit"
)

func init() {
	skipped[SSHMatch] = true
	skipped[SSHPatternsOnly] = true
}

// Notes of hosts of an SSH config.
const (
	// JumpHostNotImported is a jump host that is not imported: here a
	// ProxyJump to a host not in the file, or written user@host:port. The
	// host connects directly. (MobaXterm's reader has the same note.)
	JumpHostNotImported Note = "jump-host"
	// SSHKeyTokens is an IdentityFile with %-tokens that are not
	// expanded, left as they are.
	SSHKeyTokens Note = "ssh-key-tokens"
)

// Limits of Include: ssh's depth, and a number of files no
// configuration needs, should one include itself.
const (
	sshIncludeDepth = 16
	sshIncludeFiles = 256
)

// FindSSHConfig is ~/.ssh/config, when there is one: "" when not.
func FindSSHConfig() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	path := filepath.Join(home, ".ssh", "config")
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return path
}

// ReadSSHConfig reads the hosts of the SSH config at path, and of the
// files it includes. Lines it does not take settings from are Problems;
// Entries are by name. It fails when path cannot be read, or has no
// host.
func ReadSSHConfig(path string) (*Source, error) { return readSSHConfig(path, localUser()) }

// localUser is the name of the user running the app, as ssh logs in
// with when the config says no User: "" when it is not known.
func localUser() string {
	if u, err := user.Current(); err == nil {
		// Windows has DOMAIN\name.
		if name := u.Username[strings.LastIndexByte(u.Username, '\\')+1:]; name != "" {
			return name
		}
	}
	for _, v := range []string{"USER", "USERNAME", "LOGNAME"} {
		if name := os.Getenv(v); name != "" {
			return name
		}
	}
	return ""
}

// readSSHConfig is ReadSSHConfig, with user the local user's name.
func readSSHConfig(path, user string) (*Source, error) {
	text, err := ReadText(path)
	if err != nil {
		return nil, err
	}
	r := &sshReader{src: &Source{Kind: SSHConfig, Path: path}, base: filepath.Dir(path),
		seen: map[string]bool{}, limited: map[string]bool{}}
	r.parse(text, filepath.Base(path), nil, 0)
	if len(r.hosts) == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNoConnections)
	}
	r.entries(user)
	sortSource(&Source{Entries: r.src.Entries})
	// Problems in the order of the file, rather than sortSource's of
	// their names, where "config:10" is before "config:9".
	slices.SortStableFunc(r.src.Problems, func(a, b Problem) int {
		switch {
		case a.Reason.Failed() == b.Reason.Failed():
			return 0
		case a.Reason.Failed():
			return -1
		}
		return 1
	})
	return r.src, nil
}

type sshReader struct {
	src *Source
	// base is the folder of the file read, of the relative paths of
	// Include and of the names of the files in entries and problems.
	base  string
	files int
	lines []sshLine
	hosts []sshHost
	seen  map[string]bool // the hosts' names, in lower case
	// limited are the Include lines past the limits, as At has them.
	limited map[string]bool
}

// sshLine is a setting the reader takes: one of the keywords read.
type sshLine struct {
	key, value string
	// conds are the patterns of the Host lines the setting is under,
	// the Include's outer ones first: it applies to a name that matches
	// every one.
	conds [][]string
}

// sshHost is a name of a Host line, a host, and where it is.
type sshHost struct{ name, at string }

func (r *sshReader) problem(file string, line int, reason Reason, err error) {
	r.src.Problems = append(r.src.Problems, NewProblem(file, line, reason, err))
}

// parse reads text, of the file named name, under the Host patterns
// outer of the Include that reads it, at depth.
func (r *sshReader) parse(text, name string, outer [][]string, depth int) {
	conds, match := outer, false
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		key, args, err := sshFields(line)
		switch {
		case err != nil:
			r.problem(name, n, SSHBadLine, err)
			continue
		case key == "":
			continue
		case key == "host":
			match = false
			conds = append(slices.Clip(outer), args)
			r.host(args, name, n)
			continue
		case key == "match":
			match = true
			r.problem(name, n, SSHMatch, nil)
			continue
		case match || len(args) == 0:
			continue
		}
		switch key {
		case "include":
			r.include(args, name, n, conds, depth)
		case "port":
			if p, err := strconv.Atoi(args[0]); err != nil || p < 1 || p > 65535 {
				r.problem(name, n, SSHBadPort, fmt.Errorf("port %s", args[0]))
				continue
			}
			fallthrough
		case "hostname", "user", "identityfile", "proxyjump", "proxycommand":
			r.lines = append(r.lines, sshLine{key: key, value: args[0], conds: conds})
		}
	}
}

// host takes the names of a Host line, at line of the file name.
func (r *sshReader) host(args []string, name string, line int) {
	names := 0
	for _, a := range args {
		if strings.ContainsAny(a, "*?!") {
			continue
		}
		names++
		if !r.seen[strings.ToLower(a)] {
			r.seen[strings.ToLower(a)] = true
			r.hosts = append(r.hosts, sshHost{name: a, at: At(name, line)})
		}
	}
	switch {
	case len(args) == 0:
		r.problem(name, line, SSHBadLine, errors.New("host without a name"))
	case names == 0:
		r.problem(name, line, SSHPatternsOnly, nil)
	}
}

// include reads the files of an Include line's args, at line of the file
// name, under conds, from a file at depth.
func (r *sshReader) include(args []string, name string, line int, conds [][]string, depth int) {
	for _, arg := range args {
		path := arg
		if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				r.problem(name, line, SSHBadLine, err)
				continue
			}
			path = filepath.Join(home, path[1:])
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(r.base, path)
		}
		files, err := filepath.Glob(path)
		if err != nil {
			r.problem(name, line, SSHBadLine, fmt.Errorf("include %s: %w", arg, err))
			continue
		}
		for _, f := range files {
			if depth+1 > sshIncludeDepth || r.files >= sshIncludeFiles {
				if at := At(name, line); !r.limited[at] { // once for the line
					r.limited[at] = true
					r.problem(name, line, SSHIncludeLimit, nil)
				}
				return
			}
			r.files++
			shown := r.shown(f)
			text, err := ReadText(f)
			if err != nil {
				r.src.Problems = append(r.src.Problems, ReadProblem(shown, err))
				continue
			}
			r.parse(text, shown, conds, depth+1)
		}
	}
}

// shown is the name of the file at path in entries and problems:
// relative to the folder of the file read, when it is in it.
func (r *sshReader) shown(path string) string {
	if rel, err := filepath.Rel(r.base, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return path
}

// entries makes each host an entry, with the settings that apply to it,
// user being the local user's name.
func (r *sshReader) entries(user string) {
	byName := map[string]*Entry{}
	jumps := map[*Entry]string{}
	for _, sh := range r.hosts {
		// The first value of each keyword that applies; ProxyJump and
		// ProxyCommand are one, whichever is first.
		v := map[string]string{}
		proxy := ""
		for _, l := range r.lines {
			key := l.key
			if key == "proxyjump" || key == "proxycommand" {
				key = "proxy"
			}
			if _, ok := v[key]; ok || !sshMatchAll(sh.name, l.conds) {
				continue
			}
			v[key] = l.value
			if key == "proxy" {
				proxy = l.key
			}
		}
		addr, _ := sshExpand(v["hostname"], map[byte]string{'h': sh.name})
		if addr == "" {
			addr = sh.name
		}
		login := v["user"]
		if login == "" {
			login = user
		}
		port, _ := strconv.Atoi(v["port"])
		h := &config.Host{Name: sh.name, Addr: addr, User: login, Auth: config.AuthPassword}
		if port != 22 {
			h.Port = port
		}
		e := &Entry{File: sh.at, Host: h}
		if id := v["identityfile"]; id != "" && !strings.EqualFold(id, "none") {
			tokens := map[byte]string{'d': "~", 'u': user, 'h': addr, 'r': login, 'n': sh.name, 'p': strconv.Itoa(h.PortOr22())}
			key, ok := sshExpand(id, tokens)
			e.KeyFile, h.Auth = key, config.AuthKey
			if !ok {
				e.Note = SSHKeyTokens
			}
		}
		if p := v["proxy"]; p != "" && !strings.EqualFold(p, "none") {
			if proxy == "proxyjump" {
				jumps[e] = p
			} else if e.Note == "" {
				e.Note = ProxyNotImported
			}
		}
		byName[strings.ToLower(sh.name)] = e
		r.src.Entries = append(r.src.Entries, e)
	}
	// A chain of jump hosts, "a,b", connects through its last.
	for e, jump := range jumps {
		hops := strings.Split(jump, ",")
		via := byName[strings.ToLower(strings.TrimSpace(hops[len(hops)-1]))]
		switch {
		case via != nil && via != e:
			e.Jump = via
		case e.Note == "":
			e.Note = JumpHostNotImported
		}
	}
}

// sshMatchAll tells whether name matches every list of Host patterns of
// conds.
func sshMatchAll(name string, conds [][]string) bool {
	for _, patterns := range conds {
		if !sshMatchHost(name, patterns) {
			return false
		}
	}
	return true
}

// sshMatchHost tells whether name matches the patterns of a Host line:
// one of them, and none of its "!" ones. Case does not matter.
func sshMatchHost(name string, patterns []string) bool {
	name = strings.ToLower(name)
	matched := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		if sshGlob(strings.ToLower(strings.TrimPrefix(p, "!")), name) {
			if neg {
				return false
			}
			matched = true
		}
	}
	return matched
}

// sshGlob tells whether s matches pattern, where "*" is any text and "?"
// any one character.
func sshGlob(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	// star is the last "*" met, and from where in t it matches to.
	i, j, star, from := 0, 0, -1, 0
	for j < len(t) {
		switch {
		case i < len(p) && (p[i] == '?' || p[i] == t[j]):
			i++
			j++
		case i < len(p) && p[i] == '*':
			star, from = i, j
			i++
		case star >= 0: // the "*" takes one more character
			from++
			i, j = star+1, from
		default:
			return false
		}
	}
	for i < len(p) && p[i] == '*' {
		i++
	}
	return i == len(p)
}

// sshExpand expands the %-tokens of s with tokens, "%%" being "%": ok is
// false when a token is not one of tokens, and left as it is.
func sshExpand(s string, tokens map[byte]string) (string, bool) {
	if !strings.Contains(s, "%") {
		return s, true
	}
	var b strings.Builder
	ok := true
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		if i+1 < len(s) {
			if v, found := tokens[s[i+1]]; found && v != "" {
				b.WriteString(v)
				i++
				continue
			}
		}
		b.WriteByte('%')
		ok = false
	}
	return b.String(), ok
}

// sshFields splits a line of an SSH config into its keyword, in lower
// case, and its arguments, as ssh does: "Key value" or "Key=value",
// arguments in double or single quotes, a backslash escaping a quote, a
// backslash or a space, and "#" at the start of an argument starting a
// comment. A blank line, or a comment, has no keyword.
func sshFields(line string) (key string, args []string, err error) {
	s := strings.TrimSpace(line)
	if s == "" || s[0] == '#' {
		return "", nil, nil
	}
	end := strings.IndexAny(s, " \t=")
	if end < 0 {
		return strings.ToLower(s), nil, nil
	}
	key, s = strings.ToLower(s[:end]), strings.TrimLeft(s[end:], " \t")
	if strings.HasPrefix(s, "=") {
		s = strings.TrimLeft(s[1:], " \t")
	}
	var cur strings.Builder
	in := false // an argument is started
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			switch {
			case c == '\\' && i+1 < len(s) && (s[i+1] == quote || s[i+1] == '\\'):
				i++
				cur.WriteByte(s[i])
			case c == quote:
				quote = 0
			default:
				cur.WriteByte(c)
			}
		case c == ' ' || c == '\t':
			if in {
				args = append(args, cur.String())
				cur.Reset()
				in = false
			}
		case c == '#' && !in:
			return key, args, nil
		case c == '"' || c == '\'':
			quote, in = c, true
		case c == '\\' && i+1 < len(s) && strings.IndexByte(`\"' `, s[i+1]) >= 0:
			i++
			cur.WriteByte(s[i])
			in = true
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if quote != 0 {
		return "", nil, errors.New("a quote is not closed")
	}
	if in {
		args = append(args, cur.String())
	}
	return key, args, nil
}
