package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// archiveFixture is a session whose files are a temporary folder over
// SFTP in the process, with no SSH connection to run tar over, in a
// window that draws its menus, for the screenshots.
func archiveFixture(t *testing.T) (*App, *fakePlatform, *Session, string, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	remote := t.TempDir()
	writeTree(t, filepath.Join(remote, "site"), 12)
	os.WriteFile(filepath.Join(remote, "notes.txt"), []byte("notes"), 0o644)
	s.sftp = localSFTP(t)
	s.files = fileState{}
	s.files.init()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetMenuPresentation(ui.PopoverMenus)
		a.View(c)
	}, 1280, 820)
	p.tt = tt
	a.openDir(s, remote)
	p.until(t, "the listing", func() bool { return s.files.cwd == remote && !s.files.loading })
	return a, p, s, remote, tt
}

// writeTree writes a folder of n small files in nested folders, an
// empty folder, a script and a link.
func writeTree(t *testing.T, dir string, n int) {
	t.Helper()
	for i := range n {
		p := filepath.Join(dir, fmt.Sprintf("part%d", i%5), fmt.Sprintf("sub%d", i%3), fmt.Sprintf("file%03d.txt", i))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(strings.Repeat("data ", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	if runtime.GOOS != "windows" {
		os.Symlink("run.sh", filepath.Join(dir, "start"))
	}
}

// sameTree compares the files, modes and links of two folders.
func sameTree(t *testing.T, want, got string) {
	t.Helper()
	n := 0
	err := filepath.WalkDir(want, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		n++
		rel, _ := filepath.Rel(want, p)
		q := filepath.Join(got, rel)
		wi, _ := os.Lstat(p)
		gi, err := os.Lstat(q)
		switch {
		case err != nil:
			t.Errorf("%s is missing", rel)
		case wi.Mode().Type() != gi.Mode().Type():
			t.Errorf("%s: %v, want %v", rel, gi.Mode(), wi.Mode())
		case wi.Mode()&fs.ModeSymlink != 0:
			wt, _ := os.Readlink(p)
			gt, _ := os.Readlink(q)
			if wt != gt {
				t.Errorf("%s points to %q, want %q", rel, gt, wt)
			}
		case wi.Mode().IsRegular():
			wd, _ := os.ReadFile(p)
			gd, _ := os.ReadFile(q)
			if string(wd) != string(gd) || runtime.GOOS != "windows" && wi.Mode().Perm() != gi.Mode().Perm() {
				t.Errorf("%s: %v %q, want %v %q", rel, gi.Mode(), gd, wi.Mode(), wd)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Errorf("only %d items in %s", n, want)
	}
}

// tableMenu right-clicks the table's row of a folder, the only folder,
// or of the file called name, and returns the menu.
func tableMenu(t *testing.T, tt *ui.Tester, name string) []string {
	t.Helper()
	tt.CloseMenu()
	tt.RightClickAt(rowCenter(t, tt, name))
	tt.Frame()
	return tt.Menu()
}

// The archive items in the table's and the tree's menus, and in the
// toolbar buttons' menus; without exec on the server the transfer fails,
// and "Retry over SFTP" sends the same files the usual way.
func TestArchiveMenus(t *testing.T) {
	a, p, s, remote, tt := archiveFixture(t)
	// A single file is not offered a download as an archive.
	menu := tableMenu(t, tt, locales.KindExtFile("TXT"))
	if slices.Contains(menu, locales.DownloadAsArchive()) || !slices.Contains(menu, locales.UploadAsArchive()) {
		t.Errorf("a file's menu %q", menu)
	}
	menu = tableMenu(t, tt, locales.KindFolder())
	if !slices.Contains(menu, locales.DownloadAsArchive()) || !slices.Contains(menu, locales.UploadAsArchive()) {
		t.Errorf("a folder's menu %q", menu)
	}
	if !slices.Equal(s.files.chosenNames(), []string{"site"}) {
		t.Fatalf("chosen %q", s.files.chosenNames())
	}
	saveShot(t, tt, "archive-menu-en.png")
	// Both chosen: a file and a folder go in one archive.
	s.files.chosen.Add("notes.txt")
	tt.CloseMenu()
	tt.Frame()
	if !s.files.archivable(s.files.chosenNames()) {
		t.Error("two items are not archivable")
	}
	s.files.chosen.Remove("notes.txt")

	// The toolbar's buttons.
	tt.CloseMenu()
	if err := tt.RightClick(locales.UploadFolder()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if menu := tt.Menu(); !slices.Equal(menu, []string{locales.UploadFiles(), locales.UploadFolder(), locales.UploadAsArchive()}) {
		t.Errorf("upload button's menu %q", menu)
	}
	tt.CloseMenu()
	if err := tt.RightClick(locales.Download()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if menu := tt.Menu(); !slices.Equal(menu, []string{locales.Download(), locales.DownloadTo(), locales.DownloadAsArchive()}) {
		t.Errorf("download button's menu %q", menu)
	}
	if err := tt.ChooseMenuItem(locales.DownloadAsArchive()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the archive download", idle(a))
	j := a.transfers.jobs[0]
	if !j.archive || j.state != transferFailed || !errors.Is(j.err, remotefs.ErrNoShell) || !slices.Equal(j.srcs, []string{path.Join(remote, "site")}) {
		t.Fatalf("job %+v", j)
	}
	a.bottomTab = 1
	tt.Frame()
	if !tt.HasText(locales.TransferFailed(locales.ArchiveNoShell())) || !tt.HasText(locales.DownloadArchiveDirection()+" → "+p.downloads) {
		t.Errorf("no failed archive row; texts %q", tt.Texts())
	}
	if a.canRetry(j) {
		t.Error("an archive can be retried without an SSH connection")
	}
	if err := tt.Click(locales.RetryOverSftp()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the SFTP download", idle(a))
	if len(a.transfers.jobs) != 1 || a.transfers.jobs[0].archive || a.transfers.jobs[0].state != transferDone {
		t.Fatalf("jobs %+v", a.transfers.jobs)
	}
	// SFTP copies the files: not empty folders, and links as their files.
	if data, _ := os.ReadFile(filepath.Join(p.downloads, "site", "part1", "sub1", "file001.txt")); string(data) != "data data " {
		t.Errorf("downloaded %q", data)
	}

	// Upload as archive, from the table's menu, and again over SFTP.
	a.bottomTab = 0
	tt.Frame()
	local := t.TempDir()
	writeTree(t, filepath.Join(local, "assets"), 6)
	p.opened = []string{filepath.Join(local, "assets")}
	tableMenu(t, tt, locales.KindExtFile("TXT"))
	if err := tt.ChooseMenuItem(locales.UploadAsArchive()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the archive upload", func() bool { return len(a.transfers.jobs) == 2 && a.transfers.active() == 0 })
	up := a.transfers.jobs[1]
	if !up.archive || up.dir != upload || up.state != transferFailed {
		t.Fatalf("upload %+v", up)
	}
	a.retryOverSFTP(up)
	p.until(t, "the SFTP upload", idle(a))
	if last := a.transfers.jobs[len(a.transfers.jobs)-1]; last.archive || last.state != transferDone {
		t.Fatalf("upload over SFTP: %+v", last)
	}
	if _, err := os.Stat(filepath.Join(remote, "assets", "run.sh")); err != nil {
		t.Error(err)
	}

	// The same scene in Chinese.
	os.RemoveAll(filepath.Join(remote, "assets"))
	a.openDir(s, remote)
	p.SetLocale("zh-Hans")
	p.until(t, "the listing", func() bool { return !s.files.loading && len(s.files.entries) == 2 })
	menu = tableMenu(t, tt, locales.KindFolder())
	if !slices.Contains(menu, "打包下载") || !slices.Contains(menu, "打包上传…") {
		t.Errorf("Chinese menu %q", menu)
	}
	saveShot(t, tt, "archive-menu-zh.png")
}

// The tree's menu has the archive items for a folder, and only the upload
// one for "/".
func TestArchiveTreeMenu(t *testing.T) {
	f := newEditorFixture(t)
	writeTree(t, filepath.Join(f.dir, "srv", "www"), 3)
	f.a.openDir(f.s, f.dir)
	f.settled(t)
	f.showInTree(t, filepath.Join(f.dir, "srv"))
	f.tt.RightClickAt(rowCenter(t, f.tt, "srv"))
	if menu := f.tt.Menu(); !slices.Contains(menu, locales.DownloadAsArchive()) || !slices.Contains(menu, locales.UploadAsArchive()) {
		t.Errorf("folder menu %q", menu)
	}
	if err := f.tt.ChooseMenuItem(locales.DownloadAsArchive()); err != nil {
		t.Fatal(err)
	}
	f.p.until(t, "the archive download", idle(f.a))
	// The in-process server has no exec: tar cannot run.
	j := f.a.transfers.jobs[0]
	if !j.archive || !slices.Equal(j.srcs, []string{filepath.Join(f.dir, "srv")}) || j.state != transferFailed || !strings.Contains(j.err.Error(), "cannot run tar") {
		t.Fatalf("job %+v, err %v", j, j.err)
	}
	if !f.a.canRetry(j) || !f.a.canRetryOverSFTP(j) {
		t.Error("cannot retry")
	}
	f.a.retryOverSFTP(j)
	f.p.until(t, "the SFTP download", idle(f.a))
	if _, err := os.Stat(filepath.Join(f.p.downloads, "srv", "www", "run.sh")); err != nil {
		t.Error(err)
	}

	f.tt.CloseMenu()
	f.showInTree(t, "/")
	f.tt.RightClickAt(rowCenter(t, f.tt, "/"))
	if menu := f.tt.Menu(); slices.Contains(menu, locales.DownloadAsArchive()) || !slices.Contains(menu, locales.UploadAsArchive()) {
		t.Errorf("root menu %q", menu)
	}
}

// demoArchiveTransfers fills the queue with archive transfers as they
// look while they run.
func demoArchiveTransfers(a *App, s *Session) {
	now := time.Now()
	add := func(t *transferJob) {
		a.transfers.nextID++
		t.id, t.sess, t.archive, t.src, t.sampleAt = a.transfers.nextID, s, true, t.srcs[0], now
		a.transfers.jobs = append(a.transfers.jobs, t)
	}
	add(&transferJob{dir: download, srcs: []string{"/srv/app/node_modules"}, dstDir: "/home/me/Downloads",
		state: transferRunning, done: 48_200_000, total: 131_000_000, speed: 9_600_000,
		current: "node_modules/@babel/core/lib/transformation/file/file.js"})
	add(&transferJob{dir: upload, srcs: []string{"/home/me/site/public", "/home/me/site/assets", "/home/me/site/index.html"}, dstDir: "/var/www",
		state: transferRunning, done: 3_100_000, total: 7_400_000, speed: 1_250_000,
		current: "public/img/gallery/2026/summer-trip/beach-sunset-wide.jpg"})
	add(&transferJob{dir: download, srcs: []string{"/opt/tools"}, dstDir: "/home/me/Downloads",
		state: transferFailed, total: -1, err: &remotefs.TarError{Status: 127, Stderr: "sh: 1: exec: tar: not found"}})
	add(&transferJob{dir: download, srcs: []string{"/etc/nginx"}, dstDir: "/home/me/Downloads",
		state: transferDone, done: 61_000, total: 61_000, local: "/home/me/Downloads/nginx"})
	a.transfers.running = 2
}

// Running archive transfers in the Transfers tab: the direction, the
// file at work, and "Retry over SFTP" on the failed one.
func TestArchiveTransfersTab(t *testing.T) {
	a, p := testApp(t)
	p.downloads = "/home/me/Downloads"
	s := demoSession(a, "web-1", "45.56.78.90")
	s.sftp = localSFTP(t)
	a.active = 0
	a.bottomTab = 1
	demoArchiveTransfers(a, s)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	tt.Frame()
	for _, want := range []string{
		"node_modules", locales.ArchiveItems("public", 2), locales.DownloadArchiveDirection() + " → /home/me/Downloads",
		locales.UploadArchiveDirection() + " → web-1:/var/www", locales.RetryOverSftp(),
		locales.TransferFailed("sh: 1: exec: tar: not found"),
	} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "file.js") || !strings.Contains(texts, "beach-sunset-wide.jpg") {
		t.Errorf("the files at work don't show; texts %q", tt.Texts())
	}
	saveShot(t, tt, "archive-transfers-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	tt.Frame()
	texts = strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"打包下载 → ", "打包上传 → ", "public 及另外 2 项", "改用 SFTP 重试", "正在解包 ", "正在打包 "} {
		if !strings.Contains(texts, want) {
			t.Errorf("no %q in Chinese; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "archive-transfers-zh.png")
}

func TestTransferErrorText(t *testing.T) {
	cases := map[error]string{
		&remotefs.SkippedLinksError{Names: []string{"a/l", "b"}}:     locales.ArchiveSkippedLinks(2, "a/l, b"),
		fmt.Errorf("x: %w", &remotefs.WindowsNameError{Name: "a:b"}): locales.ArchiveWindowsName("a:b"),
		remotefs.ErrNoShell: locales.ArchiveNoShell(),
		&remotefs.TarError{Status: 2, Stderr: "tar: x: Cannot stat"}: "tar: x: Cannot stat",
		context.Canceled: context.Canceled.Error(),
	}
	for err, want := range cases {
		if got := transferErrorText(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
	if got := locales.ArchiveSkippedLinks(1, "x"); !strings.HasPrefix(got, "1 link points") {
		t.Errorf("one link: %q", got)
	}
}

// testArchiveTransfers uploads a folder of many small files as an
// archive into a folder of its own in home, downloads it back, and
// compares; then a name tar cannot find fails with tar's words.
func testArchiveTransfers(t *testing.T, a *App, p *fakePlatform, s *Session, home string) {
	t.Helper()
	remote := path.Join(home, fmt.Sprintf("fs-archive-app-%d", time.Now().UnixNano()))
	if err := s.sftp.Mkdir(remote); err != nil {
		t.Fatal(err)
	}
	sc := s.sftp
	// Before the session closes.
	defer func() {
		if err := remotefs.RemoveAll(context.Background(), sc, remote); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	}()
	local := t.TempDir()
	writeTree(t, filepath.Join(local, "site"), 400)
	n := len(a.transfers.jobs)
	a.uploadAs(s, []string{filepath.Join(local, "site")}, remote, true)
	p.until(t, "the archive upload", func() bool { return len(a.transfers.jobs) == n+1 && a.transfers.active() == 0 })
	if j := a.transfers.jobs[n]; j.state != transferDone || j.done != j.total || j.total < 400 {
		t.Fatalf("archive upload: %v, %d of %d", j.err, j.done, j.total)
	}
	if fi, err := s.sftp.Lstat(path.Join(remote, "site", "start")); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the link on the server: %v %v", fi, err)
	}
	a.downloadFrom(s, remote, []string{"site"}, "", true)
	p.until(t, "the archive download", func() bool { return len(a.transfers.jobs) == n+2 && a.transfers.active() == 0 })
	j := a.transfers.jobs[n+1]
	if j.state != transferDone || j.local != filepath.Join(p.downloads, "site") {
		t.Fatalf("archive download: %v, local %s", j.err, j.local)
	}
	sameTree(t, filepath.Join(local, "site"), filepath.Join(p.downloads, "site"))

	a.downloadFrom(s, remote, []string{"missing"}, "", true)
	p.until(t, "the failing download", func() bool { return len(a.transfers.jobs) == n+3 && a.transfers.active() == 0 })
	j = a.transfers.jobs[n+2]
	var te *remotefs.TarError
	if j.state != transferFailed || !errors.As(j.err, &te) || !strings.Contains(te.Stderr, "missing") || !a.canRetryOverSFTP(j) {
		t.Errorf("missing: state %d, %v", j.state, j.err)
	}
}
