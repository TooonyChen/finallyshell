package app

// The editor of remote text files, as FinalShell's: a window of its own
// with a tab for each file opened from the file panel, which reads the
// file over SFTP into memory and saves it back.
//
// Each tab remembers the session it came from. While that session is not
// connected the text stays and Save is off; once it reconnects, saving
// uses its new SFTP client. A save first compares the file's size and
// time with those it had when it was read (or last saved), and asks
// before overwriting someone else's change.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"finallyshell/internal/config"
	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/pkg/sftp"
)

// WindowFileEditor is the editor's window.
const WindowFileEditor WindowKind = "fileEditor"

// maxEditSize is the size of file the editor asks about before reading:
// the whole file is read into memory, and a big one takes long to come.
// Tests make it smaller.
var maxEditSize int64 = 10 << 20

type editTabState int

const (
	tabLoading  editTabState = iota // reading the file
	tabTooLarge                     // asking before reading a big file
	tabBinary                       // not text: offering the default app
	tabFailed                       // could not read it
	tabReady                        // in the editor
)

// editTab is a file open in the editor.
type editTab struct {
	id    int64
	sess  *Session
	host  string // the host's title, as the window shows it
	path  string
	state editTabState
	err   error // why it could not be read

	// Reading: the bytes so far and the file's size, and what stops it.
	done, total int64
	cancel      context.CancelFunc

	code *editor.State
	// raw is the file as it was read or last saved, which choosing
	// another encoding decodes again until the text is edited (decodedAt
	// is the buffer's version then).
	raw       []byte
	decodedAt uint64
	bom       bool
	// enc is the encoding the file saves in; savedEnc and savedEnding
	// what it was saved in last: changing either is a change to save.
	enc, savedEnc textEncoding
	savedEnding   string
	notUTF8       bool // it is not UTF-8: suggest GB18030 while enc is UTF-8
	// The file's time and size on the server as read or last saved, to
	// tell another change before a save replaces it.
	mtime time.Time
	size  int64

	saving         bool
	saveErr        error
	savedAt        time.Time // the last save that worked
	closeAfterSave bool      // closing it waits for its save
	// conflict asks before a save replaces a file that changed on the
	// server (or is gone) since it was read.
	conflict, conflictGone bool

	// Opening the file with the system's app: downloading it, and how it
	// went.
	opening    bool
	openErr    error
	openedWith string // the local copy, once opened

	// Modified, as of the buffer's version modAt: asking the buffer
	// compares the whole text.
	modKnown bool
	modAt    uint64
	modBuf   bool
}

func (t *editTab) name() string { return path.Base(t.path) }

// title is the window's title for the tab: host:path.
func (t *editTab) title() string { return t.host + ":" + t.path }

// fileEditor is the editor window's state.
type fileEditor struct {
	tabs   []*editTab
	sel    int
	nextID int64
	wrap   bool   // soft wrap, in every tab
	title  string // the window's title as last set
	// The questions: closing a tab with changes, closing the window with
	// some.
	askTab    *editTab
	askWindow bool
	// discard closes the window without asking, as the user chose.
	discard bool
	// refocus gives the text the keyboard again, after a button that
	// changed it took it.
	refocus bool
}

func (fe *fileEditor) current() *editTab {
	if fe.sel >= 0 && fe.sel < len(fe.tabs) {
		return fe.tabs[fe.sel]
	}
	return nil
}

// modified tells whether t has changes to save.
func (t *editTab) modified() bool {
	if t.code == nil {
		return false
	}
	buf := t.code.Buffer
	if v := buf.Version(); !t.modKnown || v != t.modAt {
		t.modKnown, t.modAt, t.modBuf = true, v, buf.Modified()
	}
	return t.modBuf || buf.LineEnding() != t.savedEnding || t.enc != t.savedEnc
}

func (t *editTab) markSaved() {
	t.code.Buffer.MarkSaved()
	t.modKnown = false
}

// unsavedFiles counts the tabs with changes to save.
func (a *App) unsavedFiles() int {
	n := 0
	for _, t := range a.fe.tabs {
		if t.modified() {
			n++
		}
	}
	return n
}

