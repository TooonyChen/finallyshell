package app

import (
	"finallyshell/internal/config"
	"finallyshell/locales"
)

// "Connect all" on a folder's context menu, in the connection manager
// and on the Connections page, opens a session to every connection in
// the folder and in its subfolders, one after another, each as a double
// click opens it.

// connectAllAsk is how many connections "Connect all" opens without
// asking first.
const connectAllAsk = 10

// folderHosts are the connections, not deleted, in folder and in its
// subfolders, in the order the tree of m shows them, its search aside:
// the subfolders first, then the folder's own.
func (a *App) folderHosts(m *managerState, folder string) []*config.Host {
	t := a.newManagerTree(&managerState{sort: m.sort})
	var out []*config.Host
	var walk func(it managerItem)
	walk = func(it managerItem) {
		if it.isHost() {
			if h := a.hosts.Host(it.host); h != nil && !h.Deleted() {
				out = append(out, h)
			}
			return
		}
		for _, child := range t.children(it) {
			walk(child)
		}
	}
	walk(managerItem{folder: folder})
	return out
}

// connectAll opens a session to every connection in folder and its
// subfolders, from the tree of m, asking first in its window when there
// are more than connectAllAsk.
func (a *App) connectAll(m *managerState, folder string) {
	f := a.hosts.Folder(folder)
	hosts := a.folderHosts(m, folder)
	if f == nil || len(hosts) == 0 {
		return
	}
	if len(hosts) <= connectAllAsk {
		a.connectHostsFrom(m, hosts)
		return
	}
	n := len(hosts)
	m.question = &managerQuestion{
		title: func() string { return locales.ConnectAllTitle(n) },
		body:  func() string { return locales.ConnectAllBody(f.Name) },
		ok:    locales.Connect,
		yes:   func() { a.connectHostsFrom(m, hosts) },
	}
}

// connectHostsFrom opens sessions to hosts from the tree of m, as
// connectFrom opens one: from the manager, it closes unless kept open
// and the main window comes forward.
func (a *App) connectHostsFrom(m *managerState, hosts []*config.Host) {
	if m.win == WindowManager && !a.settings.KeepManagerOpen {
		a.closeWindow(WindowManager)
	}
	a.connectEach(a.selectedNewTab(), hosts)
	if m.win == WindowManager {
		a.plat.FocusWindow(WindowMain)
	}
	a.redraw()
}

// connectEach opens a session to each of hosts in turn, as openSessionIn
// does, the first in tab when it is still a new tab. A host with a
// password to ask for waits for the prompt, so that the prompts come one
// at a time; cancelling one skips its host and goes on with the next.
func (a *App) connectEach(tab *Session, hosts []*config.Host) {
	defer a.redraw()
	for len(hosts) > 0 {
		h := hosts[0]
		if h.Deleted() || a.hosts.Host(h.ID) != h {
			hosts = hosts[1:]
			continue
		}
		again := func() { a.connectEach(tab, hosts) }
		if a.readChainSecrets(h, again) {
			return
		}
		if need := a.missingSecret(h); need != nil {
			rest := hosts[1:]
			a.askSecret(need, again)
			a.pwCancel = func() { a.connectEach(tab, rest) }
			return
		}
		a.openSessionIn(tab, h)
		tab, hosts = nil, hosts[1:]
	}
}

// passwordCancelled runs what the prompt was to do on Cancel, once the
// prompt has closed without a password.
func (a *App) passwordCancelled() {
	if a.pwOpen || a.pwCancel == nil {
		return
	}
	cancel := a.pwCancel
	a.pwCancel = nil
	cancel()
}
