package app

import (
	"strings"

	"finallyshell/internal/cloudsync"
	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// gistTokenURL opens GitHub's page for a new token with the gist scope.
const gistTokenURL = "https://github.com/settings/tokens/new?scopes=gist&description=Finallyshell%20sync"

// syncProviders are the Sync page's choices of where to sync.
var syncProviders = []choice[config.SyncProvider]{
	{config.SyncOff, locales.SyncOff},
	{config.SyncGist, locales.SyncGist},
	{config.SyncWebDAV, locales.SyncWebDAV},
	{config.SyncS3, locales.SyncS3},
}

// syncIntervals are the minutes automatic sync can wait between syncs.
var syncIntervals = []int{5, 15, 30, 60}

// syncPage is Options › Sync: where to sync, its credentials, the sync
// password, what to include, when to sync, and syncing now.
func (a *App) syncPage(c *ui.Context) {
	s := a.settings.Sync
	ui.Column(c).Gap(8).Children(func() {
		widgets.Muted(c, locales.SyncIntro()).FontSize(12)
	})
	if s.Provider != config.SyncOff {
		a.loadSyncSecrets()
		if !a.sync.stateLoaded {
			a.sync.state, a.sync.stateLoaded = config.LoadSyncState(), true
		}
	}
	ui.Form(c, func() {
		ui.Field(c, locales.SyncService(), func() {
			p := s.Provider
			if choiceSelect(c, locales.SyncService(), &p, syncProviders, nil) {
				a.changeSettings(func() { a.settings.Sync.Provider = p })
				a.sync.err, a.sync.done, a.sync.confirmReplace = nil, nil, false
			}
		})
		if s.Provider == config.SyncOff {
			return
		}
		switch s.Provider {
		case config.SyncGist:
			a.syncCredentialField(c, locales.SyncGistToken(), locales.SyncGistTokenHint())
			ui.Field(c, "", func() { ui.Link(c, locales.SyncGistNewToken(), gistTokenURL) })
			a.syncTextField(c, locales.SyncGistID(), locales.SyncGistIDPlaceholder(), s.GistID,
				func(v string) { a.settings.Sync.GistID = v }).Description(locales.SyncGistIDHint())
		case config.SyncWebDAV:
			a.syncTextField(c, locales.SyncWebDAVURL(), "https://dav.jianguoyun.com/dav/finallyshell/", s.WebDAVURL,
				func(v string) { a.settings.Sync.WebDAVURL = v })
			a.syncTextField(c, locales.SyncUser(), "", s.WebDAVUser, func(v string) { a.settings.Sync.WebDAVUser = v })
			a.syncCredentialField(c, locales.SyncWebDAVPassword(), locales.SyncWebDAVPasswordHint())
		case config.SyncS3:
			a.syncTextField(c, locales.SyncS3Endpoint(), "https://<account>.r2.cloudflarestorage.com", s.S3Endpoint,
				func(v string) { a.settings.Sync.S3Endpoint = v }).Description(locales.SyncS3EndpointHint())
			a.syncTextField(c, locales.SyncS3Region(), "us-east-1", s.S3Region, func(v string) { a.settings.Sync.S3Region = v })
			a.syncTextField(c, locales.SyncS3Bucket(), "", s.S3Bucket, func(v string) { a.settings.Sync.S3Bucket = v })
			a.syncTextField(c, locales.SyncS3Key(), cloudsync.FileName, s.S3Key, func(v string) { a.settings.Sync.S3Key = v })
			a.syncTextField(c, locales.SyncS3AccessKey(), "", s.S3AccessKeyID, func(v string) { a.settings.Sync.S3AccessKeyID = v })
			a.syncCredentialField(c, locales.SyncS3SecretKey(), "")
		}
		ui.Field(c, locales.SyncPassword(), func() {
			pw := a.sync.password
			if ui.TextInput(c, &pw).Password().Width(260).Label(locales.SyncPassword()).Changed() {
				a.setSyncPassword(pw)
			}
		}).Description(locales.SyncPasswordHint())
	})

	if s.Provider == config.SyncOff {
		return
	}
	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		withPw := s.Passwords
		if ui.Checkbox(c, &withPw, locales.SyncIncludePasswords()).Changed() {
			a.changeSettings(func() { a.settings.Sync.Passwords = withPw })
		}
		widgets.Muted(c, locales.SyncIncludePasswordsHint()).FontSize(12).PaddingStart(18)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			auto := s.Auto
			if ui.Checkbox(c, &auto, locales.SyncAutomatically()).Changed() {
				a.changeSettings(func() { a.settings.Sync.Auto = auto })
				a.sync.nextCheck = a.sync.lastTry // check at once
			}
			labels := make([]string, len(syncIntervals))
			chosen := ""
			for i, m := range syncIntervals {
				labels[i] = locales.SyncEveryMinutes(m)
				if m == s.IntervalOrDefault() {
					chosen = labels[i]
				}
			}
			if ui.Select(c, &chosen, labels).Width(160).Label(locales.SyncAutomatically()).Disabled(!s.Auto).Changed() {
				for i, l := range labels {
					if l == chosen {
						m := syncIntervals[i]
						a.changeSettings(func() { a.settings.Sync.Interval = m })
					}
				}
			}
		})
		widgets.Muted(c, locales.SyncAutomaticallyHint()).FontSize(12).PaddingStart(18)
	})

	ui.Column(c).Gap(8).Padding(12, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ready := a.syncReady()
			if ui.PrimaryButton(c, locales.SyncNow()).Disabled(!ready || a.sync.running).Clicked() {
				a.startSync(false, c.Now())
			}
			a.syncStatus(c, ready)
		})
		if !a.sync.confirmReplace {
			ui.Row(c).Children(func() {
				if ui.Button(c, locales.SyncReplaceRemote()).Disabled(!a.syncReady() || a.sync.running).Clicked() {
					a.sync.confirmReplace = true
				}
			})
			return
		}
		widgets.Muted(c, locales.SyncReplaceRemoteConfirm())
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, locales.SyncReplaceRemoteYes()).Clicked() {
				a.startSync(true, c.Now())
			}
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.sync.confirmReplace = false
			}
		})
	})
}

