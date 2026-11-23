package cloudsync

import (
	"encoding/json"
	"slices"
	"testing"
)

func quickPayload(items ...json.RawMessage) *Payload {
	raw, _ := json.Marshal(map[string]any{
		"version": 1, "groups": []json.RawMessage{host("g", "Group")},
		"commands": items, "future": map[string]int{"kept": 1},
	})
	return &Payload{QuickCommands: raw}
}

func TestMergeQuickCommands(t *testing.T) {
	a, b, c := host("a", "A"), host("b", "B"), host("c", "C")
	for _, test := range []struct {
		name                string
		base, local, remote *Payload
		want                []string
		conflicts           int
	}{
		{"first sync", nil, quickPayload(a), quickPayload(b), []string{"a=A", "b=B"}, 0},
		{"independent edits", quickPayload(a, b), quickPayload(host("a", "local"), b), quickPayload(a, host("b", "remote")), []string{"a=local", "b=remote"}, 0},
		{"delete and add", quickPayload(a, b), quickPayload(a), quickPayload(a, b, c), []string{"a=A", "c=C"}, 0},
		{"remote delete", quickPayload(a, b), quickPayload(a, b), quickPayload(b), []string{"b=B"}, 0},
		{"conflict", quickPayload(a), quickPayload(host("a", "local")), quickPayload(host("a", "remote")), []string{"a=local"}, 1},
		{"remote reorder and local edit", quickPayload(a, b, c), quickPayload(host("a", "local"), b, c), quickPayload(c, b, a), []string{"c=C", "b=B", "a=local"}, 0},
		{"local reorder", quickPayload(a, b, c), quickPayload(c, b, a), quickPayload(host("a", "remote"), b, c), []string{"c=C", "b=B", "a=remote"}, 0},
		{"both reorder", quickPayload(a, b, c), quickPayload(b, a, c), quickPayload(a, c, b), []string{"b=B", "a=A", "c=C"}, 1},
		{"old remote", quickPayload(a), quickPayload(a), &Payload{}, []string{"a=A"}, 0},
		{"remote insertion and local deletion", quickPayload(a, b, c), quickPayload(a, b), quickPayload(a, host("x", "X"), b, c), []string{"a=A", "x=X", "b=B"}, 0},
		{"local insertion and remote reorder", quickPayload(a, b, c), quickPayload(a, host("x", "X"), b, c), quickPayload(c, b, a), []string{"c=C", "b=B", "a=A", "x=X"}, 0},
		{"concurrent insertions", quickPayload(a, b), quickPayload(a, host("x", "X"), b), quickPayload(a, host("y", "Y"), b), []string{"a=A", "y=Y", "x=X", "b=B"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged, conflicts := Merge(test.base, test.local, test.remote)
			var doc struct {
				Commands []json.RawMessage
				Future   map[string]int
			}
			if err := json.Unmarshal(merged.QuickCommands, &doc); err != nil {
				t.Fatal(err)
			}
			if got := names(doc.Commands); !slices.Equal(got, test.want) || conflicts != test.conflicts || doc.Future["kept"] != 1 {
				t.Fatalf("commands %v, conflicts %d, future %v; want %v, %d", got, conflicts, doc.Future, test.want, test.conflicts)
			}
		})
	}
}

func TestQuickCommandOrderAffectsEquality(t *testing.T) {
	p := quickPayload(host("a", "A"), host("b", "B"))
	if Equal(p, quickPayload(host("b", "B"), host("a", "A"))) {
		t.Fatal("reordering would not trigger automatic sync")
	}
	copy := p.Clone()
	copy.QuickCommands[0] = ' '
	if p.QuickCommands[0] != '{' {
		t.Fatal("clone shares its command document")
	}
}
