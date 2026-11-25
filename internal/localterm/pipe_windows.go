package localterm

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pipe uses overlapped I/O so canceling UAC, closing a tab, or a vanished peer
// can cancel a blocked read/connect. The handle stays alive until I/O completes.
type pipe struct {
	mu              sync.Mutex
	readMu, writeMu sync.Mutex
	h               windows.Handle
	closed          bool
	active          sync.WaitGroup
}

func newPipe() (*pipe, string, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("finallyshell-terminal-%x", nonce)
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return nil, "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, "", err
	}
	// Permit this user, SYSTEM and administrators (including UAC with another
	// account), with no inherited or network access. Both peers also verify PID.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, "", err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	n, err := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	if err != nil {
		return nil, "", err
	}
	h, err := windows.CreateNamedPipe(n, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, &sa)
	if err != nil {
		return nil, "", err
	}
	return &pipe{h: h}, name, nil
}

func openPipe(name string) (*pipe, error) {
	n, err := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(n, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
	if err != nil {
		return nil, err
	}
	return &pipe{h: h}, nil
}

func (p *pipe) operate(fn func(windows.Handle, *windows.Overlapped, *uint32) error) (int, error) {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	o := windows.Overlapped{HEvent: event}
	var n uint32
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	h := p.h
	p.active.Add(1)
	err = fn(h, &o, &n)
	p.mu.Unlock()
	defer p.active.Done()
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		err = windows.GetOverlappedResult(h, &o, &n, true)
	}
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) {
		err = io.EOF
	}
	return int(n), err
}

func (p *pipe) connect() error {
	_, err := p.operate(func(h windows.Handle, o *windows.Overlapped, _ *uint32) error {
		err := windows.ConnectNamedPipe(h, o)
		if errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			return nil
		}
		return err
	})
	return err
}

func (p *pipe) Read(b []byte) (int, error) {
	p.readMu.Lock()
	defer p.readMu.Unlock()
	return p.operate(func(h windows.Handle, o *windows.Overlapped, n *uint32) error { return windows.ReadFile(h, b, n, o) })
}

func (p *pipe) Write(b []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.operate(func(h windows.Handle, o *windows.Overlapped, n *uint32) error { return windows.WriteFile(h, b, n, o) })
}

func (p *pipe) peer(server bool) (uint32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	var pid uint32
	var err error
	if server {
		err = windows.GetNamedPipeServerProcessId(p.h, &pid)
	} else {
		err = windows.GetNamedPipeClientProcessId(p.h, &pid)
	}
	return pid, err
}

func (p *pipe) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	h := p.h
	windows.CancelIoEx(h, nil)
	p.mu.Unlock()
	go func() { p.active.Wait(); windows.CloseHandle(h) }()
	return nil
}
