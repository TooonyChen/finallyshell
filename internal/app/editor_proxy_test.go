package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// choose picks option in the select labeled label.
func choose(t *testing.T, tt *ui.Tester, label, option string) {
	t.Helper()
	sel, ok := tt.AccessByRole(ui.RolePopUpButton, label)
	if !ok {
		t.Fatalf("no select %q; texts %q", label, tt.Texts())
	}
	b := sel.Bounds
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
	tt.Frame()
	if err := tt.Click(option); err != nil {
		t.Fatalf("%s: %v; texts %q", label, err, tt.Texts())
	}
	tt.Frame()
}

// proxyEditorHosts are a host to edit, web-1, and others to jump
// through.
func proxyEditorHosts(a *App) *config.Host {
	h := &config.Host{ID: "h1", Name: "web-1", Addr: "10.0.1.20", Port: 22, User: "deploy", Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.hosts.Add(&config.Host{ID: "bastion", Name: "bastion", Addr: "203.0.113.5", Port: 22, User: "jump", Auth: config.AuthKey, KeyPath: "~/.ssh/id_ed25519"})
	a.hosts.Add(&config.Host{ID: "db", Addr: "10.0.1.30", User: "root", Auth: config.AuthPassword})
	return h
}

// TestEditorProxyPage shows the Proxy page for each kind in both
// languages, for screenshots, and saves each.
func TestEditorProxyPage(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			saved := savedHosts(a)
			p.SetLocale(lang)
			h := proxyEditorHosts(a)
			a.editConnection(h)
			tt := editorWindow(t, a, p)
			if err := tt.Click(locales.EditorPageProxy()); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(locales.ProxyNoneHint()) || tt.HasText(locales.FieldPort()) {
				t.Errorf("none; texts %q", tt.Texts())
			}
			saveShot(t, tt, "editor-proxy-none-"+lang+".png")

			// SOCKS5, with its port.
			choose(t, tt, locales.FieldProxyType(), "SOCKS5")
			if a.editor.proxy.kind != config.ProxySOCKS5 || fieldValue(t, tt, locales.FieldPort()) != "1080" || !tt.HasText(locales.ProxySOCKS5Hint()) {
				t.Fatalf("socks5: %+v; texts %q", a.editor.proxy, tt.Texts())
			}
			// The password shows once there is a user.
			if _, ok := tt.AccessByRole(ui.RoleTextField, locales.FieldPassword()); ok {
				t.Error("a password without a user")
			}
			typeInto(t, tt, locales.FieldHost(), "198.51.100.7")
			typeInto(t, tt, locales.FieldUser(), "alice")
			tt.Frame()
			typeInto(t, tt, locales.FieldPassword(), "pa55")
			saveShot(t, tt, "editor-proxy-socks5-"+lang+".png")

			// HTTP: the port follows the kind while it is the usual one.
			choose(t, tt, locales.FieldProxyType(), "HTTP")
			if fieldValue(t, tt, locales.FieldPort()) != "8080" || fieldValue(t, tt, locales.FieldHost()) != "198.51.100.7" || !tt.HasText(locales.ProxyHTTPHint()) {
				t.Fatalf("http; texts %q", tt.Texts())
			}
			typeInto(t, tt, locales.FieldPort(), "3128")
			saveShot(t, tt, "editor-proxy-http-"+lang+".png")
			choose(t, tt, locales.FieldProxyType(), "SOCKS5")
			if fieldValue(t, tt, locales.FieldPort()) != "3128" {
				t.Errorf("a port typed changed with the kind")
			}
			choose(t, tt, locales.FieldProxyType(), "HTTP")
			if err := tt.Click(locales.Ok()); err != nil {
				t.Fatal(err)
			}
			want := &config.ProxyConfig{Kind: config.ProxyHTTP, Addr: "198.51.100.7", Port: 3128, User: "alice"}
			if !reflect.DeepEqual(h.Proxy, want) || a.proxySecrets()["proxy/h1"] != "pa55" {
				t.Fatalf("saved %+v, password %q", h.Proxy, a.proxySecrets()["proxy/h1"])
			}
			if strings.Contains(string(*saved), "pa55") || !strings.Contains(string(*saved), `"proxy":{"kind":"http","addr":"198.51.100.7","port":3128,"user":"alice"}`) {
				t.Errorf("saved %s", *saved)
			}

			// A jump host, from the other saved hosts.
			a.editConnection(h)
			tt = editorWindow(t, a, p)
			if err := tt.Click(locales.EditorPageProxy()); err != nil {
				t.Fatal(err)
			}
			if fieldValue(t, tt, locales.FieldUser()) != "alice" || fieldValue(t, tt, locales.FieldPort()) != "3128" || a.editor.proxy.password != "pa55" {
				t.Errorf("reopened: %+v; texts %q", a.editor.proxy, tt.Texts())
			}
			choose(t, tt, locales.FieldProxyType(), locales.ProxyJumpHost())
			if !tt.HasText(locales.ProxyJumpHint()) || !tt.HasText(locales.JumpHostNote()) {
				t.Errorf("jump; texts %q", tt.Texts())
			}
			sel, _ := tt.AccessByRole(ui.RolePopUpButton, locales.FieldJumpHost())
			tt.ClickAt(sel.Bounds.X+sel.Bounds.W/2, sel.Bounds.Y+sel.Bounds.H/2)
			tt.Frame()
			// Not web-1 itself.
			if !tt.HasText("bastion (jump@203.0.113.5)") || !tt.HasText("root@10.0.1.30") || tt.HasText("web-1 (deploy@10.0.1.20)") {
				t.Errorf("choices; texts %q", tt.Texts())
			}
			if err := tt.Click("bastion (jump@203.0.113.5)"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			saveShot(t, tt, "editor-proxy-jump-"+lang+".png")
			if err := tt.Click(locales.Ok()); err != nil {
				t.Fatal(err)
			}
			if want := (&config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "bastion"}); !reflect.DeepEqual(h.Proxy, want) || a.windowOpen(WindowEditor) {
				t.Fatalf("saved %+v", h.Proxy)
			}
			if _, kept := a.proxySecrets()["proxy/h1"]; kept {
				t.Error("the proxy's password outlived the proxy")
			}

			// None again: no proxy.
			a.editConnection(h)
			tt = editorWindow(t, a, p)
			tt.Click(locales.EditorPageProxy())
			choose(t, tt, locales.FieldProxyType(), locales.ProxyNone())
			tt.Click(locales.Ok())
			if h.Proxy != nil {
				t.Errorf("proxy %+v", h.Proxy)
			}
		})
	}
}

