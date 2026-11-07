package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/sshx"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// demoTunnels are a host's tunnels for the screenshots, one of each kind
// and one open to other computers.
func demoTunnels() []config.Tunnel {
	return []config.Tunnel{
		{Kind: config.TunnelLocal, ListenPort: 15432, TargetHost: "db.internal", TargetPort: 5432},
		{Kind: config.TunnelRemote, ListenPort: 8080, TargetHost: "127.0.0.1", TargetPort: 3000},
		{Kind: config.TunnelDynamic, ListenPort: 1080},
		{Kind: config.TunnelLocal, ListenHost: "0.0.0.0", ListenPort: 8443, TargetHost: "10.0.0.7", TargetPort: 443, Disabled: true},
	}
}

// openTunnelsPage opens the editor on h at its Tunnels page.
func openTunnelsPage(t *testing.T, a *App, p *fakePlatform, h *config.Host) *ui.Tester {
	t.Helper()
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	if err := tt.Click(locales.EditorPageTunnels()); err != nil {
		t.Fatal(err)
	}
	if a.editor.nav.Page != editorTunnels {
		t.Fatalf("page %q", a.editor.nav.Page)
	}
	return tt
}

func TestEditorTunnelsPage(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			h := &config.Host{ID: "h1", Name: "web-1", Addr: "203.0.113.10", User: "root", Auth: config.AuthPassword, Tunnels: demoTunnels()}
			a.hosts.Add(h)
			tt := openTunnelsPage(t, a, p, h)
			for _, want := range []string{locales.TunnelKindLocal(), locales.TunnelKindRemote(), locales.TunnelKindDynamic(),
				"127.0.0.1:15432", "db.internal:5432", "127.0.0.1:8080", "127.0.0.1:3000", "127.0.0.1:1080", locales.TunnelSocks(),
				"0.0.0.0:8443", locales.ColTunnelEnabled()} {
				if !tt.HasText(want) {
					t.Errorf("missing %q; texts %q", want, tt.Texts())
				}
			}
			if lang == "zh-Hans" && (!tt.HasText("本地 (-L)") || !tt.HasText("远程 (-R)") || !tt.HasText("动态 (-D)") || !tt.HasText("隧道")) {
				t.Errorf("not in Chinese; texts %q", tt.Texts())
			}
			saveShot(t, tt, "editor-tunnels-rows-"+lang+".png")

			// A new tunnel, wrong, then open to other computers.
			if err := tt.Click(locales.TunnelAdd()); err != nil {
				t.Fatal(err)
			}
			if !a.editor.tunnels.dialog || !tt.HasText(locales.TunnelNew()) || !tt.HasText(locales.TunnelLoopbackNote()) {
				t.Fatalf("no dialog; texts %q", tt.Texts())
			}
			typeInto(t, tt, locales.FieldTargetHost(), "")
			tt.Key(0, ui.KeyEnter) // the dialog's OK: the window behind has one too
			for _, want := range []string{locales.ErrPortInvalid(), locales.ErrTargetHostRequired()} {
				if !tt.HasText(want) {
					t.Errorf("missing %q; texts %q", want, tt.Texts())
				}
			}
			if !a.windowOpen(WindowEditor) || !a.editor.tunnels.dialog || len(a.editor.tunnels.list) != 4 {
				t.Fatal("a wrong tunnel was taken")
			}
			saveShot(t, tt, "editor-tunnel-errors-"+lang+".png")
			typeInto(t, tt, locales.FieldListenPort(), "1080") // the SOCKS proxy's
			typeInto(t, tt, locales.FieldTargetHost(), "intranet.example")
			typeInto(t, tt, locales.FieldTargetPort(), "80")
			if !tt.HasText(locales.ErrTunnelDuplicate()) {
				t.Errorf("no duplicate error; texts %q", tt.Texts())
			}
			typeInto(t, tt, locales.FieldListenPort(), "8081") // the remote one's, on the server: not the same
			if tt.HasText(locales.ErrTunnelDuplicate()) {
				t.Error("a remote port taken for a local one")
			}
			if err := tt.Click(locales.TunnelOtherHosts()); err != nil {
				t.Fatal(err)
			}
			if fieldValue(t, tt, locales.FieldListenAddr()) != "0.0.0.0" || !tt.HasText(locales.TunnelPublicWarnLocal()) {
				t.Errorf("no address or warning; texts %q", tt.Texts())
			}
			saveShot(t, tt, "editor-tunnel-public-"+lang+".png")
			// Enter in a field is the dialog's OK, not the editor's.
			tt.Key(0, ui.KeyEnter)
			te := &a.editor.tunnels
			if te.dialog || !a.windowOpen(WindowEditor) || len(te.list) != 5 || te.row != 4 {
				t.Fatalf("dialog %v, editor %v, %d tunnels, row %d", te.dialog, a.windowOpen(WindowEditor), len(te.list), te.row)
			}
			want := config.Tunnel{Kind: config.TunnelLocal, ListenHost: "0.0.0.0", ListenPort: 8081, TargetHost: "intranet.example", TargetPort: 80}
			if !reflect.DeepEqual(te.list[4], want) {
				t.Errorf("added %+v", te.list[4])
			}

			// Edit the remote one into a dynamic one: its target goes.
			te.row = 1
			tt.Frame()
			if err := tt.Click(locales.TunnelEdit()); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(locales.TunnelChange()) || fieldValue(t, tt, locales.FieldListenPort()) != "8080" ||
				fieldValue(t, tt, locales.FieldTargetHost()) != "127.0.0.1" || !tt.HasText(locales.TunnelLoopbackNoteRemote()) {
				t.Fatalf("edit dialog; texts %q", tt.Texts())
			}
			te.form.kind = config.TunnelDynamic
			tt.Frame()
			if tt.HasText(locales.FieldTargetHost()) || !tt.HasText(locales.TunnelHintDynamic()) {
				t.Errorf("dynamic has a target; texts %q", tt.Texts())
			}
			typeInto(t, tt, locales.FieldListenPort(), "1081")
			tt.Key(0, ui.KeyEnter)
			if got := te.list[1]; got.Kind != config.TunnelDynamic || got.ListenPort != 1081 || got.TargetHost != "" || got.TargetPort != 0 {
				t.Errorf("edited %+v", got)
			}

			// Escape closes the dialog alone, without its changes.
			if err := tt.Click(locales.TunnelEdit()); err != nil {
				t.Fatal(err)
			}
			typeInto(t, tt, locales.FieldListenPort(), "9999")
			tt.Key(0, ui.KeyEscape)
			if te.dialog || !a.windowOpen(WindowEditor) || te.list[1].ListenPort != 1081 {
				t.Fatalf("dialog %v, editor %v, port %d", te.dialog, a.windowOpen(WindowEditor), te.list[1].ListenPort)
			}

			// The table's checkbox turns one on; Remove takes the first.
			te.list[3].Disabled = true
			if err := tt.Click(locales.TunnelStartWithSession()); err != nil { // the first row's
				t.Fatal(err)
			}
			if te.list[0].Enabled() {
				t.Error("the checkbox did not turn the first tunnel off")
			}
			te.row = 0
			tt.Frame()
			if err := tt.Click(locales.TunnelRemove()); err != nil {
				t.Fatal(err)
			}
			if len(te.list) != 4 || te.list[0].Kind != config.TunnelDynamic {
				t.Fatalf("after removing: %+v", te.list)
			}

			// OK saves them with the host.
			saved := savedHosts(a)
			if err := tt.Click(locales.Ok()); err != nil {
				t.Fatal(err)
			}
			if a.windowOpen(WindowEditor) {
				t.Fatal("the editor stayed open")
			}
			if len(h.Tunnels) != 4 || h.Tunnels[0].ListenPort != 1081 || h.Tunnels[3].ListenPort != 8081 {
				t.Errorf("saved %+v", h.Tunnels)
			}
			var store struct{ Hosts []map[string]any }
			json.Unmarshal(*saved, &store)
			if n := len(store.Hosts[0]["tunnels"].([]any)); n != 4 {
				t.Errorf("%d tunnels written: %s", n, *saved)
			}
		})
	}
}

