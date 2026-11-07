package app

// fullScreenState is the app's full screen, which shows the terminal
// area alone, without the sidebar and the files, in a window in the
// system's full screen.
type fullScreenState struct {
	on bool
	// ownsWindow tells that entering put the window in the system's full
	// screen, for leaving to take it out: a window the user had put there
	// stays.
	ownsWindow bool
}

// fullScreener is a platform that puts the main window in the system's
// full screen; the app's full screen hides the panes without it.
type fullScreener interface {
	SetFullScreen(on bool)
	IsFullScreen() bool
}

// setFullScreen enters the app's full screen or leaves it. The key of
// full screen does both; Escape leaves too where no shell takes it.
func (a *App) setFullScreen(on bool) {
	f := &a.fullScreen
	if f.on == on {
		return
	}
	f.on = on
	p, ok := a.plat.(fullScreener)
	switch {
	case !ok:
	case on && !p.IsFullScreen():
		f.ownsWindow = true
		p.SetFullScreen(true)
	case !on && f.ownsWindow:
		f.ownsWindow = false
		p.SetFullScreen(false)
	}
}

// WindowLeftFullScreen tells the app that the main window left the
// system's full screen, by the system's means, as macOS's green button:
// the app's full screen that put it there ends with it.
func (a *App) WindowLeftFullScreen() {
	if f := &a.fullScreen; f.on && f.ownsWindow {
		f.ownsWindow = false
		a.setFullScreen(false)
		a.redraw()
	}
}

// terminalLive tells whether the selected session's shell is there to
// take keys.
func (a *App) terminalLive() bool {
	s := a.current()
	return s != nil && !s.isNewTab() && s.status == statusConnected && s.term != nil
}
