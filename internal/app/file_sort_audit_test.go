package app

import (
	"bytes"
	"fmt"
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

// Ties use complete names, and times use their actual precision rather
// than the rounded text shown in the cells. Compare extreme byte counts
// without overflow in either direction.
func TestFileSortAuditValueBoundaries(t *testing.T) {
	for _, column := range []string{"size", "modified", "perms"} {
		t.Run(column, func(t *testing.T) {
			f := fileState{row: -1, entries: []remotefs.Entry{
				{Name: "z", Size: math.MaxInt64, ModTime: time.Unix(60, 1), Mode: 0o777},
				{Name: "a", Size: 0, ModTime: time.Unix(60, 0), Mode: 0},
				{Name: "A", Size: 0, ModTime: time.Unix(60, 0), Mode: 0},
				{Name: "early", Size: math.MinInt64, Mode: 0},
			}}
			f.sort.Column = column
			f.sortEntries()
			want := []string{"early", "A", "a", "z"}
			if column == "perms" {
				want = []string{"A", "a", "early", "z"}
			}
			if got := fileSortNames(&f); !slices.Equal(got, want) {
				t.Fatalf("ascending %q, want %q", got, want)
			}
			f.sort.Descending = true
			f.sortEntries()
			want = []string{"z", "A", "a", "early"}
			if got := fileSortNames(&f); !slices.Equal(got, want) {
				t.Fatalf("descending %q, want %q", got, want)
			}
		})
	}
}

// Lowercasing at every comparison creates allocations proportional to
// comparisons instead of directory entries. Allow ample room for per-row
// keys and scratch storage without relying on wall-clock timing.
func TestFileSortAuditAllocationGrowth(t *testing.T) {
	entries := make([]remotefs.Entry, 1_000)
	for i := range entries {
		entries[i].Name = fmt.Sprintf("BACKUP-%08d-WEB-APPLICATION-LOG.txt", i*7919%len(entries))
	}
	allocs := testing.AllocsPerRun(1, func() {
		f := fileState{row: -1, entries: slices.Clone(entries)}
		f.sort.Column = "name"
		f.sortEntries()
	})
	if limit := float64(4*len(entries) + 100); allocs > limit {
		t.Fatalf("sorting %d rows allocated %.0f objects, budget %.0f; allocations should scale with rows", len(entries), allocs, limit)
	}
}

// A deterministic complete name tie-breaker must give the same visible
// order regardless of the previous column's order or server listing.
func TestFileSortAuditPermutations(t *testing.T) {
	_, p := testApp(t)
	p.SetLocale("en")
	var visit func([]remotefs.Entry, int)
	visit = func(entries []remotefs.Entry, at int) {
		if at != len(entries) {
			for i := at; i < len(entries); i++ {
				entries[at], entries[i] = entries[i], entries[at]
				visit(entries, at+1)
				entries[at], entries[i] = entries[i], entries[at]
			}
			return
		}
		for _, tc := range fileSortCases {
			for _, descending := range []bool{false, true} {
				f := fileState{row: -1, entries: slices.Clone(entries)}
				f.sort.Column, f.sort.Descending = tc.column, descending
				f.sortEntries()
				want := tc.asc
				if descending {
					want = tc.desc
				}
				if got := fileSortNames(&f); !slices.Equal(got, want) {
					t.Fatalf("%s descending=%v input=%v result=%q want=%q", tc.column, descending, entries, got, want)
				}
				if f.needsSort() {
					t.Fatal("unchanged sorted entries invalidated the cache")
				}
			}
		}
	}
	visit(fileSortSample(), 0)
}

// A refresh can move the item that anchors a Shift range. The next range
// must begin at that item, independently of its previous row number.
func TestFileSortAuditRefreshShiftAnchor(t *testing.T) {
	f := newEditorFixture(t)
	if err := f.tt.Click(locales.ColSize()); err != nil {
		t.Fatal(err)
	}
	f.tt.ClickAt(rowCenter(t, f.tt, "app.yaml"))
	before := f.s.files.row
	if err := os.WriteFile(filepath.Join(f.dir, "app.yaml"), bytes.Repeat([]byte("x"), 200_000), 0o640); err != nil {
		t.Fatal(err)
	}
	f.a.openDir(f.s, f.dir)
	f.settled(t)
	if f.s.files.cursorName() != "app.yaml" || f.s.files.row == before {
		t.Fatal("refresh lost the anchor's file or did not move its row")
	}
	if err := f.tt.ClickWith(ui.Shift, "a.out"); err != nil {
		t.Fatal(err)
	}
	if got := f.s.files.chosenNames(); !slices.Equal(got, []string{"a.out", "big.log", "app.yaml"}) {
		t.Fatalf("Shift range after refresh: %q", got)
	}
}

// Removing the cursor's file must clear it. Enter must not open a file
// that happened to move to its old row; Down chooses the first row.
func TestFileSortAuditRemovedCursor(t *testing.T) {
	f := newEditorFixture(t)
	if err := f.tt.Click(locales.ColSize()); err != nil {
		t.Fatal(err)
	}
	f.tt.ClickAt(rowCenter(t, f.tt, "app.yaml"))
	if err := os.Remove(filepath.Join(f.dir, "app.yaml")); err != nil {
		t.Fatal(err)
	}
	f.a.openDir(f.s, f.dir)
	f.settled(t)
	if f.s.files.row != -1 || len(f.s.files.chosenNames()) != 0 {
		t.Fatalf("removed file kept a visible cursor or choice: row %d, chosen %q", f.s.files.row, f.s.files.chosenNames())
	}
	f.tt.Key(0, ui.KeyEnter)
	f.tt.Frame()
	if len(f.a.fe.tabs) != 0 {
		t.Fatal("Enter opened a replacement at the removed file's row")
	}
	f.tt.Key(0, ui.KeyDown)
	f.tt.Frame()
	if f.s.files.row != 0 || !slices.Equal(f.s.files.chosenNames(), []string{f.s.files.entries[0].Name}) {
		t.Fatal("Down did not choose the first row after cursor removal")
	}
}

// A renamed item can move beyond the virtualized viewport. Actual SFTP
// rename, reveal scrolling and the subsequent keyboard action must all
// refer to the new name, rather than the old numeric row.
func TestFileSortAuditRenameBeyondViewport(t *testing.T) {
	f := newEditorFixture(t)
	for i := range 160 {
		if err := os.WriteFile(filepath.Join(f.dir, fmt.Sprintf("file-%03d.txt", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.a.openDir(f.s, f.dir)
	f.settled(t)
	f.menu(t, "app.yaml", locales.Rename())
	if f.a.fileDlg == nil {
		t.Fatal("rename dialog did not open")
	}
	f.a.fileDlg.name = "zz-renamed.yaml"
	if err := f.tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	f.settled(t)
	f.tt.Frame()
	if f.s.files.cursorName() != "zz-renamed.yaml" || f.s.files.row != len(f.s.files.entries)-1 || !slices.Equal(f.s.files.chosenNames(), []string{"zz-renamed.yaml"}) {
		t.Fatal("rename did not reveal the new sorted row")
	}
	if _, ok := f.tt.Find("zz-renamed.yaml"); !ok {
		t.Fatal("renamed row was not scrolled into the visible viewport")
	}
	f.tt.ClickAt(rowCenter(t, f.tt, "zz-renamed.yaml"))
	f.tt.Key(0, ui.KeyF2)
	f.tt.Frame()
	if f.a.fileDlg == nil || f.a.fileDlg.old != "zz-renamed.yaml" {
		t.Fatal("F2 targeted another row after rename and reveal")
	}
}

func TestFileSortAuditReorderedHeader(t *testing.T) {
	a, p := testApp(t)
	s := fileSortSession(a)
	a.active = 0
	p.SetLocale("en")
	tt := ui.NewTester(a.View, 1280, 900)
	p.tt = tt
	tt.Frame()
	tt.ClickAt(rowCenter(t, tt, "alpha.log"))
	box, ok := tt.Find(locales.ColSize())
	if !ok {
		t.Fatal("size header not found")
	}
	x, y := box.X+box.W/2, box.Y+box.H/2
	tt.Press(x, y)
	for _, dx := range []float32{5, 30, 70, 100} {
		tt.Move(x+dx, y)
	}
	tt.Release(x+100, y)
	tt.Frame()
	if !slices.Equal(s.files.table.Columns.Order, []string{"name", "type", "size", "modified", "perms", "owner"}) || s.files.sort.Column != "name" || s.files.sort.Descending {
		t.Fatalf("drag changed sort or did not move column: %+v, %q", s.files.sort, s.files.table.Columns.Order)
	}
	if err := tt.Click(locales.ColSize()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !slices.Equal(fileSortNames(&s.files), fileSortCases[1].asc) || s.files.cursorName() != "alpha.log" {
		t.Fatal("reordered header sorted the wrong column or moved the cursor")
	}
}

// Header sorts run on the UI thread. Record their cost separately from
// generating rows, including a name sort after a different column order.
func BenchmarkFileSortAuditLargeDirectory(b *testing.B) {
	for _, count := range []int{10_000, 100_000} {
		for _, column := range []string{"name", "size"} {
			b.Run(fmt.Sprintf("%s/%d", column, count), func(b *testing.B) {
				entries := make([]remotefs.Entry, count)
				for i := range entries {
					key := i * 7919 % count
					entries[i] = remotefs.Entry{Name: fmt.Sprintf("BACKUP-%08d-WEB-APPLICATION-LOG.txt", key), Size: int64(key)}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					b.StopTimer()
					f := fileState{row: -1, entries: slices.Clone(entries)}
					f.sort.Column = column
					b.StartTimer()
					f.sortEntries()
				}
			})
		}
	}
}
