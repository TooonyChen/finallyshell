package app

import (
	"os"
	"runtime"
	"slices"
	"time"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// downloadsPopover is the Downloads popover at the top right of the file
// panel, as FinalShell's: the download folder, and the transfers of the
// queue, newest first. The Transfers tab shows the same jobs, oldest
// first, with the same rows (transferRow).
type downloadsPopover struct {
	open     bool
	choosing bool // the folder dialog is open
}

// taskbarState is what the app's icon shows of the transfers
// (Platform.ShowTransfers), and when it last changed.
type taskbarState struct {
	count    int
	progress float64
	at       time.Time
}

// taskbarEvery is how often at most the app's icon shows new progress; a
// new count shows at once.
const taskbarEvery = 250 * time.Millisecond

// popoverWidth is the width of the Downloads popover.
const popoverWidth = 480

// downloadDir is where downloads go unless the user chooses: the folder
// of the settings, else the system's downloads folder.
func (a *App) downloadDir() string {
	if dir := a.settings.DownloadDir; dir != "" {
		return dir
	}
	return a.plat.DownloadsDir()
}

// setDownloadDir makes dir the download folder, kept as "" when it is the
// system's, the default.
func (a *App) setDownloadDir(dir string) {
	if dir == a.plat.DownloadsDir() {
		dir = ""
	}
	a.changeSettings(func() { a.settings.DownloadDir = dir })
}

// chooseDownloadDir asks for a new download folder over the window of
// parent.
func (a *App) chooseDownloadDir(parent WindowKind) {
	p := &a.transfers.popover
	if p.choosing {
		return
	}
	p.choosing = true
	title := locales.ChooseDefaultDownloadFolder()
	go func() {
		dir, err := a.plat.ChooseFolder(parent, title)
		a.post(func() {
			p.choosing = false
			if err == nil && dir != "" {
				a.setDownloadDir(dir)
			}
		})
	}()
}

// showDownloadDir opens the download folder in the file manager, making
// it first when it is not there yet.
func (a *App) showDownloadDir() {
	dir := a.downloadDir()
	os.MkdirAll(dir, 0o755)
	a.plat.OpenPath(dir)
}

// showFolderLabel names the file manager's command on goos.
func showFolderLabel(goos string) string {
	switch goos {
	case "darwin":
		return locales.ShowInFinder()
	case "windows":
		return locales.ShowInExplorer()
	}
	return locales.ShowInFolder()
}

// overall tells how many transfers wait or run, and the fraction done of
// the bytes of those running that know their size: -1 when none does.
func (q *transferQueue) overall() (count int, progress float64) {
	var done, total int64
	known := false
	for _, t := range q.jobs {
		if t.finished() {
			continue
		}
		count++
		if t.state == transferRunning && t.total >= 0 {
			known = true
			done += min(t.done, t.total)
			total += t.total
		}
	}
	switch {
	case count == 0:
		return 0, 0
	case !known:
		return count, -1
	case total == 0: // empty files
		return count, 1
	}
	return count, float64(done) / float64(total)
}

// showTransfers shows the transfers under way on the app's icon, as the
// queue changes: a new count at once, new progress every taskbarEvery
// at most.
func (a *App) showTransfers() { a.showTransfersAt(time.Now()) }

func (a *App) showTransfersAt(now time.Time) {
	shown := &a.transfers.shown
	count, progress := a.transfers.overall()
	if count == shown.count && (progress == shown.progress || now.Sub(shown.at) < taskbarEvery) {
		return
	}
	*shown = taskbarState{count, progress, now}
	a.plat.ShowTransfers(count, progress)
}

// cancelTransfer stops t, waiting or running.
func (a *App) cancelTransfer(t *transferJob) {
	t.stop()
	a.showTransfers()
}

// canRetry tells whether t failed or was canceled, and its session is
// still open and connected, to run it again.
func (a *App) canRetry(t *transferJob) bool {
	s := t.sess
	return (t.state == transferFailed || t.state == transferCanceled) &&
		slices.Contains(a.sessions, s) && s.status == statusConnected && s.sftp != nil
}

// retry queues t again, the same files to the same place, over its
// session's connection as it is now (a new one after a reconnect). It
// moves to the end of the queue, the newest.
func (a *App) retry(t *transferJob) {
	q := &a.transfers
	i := slices.Index(q.jobs, t)
	if i < 0 || !a.canRetry(t) {
		return
	}
	q.jobs = slices.Delete(q.jobs, i, i+1)
	t.sftp = t.sess.sftp
	t.state, t.err, t.local, t.cancel = transferWaiting, nil, "", nil
	t.done, t.total, t.speed, t.sampleDone = 0, -1, 0, 0
	q.jobs = append(q.jobs, t)
	a.pump()
	a.showTransfers()
}

// openDownload opens a finished download with the system's app for it,
// or shows it in its folder when there is none.
func (a *App) openDownload(t *transferJob) {
	if t.local == "" {
		return
	}
	if err := a.plat.OpenPath(t.local); err != nil {
		a.plat.ShowInFolder(t.local)
	}
}

// downloadsButton is the Downloads button of the file panel's toolbar,
// with the count of transfers under way on it, and its popover.
func (a *App) downloadsButton(c *ui.Context) {
	p := &a.transfers.popover
	b := widgets.IconButtonBadge(c, "circle-arrow-down", locales.DownloadsButton(), a.transfers.active())
	if b.Clicked() {
		p.open = !p.open
	}
	ui.Popover(c, b, &p.open, func() { a.downloadsBody(c) })
}

// downloadsBody is the popover: the download folder on top, the
// transfers below it, newest first.
func (a *App) downloadsBody(c *ui.Context) {
	q := &a.transfers
	ui.Column(c).Width(popoverWidth).Children(func() {
		a.downloadFolderLine(c)
		ui.Scroll(c).MaxHeight(330).Children(func() {
			if len(q.jobs) == 0 {
				widgets.Muted(c, locales.TransfersEmpty()).Padding(14, 12)
			}
			for _, t := range slices.Backward(q.jobs) {
				a.transferRow(c.Key(t.id), t)
			}
		})
		ui.Row(c).Padding(6, 10).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
			if n := q.active(); n > 0 {
				widgets.Muted(c, locales.TransfersUnderWay(n)).FontSize(12)
			}
			ui.Spacer(c)
			if smallButton(c, locales.ClearFinished()).Disabled(len(q.jobs) == q.active()).Clicked() {
				a.clearFinished()
			}
		})
	})
}

