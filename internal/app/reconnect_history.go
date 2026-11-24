package app

import (
	"context"
	"io"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
)

// paneConn holds back server output until the terminal's previous history
// is restored and the pane is installed. Closing it also releases a reader
// still waiting for installation, as when a tab closes while connecting.
type paneConn struct {
	io.ReadWriteCloser
	ready chan struct{}
	once  sync.Once
}

func newPaneConn(conn io.ReadWriteCloser) *paneConn {
	return &paneConn{ReadWriteCloser: conn, ready: make(chan struct{})}
}

func (c *paneConn) start() { c.once.Do(func() { close(c.ready) }) }
func (c *paneConn) Read(p []byte) (int, error) {
	<-c.ready
	return c.ReadWriteCloser.Read(p)
}
func (c *paneConn) Close() error {
	c.start()
	return c.ReadWriteCloser.Close()
}
func (c *paneConn) Resize(cols, rows int) error {
	if conn, ok := c.ReadWriteCloser.(interface{ Resize(int, int) error }); ok {
		return conn.Resize(cols, rows)
	}
	return nil
}

// restorePaneHistory transfers rendered output, never shell input or old
// terminal modes. It runs off the UI thread after the old reader ends, so
// the last output is included and a failed connection keeps its display.
func restorePaneHistory(ctx context.Context, term, previous *terminal.Terminal) error {
	if previous == nil {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-previous.Done():
	}
	var history []byte
	var cols, rows int
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		cols, rows = previous.Size()
		history = previous.History()
		if len(history) == 0 {
			// An ended full-screen program may have left the alternate
			// screen active. Recover the main shell's scrollback.
			previous.Feed([]byte("\x1b[?1049l\x1b[?1047l\x1b[?47l"))
			history = previous.History()
		}
		if width, _ := previous.Size(); width == cols {
			break
		}
		// The retained view can resize while reconnecting. Export again
		// if its width changed, so soft wrapping uses the matching width.
	}
	if cols > 0 && rows > 0 {
		term.Resize(cols, rows)
	}
	term.Feed(history)
	return ctx.Err()
}
