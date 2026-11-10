package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// What the command bar's popups show: the folders of the server, the
// commands on its PATH, the history the app keeps of each host, and the
// server's own shell history. Each is read on a goroutine the first time
// a popup needs it, and the result comes back through App.post; nothing
// is read while the bar is off.

// barData is what a session's popups read from its server.
type barData struct {
	// dirs are the path picker's folders, by listKey, read again each
	// time the picker opens.
	dirs map[string]*dirListing
	// cmds are the command names, read once a connection.
	cmds *nameListing
	// server is the server's shell history, read once a connection.
	server *serverHistory
}

// dirListing is a folder of the path picker.
type dirListing struct {
	dir     string // the folder, with ~ resolved, once read
	entries []barItem
	loading bool
	err     string
}

// nameListing is the commands on the server's PATH.
type nameListing struct {
	client  *ssh.Client // the connection they were read over
	names   []string
	loading bool
	err     string
}

// serverHistory is what the server's shell history files hold.
type serverHistory struct {
	client  *ssh.Client
	cmds    []string // the oldest first
	loading bool
	hidden  bool // Clear list dropped it for this connection
}

// cmdHistories is the history the app keeps of each host
// (config/history.go), read from its file the first time it is needed.
type cmdHistories struct {
	hosts map[string]*hostHistory

	// The reads and writes of the files, done in the order asked by one
	// goroutine at a time, so that a command sent before a Clear does not
	// come back after it, nor two commands swap.
	mu      sync.Mutex
	jobs    []func()
	running bool // a goroutine works through jobs
	// pending counts the jobs not done yet, which tests wait for.
	pending sync.WaitGroup
}

// do queues job, after those queued before it, off the UI thread.
func (q *cmdHistories) do(job func()) {
	q.pending.Add(1)
	q.mu.Lock()
	q.jobs = append(q.jobs, job)
	start := !q.running
	q.running = true
	q.mu.Unlock()
	if start {
		go q.work()
	}
}

// work runs the jobs queued, in order, until there are none.
func (q *cmdHistories) work() {
	for {
		q.mu.Lock()
		if len(q.jobs) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		job := q.jobs[0]
		q.jobs[0] = nil
		q.jobs = q.jobs[1:]
		q.mu.Unlock()
		job()
		q.pending.Done()
	}
}

// hostHistory is a host's history: the commands sent from the bar, the
// oldest first.
type hostHistory struct {
	loading bool
	cmds    []string
}

// hostHistory returns the history of the host id, reading it from its
// file the first time.
func (a *App) hostHistory(id string) *hostHistory {
	if h := a.history.hosts[id]; h != nil {
		return h
	}
	if a.history.hosts == nil {
		a.history.hosts = map[string]*hostHistory{}
	}
	h := &hostHistory{loading: true}
	a.history.hosts[id] = h
	a.history.do(func() {
		list, _ := config.LoadHistory(id)
		a.post(func() {
			// What was sent while it read is newer than what it read.
			for _, c := range h.cmds {
				list = config.AddCommand(list, c)
			}
			h.loading, h.cmds = false, list
		})
	})
	return h
}

// addHistory keeps cmd as the newest command of the host id, in the app
// and in its file.
func (a *App) addHistory(id, cmd string) {
	h := a.hostHistory(id)
	h.cmds = config.AddCommand(h.cmds, cmd)
	a.history.do(func() { config.AddHistory(id, cmd) })
}

// clearHostHistory is the history picker's Clear list: the host's
// history goes, and the server's is not shown again for this
// connection.
func (a *App) clearHostHistory(s *Session) {
	id := s.historyID()
	if a.history.hosts == nil {
		a.history.hosts = map[string]*hostHistory{}
	}
	a.history.hosts[id] = &hostHistory{}
	a.history.do(func() { config.DeleteHistory(id) })
	hideServerHistory(s)
	s.bar.sel, s.bar.field = 0, -1
}

// hideServerHistory keeps the server's history out of s's picker until
// it connects again.
func hideServerHistory(s *Session) {
	d := &s.bar.data
	if d.server == nil || d.server.client != s.client {
		d.server = &serverHistory{client: s.client}
	}
	d.server.hidden = true
}

// clearAllHistory deletes the history of every host, as the Command bar
// page's button does once the user confirms.
func (a *App) clearAllHistory() {
	a.history.hosts = nil
	a.history.do(func() { config.ClearAllHistory() })
	for _, s := range a.sessions {
		if !s.isNewTab() {
			hideServerHistory(s)
		}
	}
}

// deleteHostHistory deletes the history of the host id, as the host is
// deleted for good.
func (a *App) deleteHostHistory(id string) {
	delete(a.history.hosts, id)
	a.history.do(func() { config.DeleteHistory(id) })
}

