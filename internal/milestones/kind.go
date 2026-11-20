package milestones

// Kind is what an achievement is for. All its constants are declared in this file, so
// magex app:bindings writes them as one TypeScript union in a stable order.
type Kind string

// The achievements' kinds, in the order Moment.Unlocked lists them, the most significant
// first.
const (
	// KindStreak is a streak of days running at nil reaching a tier: 3, 7, 14, and 30
	// days, then every 30.
	KindStreak Kind = "streak"

	// KindCount is the inbox emptied while the user watched for the Nth time, N a tier:
	// the 10th, 25th, 50th, and 100th, then every 100th.
	KindCount Kind = "count"

	// KindFirst is the first inbox the user ever emptied while they watched.
	KindFirst Kind = "first"

	// KindSweep is SweepAt or more items cleared in one sitting, on the way to nil.
	KindSweep Kind = "sweep"

	// KindEarly is an inbox emptied in the morning, from 05:00 to noon.
	KindEarly Kind = "early"

	// KindFriday is an inbox emptied on a Friday.
	KindFriday Kind = "friday"

	// KindWeekend is an inbox emptied on a Saturday or a Sunday.
	KindWeekend Kind = "weekend"

	// KindOwl is an inbox emptied late at night, from 22:00 to 05:00.
	KindOwl Kind = "owl"
)
