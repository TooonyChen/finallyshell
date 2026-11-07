package app

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"finalshelly/internal/config"
	"finalshelly/internal/monitor"
	"finalshelly/internal/remotefs"
	"finalshelly/internal/sshx"
	"finalshelly/locales"

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
// server's stats and its files.
type Session struct {
	ID     int64
	Host   *config.Host
	status sessionStatus
	err    error // why it failed or closed; nil when the user disconnected
	ip     string

	client *ssh.Client
	term   *terminal.Terminal
	stats  monitor.Stats
	sftp   *sftp.Client
	files  fileState

	hostKeyQ *sshx.HostKeyQuestion
	cancel   context.CancelFunc
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
	s := &Session{ID: nextSessionID.Add(1), Host: h}
	s.files.init()
	return s
}

func (s *Session) label() string { return s.Host.Title() }

// connect starts connecting s on a goroutine, with the password, or the
// passphrase of its key.
func (a *App) connect(s *Session, secret string) {
	if s.cancel != nil {
		s.cancel()
	}
	s.status, s.err, s.hostKeyQ = statusConnecting, nil, nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	opts := sshx.Options{AskHostKey: func(q *sshx.HostKeyQuestion) { a.post(func() { s.hostKeyQ = q }) }}
	if s.Host.Auth == config.AuthKey {
		opts.Passphrase = secret
	} else {
		opts.Password = secret
	}
	h := *s.Host
	fail := func(err error) { a.post(func() { s.status, s.err = statusFailed, err }) }
	go func() {
		client, err := sshx.Dial(&h, opts)
		if err != nil {
			fail(err)
			return
		}
		if ctx.Err() != nil { // closed while connecting
			client.Close()
			return
		}
		shell, err := sshx.OpenShell(client)
		if err != nil {
			client.Close()
			fail(&stepError{"shell", err})
			return
		}
		term, err := terminal.New(terminal.Options{
			Conn:      shell,
			Font:      a.termFont,
			Theme:     a.termTheme,
			DarkTheme: a.termTheme,
			OnExit: func(int) {
				a.post(func() {
					if s.status == statusConnected && s.cancel != nil {
						s.status, s.err = statusClosed, errConnectionLost
					}
				})
			},
		})
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
			s.status, s.err = statusConnected, nil
			a.noteConnected(s.Host)
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
