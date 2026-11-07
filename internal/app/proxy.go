package app

// Connecting through proxies and jump hosts: the options sshx.Dial
// takes, the passwords asked for before connecting, and the errors on
// the way, in the user's words. The connection editor's Proxy page is in
// editor_proxy.go.

import (
	"errors"
	"strings"

	"finallyshell/internal/config"
	"finallyshell/internal/sshx"
	"finallyshell/locales"

	"golang.org/x/crypto/ssh"
)

// proxySecretKey is where the password of the proxy of the host with the
// ID is kept among proxySecrets.
func proxySecretKey(hostID string) string { return "proxy/" + hostID }

// proxySecrets holds the proxies' passwords, by proxySecretKey: with the
// hosts' passwords, in memory for this run, never in hosts.json. In the
// credential store each is the host's secretProxyPassword (secrets.go).
func (a *App) proxySecrets() map[string]string { return a.passwords }

// usesProxyPassword says whether connecting to h takes the password of
// its proxy: a SOCKS5 or HTTP proxy with a user.
func usesProxyPassword(h *config.Host) bool {
	p := config.ProxyOf(h)
	return p != nil && p.Kind != config.ProxyJump && p.User != ""
}

// hopOptions are the options that log in to h with secret, its password
// or its key's passphrase, through its proxy, without its jump host.
func (a *App) hopOptions(h *config.Host, secret string, ask func(*sshx.HostKeyQuestion)) sshx.Options {
	opts := sshx.Options{AskHostKey: ask}
	if h.Auth == config.AuthKey {
		opts.Passphrase = secret
	} else {
		opts.Password = secret
	}
	if usesProxyPassword(h) {
		opts.ProxyPassword = a.proxySecrets()[proxySecretKey(h.ID)]
	}
	return opts
}

// dialOptions are what sshx.Dial takes to reach h with secret: its
// proxy's password, and its jump hosts with what logging in to them
// takes, as the app knows them now. Each jump host is a copy, as the
// connecting goroutine reads it while the editor may change the saved
// one. It fails when a jump host is missing or the chain loops.
func (a *App) dialOptions(h *config.Host, secret string, ask func(*sshx.HostKeyQuestion)) (sshx.Options, error) {
	jumps, err := a.hosts.JumpHosts(h)
	if err != nil {
		return sshx.Options{}, err
	}
	// From the jump host that connects first to the one h goes through.
	var next *sshx.Hop
	for i := len(jumps) - 1; i >= 0; i-- {
		j := *jumps[i]
		opts := a.hopOptions(&j, a.passwords[j.ID], ask)
		opts.Jump = next
		next = &sshx.Hop{Host: &j, Options: opts}
	}
	opts := a.hopOptions(h, secret, ask)
	opts.Jump = next
	return opts, nil
}

// secretNeed is a secret that connecting takes: of the host, of a jump
// host on the way, or of a proxy. The prompt asks for those the app does
// not know.
type secretNeed struct {
	key   string // in a.passwords, or a.proxySecrets for a proxy's
	proxy bool
	title func() string // what is asked: the host's password, a jump host's, a proxy's
	who   string        // whose: user@host
	// via is the host connected to, when the password is for a hop on
	// the way to it; "" for the host's own.
	via string

	// host is whose secret it is, whose "Remember password" it follows;
	// store is its key in the credential store (secrets.go).
	host  *config.Host
	store string
	// passphrase is set for the passphrase of host's key, which the app
	// does not ask for before connecting: a key may have none. detail is
	// the key's file.
	passphrase bool
	detail     string
}

// loginNeed is what x logs in with as connecting to h takes it: x's
// password, or its key's passphrase. x is h, or a jump host on the way.
func loginNeed(x, h *config.Host) *secretNeed {
	n := &secretNeed{key: x.ID, store: loginSecretKey(x), host: x, title: locales.EnterPassword, who: x.Target()}
	if x.Auth == config.AuthKey {
		n.passphrase, n.title, n.detail = true, locales.EnterPassphrase, x.KeyPath
	}
	if x != h {
		if x.Name != "" {
			n.who = x.Name + " (" + n.who + ")"
		}
		n.via = h.Title()
		if !n.passphrase {
			n.title = locales.JumpHostPasswordTitle
		}
	}
	return n
}

// proxyNeed is the password of x's proxy, as connecting to h takes it.
func proxyNeed(x, h *config.Host) *secretNeed {
	p := config.ProxyOf(x)
	return &secretNeed{key: proxySecretKey(x.ID), proxy: true, title: locales.ProxyPasswordTitle,
		who: p.User + "@" + sshx.ProxyAddr(p), via: h.Title(),
		host: x, store: hostSecretKey(x.ID, secretProxyPassword)}
}

