package config

import (
	"encoding/json"
	"slices"
	"strings"
)

// Names is a list of names, kept in a string so that, unlike a slice, it
// can be compared with ==, and Settings with it. In settings.json it is
// an array of strings; the zero Names is the empty list, which omitempty
// leaves out.
type Names string

// sep joins the names: no name has it.
const sep = "\x00"

// NewNames is the list of names, or the zero Names for none.
func NewNames(names ...string) Names { return Names(strings.Join(names, sep)) }

// List is the names, nil for none.
func (n Names) List() []string {
	if n == "" {
		return nil
	}
	return strings.Split(string(n), sep)
}

// Has says whether name is in the list.
func (n Names) Has(name string) bool { return slices.Contains(n.List(), name) }

// Toggle is n with name added at the end, or taken out when it is there.
func (n Names) Toggle(name string) Names {
	l := n.List()
	if slices.Contains(l, name) {
		return NewNames(slices.DeleteFunc(l, func(s string) bool { return s == name })...)
	}
	return NewNames(append(l, name)...)
}

func (n Names) MarshalJSON() ([]byte, error) {
	l := n.List()
	if l == nil {
		l = []string{}
	}
	return json.Marshal(l)
}

func (n *Names) UnmarshalJSON(data []byte) error {
	var l []string
	if err := json.Unmarshal(data, &l); err != nil {
		return err
	}
	l = slices.DeleteFunc(l, func(s string) bool { return s == "" || strings.Contains(s, sep) })
	*n = NewNames(l...)
	return nil
}
