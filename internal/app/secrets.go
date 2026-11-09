package app

import (
	"context"
	"crypto/x509"
	"errors"
	"log"
	"runtime"
	"slices"
	"sync"

	"finallyshell/internal/config"
	"finallyshell/internal/sshx"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// Passwords and key passphrases are kept in the system's credential store
// (Platform.SecretGet, SecretSet, SecretDelete: the Keychain on macOS, the
// Credential Manager on Windows, the Secret Service on Linux), never in
// hosts.json, under the keys hostSecretKey builds; and in a.passwords (and
// proxySecrets) for this run. Every secret a connection takes, along its
// proxies and jump hosts, is a secretNeed (proxy.go). Hosts whose "Remember password" is off, and every host where
// the system has no store, keep theirs in memory alone.
//
// The store's calls may block for long, while the system asks the user to
// unlock the keyring or, on macOS, to let a rebuilt app read what it
// stored. MyGo's Secrets keeps the event loop running when called on the
// main thread (offMain runs the call on another goroutine and steps the
// native loop until it returns), but that loop delivers input and draws
// frames from inside the view that made the call, which would then run
// again in the middle of changing the state. So the app never calls the
// store on the UI thread: the calls run one at a time, in the order made,
// on a goroutine of the secret queue, and what they read comes back
// through App.post. A read after a write sees what was written.

// The names of a host's secrets, for hostSecretKey.
const (
	secretPassword   = "password"   // the password of a host that logs in with one
	secretPassphrase = "passphrase" // the passphrase of the host's private key
	// the password of the host's SOCKS5 or HTTP proxy, kept in memory
	// under proxySecretKey (proxy.go)
	secretProxyPassword = "proxy-password"
)

// hostSecretNames are the names of every secret a host may have:
// deleting the host deletes them all. A new kind of secret adds its name
// here.
var hostSecretNames = []string{secretPassword, secretPassphrase, secretProxyPassword}

// hostSecretKey is the key in the credential store of the secret called
// name of the host id: "host/<id>/<name>", e.g. "host/h1/password".
func hostSecretKey(id, name string) string { return "host/" + id + "/" + name }

// loginSecretKey is the key of what h logs in with: its password, or the
// passphrase of its key.
func loginSecretKey(h *config.Host) string {
	if h.Auth == config.AuthKey {
		return hostSecretKey(h.ID, secretPassphrase)
	}
	return hostSecretKey(h.ID, secretPassword)
}

// probeSecretKey is read, finding nothing, to learn whether the system
// has a credential store before the app needs one.
const probeSecretKey = "probe"

// storeState is what the app knows of the credential store.
type storeState int

const (
	storeUnknown storeState = iota // no call has answered yet
	storeUp
	storeDown // the system has none: secrets stay in memory
)

// secretStore is the app's side of the credential store. Its fields
// belong to the UI thread, but for the queue's.
type secretStore struct {
	state   storeState
	probing bool
	// read are the keys read this run, or written: what they hold, if
	// anything, is in a.passwords, and is not read again.
	read map[string]bool
	// untried are the passwords, and passphrases, entered in the prompt
	// with "Remember password", by host ID: stored once they log in, so
	// that a typo is never kept.
	untried map[string]string
	// logged are the store's errors already logged, by their text.
	logged map[string]bool
	queue  secretQueue
}

func (s *secretStore) markRead(key string) {
	if s.read == nil {
		s.read = map[string]bool{}
	}
	s.read[key] = true
}

// secretQueue runs jobs one at a time, in order, on a goroutine that
// lives while there are jobs.
type secretQueue struct {
	mu      sync.Mutex
	jobs    []func()
	running bool
}

func (q *secretQueue) do(job func()) {
	q.mu.Lock()
	q.jobs = append(q.jobs, job)
	start := !q.running
	q.running = true
	q.mu.Unlock()
	if start {
		go q.run()
	}
}

// idle tells whether the queue ran every job it was given.
func (q *secretQueue) idle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return !q.running
}

func (q *secretQueue) run() {
	for {
		q.mu.Lock()
		if len(q.jobs) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		job := q.jobs[0]
		q.jobs = q.jobs[1:]
		q.mu.Unlock()
		job()
	}
}

// noteStore keeps what a call's error says about the store. A store
// that is missing says so in the editor and the prompt; other errors (the
// user denied access, or canceled unlocking the keyring) count as nothing
// stored, and are logged, each once, so that they are not invisible.
func (a *App) noteStore(err error) {
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		a.secrets.state = storeDown
	case err == nil && a.secrets.state == storeUnknown:
		a.secrets.state = storeUp
	}
	if err != nil && !a.secrets.logged[err.Error()] {
		if a.secrets.logged == nil {
			a.secrets.logged = map[string]bool{}
		}
		a.secrets.logged[err.Error()] = true
		log.Printf("credential store: %v", err)
	}
}

