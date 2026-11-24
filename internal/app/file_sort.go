package app

import (
	"cmp"
	"slices"
	"strings"

	"finallyshell/internal/remotefs"

	"github.com/egoist/mygo/i18n"
)

func (f *fileState) needsSort() bool {
	return f.sort != f.sortedBy || f.sort.Column == "" ||
		f.sort.Column == "type" && f.sortLocale != i18n.Locale()
}

// cursorName follows the keyboard cursor independently of selection.
func (f *fileState) cursorName() string {
	if f.row >= 0 && f.row < len(f.entries) {
		return f.entries[f.row].Name
	}
	return ""
}

// setEntries keeps the cursor on the same name when refreshing. The tree
// gets ReadDir's name order before the table applies its own sort.
func (f *fileState) setEntries(dir string, entries []remotefs.Entry) {
	cursor := f.cursorName()
	if dir != f.cwd {
		f.chosen.Clear()
		cursor = ""
	}
	f.cwd, f.pathEdit, f.entries = dir, dir, entries
	f.subdirs[dir] = dirNames(dir, entries)
	f.row = slices.IndexFunc(entries, func(e remotefs.Entry) bool { return cursor != "" && e.Name == cursor })
	f.sortEntries()
}

// sortEntries runs only after a header changes or a directory is read,
// keeping selection and the keyboard cursor attached to file names.
func (f *fileState) sortEntries() {
	if f.sort.Column == "" {
		f.sort.Column = "name"
	}
	cursor := f.cursorName()
	// Comparisons reuse folded keys instead of allocating lowercase text
	// repeatedly on the UI thread. Sort lightweight indices to avoid
	// moving full entries or hashing names at every comparison.
	order := make([]int, len(f.entries))
	names := make([]string, len(f.entries))
	for i, e := range f.entries {
		order[i], names[i] = i, strings.ToLower(e.Name)
	}
	type kindKey struct{ label, folded string }
	var kinds []kindKey
	if f.sort.Column == "type" {
		kinds = make([]kindKey, len(f.entries))
		for i, e := range f.entries {
			label := entryKind(e)
			kinds[i] = kindKey{label, strings.ToLower(label)}
		}
	}
	nameOrder := func(x, y int) int {
		return fileTextOrder(f.entries[x].Name, f.entries[y].Name, names[x], names[y])
	}
	// Complete file-name ties make the order deterministic for distinct
	// directory entries, so an unstable sorter need not shuffle ties.
	slices.SortFunc(order, func(xi, yi int) int {
		x, y := &f.entries[xi], &f.entries[yi]
		if x.IsDir() != y.IsDir() {
			if x.IsDir() {
				return -1
			}
			return 1
		}
		var n int
		switch f.sort.Column {
		case "size":
			// Folders and broken links have no displayed file size.
			if x.IsDir() {
				return nameOrder(xi, yi)
			}
			if x.Broken != y.Broken {
				if x.Broken {
					return 1
				}
				return -1
			}
			if !x.Broken {
				n = cmp.Compare(x.Size, y.Size)
			}
		case "modified":
			n = x.ModTime.Compare(y.ModTime)
		case "type":
			xk, yk := kinds[xi], kinds[yi]
			n = fileTextOrder(xk.label, yk.label, xk.folded, yk.folded)
		case "perms":
			n = cmp.Compare(x.Mode.Perm(), y.Mode.Perm())
		default:
			return sortDir(nameOrder(xi, yi), f.sort)
		}
		if n != 0 {
			return sortDir(n, f.sort)
		}
		return nameOrder(xi, yi)
	})
	sorted := make([]remotefs.Entry, len(f.entries))
	for i, row := range order {
		sorted[i] = f.entries[row]
	}
	// listDir's reveal lookup shares this slice; retain its backing array.
	copy(f.entries, sorted)
	f.row = slices.IndexFunc(f.entries, func(e remotefs.Entry) bool { return cursor != "" && e.Name == cursor })
	f.sortedBy = f.sort
	f.sortLocale = i18n.Locale()
}

func fileTextOrder(x, y, foldedX, foldedY string) int {
	if n := strings.Compare(foldedX, foldedY); n != 0 {
		return n
	}
	return strings.Compare(x, y)
}
