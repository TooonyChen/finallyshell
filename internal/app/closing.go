package app

import (
	"strings"

	"finallyshell/locales"
)

// Confirmation is a question with two answers, which the platform asks in
// a message box of the system's.
type Confirmation struct {
	Title, Message, Detail string
	// OK is the button that goes ahead, Cancel the one that does not, which
	// Escape and closing the box answer too.
	OK, Cancel string
}

// The main window's close button and quitting, as the General options say:
//
//	                    close button                 Quit (tray menu, Cmd+Q, ...)
//	tray on             hide to the tray             ask if sessions are open*
//	tray off or none    ask if sessions are open*,   ask if sessions are open*,
//	                    then quit                    then quit
//
// * unless "Confirm before closing" is off. A system without a tray (Linux
// without AppIndicator) closes as with the tray off.

// CloseRequested tells whether the main window may close now, as the user
// asks with its close button: the platform calls it on the UI thread, and
// keeps the window open when it returns false. The app then has hidden
// the window to the tray, or is asking whether to quit, and quits itself
// if the user says yes.
func (a *App) CloseRequested() bool {
	if a.quitApproved {
		return true // the quit closes the windows
	}
	if a.settings.MinimizeToTray && a.plat.HideToTray() {
		a.closeWindows()
		return false
	}
	return a.QuitRequested()
}

// QuitRequested tells whether the app may quit now, as the tray's Quit,
// the system's (Cmd+Q on macOS) or the last window closing asks: the
// platform calls it on the UI thread before the quit closes any window.
// With sessions open it asks first, unless the options say not to, and
// returns false; the app quits itself if the user says yes. Files with
// changes in the editor it asks about whatever the options say, as the
// changes would be lost.
func (a *App) QuitRequested() bool {
	if a.quitApproved {
		return true
	}
	n, unsaved := a.liveSessions(), a.unsavedFiles()
	if n == 0 && unsaved == 0 || a.settings.NoConfirmClose && unsaved == 0 {
		a.quitApproved = true
		return true
	}
	if !a.askingQuit {
		a.askingQuit = true
		var detail []string
		if remote, local := a.localQuitCounts(); n > 0 && !a.settings.NoConfirmClose {
			if remote > 0 {
				detail = append(detail, locales.ConfirmQuitBody(remote))
			}
			if local > 0 {
				detail = append(detail, locales.ConfirmQuitLocal(local))
			}
		}
		if unsaved > 0 {
			detail = append(detail, locales.ConfirmQuitUnsaved(unsaved))
		}
		q := Confirmation{
			Title:   locales.AppTitle(),
			Message: locales.ConfirmQuitTitle(),
			Detail:  strings.Join(detail, "\n"),
			OK:      locales.ConfirmQuitOK(),
			Cancel:  locales.Cancel(),
		}
		go func() {
			yes := a.plat.Confirm(q)
			a.post(func() {
				a.askingQuit = false
				if yes {
					a.quitApproved = true
					a.plat.Quit()
				}
			})
		}()
	}
	return false
}

// liveSessions counts the sessions that are connected or connecting,
// which quitting ends: local terminals whose program runs too.
func (a *App) liveSessions() int {
	n := 0
	for _, s := range a.sessions {
		if s.status == statusConnected || s.status == statusConnecting {
			n++
		}
	}
	return n
}
