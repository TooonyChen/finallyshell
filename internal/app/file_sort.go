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
	var kinds map[string]string
	if f.sort.Column == "type" {
		kinds = make(map[string]string, len(f.entries))
		for _, e := range f.entries {
			kinds[e.Name] = entryKind(e)
		}
	}
	slices.SortStableFunc(f.entries, func(x, y remotefs.Entry) int {
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
				return fileNameOrder(x.Name, y.Name)
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
			n = fileNameOrder(kinds[x.Name], kinds[y.Name])
		case "perms":
			n = cmp.Compare(x.Mode.Perm(), y.Mode.Perm())
		default:
			return sortDir(fileNameOrder(x.Name, y.Name), f.sort)
		}
		if n != 0 {
			return sortDir(n, f.sort)
		}
		return fileNameOrder(x.Name, y.Name)
	})
	f.row = slices.IndexFunc(f.entries, func(e remotefs.Entry) bool { return cursor != "" && e.Name == cursor })
	f.sortedBy = f.sort
	f.sortLocale = i18n.Locale()
}

func fileNameOrder(x, y string) int {
	if n := strings.Compare(strings.ToLower(x), strings.ToLower(y)); n != 0 {
		return n
	}
	return strings.Compare(x, y)
}
