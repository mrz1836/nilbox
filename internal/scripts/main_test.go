package scripts

import (
	"os"
	"testing"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain plays a fake tool when this test binary was started as one, as magex or a
// shell a run starts.
func TestMain(m *testing.M) {
	testutil.FakeToolMain()

	os.Exit(m.Run())
}
