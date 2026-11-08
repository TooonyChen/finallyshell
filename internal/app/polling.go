package app

import (
	"context"
	"time"

	"golang.org/x/crypto/ssh"
)

// sessionPoll reads something of a session's server again and again,
// over its connection, while a view shows it: the Processes tab's list,
// and the next tabs that show the server as it changes. The view calls
// a.keepPolling in each frame it draws; the poll starts with the first,
// starts again on a new connection, and stops in the first frame that
// does not draw it (stopUnseenPolls), as the user goes to another tab or
// session, or when the session disconnects or its host allows no exec
// channels. One read runs at a time, the next every interval after the
// last ended, so a slow server is not asked again before it answered.
//
// Its zero value is stopped.
type sessionPoll struct {
	run *pollRun // while it polls
}

// pollRun is one start of a poll, on one connection.
type pollRun struct {
	client *ssh.Client
	cancel context.CancelFunc
	wake   chan struct{} // reads at once, as after a change the user made
	seen   bool          // drawn since the last stopUnseenPolls
}

// pollRead reads once, on the poll's goroutine, and returns what to do
// with what it read on the UI thread; that is dropped when the poll has
// stopped or started again meanwhile.
type pollRead func(ctx context.Context, client *ssh.Client) (apply func())

// pollSet is the polls that run, which stopUnseenPolls goes through.
type pollSet map[*sessionPoll]bool

// keepPolling polls with read every interval while s is connected and
// its host allows exec channels, starting p if it does not run, or runs
// on an older connection. newRead makes the read of each start, which
// may keep what it read between reads (as the CPU times for their
// deltas): a start has its own, as the one before may still be reading.
// It returns whether p polls.
func (a *App) keepPolling(p *sessionPoll, s *Session, every time.Duration, newRead func() pollRead) bool {
	if s.status != statusConnected || s.Host == nil || !execAllowed(s.Host) {
		a.stopPoll(p)
		return false
	}
	if p.run != nil && p.run.client != s.client {
		a.stopPoll(p)
	}
	if p.run == nil {
		a.startPoll(p, s.client, every, newRead())
	}
	p.run.seen = true
	return true
}

func (a *App) startPoll(p *sessionPoll, client *ssh.Client, every time.Duration, read pollRead) {
	ctx, cancel := context.WithCancel(context.Background())
	run := &pollRun{client: client, cancel: cancel, wake: make(chan struct{}, 1)}
	p.run = run
	if a.polls == nil {
		a.polls = pollSet{}
	}
	a.polls[p] = true
	go func() {
		for {
			apply := read(ctx, client)
			if ctx.Err() != nil {
				return
			}
			a.post(func() {
				if p.run == run && apply != nil {
					apply()
				}
			})
			select {
			case <-ctx.Done():
				return
			case <-run.wake:
			case <-time.After(every):
			}
		}
	}()
}

// pollNow makes p read at once, rather than at the end of its interval,
// when it runs.
func (p *sessionPoll) pollNow() {
	if p.run != nil {
		select {
		case p.run.wake <- struct{}{}:
		default:
		}
	}
}

func (a *App) stopPoll(p *sessionPoll) {
	if p.run != nil {
		p.run.cancel()
		p.run = nil
	}
	delete(a.polls, p)
}

// stopUnseenPolls stops the polls no view drew since it last ran: the
// main window's View runs it after each frame.
func (a *App) stopUnseenPolls() {
	for p := range a.polls {
		if p.run == nil || !p.run.seen {
			a.stopPoll(p)
			continue
		}
		p.run.seen = false
	}
}
