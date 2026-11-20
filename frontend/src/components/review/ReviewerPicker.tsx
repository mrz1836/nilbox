import { useId, useState } from 'react'

import { PicturesContext, usePictures } from '../../lib/pictures'
import {
  applyLabel,
  candidateHint,
  filtered,
  learnedNote,
  personFields,
  teamFields,
  teamKey,
  teamName,
} from '../../lib/reviewers'
import { useNow } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import { learnReviewers, type Review, type ReviewEntry, type Reviewers, type TeamCandidate } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { PeopleIcon } from '../icons'
import { personLabel } from '../PeopleParts'
import { fold, PickerDialog, type Choice } from '../Picker'

// OnReviewers changes who the pull request asks to review, by the picker's keys (a login, or
// teamKey's for a team): those to ask, and those to take off. It resolves with why GitHub
// didn't make it all, or '' once it did.
export type OnReviewers = (add: string[], remove: string[]) => Promise<string>

export interface ReviewerPickerProps {
  review: Review
  // candidates are who the sheet read the picker offers, which it shows at once, or
  // undefined while that read runs; onLearned hears what the picker learned since, for the
  // sheet to show too.
  candidates: Reviewers | undefined
  onLearned: (learned: Reviewers) => void
  onEdit: OnReviewers
  onClose: () => void
}

// requested are the people and teams the pull request asks to review, as the sheet's
// reviewers list them.
function requested(review: Review): ReviewEntry[] {
  return review.reviews.filter((r) => r.requested)
}

// requestKey is the picker's key for a request: the person's login, or the team's teamKey.
function requestKey(r: ReviewEntry): string {
  return r.team ? teamKey(r.author.slice(r.author.indexOf('/') + 1)) : r.author
}

// TeamGlyph stands in for a team's picture, the size of a person's.
function TeamGlyph() {
  return (
    <span
      aria-hidden="true"
      className="inline-flex size-4 shrink-0 items-center justify-center rounded bg-raised text-muted"
    >
      <PeopleIcon className="size-3" />
    </span>
  )
}

// ReviewerPicker is the review sheet's reviewer picker, open: who the pull request asks to
// review (Requested), whom GitHub suggests for it, the people who work on the repository,
// and its teams, each a row to check or uncheck, which Apply (or ⌘↩) sends. Typing filters
// them at once, on this Mac alone, by login, name, or slug, those that start with it first.
// It opens on what the sheet read before, with no request; when the app's knowledge of the
// repository is due a read (stale), it reads it once as it opens, and its answer replaces
// the list in place, keeping what the user checked. The user's own request is theirs to take
// off with Remove me as reviewer.
export function ReviewerPicker({ review, candidates, onLearned, onEdit, onClose }: ReviewerPickerProps) {
  const id = useId()
  const now = useNow()
  const [query, setQuery] = useState('')
  const stale = candidates?.stale === true
  const learned = useQuery(
    () =>
      learnReviewers(review.id).then((got) => {
        onLearned(got)

        return got
      }),
    [review.id],
    { enabled: stale },
  )
  const known = learned.data ?? candidates
  const asks = requested(review)
  const asked = new Set(asks.map((r) => fold(requestKey(r))))
  const people = (known?.people ?? []).filter((p) => !asked.has(fold(p.login)))
  const suggested = people.filter((p) => p.suggestion !== '')
  const others = people.filter((p) => p.suggestion === '')
  const teams = (known?.teams ?? []).filter((t) => !asked.has(fold(teamKey(t.slug))))
  const pictures = usePictures(
    [...asks.filter((r) => !r.team).map((r) => r.author), ...people.map((p) => p.login)].slice(0, 100),
  )

  const requestRows: Choice[] = filtered(asks, query, (r) => [r.author]).map((r) => ({
    key: requestKey(r),
    label: personLabel(r.author, ''),
    icon: r.team ? <TeamGlyph /> : <PersonAvatar login={r.author} />,
    hint: '',
    aside: r.state === '' ? '' : 'asked again',
    locked: r.mine ? 'Take yourself off with Remove me as reviewer, in the action menu.' : '',
    section: 'Requested',
  }))
  const personRows = (list: typeof people, section: string): Choice[] =>
    filtered(list, query, personFields).map((p) => ({
      key: p.login,
      label: personLabel(p.login, p.name),
      icon: <PersonAvatar login={p.login} />,
      hint: '',
      aside: candidateHint(p, now),
      locked: '',
      section,
    }))
  const teamRows: Choice[] = filtered(teams, query, teamFields).map((t: TeamCandidate) => ({
    key: teamKey(t.slug),
    label: personLabel(teamName(review.repo, t.slug), t.name),
    icon: <TeamGlyph />,
    hint: '',
    locked: '',
    section: 'Teams',
  }))
  const choices = [
    ...requestRows,
    ...personRows(suggested, 'Suggested for this pull request'),
    ...personRows(others, `Works on ${review.repo}`),
    ...teamRows,
  ]

  const busy = stale && learned.pending ? `Learning who works on ${review.repo}…` : ''
  // A failed read says why in its own words, which name the repository.
  const failed = learned.error
  const none =
    known === undefined
      ? 'Reading who works on this repository…'
      : query.trim() === ''
        ? `The app hasn’t seen anyone work on ${review.repo} yet.`
        : 'No one matches.'
  const note =
    choices.length === 0
      ? none
      : failed !== ''
        ? failed
        : learnedNote(review.repo, known?.pulls ?? 0, known?.readAt ?? '', now)

  return (
    <PicturesContext value={pictures}>
      <PickerDialog
        id={id}
        title="Reviewers"
        placeholder="Filter people and teams"
        query={query}
        onQuery={setQuery}
        choices={choices}
        note={note}
        busy={busy}
        current={asks.map(requestKey)}
        applyLabel={applyLabel}
        onApply={onEdit}
        onClose={onClose}
      />
    </PicturesContext>
  )
}
