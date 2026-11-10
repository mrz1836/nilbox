package integrations

import (
	"encoding/json"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/paths"
)

// State is what the settings card says about a tool.
type State string

// The states.
const (
	// StateFound is a copy that runs and is new enough.
	StateFound State = "found"

	// StateUpdate is a copy that runs, is new enough, and has a newer release.
	StateUpdate State = "update"

	// StateTooOld is a copy older than the oldest version the app drives.
	StateTooOld State = "tooOld"

	// StateWontRun is a copy that is there but didn't run or print its version.
	StateWontRun State = "wontRun"

	// StateMissing is no copy at all.
	StateMissing State = "missing"
)

// Tools is what the settings show about every tool.
type Tools struct {
	Tools []Tool `json:"tools"`

	// ShellChecked reports that a read of the user's login shell's environment has ended,
	// and ShellRead that one succeeded; when it failed, the tools are looked for with a
	// basic PATH, and the settings say so.
	ShellChecked bool `json:"shellChecked"`
	ShellRead    bool `json:"shellRead"`
}

// Tool is what the settings show about one tool.
type Tool struct {
	ID          integrationprefs.ID `json:"id"`
	Name        string              `json:"name"`
	Binary      string              `json:"binary"`
	Purpose     string              `json:"purpose"`
	Repo        string              `json:"repo"`
	ReleasesURL string              `json:"releasesUrl"`
	MinVersion  string              `json:"minVersion"`

	// Switch is how the user set it, and On whether it is on: what the user chose, or,
	// while automatic, whether it can be used (see Effective).
	Switch integrationprefs.Switch `json:"switch"`
	On     bool                    `json:"on"`

	// State is what the card says, and Checking reports a state from the last run while
	// the tool is looked for again.
	State    State `json:"state"`
	Checking bool  `json:"checking"`

	// Copy is the copy in use, zero when it is missing, and Others every other copy found.
	Copy   Copy   `json:"copy"`
	Others []Copy `json:"others"`

	// Chosen is the copy the user chose, with ~, or "" for the one their shell finds;
	// ChosenMissing reports one that is no longer there.
	Chosen        string `json:"chosen"`
	ChosenMissing bool   `json:"chosenMissing"`

	// Latest is the tool's latest release, zero until it was checked.
	Latest Latest `json:"latest"`

	// CheckedAt is when the tool was last looked for, or "" when it never was.
	CheckedAt string `json:"checkedAt"`
}

// Latest is a tool's latest release, as the daily check last found it.
type Latest struct {
	// Version is its version, without a leading v, or "" when the tool has no release.
	Version string `json:"version"`
	URL     string `json:"url"`

	// CheckedAt is when it was checked.
	CheckedAt string `json:"checkedAt"`
}

// Usable reports a state in which the tool can be used.
func (s State) Usable() bool {
	return s == StateFound || s == StateUpdate
}

// Effective reports whether an integration is on: on and off are what the user chose, and
// automatic is on while the tool can be used.
func Effective(sw integrationprefs.Switch, usable bool) bool {
	switch sw {
	case integrationprefs.SwitchOn:
		return true
	case integrationprefs.SwitchOff:
		return false
	case integrationprefs.SwitchAuto:
		return usable
	default:
		return usable
	}
}

// ScriptsOn reports whether the scripts are on: as the user chose, or, while automatic,
// while either tool is on.
func ScriptsOn(sw integrationprefs.Switch, mageXOn, goPreCommitOn bool) bool {
	return Effective(sw, mageXOn || goPreCommitOn)
}

// stateOf works out a tool's state from what was found and its latest release.
func stateOf(def Definition, d detection, latest latestRecord) State {
	switch {
	case !d.has:
		return StateMissing
	case !d.selected.Runs:
		return StateWontRun
	case d.selected.Version.OlderThan(def.MinVersion):
		return StateTooOld
	case latest.Version != "" && d.selected.Version.OlderThan(latest.Version):
		return StateUpdate
	default:
		return StateFound
	}
}

