package app

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/monitor"
	"finallyshell/internal/remotefs"
	"finallyshell/internal/sshx"
	"finallyshell/internal/termenc"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sessionStatus int

const (
	statusConnecting sessionStatus = iota
	statusConnected
	statusFailed
	statusClosed
)

var nextSessionID atomic.Int64

// Session is one open tab: a connection to a host, its shell, the
// server's stats and its files. A new tab has no Host yet: it shows the
// start page (see start.go). A local terminal has none either: it runs a
// program on this computer (local.go).
type Session struct {
	ID     int64
	Host   *config.Host // nil for a new tab and a local terminal
	local  bool         // a local terminal (local.go)
	status sessionStatus
	err    error // why it failed or closed; nil when the user disconnected
	ip     string

	client *ssh.Client
	panes  paneTree // its terminals (panes.go)
	stats  monitor.Stats
	sftp   *sftp.Client
	files  fileState

	tunnels tunnelState // see tunnels.go
	procs   procState   // the Processes tab, see processes.go
	net     netState    // the Network tab, see network.go

	hostKeyQ *sshx.HostKeyQuestion
	cancel   context.CancelFunc
	// ctx is the connection's, which cancel ends: the panes' followers
	// run under it.
	ctx context.Context

	start startState // the start page, while it is a new tab
	bar   cmdBar     // the command bar under the terminal (commandbar.go)

	// name is the tab's name for this run, "" for the host's title: never
	// saved (rename.go).
	name string

	// A panel row's session, whose bottom panel it shows, and the tab of
	// the panel it has chosen (paneltab.go): panelOf is nil on every
	// other row.
	panelOf  *Session
	panelTab int
}

// errConnectionLost is a session whose shell ended.
var errConnectionLost = errors.New("connection lost")

// stepError is a step after logging in that failed.
type stepError struct {
	step string // "shell", "terminal"
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }

func (e *stepError) Unwrap() error { return e.err }

func newSession(h *config.Host) *Session {
	s := &Session{ID: nextSessionID.Add(1), Host: h, start: startState{row: -1}}
	s.files.init()
	return s
}

func (s *Session) label() string {
	if s.isNewTab() {
		return locales.NewTab()
	}
	if s.name != "" {
		return s.name
	}
	if s.isLocal() {
		return locales.LocalTerminal()
	}
	return s.Host.Title()
}

// connect starts connecting s on a goroutine, with the password, or the
// passphrase of its key, which the goroutine waits for from the
// credential store when the app has not read it yet. A password the
// server refuses opens the prompt (loginFailed).
func (a *App) connect(s *Session, secret string) {
	if s.cancel != nil {
		s.cancel()
	}
	s.status, s.err, s.hostKeyQ = statusConnecting, nil, nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.ctx = cancel, ctx
	// Through the host's proxy or jump hosts, if any (see proxy.go):
	// closing the client closes theirs.
	opts, err := a.dialOptions(s.Host, secret, func(q *sshx.HostKeyQuestion) { a.post(func() { s.hostKeyQ = q }) })
	if err != nil {
		s.status, s.err = statusFailed, err
		return
	}
	// On the host's connection and each jump host's, as long as each is
	// open. One that dies is closed, which ends the shells, so the
	// session shows "Connection lost" (paneExited).
	opts.KeepAlive = a.keepAlive()
	h := *s.Host
	spec := a.shellSpec(&h) // here, on the UI thread, which owns the settings
	passphrase := a.storedPassphrase(s.Host)
	// A shell in each pane: the one of a session connecting for the first
	// time, every pane of the layout it had when it connects again.
	if s.panes.root == nil {
		s.panes.reset(newPane())
	}
	panes := s.panes.panes()
	fail := func(err error) {
		a.post(func() {
			s.status, s.err = statusFailed, err
			if ctx.Err() == nil {
				a.loginFailed(s, err)
			}
		})
	}
	go func() {
		if passphrase != nil {
			opts.Passphrase = passphrase(ctx)
		}
		client, err := sshx.Dial(&h, opts)
		if err != nil {
			fail(err)
			return
		}
		a.post(func() { a.loggedIn(s.Host) })
		if ctx.Err() != nil { // closed while connecting
			client.Close()
			return
		}
		// One after the other, in the order of the tree, which the
		// followers tell the server's shells apart by. The first pane's
		// shell is the session's: without it, connecting failed. A later
		// one the server refuses, as past its MaxSessions, leaves its pane
		// out of the layout.
		terms := make([]*terminal.Terminal, len(panes))
		var refused error
		for i, p := range panes {
			term, err := a.openPaneShell(s, p, client, spec)
			if err != nil && i == 0 {
				client.Close()
				fail(err)
				return
			}
			if err != nil && refused == nil {
				refused = err
			}
			terms[i] = term
		}
		ip := h.Addr
		if addr, ok := client.RemoteAddr().(*net.TCPAddr); ok {
			ip = addr.IP.String()
		}
		a.post(func() {
			if ctx.Err() != nil {
				for _, term := range terms {
					if term != nil {
						term.Close()
					}
				}
				client.Close()
				return
			}
			s.client, s.ip = client, ip
			for i, p := range panes {
				switch {
				case terms[i] == nil:
					s.panes.remove(p)
					continue
				case !s.panes.has(p): // closed while connecting
					terms[i].Close()
					continue
				}
				p.term = terms[i]
				s.panes.opened(p)
				a.startPane(s, p, spec)
			}
			s.panes.refocus = true
			if refused != nil {
				a.notify(paneError(refused))
			}
			s.status, s.err = statusConnected, nil
			a.noteConnected(s.Host)
			a.startTunnels(s, h.Tunnels)
		})
		// The monitor polls with an exec channel; hosts that drop extra
		// channels turn it off.
		if spec.exec {
			go monitor.Run(ctx, client, 2*time.Second, func(st monitor.Stats) {
				a.post(func() {
					if ctx.Err() == nil {
						s.stats = st
					}
				})
			})
		}
		sc, err := remotefs.NewClient(client)
		if err != nil {
			a.post(func() { s.files.err = &sftpError{err} })
			return
		}
		home, _ := sc.Getwd()
		if home == "" {
			home = "/"
		}
		a.post(func() {
			if ctx.Err() != nil {
				sc.Close()
				return
			}
			s.sftp = sc
			a.openDir(s, home)
		})
	}()
}

