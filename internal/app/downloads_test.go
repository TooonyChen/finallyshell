package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fakeDownloads stands in for the folder dialog of downloads, opening
// files, and the app's icon.
type fakeDownloads struct {
	chooseMu    sync.Mutex
	chosen      string // what ChooseFolder returns: "" for canceled
	chooseAsked []filePick

	// On the UI thread.
	openedPaths []string
	openErr     error
	iconShows   []iconShow
}

// iconShow is a call of ShowTransfers.
type iconShow struct {
	count    int
	progress float64
}

func (p *fakeDownloads) ChooseFolder(parent WindowKind, title string) (string, error) {
	p.chooseMu.Lock()
	defer p.chooseMu.Unlock()
	p.chooseAsked = append(p.chooseAsked, filePick{parent: parent, title: title})
	return p.chosen, nil
}

// chooseFolder is what the next folder dialogs return: "" for canceled.
func (p *fakeDownloads) chooseFolder(dir string) {
	p.chooseMu.Lock()
	p.chosen = dir
	p.chooseMu.Unlock()
}

func (p *fakeDownloads) folderChoices() []filePick {
	p.chooseMu.Lock()
	defer p.chooseMu.Unlock()
	return slices.Clone(p.chooseAsked)
}

func (p *fakeDownloads) OpenPath(path string) error {
	p.openedPaths = append(p.openedPaths, path)
	return p.openErr
}

func (p *fakeDownloads) ShowTransfers(count int, progress float64) {
	p.iconShows = append(p.iconShows, iconShow{count, progress})
}

// filesSession is a connected session whose files are a temporary
// folder, served over SFTP in the process, listed in the file panel of a
// tester.
func filesSession(t *testing.T, a *App, p *fakePlatform) (*Session, string, *ui.Tester) {
	t.Helper()
	s := demoSession(a, "files", "127.0.0.1")
	a.active = len(a.sessions) - 1
	remote := t.TempDir()
	s.sftp = localSFTP(t)
	s.files = fileState{}
	s.files.init()
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openDir(s, remote)
	p.until(t, "the listing", func() bool { return s.files.cwd == remote && !s.files.loading })
	return s, remote, tt
}

func idle(a *App) func() bool { return func() bool { return a.transfers.active() == 0 } }

// The popover shows the download folder and the transfers, newest first;
// a failed one runs again with Retry, a done one opens.
func TestDownloadPopover(t *testing.T) {
	a, p := testApp(t)
	s, remote, tt := filesSession(t, a, p)
	folder := filepath.Join(t.TempDir(), "incoming") // not there yet
	a.setDownloadDir(folder)
	if config.LoadSettings().DownloadDir != folder {
		t.Fatalf("saved %+v", config.LoadSettings())
	}
	os.WriteFile(filepath.Join(remote, "report.txt"), []byte("quarterly numbers"), 0o644)
	a.download(s, []string{"report.txt", "missing.txt"}, "")
	p.until(t, "the downloads", idle(a))
	done, failed := a.transfers.jobs[0], a.transfers.jobs[1]
	if done.state != transferDone || failed.state != transferFailed {
		t.Fatalf("states %d %d", done.state, failed.state)
	}
	if data, _ := os.ReadFile(filepath.Join(folder, "report.txt")); string(data) != "quarterly numbers" {
		t.Fatalf("downloaded %q", data)
	}

	if err := tt.Click(locales.DownloadsButton()); err != nil {
		t.Fatal(err)
	}
	if !a.transfers.popover.open {
		t.Fatal("the popover did not open")
	}
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "incoming") || !tt.HasText(locales.ChangeFolder()) || !tt.HasText(showFolderLabel("linux")) {
		t.Errorf("no folder line; texts %q", tt.Texts())
	}
	// Newest first: the failed one above the done one.
	fb, ok1 := tt.Find("missing.txt")
	db, ok2 := tt.Find("report.txt")
	if !ok1 || !ok2 || fb.Y >= db.Y {
		t.Errorf("order: missing %v %v, report %v %v", fb, ok1, db, ok2)
	}
	saveShot(t, tt, "downloads-failed-en.png")

	if err := tt.Click(locales.OpenFile()); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(folder, "report.txt"); !slices.Equal(p.openedPaths, []string{want}) {
		t.Errorf("opened %q", p.openedPaths)
	}
	// With no app for it, it shows in its folder.
	p.openErr = errors.New("no app")
	tt.Click(locales.OpenFile())
	if len(p.shown) != 1 || p.shown[0] != filepath.Join(folder, "report.txt") {
		t.Errorf("shown %q", p.shown)
	}
	p.openErr = nil

	// Show in folder opens the download folder.
	if err := tt.Click(showFolderLabel("linux")); err != nil {
		t.Fatal(err)
	}
	if p.openedPaths[len(p.openedPaths)-1] != folder {
		t.Errorf("opened %q", p.openedPaths)
	}

	os.WriteFile(filepath.Join(remote, "missing.txt"), []byte("found"), 0o644)
	if err := tt.Click(locales.Retry()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the retry", idle(a))
	if failed.state != transferDone || len(a.transfers.jobs) != 2 || a.transfers.jobs[1] != failed {
		t.Fatalf("retried: state %d err %v, %d jobs", failed.state, failed.err, len(a.transfers.jobs))
	}
	if data, _ := os.ReadFile(filepath.Join(folder, "missing.txt")); string(data) != "found" {
		t.Errorf("retried download %q", data)
	}
	tt.Frame()
	saveShot(t, tt, "downloads-done-en.png")

	// The Transfers tab shows the same jobs.
	a.transfers.popover.open = false
	a.bottomTab = 1
	tt.Frame()
	if !tt.HasText("missing.txt") || !tt.HasText("report.txt") {
		t.Errorf("the tab differs; texts %q", tt.Texts())
	}
	a.bottomTab = 0
	a.transfers.popover.open = true
	tt.Frame()

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"更改…", "在文件夹中显示", "打开", "清除已完成", "已完成"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in Chinese; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "downloads-done-zh.png")
	if err := tt.Click("清除已完成"); err != nil {
		t.Fatal(err)
	}
	if len(a.transfers.jobs) != 0 || !tt.HasText(locales.TransfersEmpty()) {
		t.Errorf("%d jobs left", len(a.transfers.jobs))
	}
}