// downloadFolderLine is the popover's top line: the download folder,
// shortened in the middle to fit, to change and to open.
func (a *App) downloadFolderLine(c *ui.Context) {
	dir := a.downloadDir()
	ui.Row(c).Padding(8, 10).Gap(6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Icon(c, icons.Get("folder")).FontSize(14).TextColor(ui.Hex("#eab308")).Shrink(0)
		widgets.MiddleText(c, dir, 12.5, 0).Tooltip(dir)
		if smallButton(c, locales.ChangeFolder()).Disabled(a.transfers.popover.choosing).Clicked() {
			a.chooseDownloadDir(WindowMain)
		}
		if smallButton(c, showFolderLabel(runtime.GOOS)).Clicked() {
			a.showDownloadDir()
		}
	})
}

// smallButton is a button of the popover, smaller than the window's.
func smallButton(c *ui.Context, label string) ui.Element {
	return ui.Button(c, label).FontSize(12).Padding(2, 8).Shrink(0)
}

// transfersPage is the Transfers page of the options: where downloads go.
func (a *App) transfersPage(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.OptionsDownloads()).Bold()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.DownloadFolder()).Shrink(0)
			widgets.MiddleText(c, a.downloadDir(), theme.FontSize, 8).PaddingY(4).Radius(4).
				Border(1, theme.Border).Background(sidebarBG).Tooltip(a.downloadDir())
			if ui.Button(c, locales.ChangeFolder()).Disabled(a.transfers.popover.choosing).Clicked() {
				a.chooseDownloadDir(WindowOptions)
			}
			if ui.Button(c, locales.DownloadFolderDefault()).Disabled(a.settings.DownloadDir == "").Clicked() {
				a.setDownloadDir("")
			}
		})
		widgets.Muted(c, locales.DownloadFolderNote()).FontSize(12)
		ask := a.settings.AskDownloadDir
		if ui.Checkbox(c, &ask, locales.OptionAskDownloadDir()).Changed() {
			a.changeSettings(func() { a.settings.AskDownloadDir = ask })
		}
	})
}
