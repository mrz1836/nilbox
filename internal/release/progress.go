package release

import (
	"cmp"

	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// stopOff says why a release can't be stopped while magex switches to the branch it
// releases from or brings that branch up to date (§5.7.3, §3.9 "Stop off").
const stopOff = "Stopping now could leave a rebase in progress."

// Progress returns how far magex has got, as the reader has read its output so far, and
// whether the release may be stopped now: a release's scriptrun.Spec.Progress. in is what
// the release set out to do, as its Before completed it.
//
// Stop is off from the moment magex starts switching to the branch it releases from until
// the pull ends, since SIGINT mid-pull leaves git's rebase in progress (S2's
// sigint-mid-rebase). Once magex has made the tag, and until it has sent it, Stop asks
// first, since stopping can leave the tag on this Mac only; and while magex has the clone
// on another branch than the one it was on, Stop asks first too, since stopping leaves it
// there. A line magex prints is read within the runner's tail interval, so a Stop in that
// moment is judged on the line before it.
func (r *OutputReader) Progress(in Intent) scriptrun.Progress {
	r.mu.Lock()
	step, working, tag, branch, back := r.out.Step, r.out.Working, r.out.Tag, r.out.Branch, r.out.BackOn
	r.mu.Unlock()

	branch = cmp.Or(branch, in.Branch)
	away := step.Reached(StepOnBranch) && branch != "" && branch != in.Original && back == ""

	p := scriptrun.Progress{Step: string(step), Working: string(working)}

	switch {
	case working == StepOnBranch || working == StepUpdated || step == StepOnBranch:
		p.StopRefused = stopOff
	case step == StepTagCreated:
		p.StopConfirm = "Stopping now can leave " + cmp.Or(tag, in.Version) + " on this Mac but not on GitHub"
		if away {
			p.StopConfirm += ", and your clone on " + branch
		}

		p.StopConfirm += "."
	case away:
		p.StopConfirm = "Stopping now can leave your clone on " + branch + "."
	}

	return p
}
