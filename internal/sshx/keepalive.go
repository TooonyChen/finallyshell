package sshx

import (
	"context"
	"time"

	"golang.org/x/crypto/ssh"
)

// KeepAliveConfig is how a connection is kept alive while it is quiet,
// as OpenSSH's ServerAliveInterval and ServerAliveCountMax do. The zero
// value sends none.
type KeepAliveConfig struct {
	// Interval is the time between keepalives; 0 sends none.
	Interval time.Duration
	// Timeout is how long each one waits for its answer before it counts
	// as missed: Interval when 0.
	Timeout time.Duration
	// MaxMissed is how many missed in a row close the connection: 3 when
	// 0.
	MaxMissed int
}

// KeepAlive sends a keepalive@openssh.com request on client every
// interval until ctx ends or client closes. Any answer, success or
// failure, says the server is there. After MaxMissed requests in a row go
// unanswered for Timeout each, it closes client, which ends its sessions
// and channels as a dropped network would, so that a connection that
// died without a word is noticed.
//
// The traffic also keeps NAT routers, firewalls, proxies and jump hosts
// from dropping a connection that carries nothing for minutes, as one
// whose program prints nothing does.
func KeepAlive(ctx context.Context, client *ssh.Client, cfg KeepAliveConfig) {
	if cfg.Interval <= 0 {
		return
	}
	timeout, maxMissed := cfg.Timeout, cfg.MaxMissed
	if timeout <= 0 {
		timeout = cfg.Interval
	}
	if maxMissed <= 0 {
		maxMissed = 3
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		client.Wait() // the connection closed, by anyone
		cancel()
	}()

	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	// pending is the answer of the request sent last, while it has not
	// come: requests wait for their answers one at a time, so a new one
	// would only queue behind it.
	var pending chan error
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if pending == nil {
			pending = make(chan error, 1)
			go func(answer chan<- error) {
				_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
				answer <- err
			}(pending)
		}
		wait := time.NewTimer(timeout)
		select {
		case <-ctx.Done():
			wait.Stop()
			return
		case err := <-pending:
			wait.Stop()
			if err != nil { // closed
				return
			}
			pending, missed = nil, 0
		case <-wait.C:
			if missed++; missed >= maxMissed {
				client.Close()
				return
			}
		}
	}
}