// secretsUnavailable tells whether the system is known to have no
// credential store.
func (a *App) secretsUnavailable() bool { return a.secrets.state == storeDown }

// secret reads the secret of key from the store, then calls then on the
// UI thread with it, and whether there is one. It returns at once; then
// runs later, also when the store is unavailable or fails, with ok false.
func (a *App) secret(key string, then func(value string, ok bool)) {
	if a.secretsUnavailable() {
		a.post(func() { then("", false) })
		return
	}
	a.secrets.queue.do(func() {
		v, ok, err := a.plat.SecretGet(key)
		a.post(func() {
			a.noteStore(err)
			then(v, ok && err == nil)
		})
	})
}

// setSecret keeps value under key in the store, replacing what it had.
// It returns at once.
func (a *App) setSecret(key, value string) {
	a.secrets.markRead(key)
	if a.secretsUnavailable() {
		return
	}
	a.secrets.queue.do(func() {
		err := a.plat.SecretSet(key, value)
		a.post(func() { a.noteStore(err) })
	})
}

// deleteSecret removes the secret of key from the store. It returns at
// once.
func (a *App) deleteSecret(key string) {
	a.secrets.markRead(key)
	if a.secretsUnavailable() {
		return
	}
	a.secrets.queue.do(func() {
		err := a.plat.SecretDelete(key)
		a.post(func() { a.noteStore(err) })
	})
}

// checkSecrets finds out, once, whether the system has a credential
// store, for the editor and the prompt to say so when it has none.
func (a *App) checkSecrets() {
	if a.secrets.state != storeUnknown || a.secrets.probing {
		return
	}
	a.secrets.probing = true
	a.secret(probeSecretKey, func(string, bool) { a.secrets.probing = false })
}

// unread tells whether need's secret is to be read from the store before
// it is used: the app does not know it, has not read it this run, and
// the host remembers it, where there is a store.
func (a *App) unread(need *secretNeed) bool {
	_, known := a.knownSecret(need)
	return !known && need.host.RemembersPassword() && !a.secrets.read[need.store] && !a.secretsUnavailable()
}

// readSecret reads need's secret from the store, when unread, and calls
// then, if not nil, once the app has it, or knows the store has none. It
// returns false, calling nothing, when there is nothing to read.
func (a *App) readSecret(need *secretNeed, then func()) bool {
	if !a.unread(need) {
		return false
	}
	a.secret(need.store, func(v string, ok bool) {
		a.secrets.markRead(need.store)
		// Not when the host changed the way it logs in meanwhile.
		_, known := a.knownSecret(need)
		if ok && !known && (need.proxy || loginSecretKey(need.host) == need.store) {
			a.keepSecret(need, v)
		}
		if then != nil {
			then()
		}
	})
	return true
}

// readChainSecrets reads from the store the secrets connecting to h
// takes that are unread (chainSecrets), and calls then once they are all
// read. It returns false, calling nothing, when there are none. The
// passphrase of h's own key is read as the session connects instead
// (storedPassphrase), so that the session opens at once.
func (a *App) readChainSecrets(h *config.Host, then func()) bool {
	var needs []*secretNeed
	for _, n := range a.chainSecrets(h) {
		if a.unread(n) && !(n.host == h && n.passphrase) {
			needs = append(needs, n)
		}
	}
	for i, n := range needs {
		// The reads answer in order: the last one's then runs last.
		if i == len(needs)-1 {
			a.readSecret(n, then)
		} else {
			a.readSecret(n, nil)
		}
	}
	return len(needs) > 0
}

// storedPassphrase returns, for a host that logs in with a key whose
// passphrase the app has not read from the store yet, what the session's
// goroutine calls to wait for it, as it connects: "" for a key without
// one. Else it returns nil.
func (a *App) storedPassphrase(h *config.Host) func(ctx context.Context) string {
	if h.Auth != config.AuthKey {
		return nil
	}
	ch := make(chan string, 1)
	if !a.readSecret(loginNeed(h, h), func() { ch <- a.passwords[h.ID] }) {
		return nil
	}
	return func(ctx context.Context) string {
		select {
		case v := <-ch:
			return v
		case <-ctx.Done():
			return ""
		}
	}
}

