package app

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// keyConn is a shell that never prints and keeps what the terminal
// writes to it: the bytes the keys typed.
type keyConn struct {
	mu   sync.Mutex
	got  []byte
	done chan struct{}
}

func (c *keyConn) Read([]byte) (int, error) { <-c.done; return 0, io.EOF }
func (c *keyConn) Close() error             { return nil }
func (c *keyConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.got = append(c.got, p...)
	c.mu.Unlock()
	return len(p), nil
}

// keyMark follows each key the test presses, sent with Terminal.Send,
// which keeps its place among the keys: what came before it is all the
// key sent.
const keyMark = "<mark>"

// sent returns what the terminal wrote since the last call, up to the
// mark it sends now.
func (c *keyConn) sent(t *testing.T, term *terminal.Terminal) string {
	t.Helper()
	term.Send([]byte(keyMark))
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		got := c.got
		if i := bytes.Index(got, []byte(keyMark)); i >= 0 {
			c.got = append([]byte(nil), got[i+len(keyMark):]...)
			c.mu.Unlock()
			return string(got[:i])
		}
		c.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("the mark never came; got %q", got)
		}
		time.Sleep(time.Millisecond)
	}
}

type keyCase struct {
	name string
	mods ui.Modifiers
	key  ui.Key
	want string
}

// shellKeys are keys a shell needs, with what a terminal sends for them
// in its default modes (xterm's legacy sequences, normal cursor keys).
var shellKeys = []keyCase{
	{"Ctrl+C", ui.Ctrl, ui.KeyC, "\x03"},
	{"Ctrl+D", ui.Ctrl, ui.KeyD, "\x04"},
	{"Ctrl+Z", ui.Ctrl, ui.KeyZ, "\x1a"},
	{"Ctrl+\\", ui.Ctrl, ui.KeyBackslash, "\x1c"},
	{"Ctrl+L", ui.Ctrl, ui.KeyL, "\x0c"},
	{"Ctrl+A", ui.Ctrl, ui.KeyA, "\x01"},
	{"Ctrl+E", ui.Ctrl, ui.KeyE, "\x05"},
	{"Ctrl+U", ui.Ctrl, ui.KeyU, "\x15"},
	{"Ctrl+W", ui.Ctrl, ui.KeyW, "\x17"},
	{"Ctrl+K", ui.Ctrl, ui.KeyK, "\x0b"},
	{"Ctrl+Y", ui.Ctrl, ui.KeyY, "\x19"},
	{"Ctrl+R", ui.Ctrl, ui.KeyR, "\x12"},
	{"Ctrl+S", ui.Ctrl, ui.KeyS, "\x13"},
	{"Ctrl+Q", ui.Ctrl, ui.KeyQ, "\x11"},
	{"Ctrl+T", ui.Ctrl, ui.KeyT, "\x14"},
	{"Ctrl+V", ui.Ctrl, ui.KeyV, "\x16"},
	{"Ctrl+B", ui.Ctrl, ui.KeyB, "\x02"},
	{"Ctrl+F", ui.Ctrl, ui.KeyF, "\x06"},
	{"Ctrl+N", ui.Ctrl, ui.KeyN, "\x0e"},
	{"Ctrl+P", ui.Ctrl, ui.KeyP, "\x10"},
	{"Ctrl+H", ui.Ctrl, ui.KeyH, "\x08"},
	{"Ctrl+J", ui.Ctrl, ui.KeyJ, "\n"},
	{"Ctrl+G", ui.Ctrl, ui.KeyG, "\x07"},
	{"Ctrl+O", ui.Ctrl, ui.KeyO, "\x0f"},
	{"Ctrl+X", ui.Ctrl, ui.KeyX, "\x18"},
	{"Ctrl+]", ui.Ctrl, ui.KeyBracketRight, "\x1d"},
	{"Ctrl+/", ui.Ctrl, ui.KeySlash, "\x1f"},
	{"Ctrl+Space", ui.Ctrl, ui.KeySpace, "\x00"},
	{"Tab", 0, ui.KeyTab, "\t"},
	{"Shift+Tab", ui.Shift, ui.KeyTab, "\x1b[Z"},
	{"Enter", 0, ui.KeyEnter, "\r"},
	{"Backspace", 0, ui.KeyBackspace, "\x7f"},
	{"Esc", 0, ui.KeyEscape, "\x1b"},
	{"Up", 0, ui.KeyUp, "\x1b[A"},
	{"Down", 0, ui.KeyDown, "\x1b[B"},
	{"Right", 0, ui.KeyRight, "\x1b[C"},
	{"Left", 0, ui.KeyLeft, "\x1b[D"},
	{"Ctrl+Left", ui.Ctrl, ui.KeyLeft, "\x1b[1;5D"},
	{"Home", 0, ui.KeyHome, "\x1b[H"},
	{"End", 0, ui.KeyEnd, "\x1b[F"},
	{"Delete", 0, ui.KeyDelete, "\x1b[3~"},
	{"F1", 0, ui.KeyF1, "\x1bOP"},
	// Alt as Meta: Esc and the letter, for readline's word keys.
	{"Alt+B", ui.Alt, ui.KeyB, "\x1bb"},
	{"Alt+F", ui.Alt, ui.KeyF, "\x1bf"},
	{"Alt+.", ui.Alt, ui.KeyPeriod, "\x1b."},
}

