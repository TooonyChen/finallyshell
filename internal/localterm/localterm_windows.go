package localterm

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shellExecuteEx    = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	attachConsole     = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	freeConsole       = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")
	helperCtrlHandler = syscall.NewCallback(func(kind uint32) uintptr {
		if kind == 0 || kind == 1 {
			return 1
		}
		return 0
	})
	setConsoleCtrlHandler = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")
)

func Available() bool { return true }

func Elevated() (bool, error) { return processElevated(windows.CurrentProcess()) }

func processElevated(h windows.Handle) (bool, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); err != nil {
		return false, err
	}
	defer token.Close()
	var value, n uint32
	err := windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&value)), 4, &n)
	if err != nil {
		return false, err
	}
	if n != 4 {
		return false, errors.New("invalid process elevation information")
	}
	return value != 0, nil
}

type shellExecuteInfo struct {
	Size, Mask                        uint32
	Window                            windows.Handle
	Verb, File, Parameters, Directory *uint16
	Show                              int32
	Instance                          windows.Handle
	IDList                            unsafe.Pointer
	Class                             *uint16
	ClassKey                          windows.Handle
	HotKey                            uint32
	Icon, Process                     windows.Handle
}

func elevate(exe, name string, parent uint32) (windows.Handle, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, syscall.Errno(1)) {
		return 0, err
	}
	defer windows.CoUninitialize()
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	args, _ := windows.UTF16PtrFromString(HelperFlag + " " + name + " " + strconv.FormatUint(uint64(parent), 10))
	info := shellExecuteInfo{Mask: 0x40 | 0x100 | 0x400, Verb: verb, File: file, Parameters: args}
	info.Size = uint32(unsafe.Sizeof(info))
	r, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, ErrCanceled
		}
		return 0, fmt.Errorf("requesting administrator permission: %w", callErr)
	}
	if info.Process == 0 {
		return 0, errors.New("no administrator helper process")
	}
	return info.Process, nil
}