// historyFor is what s's history picker lists, the newest first: the
// app's history of the host, then the server's commands it lacks; and
// whether either is still being read.
func (a *App) historyFor(s *Session) (cmds []string, loading bool) {
	h := a.hostHistory(s.historyID())
	seen := map[string]bool{}
	for i := len(h.cmds) - 1; i >= 0; i-- {
		if c := h.cmds[i]; !seen[c] {
			seen[c] = true
			cmds = append(cmds, c)
		}
	}
	loading = h.loading
	if a.settings.NoServerHistory || s.isLocal() { // a local terminal has no server
		return cmds, loading
	}
	sh := a.serverHistory(s)
	if sh.hidden {
		return cmds, loading
	}
	for i := len(sh.cmds) - 1; i >= 0; i-- {
		if c := sh.cmds[i]; !seen[c] {
			seen[c] = true
			cmds = append(cmds, c)
		}
	}
	return cmds, loading || sh.loading
}

// serverHistory returns s's server's shell history, reading it over SFTP
// the first time on a connection.
func (a *App) serverHistory(s *Session) *serverHistory {
	d := &s.bar.data
	if d.server != nil && d.server.client == s.client {
		return d.server
	}
	sc := s.sftp
	if sc == nil {
		return &serverHistory{} // SFTP may come yet: asked again then
	}
	sh := &serverHistory{client: s.client, loading: true}
	d.server = sh
	go func() {
		cmds := readServerHistory(sc)
		a.post(func() { sh.loading, sh.cmds = false, cmds })
	}()
	return sh
}

// serverHistoryFiles are the shell history files read in the home folder,
// and whether each is zsh's.
var serverHistoryFiles = []struct {
	name string
	zsh  bool
}{{".bash_history", false}, {".zsh_history", true}}

// maxServerHistory is how much of the end of each history file is read.
const maxServerHistory = 2 << 20

// readServerHistory reads the shell history files of the home folder
// over sc, the oldest command first; those it cannot read are left out.
func readServerHistory(sc *sftp.Client) []string {
	home, err := sc.Getwd()
	if err != nil {
		return nil
	}
	var all []string
	for _, f := range serverHistoryFiles {
		data, err := readTail(sc, path.Join(home, f.name), maxServerHistory)
		if err != nil {
			continue
		}
		all = append(all, config.ParseShellHistory(data, f.zsh)...)
	}
	return config.Deduped(all)
}

// readTail reads the last limit bytes of the file p, from the start of a
// line.
func readTail(sc *sftp.Client, p string, limit int64) ([]byte, error) {
	f, err := sc.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	cut := fi.Size() > limit
	if cut {
		if _, err := f.Seek(fi.Size()-limit, io.SeekStart); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}
	if cut {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, nil
}

// listKey is the folder the path picker lists for dirRaw, the folder
// part of the word: an absolute path, or one starting with ~ for the
// home folder, which the listing resolves. A relative one is from the
// shell's working directory, as it reports it (OSC 7) or the server
// tells (shellDir), else from the file panel's folder, else from the
// home folder.
func (a *App) listKey(s *Session, dirRaw string) string {
	dir := unescape(dirRaw)
	switch {
	case strings.HasPrefix(dir, "/"):
		return path.Clean(dir)
	case dir == "~" || strings.HasPrefix(dir, "~/"):
		return homeKey(dir[1:])
	}
	base := s.shellDir()
	if base == "" {
		base = s.files.cwd
	}
	if base == "" {
		return homeKey(dir)
	}
	return path.Join(base, dir)
}

// homeKey is the key of a folder in the home folder: "~", or "~/" and
// the path in it.
func homeKey(rest string) string {
	if c := path.Clean("/" + rest); c != "/" {
		return "~" + c
	}
	return "~"
}

// listing returns the folder of key, starting to read it the first time
// the picker asks.
func (a *App) listing(s *Session, key string) *dirListing {
	d := &s.bar.data
	if l := d.dirs[key]; l != nil {
		return l
	}
	if d.dirs == nil {
		d.dirs = map[string]*dirListing{}
	}
	l := &dirListing{}
	d.dirs[key] = l
	sc := s.sftp
	if sc == nil {
		l.err = locales.CmdBarNoSFTP()
		if s.files.err != nil {
			l.err = fileErrorText(s.files.err)
		}
		return l
	}
	l.loading = true
	go func() {
		dir, entries, err := readPickerDir(sc, key)
		a.post(func() {
			l.loading, l.dir, l.entries = false, dir, entries
			if err != nil {
				l.err = locales.CannotOpenFolder(dir, err.Error())
			}
		})
	}()
	return l
}

// readPickerDir lists the folder of key: folders first, then files, by
// name, the hidden ones after the others.
func readPickerDir(sc *sftp.Client, key string) (string, []barItem, error) {
	dir := key
	if strings.HasPrefix(key, "~") {
		home, err := sc.Getwd()
		if err != nil {
			return key, nil, err
		}
		dir = path.Join(home, key[1:])
	}
	entries, err := remotefs.ReadDir(sc, dir)
	if err != nil {
		return dir, nil, err
	}
	items := make([]barItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, barItem{name: e.Name, dir: e.IsDir(), hidden: strings.HasPrefix(e.Name, "."),
			bad: config.HasControl(e.Name, false)})
	}
	slices.SortStableFunc(items, func(x, y barItem) int {
		switch {
		case x.hidden == y.hidden:
			return 0
		case y.hidden:
			return -1
		}
		return 1
	})
	return dir, items, nil
}