func TestEditorProxyErrors(t *testing.T) {
	a, p := testApp(t)
	h := proxyEditorHosts(a)
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	tt.Click(locales.EditorPageProxy())
	choose(t, tt, locales.FieldProxyType(), "SOCKS5")
	typeInto(t, tt, locales.FieldPort(), "0")
	// From another page, OK shows the Proxy page with its errors.
	tt.Click(locales.EditorPageSSH())
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.editor.nav.Page != editorProxy || !tt.HasText(locales.ErrHostRequired()) || !tt.HasText(locales.ErrPortInvalid()) || h.Proxy != nil {
		t.Fatalf("page %q; texts %q", a.editor.nav.Page, tt.Texts())
	}
	saveShot(t, tt, "editor-proxy-errors-en.png")
	typeInto(t, tt, locales.FieldHost(), "proxy.example.com")
	typeInto(t, tt, locales.FieldPort(), "1080")
	if tt.HasText(locales.ErrHostRequired()) || tt.HasText(locales.ErrPortInvalid()) {
		t.Errorf("errors after fixing; texts %q", tt.Texts())
	}

	// A jump host must be chosen.
	choose(t, tt, locales.FieldProxyType(), locales.ProxyJumpHost())
	tt.Click(locales.Ok())
	if !tt.HasText(locales.ErrJumpHostRequired()) || !a.windowOpen(WindowEditor) {
		t.Fatalf("texts %q", tt.Texts())
	}

	// One that comes back here is a loop.
	a.hosts.Host("bastion").Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "db"}
	a.hosts.Host("db").Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "h1"}
	choose(t, tt, locales.FieldJumpHost(), "bastion (jump@203.0.113.5)")
	loop := locales.ErrJumpLoop("web-1 → bastion → 10.0.1.30 → web-1")
	if !tt.HasText(loop) || h.Proxy != nil {
		t.Fatalf("no loop; texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-proxy-loop-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("跳板机形成了循环：web-1 → bastion → 10.0.1.30 → web-1") {
		t.Errorf("not in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-proxy-loop-zh-Hans.png")
	p.SetLocale("")

	// Or one whose own jump host was deleted.
	a.hosts.Host("db").Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "gone"}
	tt.Frame()
	if !tt.HasText(locales.ErrJumpHostMissing("10.0.1.30")) {
		t.Errorf("missing; texts %q", tt.Texts())
	}
	a.hosts.Host("db").Proxy = nil
	tt.Click(locales.Ok())
	if a.windowOpen(WindowEditor) || h.Proxy == nil || h.Proxy.JumpHost != "bastion" {
		t.Fatalf("not saved: %+v", h.Proxy)
	}

	// With no other host, there is nothing to choose.
	a.newConnection()
	b := a.hosts
	a.hosts = config.NewStore()
	a.editor.proxy.hosts = a.hosts
	tt = editorWindow(t, a, p)
	tt.Type("solo.example.com")
	tt.Click(locales.EditorPageProxy())
	choose(t, tt, locales.FieldProxyType(), locales.ProxyJumpHost())
	if !tt.HasText(locales.NoJumpHosts()) {
		t.Errorf("texts %q", tt.Texts())
	}
	tt.Click(locales.Ok())
	if !tt.HasText(locales.ErrJumpHostRequired()) {
		t.Errorf("texts %q", tt.Texts())
	}
	a.hosts = b
}

// A proxy key this build does not know is kept through the editor, and
// a proxy password is not written.
func TestEditorProxyKeepsUnknownKeys(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	var h config.Host
	json.Unmarshal([]byte(`{"id": "h1", "addr": "10.0.0.1", "user": "root", "auth": "password",
		"proxy": {"kind": "socks5", "addr": "127.0.0.1", "port": 1080, "dns": "remote"}}`), &h)
	a.hosts.Add(&h)
	a.editConnection(&h)
	tt := editorWindow(t, a, p)
	tt.Click(locales.EditorPageProxy())
	typeInto(t, tt, locales.FieldPort(), "1090")
	tt.Click(locales.Ok())
	var file struct {
		Hosts []struct {
			Proxy map[string]any `json:"proxy"`
		} `json:"hosts"`
	}
	json.Unmarshal(*saved, &file)
	if len(file.Hosts) != 1 || !reflect.DeepEqual(file.Hosts[0].Proxy, map[string]any{"kind": "socks5", "addr": "127.0.0.1", "port": 1090.0, "dns": "remote"}) {
		t.Errorf("saved %s", *saved)
	}
}
