package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"strings"
	"testing"

	"finallyshell/locales"
)

// A reconnect opens a new remote shell, but keeps earlier output in the
// same pane. Repeated reconnects neither erase nor duplicate it.
func TestSSHReconnectHistory(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	p.SetLocale("en")
	old := s.term()
	for i := range 80 {
		old.Feed([]byte(fmt.Sprintf("history-%02d: 中文\r\n", i)))
	}
	for attempt := range 2 {
		srv.DropConnections()
		p.until(t, "connection lost", func() bool { return s.status == statusClosed })
		if attempt == 0 {
			if err := tt.Click(locales.Reconnect()); err != nil {
				t.Fatal(err)
			}
		} else {
			tt.ClickAt(paneAt(t, a, tt, s, s.pane()))
			press(tt, a, scReconnect)
		}
		p.until(t, "reconnected", func() bool { return s.status != statusConnecting })
		if s.status != statusConnected {
			t.Fatalf("reconnect: %v", s.err)
		}
		marker := fmt.Sprintf("new shell %d ready", attempt+1)
		if err := srv.WriteShell(attempt+1, []byte(marker+"\r\n[root@web-1 ~]# ")); err != nil {
			t.Fatal(err)
		}
		p.until(t, "new shell output", func() bool { return strings.Contains(s.term().Text(), marker) })
		p.until(t, "reconnected files", func() bool { return s.sftp != nil && !s.files.loading && s.files.err == nil })
		history := string(s.term().History())
		for i := range 80 {
			marker := fmt.Sprintf("history-%02d: 中文", i)
			if strings.Count(history, marker) != 1 {
				t.Fatalf("reconnect %d lost or duplicated %q", attempt, marker)
			}
		}
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		tt.Frame()
		saveShot(t, tt, "reconnect-history-"+lang+".png")
	}
	// The old display is eventually released, rather than retained forever.
	if len(old.History()) != 0 {
		t.Fatal("old terminal still allocated")
	}
}

// A failed attempt keeps the last display through retries, and closing a
// failed tab releases it just as closing a connected tab does.
func TestSSHReconnectFailedHistory(t *testing.T) {
	for _, closeFailed := range []bool{false, true} {
		t.Run(fmt.Sprint(closeFailed), func(t *testing.T) {
			a, p, s, _, srv := paneHost(t, followHome(t), false)
			old := s.term()
			old.Feed([]byte("before failed reconnect\r\n"))
			srv.DropConnections()
			p.until(t, "connection lost", func() bool { return s.status == statusClosed })
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			_, port, _ := net.SplitHostPort(listener.Addr().String())
			listener.Close()
			goodPort := s.Host.Port
			s.Host.Port, _ = strconv.Atoi(port)
			a.reconnect(s)
			if s.term() != old {
				t.Fatal("history disappeared while connecting")
			}
			p.until(t, "failed reconnect", func() bool { return s.status == statusFailed })
			if s.term() != old || !strings.Contains(string(old.History()), "before failed reconnect") {
				t.Fatal("failed reconnect destroyed previous output")
			}
			if closeFailed {
				a.closeSession(s)
			} else {
				s.Host.Port = goodPort
				a.reconnect(s)
				p.until(t, "retry", func() bool { return s.status != statusConnecting })
				if s.status != statusConnected || !strings.Contains(string(s.term().History()), "before failed reconnect") {
					t.Fatalf("retry lost output: %v", s.err)
				}
				current := s.term()
				a.CloseAll()
				if len(current.History()) != 0 {
					t.Fatal("application close left terminal allocated")
				}
			}
			if len(old.History()) != 0 {
				t.Fatal("closed terminal still allocated")
			}
		})
	}
}

func TestSSHReconnectPaneHistories(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	panes := s.panes.panes()
	for i, pane := range panes {
		pane.term.Feed([]byte(fmt.Sprintf("output for pane %d\r\n", i)))
	}
	// Explicitly reconnect an active connection, so the last reader must
	// finish before its output is exported.
	a.reconnect(s)
	p.until(t, "reconnected panes", func() bool { return s.status != statusConnecting })
	if s.status != statusConnected {
		t.Fatalf("reconnect: %v", s.err)
	}
	for i, pane := range panes {
		history := string(pane.term.History())
		if !strings.Contains(history, fmt.Sprintf("output for pane %d", i)) || strings.Contains(history, fmt.Sprintf("output for pane %d", 1-i)) {
			t.Fatalf("pane %d mixed or lost history: %q", i, history)
		}
	}
	if srv.Logins() != 2 {
		t.Fatalf("logins: %d", srv.Logins())
	}
	closed := panes[1].term
	a.closePane(s, panes[1])
	if len(closed.History()) != 0 {
		t.Fatal("closed pane left terminal allocated")
	}
}