// TestTerminalKeysReachShell presses keys on the terminal in the app's
// window and checks the bytes that reach the shell's connection: the
// window's shortcuts and the terminal's copy and paste keys must leave
// the shell its control keys.
func TestTerminalKeysReachShell(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	other := demoSession(a, "db-1", "10.0.0.12")
	a.active = 0
	conn := &keyConn{done: make(chan struct{})}
	defer close(conn.done)
	term, err := terminal.New(terminal.Options{Conn: conn, Font: a.termFont, Theme: a.termTheme, DarkTheme: a.termTheme})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	s.term = term
	term.Feed([]byte("[root@web-1 ~]# copy me"))

	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	focus := func() {
		tt.ClickAt(760, 260) // in the terminal
		conn.sent(t, term)
	}
	focus()
	press := func(k keyCase) {
		t.Helper()
		tt.Key(k.mods, k.key)
		if got := conn.sent(t, term); got != k.want {
			t.Errorf("%s sent %q, want %q", k.name, got, k.want)
		}
		if a.managerOpen {
			t.Errorf("%s opened the connection manager", k.name)
			a.managerOpen = false
			tt.Frame()
		}
	}
	for _, k := range shellKeys {
		press(k)
	}

	// A program that asks for application cursor keys (DECCKM), as
	// readline and full-screen programs do, gets SS3 sequences.
	term.Feed([]byte("\x1b[?1h"))
	press(keyCase{"Up (application)", 0, ui.KeyUp, "\x1bOA"})
	press(keyCase{"Down (application)", 0, ui.KeyDown, "\x1bOB"})
	term.Feed([]byte("\x1b[?1l"))
	press(keyCase{"Up (normal)", 0, ui.KeyUp, "\x1b[A"})

	// Plain Ctrl+C interrupts even with a selection: copying is
	// Ctrl+Shift+C here (Cmd+C on macOS).
	tt.SetClipboard("")
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyA) // select all
	press(keyCase{"Ctrl+C with a selection", ui.Ctrl, ui.KeyC, "\x03"})
	if got := tt.Clipboard(); got != "" {
		t.Errorf("Ctrl+C copied %q", got)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyA)
	press(keyCase{"Ctrl+Shift+C", ui.Ctrl | ui.Shift, ui.KeyC, ""})
	if got := tt.Clipboard(); !strings.Contains(got, "copy me") {
		t.Errorf("Ctrl+Shift+C copied %q", got)
	}
	tt.SetClipboard("pasted")
	press(keyCase{"Ctrl+Shift+V", ui.Ctrl | ui.Shift, ui.KeyV, "pasted"})

	// The window's shortcuts take their keys, and send nothing.
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyT)
	if got := conn.sent(t, term); got != "" || !a.managerOpen {
		t.Errorf("Ctrl+Shift+T: sent %q, manager open %v", got, a.managerOpen)
	}
	a.managerOpen = false
	tt.Frame()
	tt.Key(ui.Super, ui.KeyT) // Cmd+T on macOS
	if got := conn.sent(t, term); got != "" || !a.managerOpen {
		t.Errorf("Super+T: sent %q, manager open %v", got, a.managerOpen)
	}
	a.managerOpen = false
	tt.Frame()
	focus()
	press(keyCase{"Ctrl+C after the manager", ui.Ctrl, ui.KeyC, "\x03"})

	tt.Key(ui.Ctrl, ui.KeyTab)
	if got := conn.sent(t, term); got != "" || a.current() != other {
		t.Errorf("Ctrl+Tab: sent %q, active %d", got, a.active)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	if got := conn.sent(t, term); got != "" || a.current() != s {
		t.Errorf("Ctrl+Shift+Tab: sent %q, active %d", got, a.active)
	}
	focus()
	press(keyCase{"Ctrl+C after switching back", ui.Ctrl, ui.KeyC, "\x03"})

	tt.Key(ui.Ctrl|ui.Shift, ui.KeyW)
	if len(a.sessions) != 1 || a.sessions[0] != other {
		t.Errorf("Ctrl+Shift+W left %d sessions", len(a.sessions))
	}
}