// tool composes what the settings show about one tool.
func tool(def Definition, cfg integrationprefs.Config, d detection, latest latestRecord, checking bool,
	checkedAt time.Time, home string,
) Tool {
	state := stateOf(def, d, latest)
	sw := cfg.Switches[def.ID]
	chosen := cfg.Paths[def.ID]

	// A copy chosen since the detection ran is being looked for: until it is, the card
	// says it is checking, and nothing about the copy chosen before.
	if chosen != d.chosenPath {
		checking = true
		d.chosenMissing = false
	}

	out := Tool{
		ID: def.ID, Name: def.Name, Binary: def.Binary, Purpose: def.Purpose, Repo: def.Repo.String(),
		ReleasesURL: "https://github.com/" + def.Repo.String() + "/releases", MinVersion: def.MinVersion,
		Switch: sw, On: Effective(sw, state.Usable()), State: state, Checking: checking,
		Others: []Copy{}, ChosenMissing: d.chosenMissing,
		Latest:    Latest{Version: latest.Version, URL: latest.URL, CheckedAt: stamp(latest.CheckedAt)},
		CheckedAt: stamp(checkedAt),
	}

	if d.has {
		out.Copy = d.selected
	}

	if len(d.others) > 0 {
		out.Others = d.others
	}

	if chosen != "" {
		out.Chosen = paths.Tilde(chosen, home)
	}

	return out
}

// stamp writes t for the window, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}

// seenKey is the preference key holding a tool's last detection, and latestKey its latest
// release. The integrations package writes both.
func seenKey(id integrationprefs.ID) string { return "tools." + string(id) + ".seen" }

func latestKey(id integrationprefs.ID) string { return "tools." + string(id) + ".latest" }

// seenRecord is a tool's last detection as it is kept between runs, so the settings show
// it at once on the next launch.
type seenRecord struct {
	Has           bool      `json:"has"`
	Copy          Copy      `json:"copy"`
	Others        []Copy    `json:"others"`
	ChosenPath    string    `json:"chosenPath"`
	Chosen        bool      `json:"chosen"`
	ChosenMissing bool      `json:"chosenMissing"`
	Stat          fileStat  `json:"stat"`
	CheckedAt     time.Time `json:"checkedAt"`
}

// recordOf keeps a detection.
func recordOf(d detection, at time.Time) seenRecord {
	return seenRecord{
		Has: d.has, Copy: d.selected, Others: d.others, ChosenPath: d.chosenPath, Chosen: d.chosen,
		ChosenMissing: d.chosenMissing, Stat: d.stat,
		CheckedAt: at.UTC(),
	}
}

// detection reads a kept detection back.
func (r seenRecord) detection() detection {
	return detection{
		selected: r.Copy, has: r.Has, chosenPath: r.ChosenPath, chosen: r.Chosen, chosenMissing: r.ChosenMissing,
		others: r.Others, stat: r.Stat,
	}
}

// decodeSeen reads a kept detection; one that is missing or can't be read is a tool
// never seen.
func decodeSeen(stored string) seenRecord {
	var r seenRecord
	if stored == "" || json.Unmarshal([]byte(stored), &r) != nil {
		return seenRecord{}
	}

	return r
}

// latestRecord is a tool's latest release as the daily check keeps it.
type latestRecord struct {
	Version     string    `json:"version"`
	Tag         string    `json:"tag"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	Assets      []Asset   `json:"assets"`
	CheckedAt   time.Time `json:"checkedAt"`
}

// decodeLatest reads a kept latest release; one that is missing or can't be read was
// never checked.
func decodeLatest(stored string) latestRecord {
	var r latestRecord
	if stored == "" || json.Unmarshal([]byte(stored), &r) != nil {
		return latestRecord{}
	}

	return r
}
