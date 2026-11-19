import { useState } from 'react'

import { useQuery } from '../../lib/useQuery'
import { issueAssignable, issueLabels, type Issue, type IssueCandidate } from '../../platform'
import { LabelChip } from '../ItemSignals'
import { fold, Picker } from '../Picker'

// IssueEdit is a change to an issue's labels or assignees, as the pickers ask for it.
export interface IssueEdit {
  addLabels?: string[]
  removeLabels?: string[]
  addAssignees?: string[]
  removeAssignees?: string[]
}

// OnEdit makes an edit on GitHub, and resolves with why it couldn't, or '' once it did.
export type OnEdit = (edit: IssueEdit) => Promise<string>

// loadNote says why a picker has no choices to show: they load, or they couldn't.
function loadNote(pending: boolean, error: string): string {
  if (error !== '') {
    return `Couldn’t read them from GitHub: ${error}`
  }

  return pending ? 'Reading them from GitHub…' : ''
}

// labelsNote says what the label picker shows under its choices: that they load, why they
// couldn't, that it holds only the first of a repository's labels, which the filter searches,
// or that none matches.
function labelsNote(loading: string, held: number, total: number, shown: number): string {
  if (loading !== '') {
    return loading
  }

  if (total > held) {
    return shown === 0
      ? `None of the first ${String(held)} labels matches, of ${String(total)}.`
      : `The first ${String(held)} labels, of ${String(total)}.`
  }

  return shown === 0 ? 'No label matches.' : ''
}

// LabelPicker edits the issue's labels: the repository's labels, filtered by the field,
// each checked while the issue has it. The labels are read as it opens, which GitHub
// answers for one point, kept ten minutes; a repository with more than a hundred shows the
// first hundred in name order, and says so.
export function LabelPicker({ issue, onEdit }: { issue: Issue; onEdit: OnEdit }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const labels = useQuery(() => issueLabels(issue.id), [issue.id], { enabled: open })
  const q = query.trim().toLowerCase()
  const held = labels.data?.labels ?? []
  const shown = held.filter((l) => l.name.toLowerCase().includes(q))
  const loading = loadNote(labels.pending && labels.data === undefined, labels.error)

  return (
    <Picker
      open={open}
      onOpen={setOpen}
      title="Labels"
      buttonLabel="Edit labels"
      placeholder="Filter labels"
      query={query}
      onQuery={setQuery}
      choices={shown.map((l) => ({
        key: l.name,
        label: <LabelChip label={l} />,
        hint: l.description,
        locked: '',
      }))}
      note={labelsNote(loading, held.length, labels.data?.total ?? 0, shown.length)}
      current={issue.labels.map((l) => l.name)}
      onApply={(add, remove) => onEdit({ addLabels: add, removeLabels: remove })}
    />
  )
}

// AssigneePicker edits the issue's assignees: who it is assigned to, and the people a
// search finds it can be, a search costing one point as the user stops typing, kept ten
// minutes. The user takes themselves off with Unassign me, never here.
export function AssigneePicker({ issue, onEdit }: { issue: Issue; onEdit: OnEdit }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const found = useQuery(() => issueAssignable(issue.id, query.trim()), [issue.id, query.trim()], {
    enabled: open,
    delay: 300,
  })
  const assigned = issue.assignees.map((p) => ({ login: p.login, name: '', mine: p.mine }))
  const people: IssueCandidate[] = [
    ...assigned,
    ...(found.data ?? []).filter((p) => !assigned.some((a) => fold(a.login) === fold(p.login))),
  ]
  const loading = loadNote(found.pending && found.data === undefined, found.error)

  return (
    <Picker
      open={open}
      onOpen={setOpen}
      title="Assignees"
      buttonLabel="Edit assignees"
      placeholder="Search people"
      query={query}
      onQuery={setQuery}
      choices={people.map((p) => ({
        key: p.login,
        label: (
          <span className="truncate">
            {p.login}
            {p.mine && (
              <>
                {' '}
                <span className="text-muted">(you)</span>
              </>
            )}
          </span>
        ),
        hint: p.name,
        locked:
          p.mine && issue.assignees.some((a) => a.mine)
            ? 'Take yourself off with Unassign me, in the action menu.'
            : '',
      }))}
      note={loading === '' && people.length === 0 ? 'No one matches.' : loading}
      current={issue.assignees.map((p) => p.login)}
      onApply={(add, remove) => onEdit({ addAssignees: add, removeAssignees: remove })}
    />
  )
}
