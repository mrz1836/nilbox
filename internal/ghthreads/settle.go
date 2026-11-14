package ghthreads

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// OutcomeKind is what happened to an item the user finished in the app, or that the inbox
// saw close or leave its lists. Its values are the inbox's own, which core adapts.
type OutcomeKind string

// Kinds of outcome.
const (
	// OutcomeMerged is a pull request the user merged in the app, and OutcomeClosed a pull
	// request or an issue they closed.
	OutcomeMerged OutcomeKind = "merged"
	OutcomeClosed OutcomeKind = "closed"

	// OutcomeReviewed is a pull request the user approved or requested changes of in the
	// app.
	OutcomeReviewed OutcomeKind = "reviewed"

	// OutcomeDismissed is an item the user dismissed in the app, and OutcomeRestored one
	// they brought back, which takes the dismissal's clears back.
	OutcomeDismissed OutcomeKind = "dismissed"
	OutcomeRestored  OutcomeKind = "restored"

	// OutcomeUnassigned is an item the user took themselves off the assignees of, and
	// OutcomeUnrequested a pull request they took themselves off the reviewers of.
	OutcomeUnassigned  OutcomeKind = "unassigned"
	OutcomeUnrequested OutcomeKind = "unrequested"

	// OutcomeSeenClosed is an item the inbox found merged or closed, by anyone, and
	// OutcomeUnlisted one it found in none of its lists: the item's state is checked again.
	OutcomeSeenClosed OutcomeKind = "seen_closed"
	OutcomeUnlisted   OutcomeKind = "unlisted"
)

// Outcome is what happened to one item, as the inbox tells it.
type Outcome struct {
	Kind OutcomeKind

	// Login is the account whose inbox lists the item.
	Login string

	// Ref names the item, its kind included.
	Ref item.Ref

	// At is when it happened: the user's click, or the review's own time.
	At time.Time
}

// Settler hears what the user finished in the app from the inbox, and records the moments
// that clear the threads about it. It is the inbox's OutcomeSink, wired by core, and is
// never bound to the window.
type Settler struct {
	svc *Service
}

// NewSettler returns the Settler of svc.
func NewSettler(svc *Service) *Settler {
	return &Settler{svc: svc}
}

// Settle records outcomes in one transaction, then nudges the Syncer to decide the threads
// about their items soon, and returns without waiting for it. A merge or a close records its
// moment, and GitHub's state of the item as it answered the user's merge or close (merged
// or closed then, read now), so the moment needs no state check; a review that decides, a
// dismissal, and taking oneself off record their moments; a restore takes the dismissal's
// moment back, and keeps its queued clears; and an item the inbox saw close or leave its
// lists has its state checked first. A failure is logged: the inbox's work is done either
// way.
func (st *Settler) Settle(ctx context.Context, outcomes []Outcome) {
	s := st.svc
	now := s.clock.Now()

	var refs, check []item.Ref

	for _, o := range outcomes {
		if o.Login == "" || !o.Ref.Repo.Valid() || o.Ref.Number <= 0 {
			continue
		}

		refs = append(refs, o.Ref)

		if o.Kind == OutcomeSeenClosed || o.Kind == OutcomeUnlisted {
			check = append(check, o.Ref)
		}
	}

	if len(refs) == 0 {
		return
	}

	if err := s.recordOutcomes(ctx, outcomes, now); err != nil {
		s.logger.WarnContext(ctx, "recording what the user finished for their GitHub notifications failed",
			slog.Any("error", err))
	}

	s.nudges.add(refs, check, false)
}

// recordOutcomes records outcomes, as Settle does, at now, in one transaction. A restore waits for
// a clear on its way to GitHub, so it never marks kept one already sent.
func (s *Service) recordOutcomes(ctx context.Context, outcomes []Outcome, now time.Time) error {
	if slices.ContainsFunc(outcomes, func(o Outcome) bool { return o.Kind == OutcomeRestored }) {
		s.sending.Lock()
		defer s.sending.Unlock()
	}

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, o := range outcomes {
			if o.Login == "" || !o.Ref.Repo.Valid() || o.Ref.Number <= 0 {
				continue
			}

			if err := settleOne(ctx, q, o, now); err != nil {
				return err
			}
		}

		return nil
	})
}

