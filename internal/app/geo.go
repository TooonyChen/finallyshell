package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/geo"
	"finallyshell/internal/icons"
	"finallyshell/internal/sshx"
	"finallyshell/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// geoState is where IP addresses are, for the Network tab: ip2region's
// data, which is downloaded only when the user agrees and loaded off
// the UI thread, and the online lookups, which are off unless the user
// turns them on. Without the data nothing is looked up anywhere.
//
// Index 0 of the pairs is the IPv4 data, 1 the IPv6 data.
type geoState struct {
	db      [2]*geo.DB
	loading [2]bool
	missing [2]bool // no usable file on disk: looked for, or deleted
	notNow  [2]bool // "Not now", for this run
	sawV6   bool    // an IPv6 address to locate was shown
	dl      *geoDownload

	// The online lookups: the addresses to look up, those asked this
	// run, those the user asked for ("Look up online"), the cache, and
	// when to try again after an error.
	cache    *geo.Cache
	pending  map[string]bool
	asked    map[string]bool
	forced   map[string]bool
	looking  bool
	retryAt  time.Time
	stopLook context.CancelFunc

	size      int64 // bytes of the data on disk, for the options
	sizeKnown bool

	// What tests replace: the files, the HTTP client, the online
	// services.
	files  [2]geo.DataFile
	client *http.Client
	online *geo.Online
}

// geoDownload is a download of a data file, running or failed.
type geoDownload struct {
	v6          bool
	done, total int64
	err         error
	cancel      context.CancelFunc
}

func v6Index(v6 bool) int {
	if v6 {
		return 1
	}
	return 0
}

func (a *App) geoFile(v6 bool) geo.DataFile {
	if f := a.geo.files[v6Index(v6)]; len(f.URLs) > 0 {
		return f
	}
	if v6 {
		return geo.V6
	}
	return geo.V4
}

func (a *App) geoPath(v6 bool) string { return filepath.Join(config.GeoDir(), a.geoFile(v6).Name) }

// geoSetting is GeoLocations, or GeoLocationsV6.
func (a *App) geoSetting(v6 bool) *string {
	if v6 {
		return &a.settings.GeoLocationsV6
	}
	return &a.settings.GeoLocations
}

// locationsOn says whether the user wants IP locations: the IPv4 data is
// what turns them on.
func (a *App) locationsOn() bool { return a.settings.GeoLocations == config.GeoOn }

// locationsShown says whether the tables have a Location column: the
// user wants it, and the data is loaded.
func (a *App) locationsShown() bool { return a.locationsOn() && a.geo.db[0] != nil }

// geoTick loads the data the user wants, once, as a view that shows
// locations draws.
func (a *App) geoTick() {
	if !a.locationsOn() {
		return
	}
	for _, v6 := range []bool{false, true} {
		i := v6Index(v6)
		if v6 && a.settings.GeoLocationsV6 != config.GeoOn {
			continue
		}
		if a.geo.db[i] == nil && !a.geo.loading[i] && !a.geo.missing[i] && !a.geoDownloading(v6) {
			a.loadGeo(v6)
		}
	}
	a.kickOnline()
}

// loadGeo reads a data file on a goroutine, checking its checksum.
func (a *App) loadGeo(v6 bool) {
	i := v6Index(v6)
	a.geo.loading[i] = true
	path, sum := a.geoPath(v6), a.geoFile(v6).SHA256
	go func() {
		geo.CleanParts(filepath.Dir(path))
		db, err := geo.Load(path, sum)
		a.post(func() {
			a.geo.loading[i] = false
			if err != nil {
				a.geo.missing[i] = true
				return
			}
			if v6 && a.settings.GeoLocationsV6 != config.GeoOn {
				return // turned off while it loaded
			}
			a.geo.db[i], a.geo.missing[i] = db, false
		})
	}()
}

func (a *App) geoDownloading(v6 bool) bool {
	d := a.geo.dl
	return d != nil && d.v6 == v6 && d.err == nil
}

// downloadGeo downloads a data file on a goroutine, with its progress in
// the bar, and loads it. Asking for it is the user's yes: the setting is
// on from now.
func (a *App) downloadGeo(v6 bool) {
	if a.geo.dl != nil && a.geo.dl.err == nil {
		return
	}
	*a.geoSetting(v6) = config.GeoOn
	if v6 && a.settings.GeoLocations != config.GeoOn {
		a.settings.GeoLocations = config.GeoOn
	}
	a.saveSettings(a.settings)
	f := a.geoFile(v6)
	ctx, cancel := context.WithCancel(context.Background())
	d := &geoDownload{v6: v6, total: f.Size, cancel: cancel}
	a.geo.dl = d
	client, dir := a.geo.client, config.GeoDir()
	go func() {
		err := geo.Download(ctx, client, f, dir, func(done, total int64) {
			a.post(func() { d.done, d.total = done, total })
		})
		cancel()
		a.post(func() {
			if a.geo.dl != d {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					a.geo.dl = nil
					return
				}
				d.err = err
				return
			}
			a.geo.dl = nil
			i := v6Index(v6)
			a.geo.missing[i], a.geo.notNow[i], a.geo.sizeKnown = false, false, false
			a.loadGeo(v6)
		})
	}()
}