// readEditorSecret fills the editor's password, or passphrase, of h, and
// its proxy's password, with those saved, once they are read; for a new
// host (nil) it only finds out whether there is a store.
func (a *App) readEditorSecret(ed *connEditor, h *config.Host) {
	if h != nil {
		auth := h.Auth
		ed.readingSecret = a.readSecret(loginNeed(h, h), func() {
			if a.editor != ed || !ed.readingSecret {
				return
			}
			ed.readingSecret = false
			v := a.passwords[h.ID]
			switch {
			case auth == config.AuthKey && ed.passphrase == "":
				ed.passphrase = v
			case auth != config.AuthKey && ed.password == "":
				ed.password = v
			}
		})
		if usesProxyPassword(h) {
			ed.readingProxySecret = a.readSecret(proxyNeed(h, h), func() {
				if a.editor != ed || !ed.readingProxySecret {
					return
				}
				ed.readingProxySecret = false
				if ed.proxy.password == "" {
					ed.proxy.password = a.proxySecrets()[proxySecretKey(h.ID)]
				}
			})
		}
	}
	a.checkSecrets()
}

// setLogin keeps secret as what h logs in with this run, its password or
// the passphrase of its key. An empty password is forgotten, so that the
// prompt asks for it as the host connects; an empty passphrase is a key
// without one.
func (a *App) setLogin(h *config.Host, secret string) {
	if secret != "" || h.Auth == config.AuthKey {
		a.passwords[h.ID] = secret
	} else {
		delete(a.passwords, h.ID)
	}
}

// setRemember sets h's "Remember password", and tells whether it changed,
// for the caller to save h.
func setRemember(h *config.Host, remember bool) bool {
	changed := h.RemembersPassword() != remember
	h.NoRememberPassword = !remember
	return changed
}

// keepLogin keeps secret as what h logs in with, as the editor's OK saves
// it: for this run, and in the store with remember at once, as the user
// saved it, else out of it. With unread, the saved one not read yet, an
// empty secret leaves what the store keeps. It returns whether h's
// "Remember password" changed.
func (a *App) keepLogin(h *config.Host, secret string, remember, unread bool) bool {
	changed := setRemember(h, remember)
	key := loginSecretKey(h)
	for _, name := range []string{secretPassword, secretPassphrase} {
		if k := hostSecretKey(h.ID, name); k != key {
			a.deleteSecret(k) // h logged in the other way before
		}
	}
	delete(a.secrets.untried, h.ID)
	if unread && remember && secret == "" {
		return changed
	}
	a.setLogin(h, secret)
	if remember && secret != "" {
		a.setSecret(key, secret)
	} else {
		a.deleteSecret(key)
	}
	return changed
}

// keepProxySecret stores the password of h's proxy, as the editor's
// Proxy page saved it in proxySecrets (saveProxy), with remember, and
// deletes it from the store without, or when h has no proxy that takes
// one. With unread, the saved one not read yet, an empty one leaves what
// the store keeps.
func (a *App) keepProxySecret(h *config.Host, remember, unread bool) {
	key := hostSecretKey(h.ID, secretProxyPassword)
	delete(a.secrets.untried, proxySecretKey(h.ID))
	pw, ok := a.proxySecrets()[proxySecretKey(h.ID)]
	switch {
	case ok && remember && usesProxyPassword(h):
		a.setSecret(key, pw)
	case unread && remember && usesProxyPassword(h):
	default:
		a.deleteSecret(key)
	}
}

// passwordEntered keeps what the prompt was given, for this run, and
// goes on with what asked for it, which may ask for the next one. With
// "Remember password" it is stored once a connection logs in with it
// (loggedIn), so that a mistyped one is never kept and the one stored
// stays until one that works replaces it; without, the one stored is
// deleted now.
func (a *App) passwordEntered() {
	need := a.pwNeed
	if need == nil {
		return
	}
	if setRemember(need.host, a.pwRemember) {
		a.saveHosts(a.hosts)
		a.redraw()
	}
	a.keepSecret(need, a.pwText)
	delete(a.secrets.untried, need.key)
	switch {
	case !a.pwRemember:
		a.deleteSecret(need.store)
	case a.pwText != "":
		if a.secrets.untried == nil {
			a.secrets.untried = map[string]string{}
		}
		a.secrets.untried[need.key] = a.pwText
	}
	then := a.pwThen
	a.pwOpen, a.pwText, a.pwNeed, a.pwThen, a.pwCancel, a.pwErr = false, "", nil, nil, nil, nil
	if then != nil {
		then()
	}
}

// loggedIn is told that a session to h logged in, through every hop on
// the way: the secrets the prompt was given with "Remember password" that
// it took are stored now.
func (a *App) loggedIn(h *config.Host) {
	for _, n := range a.chainSecrets(h) {
		v, ok := a.secrets.untried[n.key]
		if cur, known := a.knownSecret(n); !ok || !known || cur != v {
			continue
		}
		delete(a.secrets.untried, n.key)
		if n.host.RemembersPassword() {
			a.setSecret(n.store, v)
		}
	}
}

