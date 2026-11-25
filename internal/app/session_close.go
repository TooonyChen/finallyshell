package app

import (
	"slices"

	"finallyshell/locales"
	"github.com/egoist/mygo/ui"
)

// sessionOwner groups an independent panel with the connection it displays.
func sessionOwner(s *Session) *Session {
	if s.isPanelTab() {
		return s.panelOf
	}
	return s
}

func (a *App) hasOtherSessions(keep *Session) bool {
	owner := sessionOwner(keep)
	for _, s := range a.sessions {
		if sessionOwner(s) != owner {
			return true
		}
	}
	return false
}

// sessionGroupMenu records a request; the list executes it after building rows.
// Without a row target the blank-area menu offers Close all only.
func (a *App) sessionGroupMenu(m *ui.Menu, keep *Session, closeGroup func(*Session)) {
	if keep != nil && m.Item(locales.CloseOtherSessions()).Danger().Disabled(!a.hasOtherSessions(keep)).Chosen() {
		closeGroup(keep)
	}
	if m.Item(locales.CloseAllSessions()).Danger().Disabled(len(a.sessions) == 0).Chosen() {
		closeGroup(nil)
	}
}

// closeSessionGroup closes every row except keep's connection and its panel.
// A nil target closes all rows; it does not run the application's shutdown path.
func (a *App) closeSessionGroup(keep *Session) {
	if keep != nil && !slices.Contains(a.sessions, keep) {
		return
	}
	owner := sessionOwner(keep)
	// Give the existing close path a chance to cancel rowless pending launches.
	// A nil session has no row to remove (for example, a new UAC terminal).
	a.closeSession(nil)
	// Closing an owner also removes its panel. Iterate a snapshot, not the slice
	// being shortened, and let closeSession handle transfers and disconnection.
	for _, s := range slices.Clone(a.sessions) {
		if keep == nil || sessionOwner(s) != owner {
			a.closeSession(s)
		}
	}
	if keep != nil {
		a.active = slices.Index(a.sessions, keep)
	}
	if a.rename.s != nil && !slices.Contains(a.sessions, a.rename.s) {
		a.endRename(false)
	}
}