// cancelGeoDownload stops the download, as "Not now" for this run.
func (a *App) cancelGeoDownload() {
	if d := a.geo.dl; d != nil {
		if d.cancel != nil {
			d.cancel()
		}
		a.geo.notNow[v6Index(d.v6)] = true
		a.geo.dl = nil
	}
}

// geoNotNow hides the question until the app starts again.
func (a *App) geoNotNow(v6 bool) {
	if d := a.geo.dl; d != nil && d.v6 == v6 {
		a.cancelGeoDownload()
	}
	a.geo.notNow[v6Index(v6)] = true
}

// geoNever is "Don't ask again": off until turned on in the options.
func (a *App) geoNever(v6 bool) {
	a.geoNotNow(v6)
	a.changeSettings(func() { *a.geoSetting(v6) = config.GeoOff })
}

// setLocations turns IP locations on or off, from the options: on
// downloads the data that is not there; off keeps the files.
func (a *App) setLocations(on bool) {
	if !on {
		a.cancelGeoDownload()
		a.stopOnline()
		a.changeSettings(func() { a.settings.GeoLocations = config.GeoOff })
		return
	}
	a.geo.notNow = [2]bool{}
	a.changeSettings(func() { a.settings.GeoLocations = config.GeoOn })
	if a.geo.db[0] == nil && !a.geo.loading[0] {
		if _, err := os.Stat(a.geoPath(false)); err == nil {
			a.geo.missing[0] = false
			a.loadGeo(false)
			return
		}
		a.downloadGeo(false)
	}
}

// setLocationsV6 turns the IPv6 data on or off, from the options.
func (a *App) setLocationsV6(on bool) {
	if !on {
		if d := a.geo.dl; d != nil && d.v6 {
			a.cancelGeoDownload()
		}
		a.geo.db[1] = nil // the file stays, read again when turned on
		a.changeSettings(func() { a.settings.GeoLocationsV6 = config.GeoOff })
		return
	}
	a.geo.notNow[1] = false
	a.changeSettings(func() { a.settings.GeoLocationsV6 = config.GeoOn })
	if a.geo.db[1] == nil && !a.geo.loading[1] {
		if _, err := os.Stat(a.geoPath(true)); err == nil {
			a.geo.missing[1] = false
			a.loadGeo(true)
			return
		}
		a.downloadGeo(true)
	}
}

// askDeleteGeoData asks, then deletes the downloaded data and the cache
// of the online lookups, and turns locations off.
func (a *App) askDeleteGeoData() {
	q := Confirmation{
		Title:   locales.AppTitle(),
		Message: locales.GeoDeleteTitle(),
		Detail:  locales.GeoDeleteDetail(),
		OK:      locales.GeoDeleteYes(),
		Cancel:  locales.Cancel(),
	}
	go func() {
		if a.plat.Confirm(q) {
			a.post(a.deleteGeoData)
		}
	}()
}

func (a *App) deleteGeoData() {
	a.cancelGeoDownload()
	a.stopOnline()
	a.geo.db, a.geo.missing = [2]*geo.DB{}, [2]bool{true, true}
	a.geo.cache, a.geo.asked, a.geo.pending, a.geo.forced = nil, nil, nil, nil
	a.changeSettings(func() {
		a.settings.GeoLocations, a.settings.GeoLocationsV6, a.settings.GeoOnline = config.GeoOff, "", false
	})
	dir := config.GeoDir()
	go func() {
		os.RemoveAll(dir)
		a.post(func() { a.geo.size, a.geo.sizeKnown = 0, true })
	}()
}

// geoDataSize is the bytes of the data on disk, read on a goroutine the
// first time it is asked for, and after each change.
func (a *App) geoDataSize() (int64, bool) {
	if !a.geo.sizeKnown {
		a.geo.sizeKnown = true // asked; the answer follows
		a.geo.size = -1
		dir := config.GeoDir()
		go func() {
			var n int64
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
					n += info.Size()
				}
			}
			a.post(func() { a.geo.size = n })
		}()
	}
	return a.geo.size, a.geo.size >= 0
}