// Removing every tunnel saves none, and a host without them writes no
// key; Cancel keeps the host's.
func TestEditorTunnelsCancelAndNone(t *testing.T) {
	a, p := testApp(t)
	h := &config.Host{ID: "h1", Addr: "203.0.113.10", User: "root", Auth: config.AuthPassword, Tunnels: demoTunnels()[:1]}
	a.hosts.Add(h)
	a.passwords["h1"] = "pw"
	tt := openTunnelsPage(t, a, p, h)
	a.editor.tunnels.row = 0
	tt.Frame()
	if err := tt.Click(locales.TunnelRemove()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.TunnelsEmpty()) {
		t.Errorf("texts %q", tt.Texts())
	}
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	if len(h.Tunnels) != 1 {
		t.Fatalf("cancel changed the tunnels: %+v", h.Tunnels)
	}
	tt = openTunnelsPage(t, a, p, h)
	a.editor.tunnels.row = 0
	tt.Frame()
	tt.Click(locales.TunnelRemove())
	tt.Click(locales.Ok())
	if h.Tunnels != nil {
		t.Errorf("tunnels %+v", h.Tunnels)
	}
	data, _ := json.Marshal(h)
	var keys map[string]any
	json.Unmarshal(data, &keys)
	if _, ok := keys["tunnels"]; ok {
		t.Errorf("wrote %s", data)
	}
}

