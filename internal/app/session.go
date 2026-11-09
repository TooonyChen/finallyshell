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
// start page (see start.go).
type Session struct {
	ID     int64
	Host   *config.Host // nil for a new tab
	status sessionStatus
	err    error // why it failed or closed; nil when the user disconnected
	ip     string

	client *ssh.Client
	term   *terminal.Terminal
	stats  monitor.Stats
	sftp   *sftp.Client
	files  fileState

	tunnels tunnelState // see tunnels.go
	procs   procState   // the Processes tab, see processes.go
	net     netState    // the Network tab, see network.go

	hostKeyQ *sshx.HostKeyQuestion
	cancel   context.CancelFunc

	start startState    // the start page, while it is a new tab
	find  findState     // the find bar over the terminal
	bar   cmdBar        // the command bar under the terminal (commandbar.go)
	shell shellDirState // the shell's directory, which the file panel follows

	// name is the tab's name for this run, "" for the host's title: never
	// saved (rename.go).
	name string
}

// errConnectionLost is a session whose shell ended.
var errConnectionLost = errors.New("connection lost")

// stepError is a step after logging in that failed.
type stepError struct {
	step string // "shell", "terminal"
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }

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
	s.cancel = cancel
	// Through the host's proxy or jump hosts, if any (see proxy.go):
	// closing the client closes theirs.
	opts, err := a.dialOptions(s.Host, secret, func(q *sshx.HostKeyQuestion) { a.post(func() { s.hostKeyQ = q }) })
	if err != nil {
		s.status, s.err = statusFailed, err
		return
	}
	h := *s.Host
	topts := a.terminalOptions(&h) // here, on the UI thread, which owns the settings
	bs, del, enc := a.settings.HostBackspace(&h), a.settings.HostDelete(&h), a.settings.HostEncoding(&h)
	passphrase := a.storedPassphrase(s.Host)
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
		shell, err := sshx.OpenShell(client, sshx.PTY{Term: h.TermTypeOrDefault(), Erase: eraseChar(bs)})
		if err != nil {
			client.Close()
			fail(&stepError{"shell", err})
			return
		}
		topts.Conn = termenc.Wrap(shell, enc)
		keys := keySequences(bs, del, func() *terminal.Terminal { return s.term })
		topts.OnKey = func(mods ui.Modifiers, key ui.Key) bool {
			if key == ui.KeyEnter {
				s.shellCommandSent() // for the file panel to follow a cd
			}
			return keys != nil && keys(mods, key)
		}
		topts.OnExit = func(int) {
			a.post(func() {
				if s.status == statusConnected && s.cancel != nil {
					s.status, s.err = statusClosed, errConnectionLost
					s.stopTunnels()
				}
			})
		}
		term, err := terminal.New(topts)
		if err != nil {
			shell.Close()
			client.Close()
			fail(&stepError{"terminal", err})
			return
		}
		ip := h.Addr
		if addr, ok := client.RemoteAddr().(*net.TCPAddr); ok {
			ip = addr.IP.String()
		}
		a.post(func() {
			if ctx.Err() != nil {
				term.Close()
				client.Close()
				return
			}
			s.client, s.term, s.ip = client, term, ip
			a.startShellDir(ctx, s, client, term, execAllowed(&h))
			a.updateTerminal(term, topts)
			s.status, s.err = statusConnected, nil
			a.noteConnected(s.Host)
			a.startTunnels(s, h.Tunnels)
		})
		// The monitor polls with an exec channel; hosts that drop extra
		// channels turn it off.
		if execAllowed(&h) {
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
		if s.term != nil {
			a.applyMouse(s.term)
			a.applyAppearance(s.term, look)
		}
	}
}

// noteConnected keeps the time h connected, for the recent hosts.
func (a *App) noteConnected(h *config.Host) {
	if a.hosts.MarkConnected(h.ID, time.Now()) {
		a.saveHosts(a.hosts)
	}
}

// disconnect ends s's connection, keeping its tab.
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
	if s.term != nil {
		s.term.Close()
		s.term = nil
	}
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
		return locales.StatusDisconnected()
	}
	return locales.StatusConnected()
}

// describeError says err in the language the app shows.
func describeError(err error) string {
	var changed *sshx.HostKeyChangedError
	var keyFile *sshx.KeyFileError
	var step *stepError
	if msg, ok := describeRoute(err); ok { // a proxy or jump host on the way
		return msg
	}
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errConnectionLost):
		return locales.ConnectionLost()
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
