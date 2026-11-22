package monitor

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a test leaves a goroutine behind: the
// monitor runs one goroutine per session, and a leak there would show up
// only as memory that keeps growing on a long-running app.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
