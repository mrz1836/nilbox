package hooks

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain plays a fake go-pre-commit when the test binary was started as one, and
// otherwise fails the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	testutil.FakeToolMain()
	goleak.VerifyTestMain(m)
}