// placeOf is where ip is, as the Location column shows it: "LAN" for an
// address of a private range, else the data's answer, filled in with
// the online services' when they are on. queue looks up online what is
// missing; sorting by the column does not.
func (a *App) placeOf(ip netip.Addr, queue bool) string {
	ip = ip.Unmap()
	if geo.IsLAN(ip) {
		return locales.GeoLAN()
	}
	db := a.geo.db[v6Index(ip.Is6())]
	if db == nil || ip.Is6() && a.settings.GeoLocationsV6 != config.GeoOn {
		return ""
	}
	loc, _ := db.Lookup(ip)
	if a.settings.GeoOnline {
		s := ip.String()
		if online, ok := a.onlinePlace(s); ok {
			if a.geo.forced[s] {
				loc = online.Fill(loc)
			} else {
				loc = loc.Fill(online)
			}
		} else if queue && (!loc.Complete() || a.geo.forced[s]) {
			a.queueOnline(s)
		}
	}
	return loc.String()
}

func onlineLang() string { return geo.Lang(i18n.Locale()) }

func (a *App) onlinePlace(ip string) (geo.Location, bool) {
	if a.geo.cache == nil {
		return geo.Location{}, false
	}
	return a.geo.cache.Get(onlineLang(), ip)
}

// queueOnline looks ip up online, with the next request.
func (a *App) queueOnline(ip string) {
	if a.geo.asked[ip] || a.geo.pending[ip] {
		return
	}
	if a.geo.pending == nil {
		a.geo.pending = map[string]bool{}
	}
	a.geo.pending[ip] = true
}

// lookUpOnline is "Look up online": ip asked again, the online answer
// first.
func (a *App) lookUpOnline(ip netip.Addr) {
	s := ip.Unmap().String()
	if a.geo.forced == nil {
		a.geo.forced = map[string]bool{}
	}
	a.geo.forced[s] = true
	delete(a.geo.asked, s)
	a.queueOnline(s)
	a.kickOnline()
}

