package app

// SSH agents: hosts that log in with the keys an agent holds
// (config.AuthAgent), the agent's address on the General page of the
// options, the Check buttons that list its keys, and its errors in the
// user's words. Logging in is sshx's (sshx/agent.go).

import (
	"errors"
	"runtime"
	"strings"

	"finallyshell/internal/sshx"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// agentCheck is what a Check button found in the SSH agent: its keys,
// or why it could not be reached. Its fields belong to the UI thread.
type agentCheck struct {
	running bool
	done    bool
	addr    string // the address checked
	keys    []sshx.AgentKey
	err     error
	// seq tells the check whose answer is wanted from those started
	// earlier, or reset since.
	seq int
}

// checkAgent lists the keys of the agent of the settings into ck, on a
// goroutine: an agent may take its time to answer.
func (a *App) checkAgent(ck *agentCheck) {
	ck.reset()
	seq, setting := ck.seq, a.settings.SSHAgent
	ck.running = true
	go func() {
		addr, keys, err := sshx.ListAgentKeys(setting)
		a.post(func() {
			if ck.seq != seq {
				return
			}
			ck.running, ck.done, ck.addr, ck.keys, ck.err = false, true, addr, keys, err
		})
	}()
}

// reset forgets what ck found, and the answer of a check still running.
func (ck *agentCheck) reset() { *ck = agentCheck{seq: ck.seq + 1} }

// agentCheckView shows what ck found, under its Check button: nothing
// before a check.
func agentCheckView(c *ui.Context, ck *agentCheck) {
	switch {
	case ck.running:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label(locales.AgentChecking())
			widgets.Muted(c, locales.AgentChecking())
		})
	case !ck.done:
	case ck.err != nil:
		ui.Text(c, describeError(ck.err)).TextColor(theme.Danger).Selectable()
	case len(ck.keys) == 0:
		ui.Text(c, locales.ErrAgentNoKeys(ck.addr)).TextColor(theme.Danger).Selectable()
	default:
		ui.Column(c).Gap(6).Padding(10, 12).Radius(6).Background(theme.Surface).
			BorderWidth(1).BorderColor(theme.Border).Children(func() {
			msg := locales.AgentConnectedAt(len(ck.keys), ck.addr)
			if name := agentName(ck.addr); name != "" {
				msg = locales.AgentConnectedNamed(len(ck.keys), name)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "✓").TextColor(theme.Success)
				ui.Text(c, msg).TextColor(theme.Success).Grow(1).MinWidth(0)
			})
			for i, k := range ck.keys {
				ui.Row(c.Key(i)).Gap(10).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, k.Type).Font("monospace").SingleLine().Shrink(0)
					ui.Text(c, shortFingerprint(k.Fingerprint)).Font("monospace").TextColor(theme.TextMuted).SingleLine().Shrink(0).
						Tooltip(k.Fingerprint)
					ui.Text(c, k.Comment).Font("monospace").SingleLine().Shrink(1).MinWidth(0)
				})
			}
			widgets.Muted(c, locales.AgentTriedInTurn()).FontSize(12)
		})
	}
}

// shortFingerprint shortens SHA256:<43 characters> to its first and last
// few, as the Check list shows it; the whole one is its tooltip.
func shortFingerprint(fp string) string {
	const head, tail = 6, 4
	b, ok := strings.CutPrefix(fp, "SHA256:")
	if !ok || len(b) <= head+tail+1 {
		return fp
	}
	return "SHA256:" + b[:head] + "…" + b[len(b)-tail:]
}

// agentName is the app an agent's address is known to be, as the Check
// says it: "" when the address does not tell.
func agentName(addr string) string {
	switch l := strings.ToLower(addr); {
	case strings.Contains(l, "bitwarden"):
		return "Bitwarden"
	case strings.Contains(l, "1password"):
		return "1Password"
	}
	return ""
}