// syncStatus says what sync is doing, did last, or lacks.
func (a *App) syncStatus(c *ui.Context, ready bool) {
	st := &a.sync
	text, color := "", theme.TextMuted
	switch {
	case st.running:
		text = locales.SyncRunning()
	case st.err != nil:
		text, color = syncErrorText(st.err), theme.Danger
	case !ready && st.loaded:
		text = locales.SyncIncomplete()
	case st.done != nil || !st.state.At.IsZero():
		text = a.syncDoneText()
	}
	if text != "" {
		ui.Text(c, text).TextColor(color).FontSize(12).Grow(1).MinWidth(0)
	}
}

// syncDoneText is what the last sync did, and when.
func (a *App) syncDoneText() string {
	st := &a.sync
	at := st.state.At.Local().Format("2006-01-02 15:04")
	var parts []string
	if r := st.done; r != nil {
		switch {
		case r.Created:
			parts = append(parts, locales.SyncCreated())
		case r.Changed && r.Uploaded:
			parts = append(parts, locales.SyncMerged())
		case r.Changed:
			parts = append(parts, locales.SyncDownloaded())
		case r.Uploaded:
			parts = append(parts, locales.SyncUploaded())
		default:
			parts = append(parts, locales.SyncUpToDate())
		}
		if r.Conflicts > 0 {
			parts = append(parts, locales.SyncConflicts(r.Conflicts))
		}
	}
	return strings.Join(append([]string{locales.SyncLastSynced(at)}, parts...), " · ")
}

// syncTextField is a field of the Sync page's settings, set with set as
// it is typed.
func (a *App) syncTextField(c *ui.Context, label, placeholder, value string, set func(string)) ui.Element {
	return ui.Field(c, label, func() {
		v := value
		if ui.TextInput(c, &v).Placeholder(placeholder).Width(360).Label(label).Changed() {
			a.changeSettings(func() { set(strings.TrimSpace(v)) })
		}
	})
}

// syncCredentialField is the provider's credential, kept in the
// credential store as it is typed.
func (a *App) syncCredentialField(c *ui.Context, label, hint string) {
	f := ui.Field(c, label, func() {
		v := ""
		if a.sync.credFor == a.settings.Sync.Provider {
			v = a.sync.credential
		}
		if ui.TextInput(c, &v).Password().Width(360).Label(label).Changed() {
			a.setSyncCredential(strings.TrimSpace(v))
		}
	})
	if hint != "" {
		f.Description(hint)
	}
}