// editable tells whether the file panel opens e in the editor: a file
// or a link to one, not a device or a pipe.
func editable(e remotefs.Entry) bool {
	return !e.IsDir() && (e.Mode.IsRegular() || e.Link)
}

// entry is the entry of the file panel's names when it is one name, as
// its context menu edits.
func (f *fileState) entry(names []string) (remotefs.Entry, bool) {
	if len(names) != 1 {
		return remotefs.Entry{}, false
	}
	i := slices.IndexFunc(f.entries, func(e remotefs.Entry) bool { return e.Name == names[0] })
	if i < 0 {
		return remotefs.Entry{}, false
	}
	return f.entries[i], true
}

// fileEditorSpec is the editor's window: big, as an editor's is.
func (a *App) fileEditorSpec() WindowSpec {
	return WindowSpec{
		Kind: WindowFileEditor, Title: a.fileEditorTitle,
		Width: 1000, Height: 700, MinWidth: 560, MinHeight: 360,
		OwnKeys: true, CloseRequested: a.closeFileEditorRequested, Closed: a.fileEditorClosed,
	}
}

func (a *App) fileEditorTitle() string {
	if t := a.fe.current(); t != nil {
		return t.title()
	}
	return locales.FileEditor()
}

// openRemoteFile opens the file p of s in the editor, in a tab of its
// own, or selects its tab when it is open already.
func (a *App) openRemoteFile(s *Session, p string) {
	fe := &a.fe
	i := slices.IndexFunc(fe.tabs, func(t *editTab) bool { return t.sess == s && t.path == p })
	if i < 0 {
		fe.nextID++
		t := &editTab{id: fe.nextID, sess: s, host: s.label(), path: p, enc: encUTF8, savedEnc: encUTF8}
		fe.tabs = append(fe.tabs, t)
		i = len(fe.tabs) - 1
		a.loadTab(t, false)
	}
	fe.sel = i
	fe.discard = false
	a.openWindow(a.fileEditorSpec(), a.fileEditorView)
}

// sftpFor is the SFTP client t saves and reads with: its session's, while
// it is connected. A tab whose session was closed takes another session
// to the same host that is.
func (a *App) sftpFor(t *editTab) *sftp.Client {
	if !slices.Contains(a.sessions, t.sess) && t.sess.Host != nil {
		for _, s := range a.sessions {
			if s.Host != nil && s.Host.ID == t.sess.Host.ID && s.status == statusConnected && s.sftp != nil {
				t.sess = s
				break
			}
		}
	}
	if s := t.sess; slices.Contains(a.sessions, s) && s.status == statusConnected && s.sftp != nil {
		return s.sftp
	}
	return nil
}

// loadTab reads t's file on a goroutine, asking first for a big one
// unless anyway is set.
func (a *App) loadTab(t *editTab, anyway bool) {
	sc := a.sftpFor(t)
	if sc == nil {
		t.state, t.err = tabFailed, errNotConnected
		return
	}
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.state, t.err, t.done, t.total = tabLoading, nil, 0, -1
	p := t.path
	go func() {
		fi, err := sc.Stat(p)
		if err == nil && fi.Mode().IsRegular() && fi.Size() > maxEditSize && !anyway {
			a.post(func() {
				if ctx.Err() == nil {
					t.state, t.total = tabTooLarge, fi.Size()
				}
			})
			return
		}
		if err == nil {
			size := fi.Size()
			a.post(func() { t.total = size })
		}
		var last time.Time
		data, info, err := remotefs.ReadText(ctx, sc, p, func(done int64) {
			if now := time.Now(); now.Sub(last) >= 50*time.Millisecond {
				last = now
				a.post(func() { t.done = done })
			}
		})
		a.post(func() {
			if ctx.Err() != nil {
				return // closed, or read again
			}
			switch {
			case errors.Is(err, remotefs.ErrBinary):
				t.state = tabBinary
			case err != nil:
				t.state, t.err = tabFailed, err
			default:
				a.fillEditTab(t, data, info)
			}
		})
	}()
}

