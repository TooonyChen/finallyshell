package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestPasteUploadFocus(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "files", "127.0.0.1")
	a.active = 0
	s.sftp = localSFTP(t)
	remote, local := t.TempDir(), t.TempDir()
	s.files.cwd, s.files.pathEdit, s.files.entries = remote, remote, nil

	conn := &keyConn{done: make(chan struct{})}
	defer close(conn.done)
	opts := a.terminalOptions(s.Host)
	opts.Conn = conn
	term, err := terminal.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	s.setTerm(term)

	report := filepath.Join(local, "report #1.txt")
	folder := filepath.Join(local, "photos")
	if err := os.WriteFile(report, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("photo"), 0o600); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	paste := func() { tt.Key(ui.Cmd, ui.KeyV) }
	box, ok := tt.Find(locales.ColName())
	if !ok {
		t.Fatal("file table is missing")
	}
	focusTable := func() { tt.ClickAt(box.X+40, box.Y+100) }

	files, err := transfer.FileData(report, folder)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := files.Read(transfer.FileList)
	if err != nil {
		t.Fatal(err)
	}
	var read bool
	offer := func(format transfer.Format, text string) {
		read = false
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Lazy(format, func() ([]byte, error) {
				read = true
				return payload, nil
			}),
			transfer.Bytes(transfer.Text, []byte(text)),
		)))
	}

	// A clipboard with both representations must follow the focused control.
	offer(transfer.FileList, "echo pasted")
	up, ok := tt.Find(locales.ParentFolder())
	if !ok {
		t.Fatal("parent folder button is missing")
	}
	tt.ClickAt(up.X-24, up.Y+up.H/2) // the path field is left of the button
	tt.Key(ui.Cmd, ui.KeyA)
	paste()
	if read || s.files.pathEdit != "echo pasted" {
		t.Fatalf("path field pasted %q, file clipboard read: %v", s.files.pathEdit, read)
	}

	tt.ClickAt(760, 260)
	conn.sent(t, term)
	terminalPaste := a.shortcut(scPaste)
	tt.Key(terminalPaste.Mods, terminalPaste.Key)
	if got := conn.sent(t, term); read || got != "echo pasted" {
		t.Fatalf("terminal pasted %q, file clipboard read: %v", got, read)
	}
	if ui.Cmd == ui.Ctrl {
		paste()
		if got := conn.sent(t, term); read || got != "\x16" {
			t.Fatalf("terminal sent %q for Ctrl+V, file clipboard read: %v", got, read)
		}
	}

	focusTable() // the empty body is focusable even with no selected row
	tt.SetClipboard(report)
	paste()
	offer(transfer.FileList, "echo pasted")
	tt.KeyRepeat(ui.Cmd, ui.KeyV)
	if read {
		t.Fatal("key repeat read the file clipboard")
	}
	paste()
	p.until(t, "pasted uploads", func() bool { return len(a.transfers.jobs) == 2 && a.transfers.active() == 0 })
	for name, want := range map[string]string{"report #1.txt": "report", "photos/a.txt": "photo"} {
		if got, err := os.ReadFile(filepath.Join(remote, name)); err != nil || string(got) != want {
			t.Fatalf("uploaded %s: %q, %v", name, got, err)
		}
	}
	p.until(t, "refreshed listing", func() bool { return !s.files.loading && len(s.files.entries) == 2 })

	// URI lists also work. On macOS the native Edit menu routes Cmd+V
	// through an edit command, so exercise that path as well as key events.
	offer(transfer.URIList, "")
	if runtime.GOOS == "darwin" {
		tt.Command("paste")
	} else {
		paste()
	}
	p.until(t, "replace confirmation", func() bool { return a.replace != nil })
	question := a.replace
	offer(transfer.FileList, "")
	paste()
	if read || a.replace != question {
		t.Fatal("paste escaped the replace dialog")
	}
	tt.Key(0, ui.KeyEscape)
	if a.replace != nil {
		t.Fatal("replace dialog did not close")
	}
	paste()
	p.until(t, "replace confirmation after focus returns", func() bool { return a.replace != nil })
}