// settleOne records o, at now, through q.
func settleOne(ctx context.Context, q *queries.Queries, o Outcome, now time.Time) error {
	switch o.Kind {
	case OutcomeMerged:
		return finished(ctx, q, o, threadrules.TriggerMerged, threadrules.StateMerged, now)
	case OutcomeClosed:
		trigger := threadrules.TriggerIssueClosed
		if o.Ref.Kind == item.KindPR {
			trigger = threadrules.TriggerPRClosed
		}

		return finished(ctx, q, o, trigger, threadrules.StateClosed, now)
	case OutcomeReviewed:
		return recordMoment(ctx, q, o, threadrules.TriggerReviewed, now)
	case OutcomeDismissed:
		return recordMoment(ctx, q, o, threadrules.TriggerDismissed, now)
	case OutcomeUnassigned, OutcomeUnrequested:
		return recordMoment(ctx, q, o, threadrules.TriggerUnassigned, now)
	case OutcomeRestored:
		return restored(ctx, q, o, now)
	case OutcomeSeenClosed, OutcomeUnlisted:
		return nil
	default:
		return nil
	}
}

// finished records the user's own merge or close o, by trigger, and the item's state as
// GitHub answered it: state since o.At, read now.
func finished(
	ctx context.Context, q *queries.Queries, o Outcome, trigger threadrules.Trigger, state threadrules.State, now time.Time,
) error {
	if err := recordMoment(ctx, q, o, trigger, now); err != nil {
		return err
	}

	closedAt, checked := o.At.UTC(), now.UTC()

	err := q.SetSubjectState(ctx, queries.SetSubjectStateParams{
		SubjectState: string(state), SubjectClosedAt: &closedAt, StateCheckedAt: &checked, AccountLogin: o.Login,
		RepoOwner: o.Ref.Repo.Owner, RepoName: o.Ref.Repo.Repo, Number: int64(o.Ref.Number),
	})
	if err != nil {
		return fmt.Errorf("store the state of %s: %w", o.Ref, err)
	}

	return nil
}

// recordMoment records o's moment, by trigger, heard of at now. An item of no kind the app
// knows records none.
func recordMoment(ctx context.Context, q *queries.Queries, o Outcome, trigger threadrules.Trigger, now time.Time) error {
	if !o.Ref.Kind.Valid() || !trigger.AppliesTo(o.Ref.Kind) {
		return nil
	}

	err := q.UpsertMoment(ctx, queries.UpsertMomentParams{
		AccountLogin: o.Login, RepoOwner: o.Ref.Repo.Owner, RepoName: o.Ref.Repo.Repo, ItemKind: string(o.Ref.Kind),
		Number: int64(o.Ref.Number), Trigger: string(trigger), At: o.At.UTC(), CreatedAt: now.UTC(),
	})
	if err != nil {
		return fmt.Errorf("record that %s was finished: %w", o.Ref, err)
	}

	return nil
}

// restored takes back the dismissal of o's item: its moment, and its queued clears, kept.
func restored(ctx context.Context, q *queries.Queries, o Outcome, now time.Time) error {
	_, err := q.DeleteMoment(ctx, queries.DeleteMomentParams{
		AccountLogin: o.Login, RepoOwner: o.Ref.Repo.Owner, RepoName: o.Ref.Repo.Repo, Number: int64(o.Ref.Number),
		Trigger: string(threadrules.TriggerDismissed),
	})
	if err != nil {
		return fmt.Errorf("take back the dismissal of %s: %w", o.Ref, err)
	}

	at := now.UTC()

	_, err = q.KeepItemClears(ctx, queries.KeepItemClearsParams{
		SettledAt: &at, AccountLogin: o.Login, RepoOwner: o.Ref.Repo.Owner, RepoName: o.Ref.Repo.Repo,
		Number: int64(o.Ref.Number), Trigger: string(threadrules.TriggerDismissed),
	})
	if err != nil {
		return fmt.Errorf("keep the clears of %s: %w", o.Ref, err)
	}

	return nil
}
