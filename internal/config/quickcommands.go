package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Quick commands are the commands the user keeps to send with a click,
// FinalShell's quick command panel (internal/app/quickcmds.go): the same
// for every host, in quickcommands.json in Dir(), in groups, both in the
// order the user keeps them.

// QuickCommandsVersion is the version of quickcommands.json this build
// writes.
const QuickCommandsVersion = 1

// QuickCommands is the content of quickcommands.json.
type QuickCommands struct {
	// Version is the file's version: QuickCommandsVersion, or more for a
	// file a newer build wrote, which saving keeps.
	Version  int             `json:"version"`
	Groups   []*QuickGroup   `json:"groups"`
	Commands []*QuickCommand `json:"commands"`

	extra extras
}

// QuickGroup is a group of quick commands.
type QuickGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	extra extras
}

// QuickCommand is a saved command.
type QuickCommand struct {
	ID string `json:"id"`
	// Group is the ID of its group, "" for none: it shows under All only,
	// as does one whose group is gone.
	Group string `json:"group,omitempty"`
	// Name is what its button says; "" for the first line of Text.
	Name string `json:"name"`
	// Text is the command, one or more lines, sent line by line.
	Text string `json:"text"`
	// Enter sends a carriage return after each line, so that every line
	// runs; without it the last line is typed but not run. A file without
	// the key has it on.
	Enter bool `json:"enter"`

	extra extras
}

type (
	quickCommandsJSON QuickCommands
	quickGroupJSON    QuickGroup
	quickCommandJSON  QuickCommand
)

var (
	quickCommandsKeys = knownKeys(reflect.TypeFor[quickCommandsJSON]())
	quickGroupKeys    = knownKeys(reflect.TypeFor[quickGroupJSON]())
	quickCommandKeys  = knownKeys(reflect.TypeFor[quickCommandJSON]())
)

func (q QuickCommands) MarshalJSON() ([]byte, error) {
	v := quickCommandsJSON(q)
	v.Version = max(v.Version, QuickCommandsVersion)
	if v.Groups == nil {
		v.Groups = []*QuickGroup{}
	}
	if v.Commands == nil {
		v.Commands = []*QuickCommand{}
	}
	return encodeObject(v, q.extra)
}

func (q *QuickCommands) UnmarshalJSON(data []byte) error {
	var v quickCommandsJSON
	ex, err := decodeObject(data, &v, quickCommandsKeys)
	if err != nil {
		return err
	}
	*q = QuickCommands(v)
	q.extra = ex
	return nil
}

func (g QuickGroup) MarshalJSON() ([]byte, error) { return encodeObject(quickGroupJSON(g), g.extra) }

func (g *QuickGroup) UnmarshalJSON(data []byte) error {
	var v quickGroupJSON
	ex, err := decodeObject(data, &v, quickGroupKeys)
	if err != nil {
		return err
	}
	*g = QuickGroup(v)
	g.extra = ex
	return nil
}

func (c QuickCommand) MarshalJSON() ([]byte, error) {
	return encodeObject(quickCommandJSON(c), c.extra)
}

func (c *QuickCommand) UnmarshalJSON(data []byte) error {
	v := quickCommandJSON{Enter: true} // the default, when the key is missing
	ex, err := decodeObject(data, &v, quickCommandKeys)
	if err != nil {
		return err
	}
	*c = QuickCommand(v)
	c.extra = ex
	return nil
}

// NewQuickCommands is an empty file's content.
func NewQuickCommands() *QuickCommands { return &QuickCommands{Version: QuickCommandsVersion} }

// ParseQuickCommands reads the content of quickcommands.json. Nulls in
// the lists are dropped, and entries without an ID, or with one another
// entry has, get one of their own.
func ParseQuickCommands(data []byte) (*QuickCommands, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 || string(data) == "null" {
		return NewQuickCommands(), nil
	}
	q := &QuickCommands{}
	if err := json.Unmarshal(data, q); err != nil {
		return nil, err
	}
	if q.Version == 0 {
		q.Version = QuickCommandsVersion
	}
	q.Groups = dropNil(q.Groups)
	q.Commands = dropNil(q.Commands)
	seen := map[string]bool{}
	for _, g := range q.Groups {
		if g.ID == "" || seen[g.ID] {
			g.ID = q.newID("g")
		}
		seen[g.ID] = true
	}
	for _, c := range q.Commands {
		if c.ID == "" || seen[c.ID] {
			c.ID = q.newID("c")
		}
		seen[c.ID] = true
	}
	return q, nil
}

func quickCommandsPath() string { return filepath.Join(Dir(), "quickcommands.json") }

// QuickCommandsBrokenName is the copy LoadQuickCommands makes, next to
// quickcommands.json, of one it cannot parse, at t:
// quickcommands.json.broken-20061231-235959.
func QuickCommandsBrokenName(t time.Time) string {
	return "quickcommands.json.broken-" + t.Format("20060102-150405")
}

