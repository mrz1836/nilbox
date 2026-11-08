package desktop_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
