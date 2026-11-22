package sshx

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a test leaves a goroutine behind. The
// connection, forward and agent loops here are the ones that would keep a
// closed session alive in the app, and nothing else in CI looks for them.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
