package draft

import (
	"context"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// scriptProblems returns the problems of the scripts sp's steps name: one of the user's
// scripts no longer saved, or, found by its ID alone, saved under another name than the
// step's (FindScript). Without the scripts to read, or with scripts that can't be read, it
// finds none here; the runner refuses a script that is gone as it starts, and the engine
// holds one renamed. It reads the scripts only for a spec with a script step.
func scriptProblems(ctx context.Context, sp spec.Spec, s Setup) []spec.Problem {
	if !HasStep(sp, catalog.StepScript) {
		return nil
	}

	mine, ok := s.Scripts(ctx)
	if !ok {
		return nil
	}

	var out []spec.Problem

	for i, st := range sp.Steps {
		if st.Script == nil {
			continue
		}

		at, renamed := FindScript(mine, *st.Script)

		var message string

		switch {
		case at < 0:
			message = fmt.Sprintf("%q is no longer one of your scripts: pick another", st.Script.Name)
		case renamed:
			message = RenamedWords(st.Script.Name, mine[at].Name)
		default:
			continue
		}

		out = append(out, spec.Problem{Part: spec.PartSteps, Field: fmt.Sprintf("[%d].script.id", i), Message: message})
	}

	return out
}

// FindScript returns the index in mine of the script step names, -1 for none, and whether
// it waits to be picked again. The script with the step's UUID is the step's, whatever
// its ID and name are now. Otherwise, as for a step picked before UUIDs, the script with
// the step's ID is, but saved under another name than the step's it is renamed, and waits:
// an ID alone may since name another script.
func FindScript(mine []Script, step spec.ScriptStep) (int, bool) {
	if step.UUID != "" {
		if at := slices.IndexFunc(mine, func(m Script) bool { return m.UUID == step.UUID }); at >= 0 {
			return at, false
		}
	}

	at := slices.IndexFunc(mine, func(m Script) bool { return m.ID == step.ID })
	if at < 0 {
		return -1, false
	}

	return at, mine[at].Name != step.Name
}

// RenamedWords says that the script a step picked as picked is now saved as name, so it
// waits to be picked again.
func RenamedWords(picked, name string) string {
	return fmt.Sprintf("The script picked as %q is now %q: pick it again", picked, name)
}

// HasStep reports a spec with a step of kind.
func HasStep(sp spec.Spec, kind catalog.StepKind) bool {
	return slices.ContainsFunc(sp.Steps, func(st spec.Step) bool { return st.Kind() == kind })
}
