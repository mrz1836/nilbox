package measure

import (
	"os"
	"testing"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain plays a fake desktop binary when the test binary was started as one.
func TestMain(m *testing.M) {
	testutil.FakeToolMain()
	os.Exit(m.Run())
}
