// Package localterm starts a Windows administrator shell in the application's
// existing ConPTY. Only the helper and shell are elevated, never the UI.
package localterm

import (
	"context"
	"errors"
	"sync"
)

const HelperFlag = "--finallyshell-elevated-terminal"

var (
	ErrUnsupported = errors.New("administrator terminals require Windows")
	ErrCanceled    = errors.New("administrator authorization canceled")
)

// Process supplies the ordinary console waiter to run in MyGo's local PTY.
// The elevated helper attaches to that console and starts the configured shell.
// Close ends the helper's control connection, which terminates its job.
type Process interface {
	CommandLine() []string
	Activate()
	Close() error
	Err() error
}

type process struct {
	Command      []string
	mu           sync.Mutex
	err          error
	closeOnce    sync.Once
	closeFn      func()
	activateOnce sync.Once
	activateFn   func()
}

func (p *process) Close() error {
	p.closeOnce.Do(func() {
		if p.closeFn != nil {
			p.closeFn()
		}
	})
	return nil
}

func (p *process) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *process) setError(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
}

// Starter is replaceable in UI tests; startup may wait for Windows UAC.
type Starter func(context.Context, []string, string) (Process, error)

// Activate starts the bounded console-attachment wait when the terminal is built.
func (p *process) Activate() {
	p.activateOnce.Do(func() {
		if p.activateFn != nil {
			p.activateFn()
		}
	})
}

func (p *process) CommandLine() []string { return append([]string(nil), p.Command...) }
