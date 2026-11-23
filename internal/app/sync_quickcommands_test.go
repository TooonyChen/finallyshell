package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/cloudsync"
	"finallyshell/internal/config"
)

func TestSyncQuickCommandsSaveFailureDuringSync(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.a.quick().Commands = []*config.QuickCommand{{ID: "saved", Text: "uptime", Enter: true}}
	if err := one.a.saveQuick(); err != nil {
		t.Fatal(err)
	}
	one.sync(t)
	two := newDevice(t, r)
	two.sync(t)
	before := config.LoadSyncState()
	path := filepath.Join(two.dir, "quickcommands.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	one.a.quick().Command("saved").Text = "whoami"
	one.sync(t)
	two.use(t)
	two.a.startSync(false, time.Now())
	two.a.quick().Command("saved").Text = "pwd"
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := two.a.saveQuick(); err == nil {
		t.Fatal("the concurrent local save was not blocked")
	}
	two.p.until(t, "the sync", func() bool { return !two.a.sync.running })
	if two.a.sync.err == nil || !bytes.Equal(config.LoadSyncState().Base, before.Base) {
		t.Fatal("an unsaved concurrent edit advanced the merge base")
	}
	if two.a.quick().Command("saved").Text != "pwd" {
		t.Fatal("the pending edit was lost")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	two.a.quickUI = quickState{}
	two.a.sync.state = config.LoadSyncState()
	two.sync(t)
	if two.a.quick().Command("saved").Text != "whoami" || r.writes() != 2 {
		t.Fatal("reloading the old local file reverted the remote edit")
	}
}

func TestSyncQuickCommandsPersistsRepairedIDs(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	path := filepath.Join(one.dir, "quickcommands.json")
	if err := os.WriteFile(path, []byte(`{"commands":[{"text":"uptime","enter":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	one.sync(t)
	two := newDevice(t, r)
	two.sync(t)
	two.a.quick().Commands[0].Text = "whoami"
	two.sync(t)
	// Reopening a file with missing IDs must not generate a different
	// command identity, duplicate the old command and lose its edit.
	one.a.quickUI = quickState{}
	one.sync(t)
	commands := one.a.quick().Commands
	if len(commands) != 1 || commands[0].Text != "whoami" {
		t.Fatalf("reloading unpersisted repaired IDs duplicated commands: %+v", commands)
	}
}

func TestSyncQuickCommandsLocalSaveFailure(t *testing.T) {
	r := &memRemote{}
	d := newDevice(t, r)
	d.a.quick().Commands = []*config.QuickCommand{{ID: "saved", Text: "uptime", Enter: true}}
	if err := d.a.saveQuick(); err != nil {
		t.Fatal(err)
	}
	d.sync(t)
	before := config.LoadSyncState()
	path := filepath.Join(d.dir, "quickcommands.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d.a.quick().Command("saved").Text = "whoami"
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.a.saveQuick(); err == nil {
		t.Fatal("the local save was not blocked")
	}
	// Hiding the warning must not allow the unsaved command to advance
	// the merge base while its on-disk version is still old.
	d.a.quickUI.saveErr = nil
	d.a.startSync(false, time.Now())
	if d.a.sync.running {
		d.p.until(t, "the sync", func() bool { return !d.a.sync.running })
	}
	if d.a.sync.err == nil {
		t.Fatal("sync succeeded after uploading an unsaved local command")
	}
	if r.writes() != 1 || !bytes.Equal(config.LoadSyncState().Base, before.Base) {
		t.Fatal("failed local save changed the remote or merge base")
	}
	if d.a.quick().Command("saved").Text != "whoami" {
		t.Fatal("the unsaved local edit was discarded")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	// Retrying first persists the edit, then uploads it. A later reload
	// must read that saved version and avoid reverting the remote.
	d.sync(t)
	d.a.quickUI = quickState{}
	d.a.sync.state = config.LoadSyncState()
	d.sync(t)
	if d.a.quick().Command("saved").Text != "whoami" || r.writes() != 2 {
		t.Fatal("a reload reverted the successfully retried local edit")
	}
}

func TestSyncQuickCommandsApplyFailure(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(map[bool]string{false: "save failure", true: "unreadable file"}[reload], func(t *testing.T) {
			r := &memRemote{}
			one := newDevice(t, r)
			one.a.quick().Commands = []*config.QuickCommand{{ID: "saved", Text: "uptime", Enter: true}}
			one.sync(t)
			two := newDevice(t, r)
			two.sync(t)
			before := config.LoadSyncState()
			path := filepath.Join(two.dir, "quickcommands.json")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			one.a.quick().Command("saved").Text = "whoami"
			one.sync(t)
			// The destination is blocked while the app has the old list
			// cached, as when a Windows file is held open. Re-reading it
			// also exercises the uncopyable-file path.
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if reload {
				two.a.quickUI.store = nil
			}
			two.syncErr(t)
			if two.a.sync.err == nil {
				t.Fatal("sync succeeded although the received commands could not be saved")
			}
			if after := config.LoadSyncState(); !bytes.Equal(after.Base, before.Base) {
				t.Fatal("failed apply advanced the merge base")
			}
			if !reload && two.a.quick().Command("saved").Text != "uptime" {
				t.Fatal("failed apply changed the in-memory commands")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			// Simulate reopening the app's old file after the failure.
			// Its retained base must still let it receive the new edit.
			two.a.quickUI = quickState{}
			two.a.sync.state = config.LoadSyncState()
			two.sync(t)
			if two.a.quick().Command("saved").Text != "whoami" || r.writes() != 2 {
				t.Fatal("retry reverted the remote edit with the unsaved old file")
			}
		})
	}
}

// A parse error must not turn the last synced commands into deletions.
// LoadQuickCommands has preserved the broken file, so sync may repair
// this computer from the remote instead.
func TestSyncQuickCommandsBrokenLocalFile(t *testing.T) {
	r := &memRemote{}
	d := newDevice(t, r)
	d.a.quick().Commands = []*config.QuickCommand{{ID: "saved", Text: "uptime", Enter: true}}
	d.sync(t)
	broken := []byte("{not json")
	if err := os.WriteFile(filepath.Join(d.dir, "quickcommands.json"), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	d.a.quickUI.store = nil
	// Dismissing the load-error banner is not a request to clear the
	// commands. This must remain true even after its error is hidden.
	d.a.quick()
	d.a.quickUI.loadErr = nil
	d.sync(t)
	if d.a.quick().Command("saved") == nil {
		t.Fatal("a broken local file deleted the previously synced command")
	}
	if r.writes() != 1 {
		t.Fatal("reading a broken local file overwrote the remote")
	}
	copies, _ := filepath.Glob(filepath.Join(d.dir, "quickcommands.json.broken-*"))
	if len(copies) != 1 {
		t.Fatal("the broken local document was not preserved")
	}
	if data, err := os.ReadFile(copies[0]); err != nil || string(data) != string(broken) {
		t.Fatalf("backup: %q, %v", data, err)
	}
	// Once the remote repairs the local file, explicit edits sync again.
	d.a.quick().Command("saved").Text = "whoami"
	if err := d.a.saveQuick(); err != nil {
		t.Fatal(err)
	}
	d.sync(t)
	other := newDevice(t, r)
	other.sync(t)
	if other.a.quick().Command("saved").Text != "whoami" {
		t.Fatal("recovered commands did not participate in later syncs")
	}
}

func TestSyncQuickCommandsManualRecovery(t *testing.T) {
	a, _ := testApp(t)
	if err := os.WriteFile(filepath.Join(config.Dir(), "quickcommands.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	q := a.quick()
	if len(a.localPayload().QuickCommands) != 0 {
		t.Fatal("an unreadable file was uploaded as an empty document")
	}
	q.AddCommand(&config.QuickCommand{Text: "pwd", Enter: true}, "")
	if err := a.saveQuick(); err != nil {
		t.Fatal(err)
	}
	if len(a.localPayload().QuickCommands) == 0 {
		t.Fatal("a successfully saved replacement was omitted from sync")
	}
}

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