// transcriptConn provides immediate server output, records stdin and
// preserves resize support through the gate.
type transcriptConn struct {
	*bytes.Reader
	mu    sync.Mutex
	input bytes.Buffer
	size  [2]int
}

func (c *transcriptConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.input.Write(p)
}
func (c *transcriptConn) Close() error { return nil }
func (c *transcriptConn) Resize(cols, rows int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.size = [2]int{cols, rows}
	return nil
}

func historyDone(t *testing.T, term *terminal.Terminal) {
	t.Helper()
	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("terminal reader did not end")
	}
}

func TestSSHRestoreHistory(t *testing.T) {
	for _, alternate := range []bool{false, true} {
		t.Run(fmt.Sprint(alternate), func(t *testing.T) {
			previousConn := newPaneConn(&transcriptConn{Reader: bytes.NewReader(nil)})
			previous, err := terminal.New(terminal.Options{Conn: previousConn})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { previous.Close() })
			previous.Resize(12, 6)
			previous.Feed([]byte("old prompt\r\n\x1b[31m中文ABCDEFGHIJKLMNO\x1b[0m\r\n"))
			if alternate {
				previous.Feed([]byte("\x1b[?1049hfullscreen content"))
			}
			previousConn.start()
			historyDone(t, previous)
			server := &transcriptConn{Reader: bytes.NewReader([]byte("new prompt\r\n"))}
			conn := newPaneConn(server)
			next, err := terminal.New(terminal.Options{Conn: conn})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { next.Close() })
			if err := restorePaneHistory(context.Background(), next, previous); err != nil {
				t.Fatal(err)
			}
			before := string(next.History())
			if !strings.Contains(before, "old prompt") || strings.Contains(before, "new prompt") || strings.Contains(before, "fullscreen content") {
				t.Fatalf("restored history before server output: %q", before)
			}
			if cols, rows := next.Size(); cols != 12 || rows != 6 {
				t.Fatalf("size: %dx%d", cols, rows)
			}
			// Replay at the same width preserves soft wraps and styled rows.
			expected := string(previous.History())
			if !strings.HasPrefix(before, expected) {
				t.Fatalf("history changed wraps or styles: %q; want prefix %q", before, expected)
			}
			conn.start()
			historyDone(t, next)
			after := string(next.History())
			if strings.Index(after, "new prompt") <= strings.Index(after, "old prompt") {
				t.Fatalf("server output preceded history: %q", after)
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			if strings.Contains(server.input.String(), "old prompt") || strings.Contains(server.input.String(), "中文") {
				t.Fatalf("history sent to SSH stdin: %q", server.input.String())
			}
			if server.size != [2]int{12, 6} {
				t.Fatalf("transport did not resize: %v", server.size)
			}
		})
	}
}

func TestSSHHistoryCanceled(t *testing.T) {
	conn := newPaneConn(&transcriptConn{Reader: bytes.NewReader(nil)})
	previous, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	previous.Feed([]byte("kept after cancellation"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := restorePaneHistory(ctx, previous, previous); err != context.Canceled {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(string(previous.History()), "kept after cancellation") {
		t.Fatal("cancellation changed output")
	}
	conn.Close() // also releases Read, which never started
	historyDone(t, previous)
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != io.EOF {
		t.Fatalf("closed gate: %v", err)
	}
}

// Choosing Disconnect in the row menu keeps SSH output as well, while
// permanent tab closure still frees it.
func TestSSHDisconnectReconnectHistory(t *testing.T) {
	a, p, s, tt, _ := paneHost(t, followHome(t), false)
	old := s.term()
	old.Feed([]byte("manually disconnected history\r\n"))
	tt.RightClickAt(rowCenter(t, tt, s.label()))
	if err := tt.ChooseMenuItem(locales.Disconnect()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.status != statusClosed || s.term() != old || !strings.Contains(string(old.History()), "manually disconnected history") {
		t.Fatal("Disconnect menu cleared the old output")
	}
	a.reconnect(s)
	p.until(t, "manual reconnect", func() bool { return s.status != statusConnecting })
	if s.status != statusConnected || !strings.Contains(string(s.term().History()), "manually disconnected history") {
		t.Fatalf("manual reconnect: %v", s.err)
	}
	current := s.term()
	a.closeSession(s)
	if len(current.History()) != 0 {
		t.Fatal("tab close left terminal allocated")
	}
}
