package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func fileCopyApp(t *testing.T) (*App, *fakePlatform, *Session, *ui.Tester, string) {
	t.Helper()
	a, p := testApp(t)
	s := demoSession(a, "files", "127.0.0.1")
	a.active = 0
	s.sftp = localSFTP(t)
	s.files.init()
	dir := filepath.ToSlash(t.TempDir())
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	return a, p, s, tt, dir
}

func TestCopyRemoteFilesFocus(t *testing.T) {
	a, p, s, tt, remote := fileCopyApp(t)
	const report = "report #1.txt"
	if err := os.WriteFile(filepath.Join(remote, report), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(remote, "photos", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.openDir(s, remote)
	p.until(t, "remote listing", func() bool { return !s.files.loading && len(s.files.entries) == 2 })

	if err := tt.Click("photos"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Shift, report); err != nil {
		t.Fatal(err)
	}
	up, ok := tt.Find(locales.ParentFolder())
	if !ok {
		t.Fatal("parent folder button is missing")
	}
	tt.ClickAt(up.X-24, up.Y+up.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	if len(a.transfers.jobs) > 0 || tt.Clipboard() != s.files.pathEdit {
		t.Fatalf("the path field didn't keep text copy: clipboard %q, %d transfers", tt.Clipboard(), len(a.transfers.jobs))
	}

	row, ok := tt.Find(report)
	if !ok {
		t.Fatal("file row is missing")
	}
	// The row is already chosen. Restore table focus, keeping the selection.
	tt.ClickAtWith(ui.Shift, row.X+row.W/2, row.Y+row.H/2)
	tt.KeyRepeat(ui.Cmd, ui.KeyC)
	if len(a.transfers.jobs) != 0 {
		t.Fatal("key repeat queued a copy")
	}
	a.transfers.running = maxRunning
	if runtime.GOOS == "darwin" {
		tt.Command("copy")
	} else {
		tt.Key(ui.Cmd, ui.KeyC)
	}
	if slices.Contains(tt.ClipboardData().Formats(), transfer.FileList) {
		t.Fatal("incomplete files offered to the clipboard")
	}
	if a.transfers.popover.open || tt.HasText("Preparing files to paste") {
		t.Fatal("copy opened a popover or showed a toast")
	}
	if err := tt.Click(locales.DownloadsButton()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.CopyToClipboard()) || !tt.HasText(locales.TransferWaiting()) {
		t.Fatalf("waiting copy not shown in Downloads: %v", tt.Texts())
	}
	a.transfers.running = 0
	a.pump()
	p.until(t, "file clipboard", func() bool {
		return len(a.transfers.jobs) == 1 && a.transfers.jobs[0].finished()
	})
	job := a.transfers.jobs[0]
	if job.state != transferDone {
		t.Fatalf("copy state %d: %v", job.state, job.err)
	}
	if !tt.HasText(locales.CopyToClipboard()) || !tt.HasText(locales.TransferDone()) || tt.HasText("Files copied.") {
		t.Fatalf("copy completion should appear only in Downloads: %v", tt.Texts())
	}
	files, err := tt.ClipboardData().Files()
	want := []string{filepath.Join(job.local, "photos"), filepath.Join(job.local, report)}
	if err != nil || !slices.Equal(files, want) {
		t.Fatalf("clipboard files %q, want %q: %v", files, want, err)
	}
	if fi, err := os.Stat(filepath.Join(files[0], "empty")); err != nil || !fi.IsDir() {
		t.Fatalf("empty subfolder: %v", err)
	}
	if b, err := os.ReadFile(files[1]); err != nil || string(b) != "report" {
		t.Fatalf("cached report %q: %v", b, err)
	}

	linux, err := fileClipboardData(files, "linux")
	if err != nil {
		t.Fatal(err)
	}
	uris, err := linux.URLs()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := linux.Read(gnomeCopiedFiles); err != nil || string(got) != "copy\n"+strings.Join(uris, "\n") {
		t.Fatalf("GNOME copy data %q: %v", got, err)
	}
}

func TestCopyRemoteFilesShowsClipboardErrorsInDownloads(t *testing.T) {
	a, p, s, tt, remote := fileCopyApp(t)
	s.files.cwd = remote
	if err := os.WriteFile(filepath.Join(remote, "notes.txt"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("clipboard busy")
	p.clipboardWriteErr = failure
	a.copyRemoteFiles(s, []string{"notes.txt"})
	job := a.transfers.jobs[0]
	if job.state != transferFailed || !errors.Is(job.err, failure) || !a.canRetry(job) {
		t.Fatalf("failed copy: state %d, error %v", job.state, job.err)
	}
	tt.Frame()
	if tt.HasText(failure.Error()) || a.transfers.popover.open {
		t.Fatal("copy failure was shown outside the Downloads queue")
	}
	if err := tt.Click(locales.DownloadsButton()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.CopyToClipboard()) || !tt.HasText(locales.TransferFailed(failure.Error())) {
		t.Fatalf("failed copy not shown in Downloads: %v", tt.Texts())
	}
	p.clipboardWriteErr = nil
	a.retry(job)
	p.until(t, "successful retry", job.finished)
	if job.state != transferDone || !tt.HasText(locales.TransferDone()) {
		t.Fatalf("retry state %d: %v", job.state, job.err)
	}
}

func TestCopyRemoteFilesClipboardChangesAndCancel(t *testing.T) {
	a, p, s, tt, remote := fileCopyApp(t)
	s.files.cwd = remote
	if err := os.WriteFile(filepath.Join(remote, "notes.txt"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}

	a.copyRemoteFiles(s, []string{"notes.txt"})
	job := a.transfers.jobs[0]
	tt.SetClipboard("newer copy")
	p.until(t, "the superseded copy", job.finished)
	if job.state != transferFailed || tt.Clipboard() != "newer copy" {
		t.Fatalf("superseded copy state %d, clipboard %q: %v", job.state, tt.Clipboard(), job.err)
	}

	a.retry(job)
	a.cancelTransfer(job)
	p.until(t, "canceled retry", job.finished)
	if job.state != transferCanceled || slices.Contains(tt.ClipboardData().Formats(), transfer.FileList) {
		t.Fatalf("canceled copy state %d, clipboard %v", job.state, tt.ClipboardData().Formats())
	}

	a.retry(job)
	p.until(t, "successful retry", job.finished)
	if job.state != transferDone {
		t.Fatalf("retry state %d: %v", job.state, job.err)
	}
	files, err := tt.ClipboardData().Files()
	if err != nil || len(files) != 1 || filepath.Base(files[0]) != "notes.txt" {
		t.Fatalf("retry clipboard %q: %v", files, err)
	}
}
