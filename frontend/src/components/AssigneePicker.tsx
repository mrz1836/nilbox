import { useId, useState } from 'react'

import {
  assigneeGroups,
  assigneeHint,
  assigneesNote,
  assignLabel,
  mergeFound,
  searchesGitHub,
  type Assigned,
  type ItemNoun,
} from '../lib/assignees'
import { PicturesContext, usePictures } from '../lib/pictures'
import { filtered } from '../lib/reviewers'
import { applyTakingSelfOff } from '../lib/sheet'
import { useNow } from '../lib/useNow'
import { useQuery } from '../lib/useQuery'
import type { AssigneeCandidates } from '../platform'
import { PersonAvatar } from './Avatar'
import { personLabel } from './PeopleParts'
import { PickerDialog, type Choice } from './Picker'

// OnAssignees changes who an issue or a pull request is assigned to, by login: those to
// assign, and those to unassign. It resolves with why GitHub didn't make it all, or '' once
// it did.
export type OnAssignees = (add: string[], remove: string[]) => Promise<string>

// AssigneeReads are the picker's reads of one item, by its ID: whom the app offers, from
// what it learned alone; a read of who can be assigned in its repository, when one is due;
// and a search for words, which asks GitHub only when what the app holds can't answer.
export interface AssigneeReads {
  candidates: (id: number) => PromiseLike<AssigneeCandidates>
  learn: (id: number) => PromiseLike<AssigneeCandidates>
  search: (id: number, query: string) => PromiseLike<AssigneeCandidates>
}

// maxPictures is the most people one read of pictures asks for (the avatars' own limit).
const maxPictures = 100

export interface AssigneePickerProps {
  itemId: number
  // repo is the item's repository, "owner/name", and noun what kind of item it is.
  repo: string
  noun: ItemNoun
  // assigned are who it is assigned to now.
  assigned: readonly Assigned[]
  // candidates are whom the sheet read the picker offers, which it shows at once, or
  // undefined while that read runs; onLearned hears what the picker learned since, for the
  // sheet to show too.
  candidates: AssigneeCandidates | undefined
  onLearned: (learned: AssigneeCandidates) => void
  reads: AssigneeReads
  // onUnassignMe takes the user off the item, as the sheet's Unassign me does, once the rest
  // of an edit that unchecks their own row went; without it, their row stays checked, and
  // ownLock says why, and where they take themselves off instead.
  onUnassignMe?: (() => void) | undefined
  ownLock: string
  onEdit: OnAssignees
  onClose: () => void
}

// AssigneePicker is a sheet's assignee picker, open: who the item is assigned to
// (Assigned), then whom the app suggests (the user, its author, the people who took part,
// and those assigned in the repository before), the people who work on the repository, the
// rest of those GitHub lists as assignable there, those who work on the owner's other
// repositories while that list is cut short, and those a search of GitHub found, each a row
// to check or uncheck, which Apply (or ⌘↩) sends. Typing filters them at once, on this Mac;
// while the app's list of who can be assigned is cut short, a pause in typing also searches,
// which asks GitHub only when no earlier search answers the words. It opens on what the sheet
// read before, with no request; when the app's knowledge of the repository is due a read
// (stale), it reads it once as it opens. Unchecking the user's own row takes them off with
// Unassign me (onUnassignMe), after the rest of the edit, never through the edit itself.
export function AssigneePicker({
  itemId,
  repo,
  noun,
  assigned,
  candidates,
  onLearned,
  reads,
  onUnassignMe,
  ownLock,
  onEdit,
  onClose,
}: AssigneePickerProps) {
  const id = useId()
  const now = useNow()
  const [query, setQuery] = useState('')
  const words = query.trim()
  const stale = candidates?.stale === true
  const learned = useQuery(
    () =>
      reads.learn(itemId).then((got) => {
        onLearned(got)

        return got
      }),
    [itemId],
    { enabled: stale },
  )
  const known = learned.data ?? candidates
  const searching = searchesGitHub(known, words)
  const found = useQuery(() => reads.search(itemId, words), [itemId, words], { enabled: searching, delay: 300 })
  const people = mergeFound(known?.people ?? [], searching ? found.data?.people : undefined)
  const groups = assigneeGroups(people, assigned, repo, query)
  const pictures = usePictures(
    [...assigned.map((a) => a.login), ...groups.flatMap((g) => g.people.map((c) => c.login))].slice(0, maxPictures),
  )

  const assignedRows: Choice[] = filtered(assigned, query, (a) => [a.login]).map((a) => ({
    key: a.login,
    label: personLabel(a.login, '', a.mine),
    icon: <PersonAvatar login={a.login} />,
    hint: '',
    locked: a.mine && onUnassignMe === undefined ? ownLock : '',
    section: 'Assigned',
  }))
  const choices: Choice[] = [
    ...assignedRows,
    ...groups.flatMap((g) =>
      g.people.map((c) => ({
        key: c.login,
        label: personLabel(c.login, c.name, c.mine),
        icon: <PersonAvatar login={c.login} />,
        hint: '',
        aside: assigneeHint(c, now, noun),
        locked: '',
        section: g.section,
      })),
    ),
  ]

  const busy =
    stale && learned.pending
      ? `Learning who can be assigned in ${repo}…`
      : searching && found.pending && !found.waiting
        ? 'Searching GitHub…'
        : ''
  // A failed read says why in its own words, which name the repository.
  const failed = learned.error !== '' ? learned.error : searching ? found.error : ''
  const none =
    known === undefined
      ? 'Reading who can be assigned…'
      : words === ''
        ? `The app doesn’t know anyone who can be assigned in ${repo} yet.`
        : searching && found.pending
          ? 'Searching…'
          : 'No one matches.'
  const note =
    choices.length === 0 ? (failed === '' ? none : failed) : failed !== '' ? failed : assigneesNote(known, repo, now)

  return (
    <PicturesContext value={pictures}>
      <PickerDialog
        id={id}
        title="Assignees"
        placeholder="Filter or search people"
        query={query}
        onQuery={setQuery}
        choices={choices}
        note={note}
        busy={busy}
        current={assigned.map((a) => a.login)}
        applyLabel={assignLabel}
        onApply={(add, remove) =>
          onUnassignMe === undefined
            ? onEdit(add, remove)
            : applyTakingSelfOff(add, remove, assigned.find((a) => a.mine)?.login ?? '', onEdit, onUnassignMe)
        }
        onClose={onClose}
      />
    </PicturesContext>
  )
}
