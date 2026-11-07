package app

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"

	"finallyshell/internal/icons"
	"finallyshell/internal/remotefs"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"github.com/pkg/sftp"
)

type direction int

const (
	upload direction = iota
	download
)

type transferState int

const (
	transferWaiting transferState = iota
	transferRunning
	transferDone
	transferFailed
	transferCanceled
)

// maxRunning is how many transfers run at once; the others wait.
const maxRunning = 3

// transferJob copies one file or directory, src into dstDir.
type transferJob struct {
	id     int64
	sess   *Session
	sftp   *sftp.Client
	dir    direction
	src    string // a local path to upload, or a remote one to download
	dstDir string // the remote or local directory it goes into
	local  string // the local copy of a finished download

	state      transferState
	err        error
	done       int64
	total      int64 // -1 until counted
	speed      float64
	sampleAt   time.Time
	sampleDone int64
	cancel     context.CancelFunc
}

func (t *transferJob) name() string {
	if t.dir == upload {
		return filepath.Base(t.src)
	}
	return path.Base(t.src)
}

func (t *transferJob) finished() bool { return t.state >= transferDone }

type transferQueue struct {
	jobs    []*transferJob
	running int
	nextID  int64
	list    ui.ListState

	popover downloadsPopover // the file panel's Downloads popover (downloads.go)
	shown   taskbarState     // what the app's icon shows of them
}

func (q *transferQueue) init() {}

// active counts the transfers waiting or running.
func (q *transferQueue) active() int {
	n := 0
	for _, t := range q.jobs {
		if !t.finished() {
			n++
		}
	}
	return n
}

func (q *transferQueue) cancelSession(s *Session) {
	for _, t := range q.jobs {
		if t.sess == s && !t.finished() {
			t.stop()
		}
	}
}

func (t *transferJob) stop() {
	if t.cancel != nil {
		t.cancel()
	}
	if t.state == transferWaiting {
		t.state = transferCanceled
	}
}

// replaceQuestion asks before a transfer replaces items of the same name.
type replaceQuestion struct {
	count int
	dir   string
	yes   func()
}

// upload sends local files and folders into the remote directory dir of
// s, asking first when names are taken.
func (a *App) upload(s *Session, paths []string, dir string) {
	sc := s.sftp
	if sc == nil || len(paths) == 0 {
		return
	}
	paths = slices.Clone(paths)
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	start := func() {
		for _, p := range paths {
			a.enqueue(&transferJob{sess: s, sftp: sc, dir: upload, src: p, dstDir: dir})
		}
	}
	go func() {
		taken := remotefs.RemoteExisting(sc, dir, names)
		a.post(func() {
			if len(taken) > 0 {
				a.replace = &replaceQuestion{count: len(taken), dir: dir, yes: start}
				return
			}
			start()
		})
	}()
}

// download fetches the names in s's current directory into the local
// directory dir; for "", into the download folder of the settings, or
// where the user chooses when they ask for each download.
func (a *App) download(s *Session, names []string, dir string) {
	sc := s.sftp
	if sc == nil || len(names) == 0 {
		return
	}
	if dir == "" {
		if a.settings.AskDownloadDir {
			a.pickAndDownload(s, names)
			return
		}
		dir = a.downloadDir()
	}
	cwd := s.files.cwd
	start := func() {
		for _, n := range names {
			a.enqueue(&transferJob{sess: s, sftp: sc, dir: download, src: path.Join(cwd, n), dstDir: dir})
		}
	}
	if taken := remotefs.LocalExisting(dir, names); len(taken) > 0 {
		a.replace = &replaceQuestion{count: len(taken), dir: dir, yes: start}
		return
	}
	start()
}

func (a *App) pickAndUpload(s *Session, folder bool) {
	if s.sftp == nil || s.files.cwd == "" {
		return
	}
	dir := s.files.cwd
	title := locales.ChooseUploadFiles()
	if folder {
		title = locales.ChooseUploadFolder()
	}
	go func() {
		paths, err := a.plat.OpenFiles(title, folder)
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() { a.upload(s, paths, dir) })
	}()
}

func (a *App) pickAndDownload(s *Session, names []string) {
	title := locales.ChooseDownloadFolder()
	go func() {
		dir, err := a.plat.ChooseFolder(WindowMain, title)
		if err != nil || dir == "" {
			return
		}
		a.post(func() { a.download(s, names, dir) })
	}()
}

func (a *App) enqueue(t *transferJob) {
	q := &a.transfers
	q.nextID++
	t.id, t.total = q.nextID, -1
	q.jobs = append(q.jobs, t)
	a.pump()
	a.showTransfers()
}

// pump starts waiting transfers while fewer than maxRunning run.
func (a *App) pump() {
	q := &a.transfers
	for _, t := range q.jobs {
		if q.running >= maxRunning {
			return
		}
		if t.state == transferWaiting {
			a.run(t)
		}
	}
}

