package autopilot

import "github.com/mrz1836/nilbox/internal/autopilot/draft"

// NameKey is draft.NameKey, the key a routine's name is stored under, for the tests that
// write a routine's row by hand.
var NameKey = draft.NameKey //nolint:gochecknoglobals // the tests' view of the draft leaf's function
