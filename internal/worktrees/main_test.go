package worktrees

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain plays a fake tool when this test binary was started as one, as a program a
// planted clone config names, and otherwise fails the run when a test leaves a goroutine
// behind.
func TestMain(m *testing.M) {
	testutil.FakeToolMain()

	goleak.VerifyTestMain(m)
}
