//go:build mage

package main

import (
	"os"
	"testing"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain plays a fake tool when this test binary was started as one
// (testutil.FakeTool), and runs the tests otherwise.
func TestMain(m *testing.M) {
	testutil.FakeToolMain()

	os.Exit(m.Run())
}
