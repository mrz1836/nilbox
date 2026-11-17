package catalog

// ScheduleStyle is how a routine On a schedule tells of the items it matches at its time:
// each match on its own, or once, with the list. Either way its steps run on each item it
// matches, and the ledger records each match apart.
type ScheduleStyle string

// The styles of a schedule.
const (
	// ScheduleEach tells of each match on its own: each item's Notify me is its own
	// notice, gathered into one banner past three as any burst is. The default.
	ScheduleEach ScheduleStyle = "each"

	// ScheduleDigest tells of the matches once, with the list: the Notify me of every item
	// that matches at one time is one notice, a digest that counts them by where each
	// stands, "5 pull requests need you: 3 failing, 2 conflicting", and lists them.
	ScheduleDigest ScheduleStyle = "digest"
)

// ScheduleStyleInfo describes a style of schedule for the builder.
type ScheduleStyleInfo struct {
	ID    ScheduleStyle `json:"id"`
	Label string        `json:"label"`
	Help  string        `json:"help"`
}

// ScheduleStyles returns the styles of a schedule, the default first.
func ScheduleStyles() []ScheduleStyleInfo {
	return []ScheduleStyleInfo{
		{
			ID: ScheduleEach, Label: "For each match",
			Help: "Runs its steps on each item that matches at the time, and tells you of each on its own.",
		},
		{
			ID: ScheduleDigest, Label: "Once, with the list",
			Help: "Runs its steps on each item that matches at the time, and tells you once, with one digest that " +
				"lists them: “5 pull requests need you: 3 failing, 2 conflicting”. Notify me comes first.",
		},
	}
}

// ScheduleStyleByID returns the style id names; "" is ScheduleEach.
func ScheduleStyleByID(id ScheduleStyle) (ScheduleStyleInfo, bool) {
	if id == "" {
		id = ScheduleEach
	}

	for _, s := range ScheduleStyles() {
		if s.ID == id {
			return s, true
		}
	}

	return ScheduleStyleInfo{}, false
}
