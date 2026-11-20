import { useState } from 'react'

import { useQuery } from '../../lib/useQuery'
import { issueLabels, type Issue } from '../../platform'
import { LabelChip } from '../ItemSignals'
import { Picker } from '../Picker'

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
