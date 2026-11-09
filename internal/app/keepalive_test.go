package app

import (
	"errors"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/internal/sshx"
	"finallyshell/locales"
)

// keepAliveApp is an app whose keepalive seconds last 10 ms, so that
// the setting's 5 sends one every 50 ms.
func keepAliveApp(t *testing.T) (*App, *fakePlatform) {
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	a.keepAliveUnit = 10 * time.Millisecond
	a.settings.KeepAliveSeconds = 5
	return a, p
}

// waitKeepAlives waits until each server has had at least n keepalives,
// for no longer than a few intervals would take.
func waitKeepAlives(t *testing.T, p *fakePlatform, n int, srvs ...*sshtest.Server) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	p.until(t, "keepalives", func() bool {
		if time.Now().After(deadline) {
			for _, srv := range srvs {
				t.Errorf("%s: %d keepalives, want %d", srv.Addr(), srv.KeepAlives(), n)
			}
			t.FailNow()
		}
		for _, srv := range srvs {
			if srv.KeepAlives() < n {
				return false
			}
		}
		return true
	})
}

// connectQuiet connects to h, trusting the host keys asked about.
func connectQuiet(t *testing.T, a *App, p *fakePlatform, h *config.Host) *Session {
	t.Helper()
	a.passwords[h.ID] = sshtest.Password
	a.openSession(h)
	s := a.current()
	if answerHostKeys(t, p, s); s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	return s
}

// TestKeepAliveIdleSession sends keepalives on a quiet session whose
// server monitor is off, and marks the session lost once the server
// stops answering them.
func TestKeepAliveIdleSession(t *testing.T) {
	srv := sshtest.Start(t)
	a, p := keepAliveApp(t)
	h := &config.Host{ID: "srv", Name: "quiet", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	s := connectQuiet(t, a, p, h)
	waitKeepAlives(t, p, 3, srv)

	srv.StopAnswering()
	deadline := time.Now().Add(3 * time.Second)
	p.until(t, "the session lost", func() bool {
		if time.Now().After(deadline) {
			t.Fatalf("status %q after the server stopped answering", s.statusText())
		}
		return s.status != statusConnected
	})
	if s.status != statusClosed || !errors.Is(s.err, errConnectionLost) || s.statusText() != locales.ConnectionLost() {
		t.Fatalf("status %d (%v)", s.status, s.err)
	}
}

// TestKeepAliveOff sends none with the setting off.
func TestKeepAliveOff(t *testing.T) {
	srv := sshtest.Start(t)
	a, p := keepAliveApp(t)
	a.settings.KeepAliveSeconds = config.KeepAliveOff
	h := &config.Host{ID: "srv", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	connectQuiet(t, a, p, h)
	end := time.Now().Add(300 * time.Millisecond) // six intervals of the setting's 5
	p.until(t, "a while", func() bool { return time.Now().After(end) })
	if n := srv.KeepAlives(); n != 0 {
		t.Fatalf("%d keepalives with the setting off", n)
	}
}

// TestKeepAliveOption sets the interval on the General page: 30 shown
// and nothing saved by default, 0 saved as off, and back.
func TestKeepAliveOption(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	for _, s := range []string{locales.OptionsConnections(), locales.OptionKeepAliveBefore(), locales.OptionKeepAliveAfter()} {
		if !tt.HasText(s) {
			t.Errorf("the General page has no %q", s)
		}
	}
	if a.keepAlive().Interval != 30*time.Second {
		t.Errorf("default keepalive %+v", a.keepAlive())
	}
	saveShot(t, tt, "options-keepalive-en.png")

	if err := tt.Click("Decrease"); err != nil {
		t.Fatal(err)
	}
	if a.settings.KeepAliveSeconds != 29 || config.LoadSettings().KeepAliveSeconds != 29 {
		t.Fatalf("29: %d, saved %d", a.settings.KeepAliveSeconds, config.LoadSettings().KeepAliveSeconds)
	}
	for range 29 {
		if err := tt.Click("Decrease"); err != nil {
			t.Fatal(err)
		}
	}
	if a.settings.KeepAliveSeconds != config.KeepAliveOff || a.keepAlive() != (sshx.KeepAliveConfig{}) {
		t.Fatalf("0: %d, %+v", a.settings.KeepAliveSeconds, a.keepAlive())
	}
	for range 30 {
		if err := tt.Click("Increase"); err != nil {
			t.Fatal(err)
		}
	}
	if a.settings != (config.Settings{}) || config.LoadSettings() != (config.Settings{}) {
		t.Fatalf("back to 30: %+v, saved %+v", a.settings, config.LoadSettings())
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"连接", "每隔", "秒发送一次保活消息"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese", s)
		}
	}
	saveShot(t, tt, "options-keepalive-zh.png")
}

// TestKeepAliveJumpHost sends keepalives to the jump host too, on its
// own connection.
func TestKeepAliveJumpHost(t *testing.T) {
	jump, target := sshtest.Start(t), sshtest.Start(t)
	a, p := keepAliveApp(t)
	j := &config.Host{ID: "j", Name: "jump-1", Addr: jump.Host, Port: jump.Port, User: "hop", Auth: config.AuthPassword, NoMonitor: true}
	h := &config.Host{ID: "t", Name: "app-1", Addr: target.Host, Port: target.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true, Proxy: jumpTo("j")}
	a.hosts.Add(j)
	a.hosts.Add(h)
	a.passwords["j"] = sshtest.Password
	connectQuiet(t, a, p, h)
	waitKeepAlives(t, p, 3, jump, target)
}