// errNotConnected is a tab whose session is not connected.
var errNotConnected = errors.New("not connected")

// fillEditTab shows the file's bytes in t's editor, as UTF-8.
func (a *App) fillEditTab(t *editTab, data []byte, fi fs.FileInfo) {
	t.raw, t.mtime, t.size = data, fi.ModTime(), fi.Size()
	t.enc, t.savedEnc = encUTF8, encUTF8
	d := decodeText(data, encUTF8)
	t.notUTF8 = !utf8.Valid(data)
	buf := editor.NewBuffer(d.text)
	buf.SetLineEnding(d.ending)
	buf.SetLanguage(languageOf(t.path, d.text))
	t.bom, t.savedEnding = d.bom, d.ending
	t.code = &editor.State{Buffer: buf, Font: codeFont, Label: t.name()}
	t.decodedAt = buf.Version()
	t.modKnown = false
	t.state = tabReady
}

// codeFont is the editor's: the terminal's default monospaced fonts.
var codeFont = ui.Font{Family: config.DefaultTermFontFamily, Size: 13}

// setEncoding makes enc the encoding t saves in, and before any edit
// reads the file again in it.
func (a *App) setEncoding(t *editTab, enc textEncoding) {
	if t.code == nil || t.enc == enc {
		return
	}
	buf := t.code.Buffer
	if buf.Version() == t.decodedAt && !t.modified() {
		d := decodeText(t.raw, enc)
		buf.Reset(d.text)
		buf.SetLineEnding(d.ending)
		t.bom, t.savedEnding = d.bom, d.ending
		t.decodedAt = buf.Version()
		t.savedEnc = enc
		t.modKnown = false
	}
	t.enc = enc
}

// saveTab saves t's text to the server, asking first when the file
// changed there since it was read, unless overwrite is set.
func (a *App) saveTab(t *editTab, overwrite bool) {
	if t.state != tabReady || t.saving {
		return
	}
	sc := a.sftpFor(t)
	if sc == nil {
		t.closeAfterSave = false
		return
	}
	buf := t.code.Buffer
	ending, enc, version := buf.LineEnding(), t.enc, buf.Version()
	data, err := encodeText(buf.String(), enc, ending, t.bom && enc == encUTF8)
	if err != nil {
		t.saveErr, t.closeAfterSave = err, false
		return
	}
	t.saving, t.saveErr, t.conflict = true, nil, false
	p, mtime, size := t.path, t.mtime, t.size
	go func() {
		if !overwrite {
			fi, err := sc.Stat(p)
			gone := errors.Is(err, fs.ErrNotExist)
			if gone || err == nil && (!fi.ModTime().Equal(mtime) || fi.Size() != size) {
				a.post(func() {
					t.saving, t.conflict, t.conflictGone = false, true, gone
					if i := slices.Index(a.fe.tabs, t); i >= 0 {
						a.fe.sel = i
					}
				})
				return
			}
		}
		_, fi, err := remotefs.SaveText(sc, p, data)
		a.post(func() {
			t.saving = false
			if err != nil {
				t.saveErr, t.closeAfterSave = err, false
				return
			}
			t.raw, t.mtime, t.size = data, fi.ModTime(), fi.Size()
			t.savedEnding, t.savedEnc, t.savedAt = ending, enc, time.Now()
			if buf.Version() == version { // not edited while it saved
				t.markSaved()
				t.decodedAt = version
			}
			if t.closeAfterSave && !t.modified() {
				a.closeTab(t)
			}
		})
	}()
}

// reloadTab reads t's file again into its editor, as one edit, which
// Undo takes back.
func (a *App) reloadTab(t *editTab) {
	sc := a.sftpFor(t)
	if sc == nil || t.code == nil {
		return
	}
	t.conflict, t.closeAfterSave, t.saveErr = false, false, nil
	p, enc := t.path, t.enc
	t.saving = true // no save meanwhile
	go func() {
		data, fi, err := remotefs.ReadText(context.Background(), sc, p, nil)
		a.post(func() {
			t.saving = false
			if err != nil {
				t.saveErr = err
				return
			}
			d := decodeText(data, enc)
			buf := t.code.Buffer
			buf.Set(d.text)
			buf.SetLineEnding(d.ending)
			t.markSaved()
			t.raw, t.mtime, t.size = data, fi.ModTime(), fi.Size()
			t.bom, t.savedEnding, t.savedEnc = d.bom, d.ending, enc
			t.decodedAt = buf.Version()
			a.fe.refocus = true
		})
	}()
}

