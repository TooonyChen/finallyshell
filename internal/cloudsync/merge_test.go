package cloudsync

import (
	"encoding/json"
	"slices"
	"testing"
)

func host(id, name string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"id": id, "name": name})
	return b
}

func names(items []json.RawMessage) []string {
	var out []string
	for _, it := range items {
		var v struct{ ID, Name string }
		json.Unmarshal(it, &v)
		out = append(out, v.ID+"="+v.Name)
	}
	return out
}

func hosts(hs ...json.RawMessage) *Payload { return &Payload{Hosts: hs} }

func TestMergeHosts(t *testing.T) {
	base := hosts(host("a", "A"), host("b", "B"), host("c", "C"), host("d", "D"), host("e", "E"))
	local := hosts(
		host("a", "A"),       // unchanged
		host("b", "B local"), // changed here
		// c deleted here, unchanged there: deleted
		host("d", "D local"), // changed on both sides: local wins
		host("e", "E"),       // deleted there, unchanged here: deleted
		host("n", "new here"),
	)
	remote := hosts(
		host("a", "A remote"), // changed there
		host("b", "B"),
		host("c", "C"),
		host("d", "D remote"),
		host("m", "new there"),
	)
	m, conflicts := Merge(base, local, remote)
	want := []string{"a=A remote", "b=B local", "d=D local", "n=new here", "m=new there"}
	if got := names(m.Hosts); !slices.Equal(got, want) {
		t.Errorf("hosts = %v, want %v", got, want)
	}
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", conflicts)
	}
}

func TestMergeDeletedButChanged(t *testing.T) {
	base := hosts(host("a", "A"), host("b", "B"))
	local := hosts(host("a", "A edited"))  // b deleted here
	remote := hosts(host("b", "B edited")) // a deleted there
	m, _ := Merge(base, local, remote)
	// An edit wins over a delete on the other side.
	if got, want := names(m.Hosts), []string{"a=A edited", "b=B edited"}; !slices.Equal(got, want) {
		t.Errorf("hosts = %v, want %v", got, want)
	}
}

func TestMergeNoBaseKeepsAll(t *testing.T) {
	local := hosts(host("a", "A local"), host("b", "B"))
	remote := hosts(host("a", "A remote"), host("c", "C"))
	m, conflicts := Merge(nil, local, remote)
	if got, want := names(m.Hosts), []string{"a=A local", "b=B", "c=C"}; !slices.Equal(got, want) {
		t.Errorf("hosts = %v, want %v", got, want)
	}
	if conflicts != 0 {
		t.Errorf("a first sync counted %d conflicts", conflicts)
	}
}

func TestMergeSettingsAndSecrets(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	base := &Payload{
		Settings: map[string]json.RawMessage{"termFontSize": raw("14"), "termTheme": raw(`"Dracula"`), "language": raw(`"en"`)},
		Secrets:  map[string]string{"host/a/password": "old", "host/b/password": "b"},
	}
	local := &Payload{
		// font size reset to the default here (its key gone); theme
		// unchanged
		Settings: map[string]json.RawMessage{"termTheme": raw(`"Dracula"`), "language": raw(`"en"`)},
		Secrets:  map[string]string{"host/a/password": "old", "host/b/password": "b"},
	}
	remote := &Payload{
		Settings: map[string]json.RawMessage{"termFontSize": raw("14"), "termTheme": raw(` "Nord" `), "language": raw(`"en"`), "wheelLines": raw("5")},
		Secrets:  map[string]string{"host/a/password": "new"}, // b's forgotten there
	}
	m, _ := Merge(base, local, remote)
	want := map[string]string{"termTheme": `"Nord"`, "language": `"en"`, "wheelLines": "5"}
	if len(m.Settings) != len(want) {
		t.Errorf("settings = %s", m.Settings)
	}
	for k, v := range want {
		if !sameJSON(m.Settings[k], raw(v)) {
			t.Errorf("settings[%s] = %s, want %s", k, m.Settings[k], v)
		}
	}
	if len(m.Secrets) != 1 || m.Secrets["host/a/password"] != "new" {
		t.Errorf("secrets = %v", m.Secrets)
	}
}

func TestEqualIgnoresOrder(t *testing.T) {
	a := &Payload{Hosts: []json.RawMessage{host("a", "A"), host("b", "B")}, Device: "one"}
	b := &Payload{Hosts: []json.RawMessage{json.RawMessage(`{"name":"B", "id":"b"}`), host("a", "A")}, Device: "two"}
	if !Equal(a, b) {
		t.Error("the same hosts in another order differ")
	}
	b.Hosts[0] = host("b", "B2")
	if Equal(a, b) {
		t.Error("a changed host is equal")
	}
}
