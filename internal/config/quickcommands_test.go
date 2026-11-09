package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cmdIDs are the IDs of cmds, in order.
func cmdIDs(cmds []*QuickCommand) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, c.ID)
	}
	return out
}

// No file is an empty list, and no error.
func TestQuickCommandsNoFile(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	q, err := LoadQuickCommands()
	if err != nil || len(q.Groups) != 0 || len(q.Commands) != 0 || q.Version != QuickCommandsVersion {
		t.Fatalf("%+v, %v", q, err)
	}
}

// The file of the issue reads as written; saving writes it back, through
// a temporary file, and reads the same.
func TestQuickCommandsRoundTrip(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	file := `{"version": 1,
 "groups":   [{"id": "g1", "name": "常用"}],
 "commands": [{"id": "c1", "group": "g1", "name": "更新系统",
               "text": "apt update\napt -y upgrade", "enter": true},
              {"id": "c2", "text": "tail -f /var/log/syslog", "enter": false},
              {"id": "c3", "name": "df", "text": "df -h"}]}`
	if err := os.WriteFile(filepath.Join(Dir(), "quickcommands.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	q, err := LoadQuickCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Groups) != 1 || q.Groups[0].Name != "常用" || !slices.Equal(cmdIDs(q.Commands), []string{"c1", "c2", "c3"}) {
		t.Fatalf("read %+v", q)
	}
	c1, c2, c3 := q.Command("c1"), q.Command("c2"), q.Command("c3")
	if c1.Group != "g1" || c1.Text != "apt update\napt -y upgrade" || !c1.Enter {
		t.Errorf("c1 %+v", c1)
	}
	if c2.Enter || c2.Title() != "tail -f /var/log/syslog" {
		t.Errorf("c2 %+v, title %q", c2, c2.Title())
	}
	if !c3.Enter {
		t.Error("a command without enter reads as false; the default is true")
	}
	if err := SaveQuickCommands(q); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(Dir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
	again, err := LoadQuickCommands()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cmdIDs(again.Commands), cmdIDs(q.Commands)) || !again.Command("c3").Enter || again.Command("c2").Enter {
		t.Errorf("read back %+v", again)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "quickcommands.json"))
	if !strings.Contains(string(data), `"enter": true`) || !strings.Contains(string(data), `"enter": false`) {
		t.Errorf("enter not written out:\n%s", data)
	}
}

