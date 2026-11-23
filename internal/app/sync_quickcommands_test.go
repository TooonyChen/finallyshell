package app

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/cloudsync"
	"finallyshell/internal/config"
)

func TestSyncQuickCommandsTwoDevices(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	q := one.a.quick()
	g := q.AddGroup("Maintenance")
	c := &config.QuickCommand{Group: g.ID, Name: "Update", Text: "apt update\napt upgrade", Enter: false}
	q.AddCommand(c, "")
	d := &config.QuickCommand{Name: "Status", Text: "uptime", Enter: true}
	q.AddCommand(d, "")
	if err := one.a.saveQuick(); err != nil {
		t.Fatal(err)
	}
	// Reading is lazy; syncing must include commands even before the tab opens.
	one.a.quickUI.store = nil
	h := one.addHost("key-host", "key-host")
	h.Auth, h.KeyPath = config.AuthKey, `%SSH_KEYS%\XXX.pem`
	one.sync(t)
	two := newDevice(t, r)
	two.sync(t)
	received := two.a.quick()
	if got := received.Command(c.ID); got == nil || got.Group != g.ID || got.Text != c.Text || got.Enter {
		t.Fatalf("received command %+v", got)
	}
	if received.Group(g.ID) == nil || received.Groups[0].Name != "Maintenance" || two.a.hosts.Host(h.ID).KeyPath != h.KeyPath {
		t.Fatal("group or portable path was lost")
	}
	loaded, err := config.LoadQuickCommands()
	if err != nil || len(loaded.Commands) != 2 {
		t.Fatalf("quickcommands.json: %+v, %v", loaded, err)
	}
	if r.writes() != 1 {
		t.Fatal("receiving commands rewrote the remote")
	}
	// Reorder and rename on one device, edit another command on the other.
	received.MoveCommand(d.ID, c.ID)
	received.Group(g.ID).Name = "Common"
	two.sync(t)
	one.a.quick().Command(c.ID).Text = "apt update"
	one.sync(t)
	if got := one.a.quick(); got.Commands[0].ID != d.ID || got.Group(g.ID).Name != "Common" || got.Command(c.ID).Text != "apt update" {
		t.Fatalf("merged commands %+v", got)
	}
	two.sync(t)
	if two.a.quick().Command(c.ID).Text != "apt update" {
		t.Fatal("independent edit was lost")
	}
	// Deletion of every command and group must also reach the other device.
	one.a.quick().RemoveCommand(c.ID)
	one.a.quick().RemoveCommand(d.ID)
	one.a.quick().RemoveGroup(g.ID)
	one.sync(t)
	two.a.quickUI.group = g.ID
	two.sync(t)
	if got := two.a.quick(); len(got.Commands) != 0 || len(got.Groups) != 0 || two.a.quickUI.group != "" {
		t.Fatal("deletions did not sync")
	}
}

func TestSyncQuickCommandAddedDuringSync(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.a.quick().Commands = []*config.QuickCommand{{ID: "remote", Text: "uptime", Enter: true}}
	one.sync(t)
	two := newDevice(t, r)
	two.a.quick().Commands = []*config.QuickCommand{{ID: "local", Text: "pwd", Enter: true}}
	two.a.startSync(false, time.Now())
	two.a.quick().Commands = append(two.a.quick().Commands, &config.QuickCommand{ID: "during", Text: "whoami", Enter: true})
	two.p.until(t, "sync", func() bool { return !two.a.sync.running })
	if two.a.sync.err != nil {
		t.Fatal(two.a.sync.err)
	}
	for _, id := range []string{"local", "remote", "during"} {
		if two.a.quick().Command(id) == nil {
			t.Fatalf("lost command %q", id)
		}
	}
	if cloudsync.Equal(two.a.localPayload(), two.a.sync.synced) {
		t.Fatal("the new command would not trigger automatic sync")
	}
	two.sync(t)
	one.sync(t)
	if one.a.quick().Command("during") == nil {
		t.Fatal("the new command did not reach the other device")
	}
}

func TestSyncUpgradesOldPayload(t *testing.T) {
	plain := []byte(`{"schema":1,"folders":[],"hosts":[{"id":"old","name":"Old host"}],"settings":{}}`)
	envelope, err := cloudsync.Seal("correct horse", plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &memRemote{data: envelope}
	d := newDevice(t, r)
	d.a.quick().Commands = []*config.QuickCommand{{ID: "new", Text: "uptime", Enter: true}}
	d.sync(t)
	if d.a.hosts.Host("old") == nil || d.a.quick().Command("new") == nil {
		t.Fatal("upgrading lost the old hosts or local commands")
	}
	plain, err = cloudsync.Open("correct horse", r.data)
	if err != nil {
		t.Fatal(err)
	}
	var p cloudsync.Payload
	if err := json.Unmarshal(plain, &p); err != nil || p.Schema != cloudsync.PayloadSchema || len(p.QuickCommands) == 0 {
		t.Fatalf("upgraded payload %s, %v", plain, err)
	}
	if got := hostNames(d.a); !slices.Equal(got, []string{"Old host"}) {
		t.Fatalf("hosts %v", got)
	}
}
