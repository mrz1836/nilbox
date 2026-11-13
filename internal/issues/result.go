package issues

import "github.com/mrz1836/nilbox/internal/textutil"

// result is what the action came to.
func (a *action) result() Result {
	return Result{
		Outcome: a.outcome, Message: a.message(), Reason: a.reason, Steps: textutil.NonNil(a.steps),
		Commented: a.commented, LeftInbox: a.acted.Closed,
	}
}

// message says what the action came to, for the toast or the sheet's notice. One that
// posted the user's text says so, whatever it came to, so the user never posts it twice.
func (a *action) message() string {
	ref := a.ref.String()

	switch a.outcome {
	case OutcomeClosed:
		if a.commented {
			return "Closed " + ref + " as " + closedWords(a.kind) + " with your comment"
		}

		return "Closed " + ref + " as " + closedWords(a.kind)
	case OutcomeCommentedNotClosed:
		return "Commented on " + ref + ", but it wasn't closed: " + a.reason
	case OutcomeNewActivity:
		return a.reason + ", so " + ref + " wasn't closed. Read what's new, then close it again."
	case OutcomeAlreadyClosed:
		if a.commented {
			return "Commented on " + ref + ", but someone closed it first" + closedAs(a.closedAs)
		}

		return ref + " was already closed" + closedAs(a.closedAs)
	case OutcomeCommented:
		return "Commented on " + ref
	default:
		return ""
	}
}

// closedAs says what an issue someone else closed is closed as, "" when GitHub doesn't
// say.
func closedAs(reason StateReason) string {
	switch reason {
	case ReasonCompleted:
		return " as completed"
	case ReasonNotPlanned:
		return " as not planned"
	case ReasonDuplicate:
		return " as a duplicate"
	case ReasonNone, ReasonReopened:
		return ""
	default:
		return ""
	}
}
