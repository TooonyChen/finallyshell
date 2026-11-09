package monitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"finallyshell/internal/sshtest"
)

// TestRunScriptHangs keeps polling when the stats script never ends, as
// on a server where df hangs on a stale mount: each read gives up after
// its timeout and the next one starts.
func TestRunScriptHangs(t *testing.T) {
	srv := sshtest.Start(t)
	srv.HangExec()
	client := srv.Dial(t)
	ctx, cancel := context.WithCancel(context.Background())
	var published atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, client, 10*time.Millisecond, 50*time.Millisecond, func(Stats) { published.Add(1) })
	}()
	deadline := time.Now().Add(3 * time.Second)
	for published.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d stats in 3 s with the script hanging", published.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not end with its context")
	}
}