// demoTransfers fills the queue with transfers in every state, as they
// look while some run.
func demoTransfers(a *App, s *Session) {
	now := time.Now()
	add := func(dir direction, src string, st transferState, done, total int64, speed float64, err error) {
		a.transfers.nextID++
		local := ""
		if st == transferDone && dir == download {
			local = filepath.Join("/home/me/Downloads", filepath.Base(src))
		}
		dst := "/home/me/Downloads"
		if dir == upload {
			dst = "/var/www"
		}
		a.transfers.jobs = append(a.transfers.jobs, &transferJob{
			id: a.transfers.nextID, sess: s, dir: dir, src: src, dstDir: dst, local: local,
			state: st, done: done, total: total, speed: speed, err: err, sampleAt: now,
		})
	}
	add(download, "/var/log/nginx/access.log", transferDone, 18_400_000, 18_400_000, 0, nil)
	add(upload, "/home/me/site/index.html", transferDone, 12_000, 12_000, 0, nil)
	add(download, "/var/backups/db-2026-11-06.sql.gz", transferFailed, 0, -1, 0, errors.New("permission denied"))
	add(download, "/srv/media/promo.mp4", transferCanceled, 40_000_000, 210_000_000, 0, nil)
	add(download, "/srv/releases/app-2.4.1.tar.gz", transferRunning, 63_500_000, 148_000_000, 4_200_000, nil)
	add(upload, "/home/me/photos", transferRunning, 2_100_000, 9_800_000, 820_000, nil)
	add(download, "/srv/releases/app-2.4.0.tar.gz", transferWaiting, 0, -1, 0, nil)
	a.transfers.running = 2
}

