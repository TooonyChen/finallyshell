package localterm

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo/plugins/terminal"
)

func TestMain(m *testing.M) {
	if handled, code := RunHelper(os.Args); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func connectedTestPipe(t *testing.T) (*pipe, *pipe) {
	t.Helper()
	s, name, err := newPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	done := make(chan error, 1)
	go func() { done <- s.connect() }()
	c, err := openPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connect timeout")
	}
	return s, c
}

func TestPipeCloseCancelsPendingRead(t *testing.T) {
	s, _ := connectedTestPipe(t)
	done := make(chan error, 1)
	go func() { var b [1]byte; _, err := s.Read(b[:]); done <- err }()
	s.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read succeeded after close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked read survived close")
	}
}

func TestPipeCloseCancelsPendingConnect(t *testing.T) {
	s, _, err := newPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() { done <- s.connect() }()
	s.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("connect succeeded after close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connect survived close")
	}
}

func TestPipePeerAndBrokenConnection(t *testing.T) {
	s, c := connectedTestPipe(t)
	server, err := c.peer(true)
	if err != nil || server != uint32(os.Getpid()) {
		t.Fatalf("server %d %v", server, err)
	}
	client, err := s.peer(false)
	if err != nil || client != uint32(os.Getpid()) {
		t.Fatalf("client %d %v", client, err)
	}
	c.Close()
	var b [1]byte
	if _, err := s.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("broken pipe: %v", err)
	}
}

func TestWaiterScriptContainsOnlyGeneratedIdentifiers(t *testing.T) {
	name := "finallyshell-terminal-" + strings.Repeat("a", 48)
	if !validPipeName(name) || validPipeName(name+";calc") || validPipeName(`\\remote\pipe\x`) {
		t.Fatal("invalid name validation")
	}
	b, err := base64.StdEncoding.DecodeString(waiterScript(name, 42))
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	s := string(utf16.Decode(u))
	for _, want := range []string{name, "$server -ne 42", "SetConsoleCtrlHandler", "$pipe.WriteByte(1)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "ExecutionPolicy") || strings.Contains(s, "Invoke-Expression") {
		t.Fatal("waiter weakens policy or executes received text")
	}
}

// Opt-in: requests real Windows UAC. TestMain gives the test binary the same
// helper dispatch as the application, before the test runner is initialized.
func TestElevatedConsoleIntegration(t *testing.T) {
	if os.Getenv("FINALLYSHELL_TEST_ADMIN") != "1" {
		t.Skip("set FINALLYSHELL_TEST_ADMIN=1 on Windows to approve UAC")
	}
	if err := terminal.Load(); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := []string{"powershell.exe", "-NoLogo", "-NoProfile", "-NoExit", "-Command", `$p=[Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent()); Write-Output ('ELEVATED='+$p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))`}
	proc, err := Start(ctx, command, home)
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	proc.Activate()
	term, err := terminal.New(terminal.Options{Command: proc.CommandLine()})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	waitText := func(want string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(term.Text(), want) {
				return
			}
			if err := proc.Err(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no %q: %q", want, term.Text())
	}
	waitText("ELEVATED=True")
	term.Resize(100, 30)
	term.Send([]byte("Write-Output ('COLS_'+[Console]::WindowWidth)\r"))
	waitText("COLS_100")
	term.Send([]byte("Start-Sleep 30\r"))
	waitText("Start-Sleep 30")
	term.Send([]byte{3})
	term.Send([]byte("Write-Output ('CONTROL_'+(6*7))\r"))
	waitText("CONTROL_42")
	term.Send([]byte("exit 7\r"))
	select {
	case <-term.Done():
		if term.ExitCode() != 7 {
			t.Fatalf("exit code %d; error %v", term.ExitCode(), proc.Err())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shell did not exit")
	}
}

func TestPipeClientAllowsIdentificationOnly(t *testing.T) {
	s, c := connectedTestPipe(t)
	done := make(chan error, 1)
	go func() { _, err := c.Write([]byte{1}); done <- err }()
	var b [1]byte
	if _, err := s.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fn := windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")
	if r, _, err := fn.Call(uintptr(s.h)); r == 0 {
		t.Fatal(err)
	}
	defer windows.RevertToSelf()
	var token windows.Token
	if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token); err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	var level, n uint32
	if err := windows.GetTokenInformation(token, windows.TokenImpersonationLevel, (*byte)(unsafe.Pointer(&level)), 4, &n); err != nil {
		t.Fatal(err)
	}
	if level != windows.SecurityIdentification {
		t.Fatalf("client granted impersonation level %d", level)
	}
}