func TestTunnelFormChecks(t *testing.T) {
	te := tunnelEditor{list: demoTunnels(), editing: -1}
	for _, tc := range []struct {
		name string
		form tunnelForm
		want map[tunnelField]string
	}{
		{"fine", tunnelForm{kind: config.TunnelLocal, listenPort: "9000", targetHost: "db", targetPort: "5432"}, nil},
		{"IPv6 target", tunnelForm{kind: config.TunnelLocal, listenPort: "9000", targetHost: "::1", targetPort: "22"}, nil},
		{"no port", tunnelForm{kind: config.TunnelLocal, listenPort: "0", targetHost: "db", targetPort: "x"},
			map[tunnelField]string{fieldListenPort: locales.ErrPortInvalid(), fieldTargetPort: locales.ErrPortInvalid()}},
		{"bad host", tunnelForm{kind: config.TunnelRemote, listenPort: "9000", targetHost: "a b", targetPort: "1"},
			map[tunnelField]string{fieldTargetHost: locales.ErrListenAddrInvalid()}},
		{"dynamic needs no target", tunnelForm{kind: config.TunnelDynamic, listenPort: "9000"}, nil},
		{"public needs an address", tunnelForm{kind: config.TunnelDynamic, listenPort: "9000", public: true, listenHost: " "},
			map[tunnelField]string{fieldListenHost: locales.ErrListenAddrInvalid()}},
		{"same port", tunnelForm{kind: config.TunnelDynamic, listenPort: "15432"},
			map[tunnelField]string{fieldListenPort: locales.ErrTunnelDuplicate()}},
		{"same port on all addresses", tunnelForm{kind: config.TunnelDynamic, listenPort: "1080", public: true, listenHost: "::"},
			map[tunnelField]string{fieldListenPort: locales.ErrTunnelDuplicate()}},
		{"same port on another address", tunnelForm{kind: config.TunnelDynamic, listenPort: "1080", public: true, listenHost: "192.168.1.5"}, nil},
		{"same port on the server", tunnelForm{kind: config.TunnelRemote, listenPort: "8080", targetHost: "x", targetPort: "1"},
			map[tunnelField]string{fieldListenPort: locales.ErrTunnelDuplicate()}},
		{"remote on a host name", tunnelForm{kind: config.TunnelRemote, listenPort: "9000", public: true, listenHost: "myserver.example", targetHost: "x", targetPort: "1"},
			map[tunnelField]string{fieldListenHost: locales.ErrRemoteListenAddrIP()}},
		{"remote on an IPv6 address", tunnelForm{kind: config.TunnelRemote, listenPort: "9000", public: true, listenHost: "::", targetHost: "x", targetPort: "1"}, nil},
		{"remote on localhost", tunnelForm{kind: config.TunnelRemote, listenPort: "9000", public: true, listenHost: "localhost", targetHost: "x", targetPort: "1"}, nil},
		{"local on a host name", tunnelForm{kind: config.TunnelLocal, listenPort: "9000", public: true, listenHost: "myhost.lan", targetHost: "x", targetPort: "1"}, nil},
	} {
		te.form = tc.form
		got := te.problems()
		if len(got) != len(tc.want) || len(tc.want) > 0 && !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// A remote tunnel on localhost listens on the server's loopback, as
	// one without an address does.
	f := tunnelForm{kind: config.TunnelRemote, listenPort: "9000", public: true, listenHost: "localhost", targetHost: "x", targetPort: "1"}
	if got := f.tunnel(); got.ListenHost != "" {
		t.Errorf("localhost kept as %q", got.ListenHost)
	}
	f.kind = config.TunnelLocal
	if got := f.tunnel(); got.ListenHost != "localhost" {
		t.Errorf("a local tunnel's localhost became %q", got.ListenHost)
	}
	// A hand-written remote tunnel on a host name says why it cannot
	// start.
	err := &sshx.TunnelError{Addr: "myserver:9000", Remote: true, Err: sshx.ErrRemoteListenHost}
	if got := describeTunnelError(err); got != locales.ErrTunnelRemoteHostName("myserver:9000") {
		t.Errorf("described as %q", got)
	}

	// A tunnel edited is not the same as itself.
	te.editing = 0
	te.form = tunnelForm{kind: config.TunnelLocal, listenPort: "15432", targetHost: "db", targetPort: "1"}
	if got := te.problems(); len(got) != 0 {
		t.Errorf("itself: %v", got)
	}
}