// knownAgents are the addresses of common agents, for the table of the
// General page: the app, its address.
var knownAgents = []struct {
	app, addr string
}{
	{"Bitwarden", "~/.bitwarden-ssh-agent.sock"},
	{"1Password", "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"},
	{"Windows", `\\.\pipe\openssh-ssh-agent`},
}

// describeAgentError says an SSH agent's error; ok is false for another
// error.
func describeAgentError(err error) (msg string, ok bool) {
	var ae *sshx.AgentError
	if !errors.As(err, &ae) {
		return "", false
	}
	switch {
	case errors.Is(ae.Err, sshx.ErrNoAgentAddr):
		return locales.ErrAgentNoAddress(), true
	case errors.Is(ae.Err, sshx.ErrAgentNoKeys):
		return locales.ErrAgentNoKeys(ae.Addr), true
	case errors.Is(ae.Err, sshx.ErrAgentRefused):
		return locales.ErrAgentRefused(), true
	case errors.Is(ae.Err, sshx.ErrAgentSign):
		detail := strings.TrimPrefix(ae.Err.Error(), sshx.ErrAgentSign.Error()+": ")
		return locales.ErrAgentSign(detail), true
	}
	return locales.ErrAgentNotFound(ae.Addr, ae.Err.Error()), true
}

// isAgentError tells whether err is an SSH agent's, of the host or a
// jump host on the way, whose message offers the agent's settings.
func isAgentError(err error) bool {
	var ae *sshx.AgentError
	return errors.As(err, &ae)
}

// openAgentSettings opens the options on the General page, where the
// agent's address is.
func (a *App) openAgentSettings() {
	a.optionsNav.Page = pageGeneral
	a.openOptions()
}

// agentOption is the SSH agent address of the General page's
// Connections part: the address, its Check button, and the addresses of
// common agents.
func (a *App) agentOption(c *ui.Context) {
	ui.Form(c, func() {
		ui.Field(c, locales.OptionSSHAgent(), func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				v := a.settings.SSHAgent
				def := "$SSH_AUTH_SOCK"
				if runtime.GOOS == "windows" {
					def = sshx.DefaultAgentAddr()
				}
				if ui.TextInput(c, &v).Placeholder(locales.OptionSSHAgentDefault(def)).Font("monospace").
					Grow(1).MinWidth(120).Label(locales.OptionSSHAgent()).Changed() {
					a.agentOptionCheck.reset()
					a.changeSettings(func() { a.settings.SSHAgent = v })
				}
				if ui.Button(c, locales.AgentCheck()).Label(locales.AgentCheckLabel()).Disabled(a.agentOptionCheck.running).Clicked() {
					a.checkAgent(&a.agentOptionCheck)
				}
			})
			agentCheckView(c, &a.agentOptionCheck)
			widgets.Muted(c, locales.OptionSSHAgentNote()).FontSize(12)
			knownAgentsTable(c)
			widgets.Muted(c, locales.OptionThisComputerOnly()).FontSize(12)
		})
	})
}

// knownAgentsTable is the table of common agents' addresses.
func knownAgentsTable(c *ui.Context) {
	ui.Column(c).Radius(6).BorderWidth(1).BorderColor(theme.Border).Children(func() {
		ui.Row(c).Gap(10).Padding(5, 10).Background(theme.Surface).Children(func() {
			widgets.Muted(c, locales.AgentTableApp()).FontSize(12).Width(96).Shrink(0)
			widgets.Muted(c, locales.AgentTableAddress()).FontSize(12).Grow(1)
		})
		for i, k := range knownAgents {
			addr := k.addr
			if k.app == "Windows" {
				addr = locales.AgentWindowsAddr(addr)
			}
			ui.Column(c.Key(i)).Children(func() {
				ui.Divider(c)
				ui.Row(c).Gap(10).Padding(5, 10).Children(func() {
					ui.Text(c, k.app).Font("monospace").FontSize(12).Width(96).Shrink(0)
					ui.Text(c, addr).Font("monospace").FontSize(12).Grow(1).MinWidth(0).Selectable()
				})
			})
		}
	})
}
