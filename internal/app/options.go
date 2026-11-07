package app

import (
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// optionsWindow is how the options window opens: FinalShell's 选项, a tree
// of pages on the left and the page chosen on the right.
var optionsWindow = WindowSpec{
	Kind:  WindowOptions,
	Title: locales.Options,
	Width: 760, Height: 600,
	MinWidth: 560, MinHeight: 400,
}

// The pages of the options window.
const (
	pageGeneral    = "general"
	pageTransfers  = "transfers"
	pageTerminal   = "terminal"
	pageMouse      = "terminal/mouse"
	pageShortcuts  = "terminal/shortcuts"
	pageColors     = "terminal/colors"
	pageBackground = "terminal/background"
	pageFont       = "terminal/font"
	pageFiles      = "files"
	pageFileIcons  = "files/icons"
)

// optionPage is a page of the options window.
type optionPage struct {
	id     string
	parent string // the page whose entry this one's sits under, "" at the top
	title  func() string
	// view builds the page below its title; nil shows that it is still to
	// come.
	view func(a *App, c *ui.Context)
}

// optionPages are the options window's pages, in the order of its tree,
// each parent before its children. A new page is a line here and a view
// of its own; one still to come has no view yet. Each page is an entry of
// several lines so that pages added side by side change lines apart.
var optionPages = []optionPage{
	{
		id: pageGeneral, title: locales.OptionsGeneral,
		view: (*App).generalPage,
	},
	{
		id: pageTransfers, title: locales.OptionsTransfers,
		view: (*App).transfersPage,
	},
	{
		id: pageTerminal, title: locales.OptionsTerminal,
		view: nil,
	},
	{
		id: pageMouse, parent: pageTerminal, title: locales.OptionsMouse,
		view: (*App).mousePage,
	},
	{
		id: pageShortcuts, parent: pageTerminal, title: locales.OptionsShortcuts,
		view: (*App).shortcutsPageView,
	},
	{
		id: pageColors, parent: pageTerminal, title: locales.OptionsColorScheme,
		view: (*App).colorsPage,
	},
	{
		id: pageBackground, parent: pageTerminal, title: locales.OptionsBackground,
		view: (*App).backgroundPage,
	},
	{
		id: pageFont, parent: pageTerminal, title: locales.OptionsFont,
		view: (*App).fontPage,
	},
	{
		id: pageFiles, title: locales.OptionsFileManager,
		view: nil,
	},
	{
		id: pageFileIcons, parent: pageFiles, title: locales.OptionsFileIcons,
		view: (*App).fileIconsPage,
	},
}

// openOptions opens the options window, or brings it to the front.
func (a *App) openOptions() { a.openWindow(optionsWindow, a.optionsView) }

// optionsView is the options window: the pages, and Done below them.
// The options apply as they change, so there is nothing to confirm.
func (a *App) optionsView(c *ui.Context) {
	pages := make([]widgets.NavPage, len(optionPages))
	for i, p := range optionPages {
		pages[i] = widgets.NavPage{ID: p.id, Parent: p.parent, Title: p.title, View: func(c *ui.Context) {
			if p.view == nil {
				widgets.Muted(c, locales.OptionsComingLater())
				return
			}
			p.view(a, c)
		}}
	}
	ui.Column(c).Fill().Children(func() {
		widgets.NavPages(c, &a.optionsNav, pages).Grow(1).MinHeight(0)
		ui.Row(c).Padding(10, 14).Justify(ui.End).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
			if ui.PrimaryButton(c, locales.Done()).Clicked() {
				a.closeWindow(WindowOptions)
			}
		})
	})
}

// generalPage is the General page: the language, how tabs open, and what
// closing the window does.
func (a *App) generalPage(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.SettingsLanguage()).Bold()
		chosen := a.settings.Language
		ui.RadioGroup(c, func() {
			ui.Radio(c, &chosen, "", locales.FollowSystem())
			for _, tag := range a.plat.Languages() {
				ui.Radio(c, &chosen, tag, i18n.Autonym(tag))
			}
		}).Row().Gap(16).Label(locales.SettingsLanguage())
		if chosen != a.settings.Language {
			a.setLanguage(chosen)
		}
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.OptionsTabsAndWindow()).Bold()
		autoSelect := !a.settings.NoAutoSelectTab
		if ui.Checkbox(c, &autoSelect, locales.OptionAutoSelectTab()).Changed() {
			a.changeSettings(func() { a.settings.NoAutoSelectTab = !autoSelect })
		}
		tray := a.settings.MinimizeToTray
		if ui.Checkbox(c, &tray, locales.OptionMinimizeToTray()).Changed() {
			a.changeSettings(func() { a.settings.MinimizeToTray = tray })
		}
		confirm := !a.settings.NoConfirmClose
		if ui.Checkbox(c, &confirm, locales.OptionConfirmClose()).Changed() {
			a.changeSettings(func() { a.settings.NoConfirmClose = !confirm })
		}
		widgets.Muted(c, locales.OptionConfirmCloseNote()).FontSize(12).PaddingStart(18)
	})
}

// changeSettings changes the settings with fn, saves them and shows the
// change in every window.
func (a *App) changeSettings(fn func()) {
	fn()
	a.saveSettings(a.settings)
	a.redraw()
}