// shellSpec is what a session's shells and their terminals take from the
// settings, read on the UI thread, for the shells a goroutine opens.
type shellSpec struct {
	topts terminal.Options
	pty   sshx.PTY
	enc   config.Encoding
	bs    config.BackspaceKey
	del   config.DeleteKey
	exec  bool // the host allows exec channels: the monitor, the shell's follower
}

// shellSpec is the spec of h's shells as the settings have it now.
func (a *App) shellSpec(h *config.Host) shellSpec {
	bs, del := a.settings.HostBackspace(h), a.settings.HostDelete(h)
	return shellSpec{
		topts: a.terminalOptions(h),
		pty:   sshx.PTY{Term: h.TermTypeOrDefault(), Erase: eraseChar(bs)},
		enc:   a.settings.HostEncoding(h),
		bs:    bs,
		del:   del,
		exec:  execAllowed(h),
	}
}

// openPaneShell opens a shell for p on client, as spec says, and the
// terminal that shows it. It runs on a goroutine: it blocks until the
// server answers.
func (a *App) openPaneShell(s *Session, p *pane, client *ssh.Client, spec shellSpec) (*terminal.Terminal, error) {
	shell, err := sshx.OpenShell(client, spec.pty)
	if err != nil {
		return nil, &stepError{"shell", err}
	}
	topts := spec.topts
	topts.Conn = termenc.Wrap(shell, spec.enc)
	keys := keySequences(spec.bs, spec.del, func() *terminal.Terminal { return p.term })
	topts.OnKey = func(mods ui.Modifiers, key ui.Key) bool {
		p.shellKey(mods, key) // for the file panel to follow a cd, and the tab its agent
		return keys != nil && keys(mods, key)
	}
	topts.OnExit = func(int) { a.post(func() { a.paneExited(s, p) }) }
	term, err := terminal.New(topts)
	if err != nil {
		shell.Close()
		return nil, &stepError{"terminal", err}
	}
	return term, nil
}

// keepAliveTimeout is the longest a keepalive waits for its answer
// before it counts as missed, in seconds of the setting; shorter
// intervals wait an interval.
const keepAliveTimeout = 15

// keepAlive is the keepalive of a new connection, as the settings have
// it: every interval, giving up on a connection after three answers in
// a row have not come. It reads the settings, so it runs on the UI
// thread.
func (a *App) keepAlive() sshx.KeepAliveConfig {
	n := a.settings.KeepAliveInterval()
	if n == 0 {
		return sshx.KeepAliveConfig{}
	}
	unit := a.keepAliveUnit
	if unit == 0 {
		unit = time.Second
	}
	return sshx.KeepAliveConfig{
		Interval:  time.Duration(n) * unit,
		Timeout:   time.Duration(min(n, keepAliveTimeout)) * unit,
		MaxMissed: 3,
	}
}