// chainSecrets are the secrets connecting to h takes, in the order they
// are on the way: h's password or its key's passphrase, its proxy's
// password, then those of its jump hosts and their proxies. A jump host
// that is missing or loops takes none: connecting says so.
func (a *App) chainSecrets(h *config.Host) []*secretNeed {
	hosts := []*config.Host{h}
	if jumps, err := a.hosts.JumpHosts(h); err == nil {
		hosts = append(hosts, jumps...)
	}
	var needs []*secretNeed
	for _, x := range hosts {
		needs = append(needs, loginNeed(x, h))
		if usesProxyPassword(x) {
			needs = append(needs, proxyNeed(x, h))
		}
	}
	return needs
}

// missingSecret is the first password that connecting to h takes and
// the app does not know, in the order chainSecrets has them; nil for
// none. A key's passphrase is never missing.
func (a *App) missingSecret(h *config.Host) *secretNeed {
	for _, n := range a.chainSecrets(h) {
		if _, known := a.knownSecret(n); !known && !n.passphrase {
			return n
		}
	}
	return nil
}

// askSecret shows the password prompt for need; then runs once it is
// entered (passwordEntered).
func (a *App) askSecret(need *secretNeed, then func()) {
	a.pwOpen, a.pwNeed, a.pwThen, a.pwText = true, need, then, ""
	a.pwRemember, a.pwErr = need.host.RemembersPassword(), nil
	a.checkSecrets()
}

// knownSecret is need's secret as the app knows it this run.
func (a *App) knownSecret(need *secretNeed) (string, bool) {
	m := a.passwords
	if need.proxy {
		m = a.proxySecrets()
	}
	v, ok := m[need.key]
	return v, ok
}

// keepSecret keeps the password entered for need, for this run.
func (a *App) keepSecret(need *secretNeed, pw string) {
	if need.proxy {
		a.proxySecrets()[need.key] = pw
	} else {
		a.passwords[need.key] = pw
	}
}

// forgetSecret forgets need's secret for this run.
func (a *App) forgetSecret(need *secretNeed) {
	if need.proxy {
		delete(a.proxySecrets(), need.key)
	} else {
		delete(a.passwords, need.key)
	}
}

// describeRoute says, in the language shown, an error of a proxy or jump
// host on the way; ok is false for an error of another kind.
func describeRoute(err error) (msg string, ok bool) {
	var hop *sshx.HopError
	var loop *config.JumpLoopError
	var missing *config.JumpError
	switch {
	case errors.As(err, &hop):
		if hop.Proxy != "" {
			return locales.ErrViaProxy(hop.Proxy, describeProxyFailure(hop.Err)), true
		}
		return locales.ErrViaJump(hop.JumpHost, describeJumpFailure(hop.Err)), true
	case errors.As(err, &loop):
		return locales.ErrJumpLoop(strings.Join(loop.Names, " → ")), true
	case errors.As(err, &missing):
		return locales.ErrJumpHostMissing(missing.From), true
	case errors.Is(err, sshx.ErrNoJumpHost):
		return locales.ErrJumpHostRequired(), true
	}
	return "", false
}

// describeProxyFailure says why a SOCKS5 or HTTP proxy failed.
func describeProxyFailure(err error) string {
	var target *sshx.ProxyTargetError
	switch {
	case errors.Is(err, sshx.ErrProxyAuth):
		return locales.ErrProxyAuth()
	case errors.As(err, &target):
		return locales.ErrCannotReach(target.Addr, target.Reason)
	}
	switch sshx.Classify(err) {
	case "refused":
		return locales.ErrProxyRefused()
	case "timeout":
		return locales.ErrTimeout()
	case "nohost":
		return locales.ErrNoSuchHost()
	}
	return err.Error()
}

// describeJumpFailure says why a jump host failed: as any host when
// logging in to it, or that it could not open the connection onward.
func describeJumpFailure(err error) string {
	var fwd *sshx.ForwardError
	if !errors.As(err, &fwd) {
		return describeError(err)
	}
	var rejected *ssh.OpenChannelError
	switch {
	case errors.As(fwd.Err, &rejected) && rejected.Reason == ssh.Prohibited:
		return locales.ErrForwardProhibited(fwd.Addr)
	case errors.As(fwd.Err, &rejected):
		return locales.ErrCannotReach(fwd.Addr, rejected.Message)
	case sshx.Classify(fwd.Err) == "timeout":
		return locales.ErrCannotReach(fwd.Addr, locales.ErrTimeout())
	}
	return locales.ErrCannotReach(fwd.Addr, fwd.Err.Error())
}