// loginFailed asks again for the secret that did not work as s connected
// (refusedSecret), as when the one saved is out of date: the session
// connects again with the one entered, which replaces the saved one once
// it logs in, with "Remember password".
func (a *App) loginFailed(s *Session, err error) {
	h := s.Host
	if h == nil || a.pwOpen || !slices.Contains(a.sessions, s) {
		return
	}
	need := a.refusedSecret(h, err)
	if need == nil {
		return
	}
	delete(a.secrets.untried, need.key) // nothing new is stored
	a.forgetSecret(need)
	a.secrets.markRead(need.store) // what the store has is wrong
	a.askSecret(need, func() { a.reconnect(s) })
	a.pwErr = err
}

// refusedSecret is the secret on the way to h that err says did not
// work, nil for none: a proxy refusing its user, a jump host refusing
// its password (or there being none), or its key's passphrase missing or
// wrong, and the same of h.
func (a *App) refusedSecret(h *config.Host, err error) *secretNeed {
	var hop *sshx.HopError
	if !errors.As(err, &hop) {
		if loginRefused(h, err) {
			return loginNeed(h, h)
		}
		return nil
	}
	for _, n := range a.chainSecrets(h) {
		if n.host.ID != hop.HostID {
			continue
		}
		switch {
		case hop.Proxy != "" && n.proxy && errors.Is(hop.Err, sshx.ErrProxyAuth):
			return n
		case hop.Proxy == "" && !n.proxy && n.host != h && loginRefused(n.host, hop.Err):
			return n
		}
	}
	return nil
}

// loginRefused tells whether err is h's secret not working: for a
// password host, the server refusing it (or there being none); for a key,
// its passphrase missing or wrong. A server refusing the key itself is
// not: no passphrase helps.
func loginRefused(h *config.Host, err error) bool {
	if h.Auth == config.AuthKey {
		return errors.Is(err, sshx.ErrNeedPassphrase) || errors.Is(err, x509.IncorrectPasswordError)
	}
	return errors.Is(err, sshx.ErrNoAuth) || sshx.Classify(err) == "auth"
}

// deleteHostSecrets deletes every secret the store keeps for the host
// with the ID, as it is deleted for good (deleteHostForever).
func (a *App) deleteHostSecrets(id string) {
	for _, name := range hostSecretNames {
		a.deleteSecret(hostSecretKey(id, name))
	}
}

// copyHostSecrets copies the secrets the store keeps for from to the
// same names of to, a copy of it (duplicateHost), when to remembers its
// passwords. Each is read and written in one job of the queue, so that
// what the user saves for to afterwards comes after it, and is kept.
func (a *App) copyHostSecrets(from, to *config.Host) {
	if !to.RemembersPassword() || a.secretsUnavailable() {
		return
	}
	for _, name := range hostSecretNames {
		src, dst := hostSecretKey(from.ID, name), hostSecretKey(to.ID, name)
		a.secrets.queue.do(func() {
			v, ok, err := a.plat.SecretGet(src)
			if err == nil && ok {
				err = a.plat.SecretSet(dst, v)
			}
			a.post(func() { a.noteStore(err) })
		})
	}
}

// rememberField is the editor's "Remember password" check box, or
// "Remember passphrase", and what it means: where the secret goes, or
// that it stays in memory as the system has no store.
func (a *App) rememberField(c *ui.Context, remember *bool, passphrase bool) {
	ui.Field(c, "", func() { a.rememberBox(c, remember, passphrase) }).Description(a.rememberNote(*remember))
}

// rememberBox is the check box alone, for the prompt; unchecked and
// disabled where the system has no store.
func (a *App) rememberBox(c *ui.Context, remember *bool, passphrase bool) ui.Element {
	label := locales.RememberPassword()
	if passphrase {
		label = locales.RememberPassphrase()
	}
	if a.secretsUnavailable() {
		off := false
		return ui.Checkbox(c, &off, label).Disabled(true)
	}
	return ui.Checkbox(c, remember, label)
}

// rememberNote says where a password goes, with remember or without.
func (a *App) rememberNote(remember bool) string {
	switch {
	case a.secretsUnavailable():
		return locales.SecretsUnavailable(storeName())
	case remember:
		return locales.SecretStoredNote(storeName())
	}
	return locales.SecretMemoryNote()
}

// storeName is what the system calls its credential store.
func storeName() string {
	switch runtime.GOOS {
	case "darwin", "ios":
		return locales.StoreKeychain()
	case "windows":
		return locales.StoreCredentialManager()
	}
	return locales.StoreSecretService()
}
