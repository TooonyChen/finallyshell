package app

import (
	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// networkPage is the Network monitor page: IP locations, from the data
// the user downloads and, if turned on, online; and what Trace Route
// runs. The explanations are here rather than in the tab.
func (a *App) networkPage(c *ui.Context) {
	on := a.locationsOn()
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.OptionGeoHeading()).Bold()
		show := on
		if ui.Checkbox(c, &show, locales.OptionShowLocations(geoDownloadSize(false))).Changed() {
			a.setLocations(show)
		}
		widgets.Muted(c, locales.OptionShowLocationsNote()).FontSize(12).PaddingStart(18)
		v6 := a.settings.GeoLocationsV6 == config.GeoOn
		if ui.Checkbox(c, &v6, locales.OptionLocationsV6(geoDownloadSize(true))).Disabled(!on).Changed() {
			a.setLocationsV6(v6)
		}
		if a.geo.dl != nil {
			ui.Row(c).PaddingStart(18).Children(func() { a.geoBar(c) })
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
			size, known := a.geoDataSize()
			switch {
			case known && size > 0:
				ui.Text(c, locales.OptionGeoData(formatSize(size)))
			case known:
				widgets.Muted(c, locales.OptionGeoNoData())
			}
			if ui.Button(c, locales.GeoDeleteData()).Disabled(!known || size == 0).Clicked() {
				a.askDeleteGeoData()
			}
		})
		widgets.Muted(c, locales.GeoCredit()).FontSize(12)
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		online := a.settings.GeoOnline
		if ui.Checkbox(c, &online, locales.OptionGeoOnline()).Disabled(!on).Changed() {
			a.setOnline(online)
		}
		widgets.Muted(c, locales.OptionGeoOnlineNote()).FontSize(12).PaddingStart(18)
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.OptionTraceHeading()).Bold()
		widgets.Muted(c, locales.OptionTraceNote()).FontSize(12)
	})
}