// terminalOptions are the options of a new terminal for h as the
// settings have them; connect adds the shell and its hooks. It reads the
// settings, so it runs on the UI thread.
func (a *App) terminalOptions(h *config.Host) terminal.Options {
	o := terminal.Options{Font: a.termFont, Theme: a.termTheme, DarkTheme: a.termTheme}
	a.mouseOptions(&o)
	a.keyOptions(&o)
	a.appearanceOptions(&o)
	return o
}

// applyTerminalSettings gives every open terminal the settings that a
// terminal can change as it runs; the rest apply as sessions connect.
func (a *App) applyTerminalSettings() {
	look := a.loadAppearance()
	for _, s := range a.sessions {
		for _, p := range s.panes.panes() {
			if p.term != nil {
				a.applyMouse(p.term)
				a.applyAppearance(p.term, look)
			}
		}
	}
}

// noteConnected keeps the time h connected, for the recent hosts.
func (a *App) noteConnected(h *config.Host) {
	if a.hosts.MarkConnected(h.ID, time.Now()) {
		a.saveHosts(a.hosts)
	}
}

// disconnect ends s's connection, keeping its tab and the layout of its
// panes, whose shells end.
func (s *Session) disconnect() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.hostKeyQ != nil {
		s.hostKeyQ.Answer <- false
		s.hostKeyQ = nil
	}
	s.stopTunnels()
	if s.sftp != nil {
		s.sftp.Close()
		s.sftp = nil
	}
	for _, p := range s.panes.panes() {
		p.end()
	}
	s.panes.order = nil
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
}

func (s *Session) statusColor(t *ui.Theme) ui.Color {
	switch s.status {
	case statusConnected:
		return t.Success
	case statusConnecting:
		return t.Warning
	case statusFailed:
		return t.Danger
	}
	return t.TextMuted
}

// statusText is a short word for the session's state.
func (s *Session) statusText() string {
	switch s.status {
	case statusConnecting:
		return locales.StatusConnecting()
	case statusFailed:
		return locales.StatusFailed()
	case statusClosed:
		if errors.Is(s.err, errConnectionLost) {
			return locales.ConnectionLost()
		}
		if errors.Is(s.err, errShellExited) {
			return locales.StatusExited()
		}
		return locales.StatusDisconnected()
	}
	return locales.StatusConnected()
}

// describeError says err in the language the app shows.
func describeError(err error) string {
	var changed *sshx.HostKeyChangedError
	var keyFile *sshx.KeyFileError
	var step *stepError
	var local *localStartError
	if msg, ok := describeRoute(err); ok { // a proxy or jump host on the way
		return msg
	}
	if msg, ok := describeAgentError(err); ok {
		return msg
	}
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errConnectionLost):
		return locales.ConnectionLost()
	case errors.Is(err, errShellExited):
		return locales.StatusExited()
	case errors.As(err, &local):
		if errors.Is(local.err, config.ErrUnclosedQuote) {
			return locales.ErrLocalShellQuote(local.program)
		}
		return locales.ErrLocalStart(local.program, local.err.Error())
	case errors.As(err, &changed):
		return locales.ErrHostKeyChanged(changed.Fingerprint, changed.File)
	case errors.Is(err, sshx.ErrHostKeyRejected):
		return locales.ErrHostKeyRejected()
	case errors.Is(err, sshx.ErrHostKeyTimeout):
		return locales.ErrHostKeyTimeout()
	case errors.Is(err, sshx.ErrNeedPassphrase):
		return locales.ErrNeedPassphrase()
	case errors.Is(err, sshx.ErrNoAuth):
		return locales.ErrNoAuth()
	case errors.Is(err, x509.IncorrectPasswordError):
		return locales.ErrWrongPassphrase()
	case errors.As(err, &keyFile):
		if keyFile.Parse {
			return locales.ErrParseKey(keyFile.Err.Error())
		}
		return locales.ErrReadKey(keyFile.Err.Error())
	case errors.As(err, &step):
		if step.step == "terminal" {
			return locales.ErrTerminal(step.err.Error())
		}
		return locales.ErrOpenShell(step.err.Error())
	}
	switch sshx.Classify(err) {
	case "auth":
		return locales.ErrAuthFailed()
	case "refused":
		return locales.ErrRefused()
	case "timeout":
		return locales.ErrTimeout()
	case "nohost":
		return locales.ErrNoSuchHost()
	}
	return err.Error()
}