// TestSSHControlKeys types control keys into the terminal of a session
// to a real server (see TestSSH), through the window as a user does, and
// checks what the shell did with them.
func TestSSHControlKeys(t *testing.T) {
	a, p, s := connectTestServer(t)
	defer a.CloseAll()
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.ClickAt(760, 260) // in the terminal

	enter := func(text string) {
		tt.Type(text)
		tt.Key(0, ui.KeyEnter)
	}
	// shows waits for n lines of output that are exactly line.
	shows := func(line string, n int) {
		t.Helper()
		p.until(t, strconv.Quote(line), func() bool {
			count := 0
			for l := range strings.SplitSeq(s.term.Text(), "\n") {
				if strings.TrimRight(l, " ") == line {
					count++
				}
			}
			return count >= n
		})
	}
	// Arithmetic in the commands makes their output differ from their
	// echo.
	enter("unset HISTFILE; echo ready-$((1+1))")
	shows("ready-2", 1)

	// Ctrl+C interrupts a program.
	enter("echo sleeping-$((2+3)); sleep 30")
	shows("sleeping-5", 1)
	time.Sleep(300 * time.Millisecond) // for sleep to start
	start := time.Now()
	tt.Key(ui.Ctrl, ui.KeyC)
	enter("echo rc=$?")
	shows("rc=130", 1)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Ctrl+C took %v", d)
	}

	// Ctrl+D ends cat's input.
	enter("cat")
	enter("kitty-cat")
	shows("kitty-cat", 2) // the echo, and cat's copy
	tt.Key(ui.Ctrl, ui.KeyD)
	enter("echo cat-done-$((6*7))")
	shows("cat-done-42", 1)

	// Ctrl+Z stops a program.
	enter("sleep 31")
	time.Sleep(300 * time.Millisecond)
	tt.Key(ui.Ctrl, ui.KeyZ)
	enter("jobs -s; echo jobs-$((4+5))")
	shows("jobs-9", 1)
	if text := s.term.Text(); !strings.Contains(text, "Stopped") || !strings.Contains(text, "sleep 31") {
		t.Errorf("no stopped job:\n%s", text)
	}
	enter("kill -9 %1")

	// readline's editing keys.
	tt.Type("garbage-xyz")
	tt.Key(ui.Ctrl, ui.KeyU) // kill the line
	enter("echo u-$((1+1))")
	shows("u-2", 1)

	tt.Type("a-$((2+2))")
	tt.Key(ui.Ctrl, ui.KeyA) // to the start
	tt.Type("echo ")
	tt.Key(ui.Ctrl, ui.KeyE) // to the end
	enter("-end")
	shows("a-4-end", 1)

	tt.Type("echo w-$((3+3)) junk")
	tt.Key(ui.Ctrl, ui.KeyW) // kill a word back
	tt.Key(0, ui.KeyEnter)
	shows("w-6", 1)

	tt.Type("echo t-ab")
	tt.Key(ui.Ctrl, ui.KeyT) // transpose
	tt.Key(0, ui.KeyEnter)
	shows("t-ba", 1)

	tt.Type("echo m-$((1+8)) tail")
	tt.Key(ui.Alt, ui.KeyB)  // a word back
	tt.Key(ui.Ctrl, ui.KeyK) // kill to the end
	tt.Key(0, ui.KeyEnter)
	shows("m-9", 1)

	tt.Type("echo e-$((2+8)) tail")
	tt.Key(0, ui.KeyEscape) // Esc b: a word back, as Alt+B
	tt.Type("b")
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Key(0, ui.KeyEnter)
	shows("e-10", 1)

	tt.Type("ech")
	tt.Key(0, ui.KeyTab) // completes echo
	enter(" tab-$((4+4))")
	shows("tab-8", 1)

	enter("echo h-$((5+5))")
	shows("h-10", 1)
	tt.Key(0, ui.KeyUp) // the last command again
	tt.Key(0, ui.KeyEnter)
	shows("h-10", 2)

	enter("echo r-$((6+6))")
	shows("r-12", 1)
	enter("true")
	tt.Key(ui.Ctrl, ui.KeyR) // search the history
	tt.Type("r-$((6")
	tt.Key(0, ui.KeyEnter)
	shows("r-12", 2)

	// Ctrl+L clears the screen: the next command runs on its second row,
	// as the terminal answers the cursor's position.
	tt.Key(ui.Ctrl, ui.KeyL)
	enter(`IFS='[;' read -rsdR -p $'\e[6n' _ row _; echo row-$row`)
	shows("row-2", 1)
}