// A file that does not parse reads as an empty list, with an error
// naming the copy kept of it, which the next save leaves alone.
func TestQuickCommandsBrokenFile(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	at := time.Date(2026, 11, 9, 10, 30, 0, 0, time.Local)
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
	bad := []byte(`{"version": 1, "commands": [{"id": "c1", "text": "uptime"`)
	if err := os.WriteFile(filepath.Join(Dir(), "quickcommands.json"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	q, err := LoadQuickCommands()
	var broken *BrokenFileError
	if !errors.As(err, &broken) || q == nil || len(q.Commands) != 0 {
		t.Fatalf("%+v, %v", q, err)
	}
	want := filepath.Join(Dir(), "quickcommands.json.broken-20261109-103000")
	if broken.Copy != want || !strings.Contains(err.Error(), want) {
		t.Errorf("copy %q, error %v", broken.Copy, err)
	}
	q.AddCommand(&QuickCommand{Text: "ls", Enter: true}, "")
	if err := SaveQuickCommands(q); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(want); string(data) != string(bad) {
		t.Errorf("the copy holds %q", data)
	}

	// An empty file, or null, is no list at all; nulls in the lists go,
	// and entries without IDs, or with one taken, get their own.
	for _, s := range []string{"", "  \n", "null"} {
		if q, err := ParseQuickCommands([]byte(s)); err != nil || len(q.Commands) != 0 {
			t.Errorf("%q: %+v, %v", s, q, err)
		}
	}
	q, err = ParseQuickCommands([]byte(`{"groups": [null, {"name": "a"}, {"id": "x", "name": "b"}],
		"commands": [null, {"text": "ls"}, {"id": "x", "text": "pwd"}, {"id": "y", "text": "id"}, {"id": "y", "text": "w"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range append(cmdIDs(q.Commands), q.Groups[0].ID, q.Groups[1].ID) {
		if id == "" || seen[id] {
			t.Errorf("ID %q empty or twice", id)
		}
		seen[id] = true
	}
	if len(q.Groups) != 2 || len(q.Commands) != 4 || q.Version != QuickCommandsVersion {
		t.Errorf("parsed %+v", q)
	}
}

// Keys a newer build wrote stay, and so does its version.
func TestQuickCommandsUnknownKeys(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	file := `{"version": 3, "sync": {"at": 1}, "groups": [{"id": "g1", "name": "a", "color": "red"}],
		"commands": [{"id": "c1", "text": "ls", "enter": true, "shortcut": "F5"}]}`
	q, err := ParseQuickCommands([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveQuickCommands(q); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "quickcommands.json"))
	for _, want := range []string{`"version": 3`, `"sync"`, `"color": "red"`, `"shortcut": "F5"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %s:\n%s", want, data)
		}
	}
}

func TestQuickCommandsEdits(t *testing.T) {
	q := NewQuickCommands()
	g := q.AddGroup("Docker")
	h := q.AddGroup("Logs")
	if g.ID == h.ID || q.Group(g.ID) != g || q.Group("") != nil {
		t.Fatalf("groups %+v", q.Groups)
	}
	add := func(name, group, after string) *QuickCommand {
		c := &QuickCommand{Name: name, Group: group, Text: name, Enter: true}
		q.AddCommand(c, after)
		return c
	}
	a := add("a", g.ID, "")
	b := add("b", "", "")
	c := add("c", h.ID, "")
	d := add("d", g.ID, a.ID) // right after a
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{a.ID, d.ID, b.ID, c.ID}) {
		t.Fatalf("order %v", got)
	}
	if got := cmdIDs(q.InGroup(g.ID)); !slices.Equal(got, []string{a.ID, d.ID}) {
		t.Errorf("group %v", got)
	}
	if len(q.InGroup("")) != 4 {
		t.Error("All is not every command")
	}

	// Dragging down puts it after the one dropped on, up before it, and
	// on nothing at the end.
	q.MoveCommand(a.ID, b.ID)
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{d.ID, b.ID, a.ID, c.ID}) {
		t.Errorf("down: %v", got)
	}
	q.MoveCommand(c.ID, d.ID)
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{c.ID, d.ID, b.ID, a.ID}) {
		t.Errorf("up: %v", got)
	}
	q.MoveCommand(c.ID, "")
	if got := cmdIDs(q.Commands); !slices.Equal(got, []string{d.ID, b.ID, a.ID, c.ID}) {
		t.Errorf("to the end: %v", got)
	}
	if q.MoveCommand(c.ID, c.ID) || q.MoveCommand(c.ID, "") || q.MoveCommand("nope", a.ID) || q.MoveCommand(a.ID, "nope") {
		t.Error("a move that changes nothing reported a change")
	}

	// A group removed leaves its commands in none.
	if !q.RemoveGroup(g.ID) || q.RemoveGroup(g.ID) || a.Group != "" || d.Group != "" || c.Group != h.ID {
		t.Errorf("after removing the group: %+v %+v %+v", a, d, c)
	}
	if !q.RemoveCommand(b.ID) || q.RemoveCommand(b.ID) || q.Command(b.ID) != nil || len(q.Commands) != 3 {
		t.Errorf("after removing b: %v", cmdIDs(q.Commands))
	}
}

func TestQuickCommandLines(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"", nil},
		{"  \n\t\n", nil},
		{"df -h", []string{"df -h"}},
		{"df -h\n", []string{"df -h"}},
		{"apt update\r\napt -y upgrade\r\n\r\n", []string{"apt update", "apt -y upgrade"}},
		{"cat <<EOF\n\nhi\nEOF", []string{"cat <<EOF", "", "hi", "EOF"}},
		{"  indented  ", []string{"  indented  "}},
	} {
		if got := QuickCommandLines(tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.text, got, tc.want)
		}
	}
	for _, tc := range []struct{ name, text, want string }{
		{"Disk", "df -h", "Disk"},
		{"  ", "\n  apt update\napt upgrade", "apt update"},
		{"", "", ""},
	} {
		c := &QuickCommand{Name: tc.name, Text: tc.text}
		if got := c.Title(); got != tc.want {
			t.Errorf("%q %q: title %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
}