// requestCloseTab closes t, asking first when it has changes.
func (a *App) requestCloseTab(t *editTab) {
	if t == nil {
		return
	}
	if t.modified() {
		a.fe.askTab = t
		if i := slices.Index(a.fe.tabs, t); i >= 0 {
			a.fe.sel = i
		}
		return
	}
	a.closeTab(t)
}

// closeTab closes t as it is; the last tab closes the window.
func (a *App) closeTab(t *editTab) {
	fe := &a.fe
	i := slices.Index(fe.tabs, t)
	if i < 0 {
		return
	}
	if t.cancel != nil {
		t.cancel()
	}
	if fe.askTab == t {
		fe.askTab = nil
	}
	fe.tabs = slices.Delete(fe.tabs, i, i+1)
	if fe.sel > i || fe.sel >= len(fe.tabs) {
		fe.sel--
	}
	if len(fe.tabs) == 0 {
		fe.discard = true
		a.closeWindow(WindowFileEditor)
	}
}

// closeFileEditorRequested tells whether the editor's window may close
// as the user asks: with changes to save it asks first and returns false.
func (a *App) closeFileEditorRequested() bool {
	if a.fe.discard || a.quitApproved || a.unsavedFiles() == 0 {
		return true
	}
	a.fe.askWindow = true
	a.plat.FocusWindow(WindowFileEditor)
	return false
}

// requestCloseFileEditor closes the editor's window, as Cmd+W does with
// no tab open, asking first when there are changes to save.
func (a *App) requestCloseFileEditor() {
	if a.closeFileEditorRequested() {
		a.fe.discard = true
		a.closeWindow(WindowFileEditor)
	}
}

// saveAllAndClose saves each tab with changes and closes it once saved,
// closing the others now: the window closes with the last.
func (a *App) saveAllAndClose() {
	for _, t := range slices.Clone(a.fe.tabs) {
		if t.modified() {
			t.closeAfterSave = true
			a.saveTab(t, false)
		} else {
			a.closeTab(t)
		}
	}
}

// fileEditorClosed forgets the tabs as the window closes.
func (a *App) fileEditorClosed() {
	for _, t := range a.fe.tabs {
		if t.cancel != nil {
			t.cancel()
		}
	}
	a.fe = fileEditor{nextID: a.fe.nextID, wrap: a.fe.wrap}
}

// openWithSystem downloads t's file to a folder of the app's and opens
// it with the system's app for it, as for a binary file.
func (a *App) openWithSystem(t *editTab) {
	client := a.sftpFor(t)
	if client == nil || t.opening {
		return
	}
	t.opening, t.openErr = true, nil
	root := a.plat.TempDir()
	dir := filepath.Join(root, fmt.Sprintf("%d", time.Now().UnixNano()))
	ctx, cancel := context.WithCancel(context.Background())
	if t.cancel != nil {
		t.cancel()
	}
	t.cancel = cancel
	p := t.path
	go func() {
		removeOld(root, 24*time.Hour)
		local := ""
		err := os.MkdirAll(dir, 0o700)
		if err == nil {
			local, err = remotefs.Download(ctx, client, p, dir, nil)
		}
		if err == nil {
			err = a.plat.OpenPath(local)
		}
		a.post(func() {
			t.opening, t.openErr = false, err
			if err == nil {
				t.openedWith = local
			}
		})
	}()
}

// removeOld removes the folders in root older than age: the copies of
// files opened with other apps, which those may still read for a while.
func removeOld(root string, age time.Duration) {
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && e.IsDir() && time.Since(fi.ModTime()) > age && strings.Trim(e.Name(), "0123456789") == "" {
			os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}