// Start asks UAC before opening a tab. A short PowerShell waiter then supplies
// the existing ConPTY's console: it does not execute the user's commands or
// read terminal input. The elevated helper attaches and starts the real shell.
func Start(ctx context.Context, args []string, dir string) (Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args, err := shellCommand(args)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	waiter := filepath.Join(sys, "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(waiter); err != nil {
		return nil, fmt.Errorf("Windows PowerShell console waiter: %w", err)
	}
	control, name, err := newPipe()
	if err != nil {
		return nil, err
	}
	startup, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stop := context.AfterFunc(startup, func() { control.Close() })
	defer stop()
	connected := make(chan error, 1)
	go func() { connected <- control.connect() }()
	h, err := elevate(exe, name, windows.GetCurrentProcessId())
	if err != nil {
		control.Close()
		return nil, err
	}
	defer windows.CloseHandle(h)
	if err := startup.Err(); err != nil {
		control.Close()
		return nil, err
	}
	select {
	case err = <-connected:
	case <-startup.Done():
		err = startup.Err()
	}
	if err != nil {
		control.Close()
		return nil, err
	}
	helperPID, err := windows.GetProcessId(h)
	if err == nil {
		var pid uint32
		pid, err = control.peer(false)
		if err == nil && pid != helperPID {
			err = errors.New("unexpected administrator helper")
		}
	}
	if err != nil {
		control.Close()
		return nil, err
	}
	c := &channel{ReadWriteCloser: control}
	ready, err := c.receive()
	if err != nil || ready.Kind != "authorized" {
		control.Close()
		if err == nil {
			err = errors.New("administrator helper was not authorized")
		}
		return nil, err
	}
	relay, relayName, err := newPipe()
	if err != nil {
		control.Close()
		return nil, err
	}
	proc := &process{}
	proc.closeFn = func() { control.Close(); relay.Close() }
	proc.Command = []string{waiter, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", waiterScript(relayName, windows.GetCurrentProcessId())}
	// Stop watching the startup context before returning ownership to the UI.
	if !stop() || startup.Err() != nil {
		proc.Close()
		return nil, startup.Err()
	}
	proc.activateFn = func() { go proc.serve(control, c, relay, waiter, message{Kind: "start", Args: args, Dir: dir}) }
	return proc, nil
}

func (p *process) serve(control *pipe, c *channel, relay *pipe, waiter string, start message) {
	defer p.Close()
	// A terminal never shown, or one which cannot start its console waiter,
	// must not leave the UAC-authorized helper waiting forever.
	timer := time.AfterFunc(30*time.Second, func() { relay.Close(); control.Close() })
	defer timer.Stop()
	if err := relay.connect(); err != nil {
		p.setError(err)
		return
	}
	pid, err := relay.peer(false)
	if err == nil {
		err = verifyProcess(pid, waiter, windows.GetCurrentProcessId())
	}
	if err != nil {
		p.setError(err)
		return
	}
	start.ConsolePID = pid
	if err := c.send(start); err != nil {
		p.setError(err)
		return
	}
	m, err := c.receive()
	if err != nil {
		p.setError(err)
		return
	}
	if m.Kind == "started" {
		timer.Stop()
		m, err = c.receive()
	}
	if err != nil {
		p.setError(err)
		return
	}
	if m.Kind == "error" {
		p.setError(errors.New(m.Error))
		// Only display trusted helper errors, never PowerShell source text.
		text := strings.Map(func(r rune) rune {
			if r < 32 {
				return ' '
			}
			return r
		}, m.Error)
		writeAll(relay, []byte("ERROR "+text+"\n"))
	} else if m.Kind == "exit" {
		writeAll(relay, []byte("EXIT "+strconv.FormatUint(uint64(m.Code), 10)+"\n"))
	} else {
		p.setError(errors.New("unexpected administrator terminal response"))
		return
	}
	ackTimer := time.AfterFunc(5*time.Second, func() { relay.Close() })
	defer ackTimer.Stop()
	var ack [1]byte
	relay.Read(ack[:])
}

func shellCommand(args []string) ([]string, error) {
	args = append([]string(nil), args...)
	if len(args) == 0 {
		for _, name := range []string{"pwsh.exe", "powershell.exe", os.Getenv("COMSPEC"), "cmd.exe"} {
			if name == "" {
				continue
			}
			if path, err := exec.LookPath(name); err == nil {
				return []string{path}, nil
			}
		}
		return nil, errors.New("no local shell found")
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return nil, err
	}
	args[0], err = filepath.Abs(path)
	return args, err
}

func waiterScript(name string, parent uint32) string {
	// Names are generated locally from random bytes; no user command, argument,
	// directory or credential is interpolated into this script.
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class FinallyshellConsole { [DllImport("kernel32.dll")] public static extern bool SetConsoleCtrlHandler(IntPtr handler, bool ignore); [DllImport("kernel32.dll")] public static extern bool GetNamedPipeServerProcessId(IntPtr pipe, out uint pid); }'
if (-not [FinallyshellConsole]::SetConsoleCtrlHandler([IntPtr]::Zero,$true)) { throw 'Cannot protect console waiter from Ctrl+C' }
$pipe=[System.IO.Pipes.NamedPipeClientStream]::new('.', '%s', [System.IO.Pipes.PipeDirection]::InOut)
try {
 $pipe.Connect(30000)
 [uint32]$server=0
 if (-not [FinallyshellConsole]::GetNamedPipeServerProcessId($pipe.SafePipeHandle.DangerousGetHandle(),[ref]$server) -or $server -ne %d) { throw 'Unexpected terminal controller' }
 $reader=[System.IO.StreamReader]::new($pipe,[System.Text.Encoding]::UTF8)
 $line=$reader.ReadLine()
 if ($null -eq $line) { throw 'Terminal controller disconnected' }
 if ($line.StartsWith('EXIT ')) { $pipe.WriteByte(1); $pipe.Flush(); exit [BitConverter]::ToInt32([BitConverter]::GetBytes([uint32]::Parse($line.Substring(5))),0) }
 if ($line.StartsWith('ERROR ')) { [Console]::Error.WriteLine($line.Substring(6)); $pipe.WriteByte(1); $pipe.Flush(); exit 1 }
 throw 'Invalid terminal controller response'
} catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 } finally { $pipe.Dispose() }
`, name, parent)
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, r := range u {
		binary.LittleEndian.PutUint16(b[i*2:], r)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// RunHelper dispatches before UI initialization and the single-instance lock.
// The mode refuses to run without a genuinely elevated token and its original
// controller's process identity. Its pipe never accepts arbitrary reconnects.
func RunHelper(args []string) (bool, int) {
	if len(args) < 2 || args[1] != HelperFlag {
		return false, 0
	}
	if len(args) != 4 || !validPipeName(args[2]) {
		return true, 1
	}
	pid, err := strconv.ParseUint(args[3], 10, 32)
	if err != nil || pid == 0 {
		return true, 1
	}
	if elevated, err := Elevated(); err != nil || !elevated {
		return true, 1
	}
	exe, err := os.Executable()
	if err != nil || verifyProcess(uint32(pid), exe, 0) != nil {
		return true, 1
	}
	p, err := openPipe(args[2])
	if err != nil {
		return true, 1
	}
	defer p.Close()
	server, err := p.peer(true)
	if err != nil || server != uint32(pid) {
		return true, 1
	}
	timer := time.AfterFunc(2*time.Minute, func() { p.Close() })
	defer timer.Stop()
	c := &channel{ReadWriteCloser: p}
	if err := c.send(message{Kind: "authorized"}); err != nil {
		return true, 1
	}
	timer.Stop()
	m, err := c.receive()
	if err != nil || m.Kind != "start" || m.ConsolePID == 0 || len(m.Args) == 0 {
		return true, 1
	}
	timer.Stop()
	if err := runShell(p, c, m, uint32(pid)); err != nil {
		c.send(message{Kind: "error", Error: err.Error()})
		return true, 1
	}
	return true, 0
}

func validPipeName(name string) bool {
	const prefix = "finallyshell-terminal-"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+48 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(name, prefix))
	return err == nil
}

func verifyProcess(pid uint32, path string, parent uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(windows.UTF16ToString(buf[:n])), filepath.Clean(path)) {
		return errors.New("unexpected terminal process image")
	}
	if parent == 0 {
		return nil
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snapshot, &e); err == nil; err = windows.Process32Next(snapshot, &e) {
		if e.ProcessID == pid {
			if e.ParentProcessID != parent {
				return errors.New("unexpected terminal process parent")
			}
			return nil
		}
	}
	return errors.New("terminal process disappeared")
}

func runShell(p *pipe, c *channel, m message, parent uint32) error {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	waiter := filepath.Join(sys, "WindowsPowerShell", "v1.0", "powershell.exe")
	if err := verifyProcess(m.ConsolePID, waiter, parent); err != nil {
		return err
	}
	freeConsole.Call()
	if r, _, err := attachConsole.Call(uintptr(m.ConsolePID)); r == 0 {
		return fmt.Errorf("attaching administrator shell to console: %w", err)
	}
	defer freeConsole.Call()
	// The waiter ignores Ctrl+C, but the real shell must receive it normally.
	if r, _, err := setConsoleCtrlHandler.Call(0, 0); r == 0 {
		return err
	}
	if r, _, err := setConsoleCtrlHandler.Call(helperCtrlHandler, 1); r == 0 {
		return err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	cmd := windows.ComposeCommandLine(m.Args)
	cmdPtr, err := windows.UTF16PtrFromString(cmd)
	if err != nil {
		return err
	}
	filePtr, err := windows.UTF16PtrFromString(m.Args[0])
	if err != nil {
		return err
	}
	dirPtr, err := windows.UTF16PtrFromString(m.Dir)
	if err != nil {
		return err
	}
	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(filePtr, cmdPtr, nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_UNICODE_ENVIRONMENT, nil, dirPtr, &si, &pi); err != nil {
		return fmt.Errorf("starting administrator shell: %w", err)
	}
	defer windows.CloseHandle(pi.Process)
	defer windows.CloseHandle(pi.Thread)
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		return err
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		return err
	}
	if err := c.send(message{Kind: "started"}); err != nil {
		return err
	}
	// Broken control pipes terminate the job, including descendants. Closing
	// or crashing the ordinary UI/waiter cannot leave an administrator shell.
	done := make(chan struct{})
	go func() { var b [1]byte; p.Read(b[:]); close(done) }()
	shellDone := make(chan error, 1)
	go func() { _, err := windows.WaitForSingleObject(pi.Process, windows.INFINITE); shellDone <- err }()
	select {
	case <-done:
		windows.TerminateJobObject(job, 1)
		<-shellDone
		return nil
	case err := <-shellDone:
		if err != nil {
			return err
		}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(pi.Process, &code); err != nil {
		return err
	}
	if err := c.send(message{Kind: "exit", Code: code}); err != nil {
		return err
	}
	<-done // Keep the response alive until the controller closes its pipe.
	return nil
}

var _ io.ReadWriteCloser = (*pipe)(nil)