// kickOnline sends the addresses waiting to be looked up online, on a
// goroutine, one lookup at a time; geo.Online keeps to the services'
// limits. Nothing is sent unless the user turned it on.
func (a *App) kickOnline() {
	g := &a.geo
	if g.looking || len(g.pending) == 0 || !a.settings.GeoOnline || !a.locationsShown() || time.Now().Before(g.retryAt) {
		return
	}
	ips := slices.Sorted(maps.Keys(g.pending))
	forced := map[string]bool{}
	for _, ip := range ips {
		forced[ip] = g.forced[ip]
	}
	g.pending = nil
	if g.asked == nil {
		g.asked = map[string]bool{}
	}
	for _, ip := range ips {
		g.asked[ip] = true
	}
	if g.online == nil {
		g.online = &geo.Online{Client: g.client}
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.looking, g.stopLook = true, cancel
	cache, online, lang, path := g.cache, g.online, onlineLang(), filepath.Join(config.GeoDir(), "cache.json")
	go func() {
		defer cancel()
		if cache == nil {
			cache = geo.LoadCache(path)
		}
		var need []string
		for _, ip := range ips {
			if _, ok := cache.Get(lang, ip); !ok || forced[ip] {
				need = append(need, ip)
			}
		}
		var err error
		if len(need) > 0 {
			var got map[string]geo.Location
			got, err = online.Lookup(ctx, need, lang)
			for ip, loc := range got {
				cache.Put(lang, ip, loc)
			}
			cache.Save()
		}
		a.post(func() {
			if g.stopLook == nil {
				return // turned off meanwhile
			}
			g.looking, g.stopLook, g.cache = false, nil, cache
			if err != nil {
				// Those not answered are asked again, later.
				for _, ip := range need {
					if _, ok := cache.Get(lang, ip); !ok {
						delete(g.asked, ip)
					}
				}
				g.retryAt = time.Now().Add(30 * time.Second)
				return
			}
			a.kickOnline()
		})
	}()
}

// stopOnline stops the online lookup that runs, and forgets the queue.
func (a *App) stopOnline() {
	if a.geo.stopLook != nil {
		a.geo.stopLook()
	}
	a.geo.looking, a.geo.stopLook, a.geo.pending = false, nil, nil
}

// setOnline turns the online lookups on or off.
func (a *App) setOnline(on bool) {
	if !on {
		a.stopOnline()
		a.geo.asked = nil
	}
	a.changeSettings(func() { a.settings.GeoOnline = on })
}

// geoQuestion is what the bar above the Network tab's table asks or
// shows, if anything.
type geoQuestion int

const (
	geoNoBar geoQuestion = iota
	geoAskV4
	geoAskV6
	geoDownloadingBar
	geoFailedBar
)

func (a *App) geoBarState() geoQuestion {
	g := &a.geo
	switch {
	case g.dl != nil && g.dl.err != nil:
		return geoFailedBar
	case g.dl != nil:
		return geoDownloadingBar
	}
	// Not asked yet, or on with no file (deleted by hand, or another
	// device's settings).
	if v4 := a.settings.GeoLocations; !g.notNow[0] && !g.loading[0] && (v4 == "" || v4 == config.GeoOn && g.missing[0]) {
		return geoAskV4
	}
	if v6 := a.settings.GeoLocationsV6; a.locationsShown() && g.sawV6 && g.db[1] == nil && !g.notNow[1] && !g.loading[1] &&
		(v6 == "" || v6 == config.GeoOn && g.missing[1]) {
		return geoAskV6
	}
	return geoNoBar
}

// geoBar is the bar above the Network tab's table that asks to download
// the location data, shows the download, or why it failed.
func (a *App) geoBar(c *ui.Context) {
	state := a.geoBarState()
	if state == geoNoBar {
		return
	}
	ui.Row(c.Key("geo-bar")).Padding(5, 8, 5, 10).Gap(8).AlignItems(ui.Center).Background(theme.Accent.Alpha(0.08)).
		BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
		ui.Icon(c, icons.Get("map-pin")).FontSize(14).TextColor(theme.Accent).Shrink(0)
		switch state {
		case geoAskV4, geoAskV6:
			v6 := state == geoAskV6
			text := locales.GeoAskV4(geoDownloadSize(false))
			if v6 {
				text = locales.GeoAskV6(geoDownloadSize(true))
			}
			ui.Text(c, text).Shrink(1).MinWidth(0).SingleLine().Tooltip(text)
			ui.Spacer(c)
			if ui.PrimaryButton(c, locales.GeoDownload()).Clicked() {
				a.downloadGeo(v6)
			}
			if ui.Button(c, locales.GeoNotNow()).Clicked() {
				a.geoNotNow(v6)
			}
			if ui.Button(c, locales.GeoNever()).Clicked() {
				a.geoNever(v6)
			}
		case geoDownloadingBar:
			d := a.geo.dl
			pct := 0.0
			if d.total > 0 {
				pct = float64(d.done) / float64(d.total)
			}
			ui.Text(c, locales.GeoDownloading(int(pct*100))).SingleLine().Shrink(0)
			ui.Progress(c, pct).Width(200).Shrink(1).Label(locales.GeoDownloading(int(pct * 100)))
			ui.Spacer(c)
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.cancelGeoDownload()
			}
		case geoFailedBar:
			d := a.geo.dl
			msg := locales.GeoDownloadFailed(geoErrorText(d.err))
			ui.Text(c, msg).TextColor(theme.Danger).Shrink(1).MinWidth(0).SingleLine().Tooltip(msg)
			ui.Spacer(c)
			if ui.PrimaryButton(c, locales.Retry()).Clicked() {
				v6 := d.v6
				a.geo.dl = nil
				a.downloadGeo(v6)
			}
			if ui.Button(c, locales.GeoNotNow()).Clicked() {
				a.geoNotNow(d.v6)
			}
		}
	})
}

// geoErrorText is why a download failed: for each source, its host and
// what it answered ("raw.githubusercontent.com: connection refused;
// cdn.jsdelivr.net: 404 Not Found").
func geoErrorText(err error) string {
	var de *geo.DownloadError
	if !errors.As(err, &de) {
		return geoSourceText(err)
	}
	msgs := make([]string, len(de.Sources))
	for i, s := range de.Sources {
		msgs[i] = locales.GeoSourceFailed(s.Host, geoSourceText(s.Err))
	}
	return strings.Join(msgs, locales.GeoSourcesSeparator())
}

// geoSourceText is why the download from one source failed.
func geoSourceText(err error) string {
	var st *geo.StatusError
	var ue *url.Error
	switch {
	case errors.Is(err, geo.ErrChecksum):
		return locales.GeoChecksum()
	case errors.Is(err, geo.ErrNoAnswer):
		return locales.GeoNoAnswer()
	case errors.As(err, &st):
		return st.Status
	}
	switch sshx.Classify(err) {
	case "refused":
		return locales.GeoRefused()
	case "timeout":
		return locales.GeoNoAnswer()
	case "nohost":
		return locales.GeoNoHost()
	}
	if errors.As(err, &ue) {
		err = ue.Err // without the URL, which the host already says
	}
	return strings.TrimSpace(err.Error())
}

// geoDownloadSize is about how much a data file downloads, as the
// question and the options say it: "11 MB", of the real file (tests
// serve smaller ones).
func geoDownloadSize(v6 bool) string {
	n := geo.V4.Size
	if v6 {
		n = geo.V6.Size
	}
	return fmt.Sprintf("%d MB", (n+500_000)/1_000_000)
}
