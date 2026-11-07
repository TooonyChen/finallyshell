package app

import (
	"fmt"
	"math"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// backgroundFits are the ways the Background image page offers to fit
// the image, with their names.
var backgroundFits = []struct {
	fit  string
	name func() string
}{
	{"", locales.BackgroundCover},
	{config.BackgroundContain, locales.BackgroundContain},
	{config.BackgroundTile, locales.BackgroundTile},
}

// backgroundPage is the Background image page: the image under the
// terminal, how it fits, and how much of it shows, over a preview.
func (a *App) backgroundPage(c *ui.Context) {
	st := &a.look
	ui.Form(c, func() {
		ui.Field(c, locales.BackgroundImage(), func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(96, 60).Shrink(0).Radius(4).Border(1, theme.Border).Background(a.termColors().Background).Clip().Center().Children(func() {
					if st.image != nil {
						ui.Image(c, st.image).Size(96, 60).Fit(ui.Cover)
					} else {
						ui.Text(c, locales.BackgroundNone()).FontSize(12).TextColor(a.termColors().Foreground.Alpha(0.7))
					}
				})
				if ui.Button(c, locales.ChooseImage()).Disabled(st.choosing).Clicked() {
					a.chooseBackground()
				}
				if ui.Button(c, locales.RemoveImage()).Disabled(a.settings.TermBackground == "").Clicked() {
					a.removeBackground()
				}
			})
		}).Error(a.imageErrorText()).Description(locales.BackgroundCopyNote())
		ui.Field(c, locales.BackgroundFit(), func() {
			chosen := a.settings.TermBackgroundFit
			ui.RadioGroup(c, func() {
				for _, f := range backgroundFits {
					ui.Radio(c, &chosen, f.fit, f.name())
				}
			}).Row().Gap(16).Label(locales.BackgroundFit()).Disabled(st.image == nil)
			if chosen != a.settings.TermBackgroundFit {
				a.changeTerminalSettings(func() { a.settings.TermBackgroundFit = chosen })
			}
		})
		ui.Field(c, locales.BackgroundOpacity(), func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				pct := float64(a.backgroundOpacity() * 100)
				if ui.Slider(c, &pct, 5, 100).Width(220).Label(locales.BackgroundOpacity()).Disabled(st.image == nil).Changed() {
					v := float32(math.Round(pct))
					a.changeTerminalSettings(func() { a.settings.TermBackgroundOpacity = orZero(v, config.DefaultBackgroundOpacity) })
				}
				pctText := ui.Text(c, fmt.Sprintf("%.0f%%", a.backgroundOpacity()*100)).Width(40)
				if st.image == nil {
					pctText.TextColor(theme.TextMuted)
				}
			})
		}).Description(locales.BackgroundOpacityHint())
	})
	a.lookPreview(c, false)
	s := a.settings
	a.lookFooter(c, locales.LookAppliesNow(), func() {
		a.settings.TermBackgroundFit, a.settings.TermBackgroundOpacity = "", 0
	}, s.TermBackgroundFit == "" && s.TermBackgroundOpacity == 0)
}

// imageErrorText says why the background image does not show, or "".
func (a *App) imageErrorText() string {
	if a.look.imageErr == nil {
		return ""
	}
	return describeImageError(a.look.imageErr)
}