// LoadQuickCommands reads quickcommands.json: an empty list when there is
// no file yet, or with an error. A file that does not parse is copied to
// QuickCommandsBrokenName first, as hosts.json is, since the next save
// replaces it; the error then names the copy.
func LoadQuickCommands() (*QuickCommands, error) {
	data, err := os.ReadFile(quickCommandsPath())
	if os.IsNotExist(err) {
		return NewQuickCommands(), nil
	}
	if err != nil {
		return NewQuickCommands(), err
	}
	q, err := ParseQuickCommands(data)
	if err != nil {
		broken := filepath.Join(Dir(), QuickCommandsBrokenName(now()))
		if werr := writeFileAtomic(broken, data); werr != nil { // best effort
			return NewQuickCommands(), fmt.Errorf("%s: %w", quickCommandsPath(), err)
		}
		return NewQuickCommands(), &BrokenFileError{Path: quickCommandsPath(), Copy: broken, Err: err}
	}
	return q, nil
}

// BrokenFileError is a file that could not be read, copied aside to Copy
// before anything replaces it.
type BrokenFileError struct {
	Path, Copy string
	Err        error
}

func (e *BrokenFileError) Error() string {
	return fmt.Sprintf("%s: %v (copied to %s)", e.Path, e.Err, e.Copy)
}

func (e *BrokenFileError) Unwrap() error { return e.Err }

// SaveQuickCommands writes q as quickcommands.json, replacing the file at
// once.
func SaveQuickCommands(q *QuickCommands) error {
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(quickCommandsPath(), append(data, '\n'))
}

// newID returns an ID, starting with prefix, that no group or command of
// q has.
func (q *QuickCommands) newID(prefix string) string {
	n := time.Now().UnixNano()
	for {
		id := fmt.Sprintf("%s%d", prefix, n)
		if q.Group(id) == nil && q.Command(id) == nil {
			return id
		}
		n++
	}
}

// Group is the group with the ID; nil when there is none, as for "".
func (q *QuickCommands) Group(id string) *QuickGroup {
	if id == "" {
		return nil
	}
	for _, g := range q.Groups {
		if g.ID == id {
			return g
		}
	}
	return nil
}

// Command is the command with the ID; nil when there is none.
func (q *QuickCommands) Command(id string) *QuickCommand {
	for _, c := range q.Commands {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// AddGroup adds a group named name at the end, and returns it.
func (q *QuickCommands) AddGroup(name string) *QuickGroup {
	g := &QuickGroup{ID: q.newID("g"), Name: name}
	q.Groups = append(q.Groups, g)
	return g
}

// RemoveGroup removes the group with the ID; its commands stay, in no
// group. It reports whether there was one.
func (q *QuickCommands) RemoveGroup(id string) bool {
	i := slices.IndexFunc(q.Groups, func(g *QuickGroup) bool { return g.ID == id })
	if i < 0 {
		return false
	}
	q.Groups = slices.Delete(q.Groups, i, i+1)
	for _, c := range q.Commands {
		if c.Group == id {
			c.Group = ""
		}
	}
	return true
}

// InGroup are the commands of the group with the ID, in their order:
// every command for "".
func (q *QuickCommands) InGroup(id string) []*QuickCommand {
	if id == "" {
		return q.Commands
	}
	var out []*QuickCommand
	for _, c := range q.Commands {
		if c.Group == id {
			out = append(out, c)
		}
	}
	return out
}

// AddCommand adds c, giving it an ID, after the command with the ID
// after, or at the end when there is none.
func (q *QuickCommands) AddCommand(c *QuickCommand, after string) {
	c.ID = q.newID("c")
	i := slices.IndexFunc(q.Commands, func(x *QuickCommand) bool { return x.ID == after })
	if i < 0 {
		q.Commands = append(q.Commands, c)
		return
	}
	q.Commands = slices.Insert(q.Commands, i+1, c)
}

// RemoveCommand removes the command with the ID, and reports whether
// there was one.
func (q *QuickCommands) RemoveCommand(id string) bool {
	n := len(q.Commands)
	q.Commands = slices.DeleteFunc(q.Commands, func(c *QuickCommand) bool { return c.ID == id })
	return len(q.Commands) < n
}

// MoveCommand moves the command with the ID to where the command to is,
// as dragging it there does: after it when it moves down the list,
// before it when it moves up; to "" moves it to the end. It reports
// whether the order changed.
func (q *QuickCommands) MoveCommand(id, to string) bool {
	from := slices.IndexFunc(q.Commands, func(c *QuickCommand) bool { return c.ID == id })
	if from < 0 || id == to {
		return false
	}
	c := q.Commands[from]
	rest := slices.Delete(slices.Clone(q.Commands), from, from+1)
	at := len(rest)
	if to != "" {
		i := slices.IndexFunc(rest, func(x *QuickCommand) bool { return x.ID == to })
		if i < 0 {
			return false
		}
		at = i
		if i >= from { // it moves down: after the command dropped on
			at = i + 1
		}
	}
	if at == from {
		return false
	}
	q.Commands = slices.Insert(rest, at, c)
	return true
}

// QuickCommandLines are the lines of a command's text, as they are sent:
// with Windows line ends as Unix ones, and without the empty lines at its
// end, which would only send a carriage return of their own. Blanks are
// kept, as a line may want them; a text of blanks has no lines.
func QuickCommandLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// Title is what the command's button says: its name, else its first line
// that is not blank.
func (c *QuickCommand) Title() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	for _, l := range QuickCommandLines(c.Text) {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