func (a *App) run(t *transferJob) {
	q := &a.transfers
	q.running++
	t.state = transferRunning
	t.sampleAt = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go func() {
		var total int64
		var local string
		var err error
		if t.dir == upload {
			total, err = remotefs.LocalSize(t.src)
		} else if total, err = remotefs.RemoteSize(t.sftp, t.src); err == nil {
			// The download folder of the settings may be gone.
			err = os.MkdirAll(t.dstDir, 0o755)
		}
		if err == nil {
			a.post(func() {
				t.total = total
				a.showTransfers()
			})
			var last time.Time
			progress := func(done int64) {
				if now := time.Now(); now.Sub(last) >= 150*time.Millisecond {
					last = now
					a.post(func() {
						t.progress(done, now)
						a.showTransfers()
					})
				}
			}
			if t.dir == upload {
				err = remotefs.Upload(ctx, t.sftp, t.src, t.dstDir, progress)
			} else {
				local, err = remotefs.Download(ctx, t.sftp, t.src, t.dstDir, progress)
			}
		}
		a.post(func() {
			cancel()
			q.running--
			switch {
			case err == nil:
				t.state, t.done, t.local = transferDone, t.total, local
			case remotefs.IsCanceled(err):
				t.state = transferCanceled
			default:
				t.state, t.err = transferFailed, err
			}
			if t.dir == upload {
				a.refreshIfShowing(t.sess, t.dstDir)
			}
			a.pump()
			a.showTransfers()
		})
	}()
}

// progress records done bytes, and the speed over the last second.
func (t *transferJob) progress(done int64, now time.Time) {
	t.done = done
	if dt := now.Sub(t.sampleAt).Seconds(); dt >= 1 {
		t.speed = float64(done-t.sampleDone) / dt
		t.sampleAt, t.sampleDone = now, done
	}
}

func (a *App) clearFinished() {
	a.transfers.jobs = slices.DeleteFunc(a.transfers.jobs, (*transferJob).finished)
}

func (a *App) transferPanel(c *ui.Context) {
	q := &a.transfers
	ui.Column(c).Grow(1).Children(func() {
		ui.Row(c).Padding(4, 8).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#e5e7eb")).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, locales.ClearFinished()).Disabled(len(q.jobs) == a.transfers.active()).Clicked() {
				a.clearFinished()
			}
		})
		jobs := q.jobs
		q.list.Key = func(i int) any { return jobs[i].id }
		ui.List(c, &q.list, len(jobs), func(i int) {
			a.transferRow(c, jobs[i])
		}).Grow(1).Children(func() {
			if len(jobs) == 0 {
				widgets.Muted(c, locales.TransfersEmpty()).Padding(12)
			}
		})
	})
}

// transferRow is a transfer in the Transfers tab and in the Downloads
// popover: its name, where it goes, its progress or how it ended, and
// what can be done with it.
func (a *App) transferRow(c *ui.Context, t *transferJob) {
	row := ui.Row(c).Gap(10).Padding(6, 10).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#f1f5f9"))
	row.Children(func() {
		ic, dirLabel, dest := icons.Get("upload"), locales.UploadDirection(), t.sess.label()+":"+t.dstDir
		if t.dir == download {
			ic, dirLabel, dest = icons.Get("download"), locales.DownloadDirection(), t.dstDir
		}
		ui.Icon(c, ic).FontSize(16).TextColor(theme.TextMuted)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, t.name()).SingleLine().Shrink(1).MinWidth(0)
				widgets.Muted(c, dirLabel+" → "+dest).FontSize(11.5).SingleLine().Grow(1).MinWidth(0)
			})
			switch t.state {
			case transferWaiting:
				widgets.Muted(c, locales.TransferWaiting()).FontSize(11.5)
			case transferRunning:
				frac := -1.0
				if t.total > 0 {
					frac = float64(t.done) / float64(t.total)
				} else if t.total == 0 {
					frac = 1
				}
				ui.Progress(c, frac).Label(t.name())
				if t.total >= 0 {
					widgets.Muted(c, t.progressText()).FontSize(11.5).SingleLine()
				}
			case transferDone:
				ui.Text(c, locales.TransferDone()).FontSize(11.5).TextColor(theme.Success)
			case transferCanceled:
				widgets.Muted(c, locales.TransferCanceled()).FontSize(11.5)
			case transferFailed:
				ui.Text(c, locales.TransferFailed(t.err.Error())).FontSize(11.5).TextColor(theme.Danger).MaxLines(2)
			}
		})
		switch {
		case !t.finished():
			if widgets.IconButton(c, "x", locales.Cancel()).Clicked() {
				a.cancelTransfer(t)
			}
		case t.state == transferFailed || t.state == transferCanceled:
			ok := a.canRetry(t)
			if widgets.IconButton(c, "rotate-cw", locales.Retry()).Disabled(!ok).Clicked() && ok {
				a.retry(t)
			}
		case t.state == transferDone && t.local != "":
			if widgets.IconButton(c, "external-link", locales.OpenFile()).Clicked() {
				a.openDownload(t)
			}
			if widgets.IconButton(c, "folder", locales.ShowInFolder()).Clicked() {
				a.plat.ShowInFolder(t.local)
			}
		}
	})
}

// progressText is how far a running transfer is: the bytes done of all,
// the speed, and the time left once there is a speed.
func (t *transferJob) progressText() string {
	done, total := formatSize(t.done), formatSize(t.total)
	speed := formatSize(int64(t.speed))
	if t.speed <= 0 || t.done >= t.total {
		return locales.TransferProgress(done, total, speed)
	}
	left := time.Duration(float64(t.total-t.done) / t.speed * float64(time.Second))
	return locales.TransferProgressLeft(done, total, speed, formatTimeLeft(left))
}

// formatTimeLeft is d as a clock: 0:07, 4:05, 1:02:03.
func formatTimeLeft(d time.Duration) string {
	s := int64(d.Round(time.Second) / time.Second)
	if s < 3600 {
		return fmt.Sprintf("%d:%02d", s/60, s%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}
