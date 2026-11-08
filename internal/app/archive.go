package app

import (
	"errors"
	"path"
	"slices"
	"strings"

	"finallyshell/internal/icons"
	"finallyshell/internal/remotefs"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// Archive transfers: a selection packed with tar and gzip, sent over one
// exec channel as a single transfer of the queue (remotefs/archive.go).
// Much faster than SFTP for folders of many small files.

// archivable tells whether the names chosen in the file table gain from
// an archive transfer: more than one, or a folder; not a single file.
func (f *fileState) archivable(names []string) bool {
	if len(names) > 1 {
		return true
	}
	e, ok := f.entry(names)
	return ok && e.IsDir() && !e.Link
}

// uploadMenuItems are the menu items that upload into the remote folder
// dir: files, a folder, or folders as an archive.
func (a *App) uploadMenuItems(m *ui.Menu, s *Session, dir string) {
	if m.Item(locales.UploadFiles()).Chosen() {
		a.pickAndUpload(s, dir, false)
	}
	if m.Item(locales.UploadFolder()).Chosen() {
		a.pickAndUpload(s, dir, true)
	}
	if m.Item(locales.UploadAsArchive()).Chosen() {
		a.pickAndUploadArchive(s, dir)
	}
}

// downloadMenuItems are the menu items that download the names chosen in
// the current folder: to the download folder, to a folder the user
// chooses, or as an archive.
func (a *App) downloadMenuItems(m *ui.Menu, s *Session, names []string) {
	f := &s.files
	if m.Item(locales.Download()).Disabled(len(names) == 0).Chosen() {
		a.download(s, names, "")
	}
	if m.Item(locales.DownloadTo()).Disabled(len(names) == 0).Chosen() {
		a.pickAndDownload(s, names)
	}
	// Not for a single file, where it gains nothing.
	if f.archivable(names) && m.Item(locales.DownloadAsArchive()).Chosen() {
		a.downloadFrom(s, f.cwd, names, "", true)
	}
}

// pickAndUploadArchive uploads folders the user chooses into the remote
// folder dir, as one archive transfer.
func (a *App) pickAndUploadArchive(s *Session, dir string) {
	if s.sftp == nil || dir == "" {
		return
	}
	title := locales.ChooseArchiveUpload()
	go func() {
		paths, err := a.plat.OpenFiles(title, true)
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() { a.uploadAs(s, paths, dir, true) })
	}()
}

// archiveLabel is the direction of an archive transfer, in its row.
func (t *transferJob) archiveLabel() string {
	if t.dir == upload {
		return locales.UploadArchiveDirection()
	}
	return locales.DownloadArchiveDirection()
}

// currentLine is the file a running archive transfer packs or unpacks,
// shortened in the middle to fit, so the file's name shows.
func (t *transferJob) currentLine(c *ui.Context) {
	label := locales.ArchiveUnpacking(t.current)
	if t.dir == upload {
		label = locales.ArchivePacking(t.current)
	}
	const size = 11.5
	ui.Row(c).MinWidth(0).Children(func() {
		box := ui.Box(c).Grow(1).MinWidth(0).Clip()
		text := label
		if w := box.Bounds().W - 1; w > 0 {
			text = widgets.MiddleEllipsis(c, label, w, size)
		}
		box.Children(func() { widgets.Muted(c, text).FontSize(size).SingleLine() })
	})
}

// transferErrorText is why a transfer failed, in the app's words where
// it has them.
func transferErrorText(err error) string {
	var bad *remotefs.WindowsNameError
	switch {
	case errors.As(err, &bad):
		return locales.ArchiveWindowsName(bad.Name)
	case errors.Is(err, remotefs.ErrNoShell):
		return locales.ArchiveNoShell()
	}
	return err.Error()
}

// maxTooltipNames is how many names the tooltip of skipped links lists.
const maxTooltipNames = 20

// skippedLinesOf warns, in a finished archive download's row, of the
// links it did not make, with their names in the tooltip. The rest of
// the download is there, so it is done, not failed.
func skippedLinesOf(c *ui.Context, s remotefs.SkippedLinks) {
	line := func(text string, names []string) {
		if len(names) == 0 {
			return
		}
		tip := names
		if len(tip) > maxTooltipNames {
			tip = append(slices.Clip(tip[:maxTooltipNames]), "…")
		}
		ui.Row(c).Gap(5).AlignItems(ui.Center).Tooltip(strings.Join(tip, "\n")).Children(func() {
			ui.Icon(c, icons.Get("triangle-alert")).FontSize(12).TextColor(theme.Warning).Shrink(0)
			ui.Text(c, text).FontSize(11.5).TextColor(theme.Warning).SingleLine().Shrink(1).MinWidth(0)
		})
	}
	line(locales.ArchiveLinksOutside(len(s.Outside)), s.Outside)
	line(locales.ArchiveLinksNotAllowed(len(s.NotAllowed)), s.NotAllowed)
}

// canRetryOverSFTP tells whether t is a failed archive transfer whose
// session can still send its files over SFTP.
func (a *App) canRetryOverSFTP(t *transferJob) bool {
	s := t.sess
	return t.archive && t.state == transferFailed &&
		slices.Contains(a.sessions, s) && s.status == statusConnected && s.sftp != nil
}

// retryOverSFTP replaces the failed archive transfer t with plain SFTP
// transfers of the same files to the same place, at the end of the
// queue. The user agreed to replace what is there for t already.
func (a *App) retryOverSFTP(t *transferJob) {
	q := &a.transfers
	i := slices.Index(q.jobs, t)
	if i < 0 || !a.canRetryOverSFTP(t) {
		return
	}
	q.jobs = slices.Delete(q.jobs, i, i+1)
	for _, src := range t.srcs {
		a.enqueue(&transferJob{sess: t.sess, sftp: t.sess.sftp, dir: t.dir, src: src, dstDir: t.dstDir})
	}
}

// downloadDirArchive downloads the remote folder dir, from the folder
// tree, as an archive.
func (a *App) downloadDirArchive(s *Session, dir string) {
	if dir == "/" || dir == "" {
		return
	}
	a.downloadFrom(s, path.Dir(dir), []string{path.Base(dir)}, "", true)
}
