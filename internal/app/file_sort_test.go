package app

import (
	"bytes"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func fileSortSample() []remotefs.Entry {
	stamp := func(second int64) time.Time { return time.Unix(1_700_000_000+second, 0) }
	return []remotefs.Entry{
		{Name: "z-dir", Mode: fs.ModeDir | 0o755, ModTime: stamp(30)},
		{Name: "a-dir", Mode: fs.ModeDir | 0o700, ModTime: stamp(20)},
		{Name: "middle.sh", Size: 100, Mode: 0o755, ModTime: stamp(10)},
		{Name: "zeta.bin", Size: 9, Mode: 0o644, ModTime: stamp(20)},
		{Name: "alpha.log", Size: 2, Mode: 0o600, ModTime: stamp(30)},
	}
}

func fileSortNames(f *fileState) []string {
	names := make([]string, len(f.entries))
	for i, e := range f.entries {
		names[i] = e.Name
	}
	return names
}

var fileSortCases = []struct {
	column string
	asc    []string
	desc   []string
}{
	{"name", []string{"a-dir", "z-dir", "alpha.log", "middle.sh", "zeta.bin"}, []string{"z-dir", "a-dir", "zeta.bin", "middle.sh", "alpha.log"}},
	{"size", []string{"a-dir", "z-dir", "alpha.log", "zeta.bin", "middle.sh"}, []string{"a-dir", "z-dir", "middle.sh", "zeta.bin", "alpha.log"}},
	{"type", []string{"a-dir", "z-dir", "zeta.bin", "alpha.log", "middle.sh"}, []string{"a-dir", "z-dir", "middle.sh", "alpha.log", "zeta.bin"}},
	{"modified", []string{"a-dir", "z-dir", "middle.sh", "zeta.bin", "alpha.log"}, []string{"z-dir", "a-dir", "alpha.log", "zeta.bin", "middle.sh"}},
	{"perms", []string{"a-dir", "z-dir", "alpha.log", "zeta.bin", "middle.sh"}, []string{"z-dir", "a-dir", "middle.sh", "zeta.bin", "alpha.log"}},
}

func TestFileSortOrder(t *testing.T) {
	_, p := testApp(t)
	p.SetLocale("en")
	for _, tc := range fileSortCases {
		for _, descending := range []bool{false, true} {
			t.Run(tc.column+map[bool]string{false: "/ascending", true: "/descending"}[descending], func(t *testing.T) {
				f := fileState{entries: fileSortSample(), row: 2}
				f.sort.Column, f.sort.Descending = tc.column, descending
				f.chosen.Add("middle.sh")
				f.chosen.Add("alpha.log")
				f.sortEntries()
				want := tc.asc
				if descending {
					want = tc.desc
				}
				if got := fileSortNames(&f); !slices.Equal(got, want) {
					t.Fatalf("names %q, want %q", got, want)
				}
				if f.cursorName() != "middle.sh" || f.chosen.Len() != 2 || !f.chosen.Has("alpha.log") {
					t.Fatal("sorting changed the cursor or selection")
				}
			})
		}
	}
	// Compare bytes without subtraction overflow, and keep name ties
	// deterministic even when the initial listing uses another order.
	f := fileState{row: -1, entries: []remotefs.Entry{
		{Name: "huge", Size: math.MaxInt64}, {Name: "a", Size: 9}, {Name: "A", Size: 9},
		{Name: "small", Size: 2}, {Name: "missing", Link: true, Broken: true, Size: 1},
		{Name: "linked-dir", Link: true, Mode: fs.ModeDir},
	}}
	f.sort.Column = "size"
	f.sortEntries()
	if got := fileSortNames(&f); !slices.Equal(got, []string{"linked-dir", "small", "A", "a", "huge", "missing"}) {
		t.Fatalf("size edge cases %q", got)
	}
	f.sort.Descending = true
	f.sortEntries()
	if got := fileSortNames(&f); !slices.Equal(got, []string{"linked-dir", "huge", "A", "a", "small", "missing"}) {
		t.Fatalf("descending size edge cases %q", got)
	}
}

func fileSortSession(a *App) *Session {
	s := demoSession(a, "files", "10.0.0.7")
	f := &s.files
	f.cwd, f.pathEdit, f.entries = "/srv/app", "/srv/app", fileSortSample()
	f.subdirs = map[string][]string{"/": {"/srv"}, "/srv": {"/srv/app"}, "/srv/app": {"/srv/app/a-dir", "/srv/app/z-dir"}}
	f.tree.Open.Add("/srv")
	f.tree.Open.Add("/srv/app")
	return s
}

func TestFileTableSortHeaders(t *testing.T) {
	a, p := testApp(t)
	s := fileSortSession(a)
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 900)
	p.tt = tt
	for _, language := range []string{"en", "zh-Hans"} {
		p.SetLocale(language)
		s.files.sort.Column, s.files.sort.Descending = "name", false
		tt.Frame()
		tt.ClickAt(rowCenter(t, tt, "alpha.log"))
		if err := tt.ClickWith(ui.Cmd, "middle.sh"); err != nil {
			t.Fatal(err)
		}
		if !s.files.chosen.Has("alpha.log") || !s.files.chosen.Has("middle.sh") {
			t.Fatal("could not select two file rows")
		}
		cursor := s.files.cursorName()
		for _, tc := range fileSortCases {
			header := map[string]string{"name": locales.ColName(), "size": locales.ColSize(), "type": locales.ColType(), "modified": locales.ColModified(), "perms": locales.ColPermissions()}[tc.column]
			// Name is already active; its first click reverses the default.
			for _, descending := range []bool{tc.column == "name", tc.column != "name"} {
				if err := tt.Click(header); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				want := tc.asc
				if descending {
					want = tc.desc
				}
				if s.files.sort.Column != tc.column || s.files.sort.Descending != descending || !slices.Equal(fileSortNames(&s.files), want) {
					t.Fatalf("%s header: sort %+v, rows %q; want descending=%v rows %q", header, s.files.sort, fileSortNames(&s.files), descending, want)
				}
				var lastY float32 = -1
				for _, name := range want {
					// These folder names also appear in the separate tree.
					if name == "a-dir" || name == "z-dir" {
						continue
					}
					box, ok := tt.Find(name)
					if !ok || box.Y <= lastY {
						t.Fatalf("%s is not rendered in the sorted row order", name)
					}
					lastY = box.Y
				}
				if s.files.cursorName() != cursor || s.files.chosen.Len() != 2 {
					t.Fatal("a header click changed file selection or cursor")
				}
			}
			if tc.column == "size" || tc.column == "type" || tc.column == "perms" {
				saveShot(t, tt, "files-sort-"+tc.column+"-"+language+".png")
			}
		}
		before := s.files.sort
		if err := tt.Click(locales.ColOwner()); err != nil {
			t.Fatal(err)
		}
		if s.files.sort != before {
			t.Fatal("the owner column unexpectedly sorts")
		}
		if err := tt.ClickWith(ui.Shift, "alpha.log"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(s.files.chosenNames(), []string{"middle.sh", "zeta.bin", "alpha.log"}) {
			t.Fatal("Shift selection did not follow the reordered file range")
		}
	}
}

func TestFileSortTypeLanguageChange(t *testing.T) {
	a, p := testApp(t)
	s := fileSortSession(a)
	s.files.entries = []remotefs.Entry{{Name: "archive.zip"}, {Name: "plain"}, {Name: "missing", Link: true, Broken: true}, {Name: "linked", Link: true}}
	s.files.sort.Column = "type"
	s.files.row = 1
	s.files.chosen.Add("plain")
	a.active = 0
	p.SetLocale("en")
	tt := ui.NewTester(a.View, 1280, 900)
	p.tt = tt
	tt.Frame()
	if got := fileSortNames(&s.files); !slices.Equal(got, []string{"missing", "plain", "linked", "archive.zip"}) {
		t.Fatalf("English type order %q", got)
	}
	p.SetLocale("zh-Hans")
	tt.Frame()
	if got := fileSortNames(&s.files); !slices.Equal(got, []string{"archive.zip", "missing", "plain", "linked"}) {
		t.Fatalf("Chinese type order did not follow the displayed labels: %q", got)
	}
	if s.files.cursorName() != "plain" || !s.files.chosen.Has("plain") || s.files.needsSort() {
		t.Fatal("language change lost the cursor or failed to update the sort cache")
	}
}

func TestFileSortRefreshRevealAndKeyboard(t *testing.T) {
	f := newEditorFixture(t)
	if err := f.tt.Click(locales.ColSize()); err != nil {
		t.Fatal(err)
	}
	f.tt.ClickAt(rowCenter(t, f.tt, "app.yaml"))
	row := f.s.files.row
	if err := os.WriteFile(filepath.Join(f.dir, "app.yaml"), bytes.Repeat([]byte("a"), 30_000), 0o640); err != nil {
		t.Fatal(err)
	}
	f.a.openDir(f.s, f.dir)
	f.settled(t)
	if f.s.files.cursorName() != "app.yaml" || f.s.files.row == row || !f.s.files.chosen.Has("app.yaml") || f.s.files.sort.Column != "size" {
		t.Fatal("refresh did not preserve the sorted file cursor")
	}
	f.tt.Key(0, ui.KeyF2)
	f.tt.Frame()
	if f.a.fileDlg == nil || f.a.fileDlg.old != "app.yaml" {
		t.Fatal("F2 renamed a different file after sorting")
	}
	f.tt.Key(0, ui.KeyEscape)
	f.tt.Frame()
	if err := os.WriteFile(filepath.Join(f.dir, "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.s.files.reveal = "notes.txt"
	f.a.openDir(f.s, f.dir)
	f.settled(t)
	if f.s.files.cursorName() != "notes.txt" || f.s.files.row != 0 || !slices.Equal(f.s.files.chosenNames(), []string{"notes.txt"}) {
		t.Fatal("reveal used the row from before sorting")
	}
	// Re-enter the table after closing the rename dialog's text input.
	f.tt.ClickAt(rowCenter(t, f.tt, "notes.txt"))
	f.tt.Key(0, ui.KeyEnter)
	f.tt.Frame()
	if !f.a.windowOpen(WindowFileEditor) {
		t.Fatal("Enter did not open the selected file")
	}
	tab := f.tab(t, "notes.txt")
	if tab.path != filepath.Join(f.dir, "notes.txt") {
		t.Fatal("Enter opened the wrong file")
	}
	nested := filepath.Join(f.dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"z.txt": "z", "a.txt": "aa"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.a.openDir(f.s, nested)
	f.settled(t)
	if !slices.Equal(fileSortNames(&f.s.files), []string{"z.txt", "a.txt"}) || f.s.files.row != -1 || f.s.files.chosen.Len() != 0 {
		t.Fatal("changing directory reset sorting or retained an old selection")
	}
}

func TestFileSortSessionIsolation(t *testing.T) {
	a, p := testApp(t)
	first, second := fileSortSession(a), fileSortSession(a)
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 900)
	p.tt = tt
	tt.Frame()
	if err := tt.Click(locales.ColName()); err != nil {
		t.Fatal(err)
	}
	a.active = 1
	tt.Frame()
	if err := tt.Click(locales.ColPermissions()); err != nil {
		t.Fatal(err)
	}
	a.active = 0
	tt.Frame()
	if first.files.sort.Column != "name" || !first.files.sort.Descending || second.files.sort.Column != "perms" || second.files.sort.Descending {
		t.Fatal("sessions shared their file sort")
	}
}