// parentDir is the folder part of p one folder up, for Backspace in an
// empty filter; false at the top.
func (a *App) parentDir(s *Session, p pathWord) (string, bool) {
	d := p.dirRaw
	switch {
	case d == "" || d == "./":
		return "../", true
	case d == "../" || strings.HasSuffix(d, "/../"):
		return d + "../", true
	case d == "/":
		return "", false
	case d == "~/":
		// The home folder's parent, by its path, once known.
		if l := s.bar.data.dirs["~"]; l != nil && l.dir != "" && l.dir != "/" {
			return escapePath(strings.TrimSuffix(path.Dir(l.dir), "/")) + "/", true
		}
		return "", false
	}
	r := []rune(strings.TrimSuffix(d, "/"))
	for i := len(r) - 1; i >= 0; i-- {
		if r[i] == '/' && !escapedAt(r, i) {
			return string(r[:i+1]), true
		}
	}
	return "", true // a folder of the current one: the current one
}

// commandNames returns the commands on the server's PATH, starting to
// read them the first time on a connection.
func (a *App) commandNames(s *Session) *nameListing {
	d := &s.bar.data
	if d.cmds != nil && d.cmds.client == s.client {
		return d.cmds
	}
	l := &nameListing{client: s.client, loading: true}
	d.cmds = l
	client, sc, exec := s.client, s.sftp, execAllowed(s.Host)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), commandsTimeout)
		defer cancel()
		names, err := readCommandNames(ctx, client, sc, exec)
		a.post(func() {
			l.loading, l.names = false, names
			if err != nil {
				l.err = locales.CmdBarNoCommands()
			}
		})
	}()
	return l
}

// commandsTimeout is how long reading the command names may take.
const commandsTimeout = 20 * time.Second

// commandsScript lists the commands a shell finds, on stdin of sh:
// bash's compgen, which has the builtins and keywords too, or else the
// files of each folder on the PATH, for a server without bash.
const commandsScript = `if command -v bash >/dev/null 2>&1 && bash -c 'compgen -c' 2>/dev/null; then exit 0; fi
IFS=:
for d in $PATH; do ls -1 "$d" 2>/dev/null; done
`

// fallbackPath are the folders listed over SFTP for a host that allows
// no exec channel (its server monitor is off), or a server that ran no
// script.
var fallbackPath = []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}

var errNoCommands = errors.New("no commands")

// readCommandNames lists the server's commands: with the script, over
// one exec channel of client, when exec allows it, else over SFTP.
func readCommandNames(ctx context.Context, client *ssh.Client, sc *sftp.Client, exec bool) ([]string, error) {
	if exec && client != nil {
		if out, err := runCommandsScript(ctx, client); err == nil {
			if names := commandNameList(strings.Split(out, "\n")); len(names) > 0 {
				return names, nil
			}
		}
	}
	if sc == nil {
		return nil, errNoCommands
	}
	var names []string
	for _, d := range fallbackPath {
		infos, err := sc.ReadDir(d)
		if err != nil {
			continue
		}
		for _, fi := range infos {
			if !fi.IsDir() && (fi.Mode()&0o111 != 0 || fi.Mode()&fs.ModeSymlink != 0) {
				names = append(names, fi.Name())
			}
		}
	}
	names = commandNameList(names)
	if len(names) == 0 {
		return nil, errNoCommands
	}
	return names, nil
}

// runCommandsScript runs commandsScript over a new exec channel.
func runCommandsScript(ctx context.Context, client *ssh.Client) (string, error) {
	s, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	s.Stdin = strings.NewReader(commandsScript)
	out, err := s.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := s.Start("sh -s"); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(out, 4<<20))
	s.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(data), err
}

// commandNameList is the names of lines, sorted, each once, without
// those that cannot be a command's, or be inserted in the bar: a name
// with a control character is dropped (config.HasControl).
func commandNameList(lines []string) []string {
	var names []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.ContainsAny(l, "/ ") && !config.HasControl(l, false) {
			names = append(names, l)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