// The popover while transfers run: the badge on the button, progress,
// speed and time left.
func TestDownloadPopoverRunning(t *testing.T) {
	a, p := testApp(t)
	p.downloads = "/home/me/Downloads"
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	demoTransfers(a, s)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	if !tt.HasText("3") { // the badge
		t.Errorf("no badge; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.DownloadsButton()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"app-2.4.1.tar.gz", locales.TransfersUnderWay(3), locales.TransferWaiting(), locales.TransferCanceled(),
		locales.TransferProgressLeft("60.6 MB", "141.1 MB", "4.0 MB", "0:20"),
	} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "downloads-running-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"3 个传输进行中", "剩余 0:20", "等待中"} {
		if !strings.Contains(strings.Join(tt.Texts(), "\n"), want) {
			t.Errorf("no %q in Chinese; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "downloads-running-zh.png")

	// Cancel stops the waiting one at once.
	waiting := a.transfers.jobs[6]
	a.cancelTransfer(waiting)
	if waiting.state != transferCanceled || a.transfers.active() != 2 {
		t.Errorf("state %d, %d active", waiting.state, a.transfers.active())
	}
}

// Retry needs the job's session open and connected, and runs over its
// connection as it is now.
func TestRetryNeedsConnectedSession(t *testing.T) {
	a, p := testApp(t)
	s, remote, tt := filesSession(t, a, p)
	a.download(s, []string{"later.txt"}, "")
	p.until(t, "the failed download", idle(a))
	job := a.transfers.jobs[0]
	if job.state != transferFailed || !a.canRetry(job) {
		t.Fatalf("state %d, retry %v", job.state, a.canRetry(job))
	}
	os.WriteFile(filepath.Join(remote, "later.txt"), []byte("now"), 0o644)

	// Disconnected: Retry is disabled.
	s.status = statusClosed
	a.transfers.popover.open = true
	tt.Frame()
	tt.Click(locales.Retry())
	a.retry(job)
	if job.state != transferFailed || a.canRetry(job) {
		t.Errorf("retried while disconnected: state %d", job.state)
	}

	// Reconnected, over a new SFTP client.
	s.status = statusConnected
	old := job.sftp
	s.sftp = localSFTP(t)
	a.retry(job)
	p.until(t, "the retry", idle(a))
	if job.state != transferDone || job.sftp == old {
		t.Errorf("state %d err %v, same client %v", job.state, job.err, job.sftp == old)
	}

	// A canceled one of a closed session cannot run again. (The session
	// leaves the list as closeSession takes it out; closing the pipe of
	// localSFTP would wait for its server.)
	job.state = transferCanceled
	if !a.canRetry(job) {
		t.Fatal("no retry for a canceled job")
	}
	a.sessions = slices.DeleteFunc(a.sessions, func(x *Session) bool { return x == s })
	if a.canRetry(job) {
		t.Error("retry for a closed session")
	}
}

// With "Ask where to save each download" on, Download asks for a folder;
// canceling downloads nothing.
func TestAskWhereToSaveDownloads(t *testing.T) {
	a, p := testApp(t)
	s, remote, _ := filesSession(t, a, p)
	os.WriteFile(filepath.Join(remote, "a.txt"), []byte("a"), 0o644)
	a.changeSettings(func() { a.settings.AskDownloadDir = true })
	if !config.LoadSettings().AskDownloadDir {
		t.Fatal("not saved")
	}

	a.download(s, []string{"a.txt"}, "")
	p.until(t, "the question", func() bool { return len(p.folderChoices()) == 1 })
	p.drain()
	if len(a.transfers.jobs) != 0 {
		t.Fatalf("canceled, yet %d jobs", len(a.transfers.jobs))
	}

	chosen := t.TempDir()
	p.chooseFolder(chosen)
	a.download(s, []string{"a.txt"}, "")
	p.until(t, "the download", func() bool { return len(a.transfers.jobs) == 1 && a.transfers.active() == 0 })
	if data, _ := os.ReadFile(filepath.Join(chosen, "a.txt")); string(data) != "a" {
		t.Errorf("downloaded %q", data)
	}
	if _, err := os.Stat(filepath.Join(p.downloads, "a.txt")); err == nil {
		t.Error("downloaded to the download folder too")
	}
	asked := p.folderChoices()
	if len(asked) != 2 || asked[1].parent != WindowMain || asked[1].title != locales.ChooseDownloadFolder() {
		t.Errorf("asked %+v", asked)
	}
}

// The Transfers page of the options changes the download folder, resets
// it, and turns asking on; each is saved.
func TestTransfersOptions(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	if err := tt.Click(locales.OptionsTransfers()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(p.downloads) || !tt.HasText(locales.OptionAskDownloadDir()) {
		t.Fatalf("page %q; texts %q", a.optionsNav.Page, tt.Texts())
	}

	chosen := filepath.Join(t.TempDir(), "sftp")
	p.chooseFolder(chosen)
	if err := tt.Click(locales.ChangeFolder()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the new folder", func() bool { return a.settings.DownloadDir == chosen })
	if config.LoadSettings().DownloadDir != chosen || a.downloadDir() != chosen {
		t.Errorf("saved %+v", config.LoadSettings())
	}
	if asked := p.folderChoices(); len(asked) != 1 || asked[0].parent != WindowOptions || asked[0].title != locales.ChooseDefaultDownloadFolder() {
		t.Errorf("asked %+v", asked)
	}
	if err := tt.Click(locales.OptionAskDownloadDir()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// The long path shows shortened by its middle.
	if !slices.ContainsFunc(tt.Texts(), func(s string) bool {
		return s == chosen || strings.HasPrefix(s, chosen[:6]) && strings.HasSuffix(s, "/sftp") && strings.Contains(s, "…")
	}) {
		t.Errorf("folder not shown; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-transfers-en.png")
	if b := New(p); b.settings.DownloadDir != chosen || !b.settings.AskDownloadDir {
		t.Errorf("read back %+v", b.settings)
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"传输", "下载目录", "更改…", "使用默认", "每次下载时询问保存位置"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in Chinese; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "options-transfers-zh.png")

	if err := tt.Click("使用默认"); err != nil {
		t.Fatal(err)
	}
	if a.settings.DownloadDir != "" || config.LoadSettings().DownloadDir != "" || a.downloadDir() != p.downloads {
		t.Errorf("reset: %+v", config.LoadSettings())
	}
	// Choosing the system's folder is the default too.
	p.chooseFolder(p.downloads)
	a.chooseDownloadDir(WindowOptions)
	p.until(t, "the dialog", func() bool { return !a.transfers.popover.choosing })
	if a.settings.DownloadDir != "" {
		t.Errorf("kept %q", a.settings.DownloadDir)
	}
}

// The app's icon shows the count of transfers at once, their progress
// a few times a second at most, and nothing once they end.
func TestShowTransfersOnAppIcon(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	t0 := time.Now()
	a.showTransfersAt(t0)
	if len(p.iconShows) != 0 {
		t.Fatalf("idle shows %v", p.iconShows)
	}
	a.transfers.jobs = []*transferJob{
		{sess: s, state: transferRunning, done: 0, total: -1},
		{sess: s, state: transferWaiting, total: -1},
	}
	a.showTransfersAt(t0)
	j := &transferJob{sess: s, state: transferRunning, done: 25, total: 100}
	a.transfers.jobs = append(a.transfers.jobs, j)
	a.showTransfersAt(t0.Add(10 * time.Millisecond))
	j.done = 50
	a.showTransfersAt(t0.Add(100 * time.Millisecond)) // too soon
	a.showTransfersAt(t0.Add(300 * time.Millisecond))
	a.transfers.jobs[0].total, a.transfers.jobs[0].done = 100, 100
	a.showTransfersAt(t0.Add(310 * time.Millisecond)) // too soon again
	a.transfers.jobs[1].state = transferCanceled
	a.showTransfersAt(t0.Add(320 * time.Millisecond)) // a new count
	for _, j := range a.transfers.jobs {
		j.state = transferDone
	}
	a.showTransfersAt(t0.Add(330 * time.Millisecond))
	want := []iconShow{{2, -1}, {3, 0.25}, {3, 0.5}, {2, 0.75}, {0, 0}}
	if !slices.Equal(p.iconShows, want) {
		t.Errorf("shows %v, want %v", p.iconShows, want)
	}

	// A real download shows on the icon, then clears it.
	a, p = testApp(t)
	s, remote, _ := filesSession(t, a, p)
	os.WriteFile(filepath.Join(remote, "big.bin"), make([]byte, 2_000_000), 0o644)
	a.download(s, []string{"big.bin"}, "")
	p.until(t, "the download", idle(a))
	shows := p.iconShows
	if len(shows) < 2 || shows[0].count != 1 || shows[len(shows)-1] != (iconShow{}) {
		t.Errorf("shows %v", shows)
	}
}

func TestFormatting(t *testing.T) {
	for d, want := range map[time.Duration]string{
		7 * time.Second: "0:07", 245 * time.Second: "4:05", 3723 * time.Second: "1:02:03", 1400 * time.Millisecond: "0:01",
	} {
		if got := formatTimeLeft(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
	_, p := testApp(t)
	for goos, want := range map[string]string{"darwin": "Show in Finder", "windows": "Show in Explorer", "linux": "Show in folder"} {
		if got := showFolderLabel(goos); got != want {
			t.Errorf("%s: %q", goos, got)
		}
	}
	p.SetLocale("zh-Hans")
	for goos, want := range map[string]string{"darwin": "在访达中显示", "windows": "在资源管理器中显示", "linux": "在文件夹中显示"} {
		if got := showFolderLabel(goos); got != want {
			t.Errorf("%s: %q", goos, got)
		}
	}

	var got []string
	tt := ui.NewTester(func(c *ui.Context) {
		got = got[:0]
		for _, s := range []string{"/home/me/Downloads", "/Users/someone/Library/Mobile Documents/com~apple~CloudDocs/Downloads/servers"} {
			got = append(got, widgets.MiddleEllipsis(c, s, 200, 12.5))
		}
	}, 400, 100)
	tt.Frame()
	if got[0] != "/home/me/Downloads" {
		t.Errorf("short path %q", got[0])
	}
	if !strings.HasPrefix(got[1], "/Users/") || !strings.HasSuffix(got[1], "servers") || !strings.Contains(got[1], "…") {
		t.Errorf("long path %q", got[1])
	}
}
