import { useState } from 'react'

import { assigneeHint, quickPicks, type Assigned, type ItemNoun } from '../lib/assignees'
import { useNow } from '../lib/useNow'
import { useQuery } from '../lib/useQuery'
import { openExternal } from '../platform'
import { AssigneePicker, type AssigneeReads, type OnAssignees } from './AssigneePicker'
import { PersonAvatar } from './Avatar'
import { captionClass, quietButtonClass } from './controls'
import { XIcon } from './icons'
import { NobodyLine, RowAction, SuggestionChips } from './PeopleParts'

// AssigneesEditing is what a sheet's assignees need to change: the picker's reads of the
// item, why the user's own assignment can't be taken off here (ownLock), and the edit.
export interface AssigneesEditing {
  reads: AssigneeReads
  ownLock: string
  onEdit: OnAssignees
}

export interface AssigneesSectionProps {
  // look is where the section sits: the review sheet's summary, small, or the issue sheet's
  // details, beside its other parts.
  look: 'summary' | 'details'
  itemId: number
  // repo is the item's repository, "owner/name", noun what kind of item it is, and url its
  // page, where the assignees the sheet doesn't name are.
  repo: string
  noun: ItemNoun
  url: string
  // assignees are who it is assigned to, and more how many others GitHub has.
  assignees: readonly Assigned[]
  more: number
  // editing changes who it is assigned to, while the user may; without it, they only show.
  editing?: AssigneesEditing | undefined
  // onUnassignMe takes the user off it, as the sheet's Unassign me does, from × on their own
  // line, whether or not they may change the others; without it, their line offers none.
  onUnassignMe?: (() => void) | undefined
}

// AssigneesSection lists who an issue or a pull request is assigned to, each in a line with
// their picture, the user's marked. While the user may change them (editing), it has Edit,
// which opens the assignee picker, and a line offers × to unassign anyone but the user, a
// line fading while its edit runs. The user's own assignment is never the edit's to take
// off: given onUnassignMe, their line offers × for Unassign me, and unchecking their row in
// the picker runs it once the rest of the edit went. With no one
// assigned, it offers its first few suggestions as one-click assignments, the user's first
// ("Assign yourself"). Whom it suggests it reads from what the app learned, asking GitHub
// nothing. In the summary's look, a pull request no one is assigned to that the user can't
// change shows no section.
export function AssigneesSection({
  look,
  itemId,
  repo,
  noun,
  url,
  assignees,
  more,
  editing,
  onUnassignMe,
}: AssigneesSectionProps) {
  const now = useNow()
  const [picking, setPicking] = useState(false)
  // busy is the login whose edit runs, or ''; failed why the last one failed.
  const [busy, setBusy] = useState('')
  const [failed, setFailed] = useState('')
  const candidates = useQuery(() => editing?.reads.candidates(itemId) ?? Promise.resolve(undefined), [itemId], {
    enabled: editing !== undefined,
    log: 'Read who to suggest as assignees',
  })

  if (assignees.length === 0 && editing === undefined && look === 'summary') {
    return null
  }

  const quick = async (login: string, add: string[], remove: string[]) => {
    if (editing === undefined) {
      return
    }

    setBusy(login)
    setFailed('')

    const why = await editing.onEdit(add, remove)

    setBusy('')
    setFailed(why)
    void candidates.refetch()
  }

  const picks = quickPicks(candidates.data, assignees).map((p) => ({
    login: p.login,
    text: p.you ? 'Assign yourself' : p.login,
    label: p.you ? 'Assign yourself' : `Assign ${p.login}`,
    title: [p.candidate.name, assigneeHint(p.candidate, now, noun)].filter((w) => w !== '').join(' · '),
  }))
  const small = look === 'summary'

  return (
    <section aria-label="Assignees" className={small ? 'space-y-1 text-xs' : 'space-y-1.5'}>
      <div className="flex items-center justify-between gap-2">
        {small ? <p className="text-muted">Assignees</p> : <h3 className={captionClass}>Assignees</h3>}
        {editing !== undefined && (
          <button
            type="button"
            aria-label="Edit assignees"
            title="Edit assignees"
            aria-haspopup="dialog"
            aria-expanded={picking}
            onClick={(e) => {
              // The desktop's WebKit focuses no button on a click. Focusing this one first
              // makes it where the picker gives focus back as it closes.
              e.currentTarget.focus()
              setPicking(true)
            }}
            className={quietButtonClass}
          >
            Edit
          </button>
        )}
      </div>
      {assignees.length === 0 ? (
        <div className="space-y-1.5">
          <NobodyLine size={small ? '' : 'text-sm'}>No one is assigned.</NobodyLine>
          {editing !== undefined && (
            <SuggestionChips
              label="Suggested assignees"
              chips={picks}
              busy={busy}
              onPick={(login) => {
                void quick(login, [login], [])
              }}
              onMore={() => {
                setPicking(true)
              }}
            />
          )}
        </div>
      ) : (
        <ul className={small ? 'space-y-0.5' : 'space-y-1 text-sm'}>
          {assignees.map((a) => (
            <li
              key={a.login}
              aria-busy={busy === a.login || undefined}
              className={`group flex min-w-0 items-center gap-1 ${busy === a.login ? 'opacity-50' : ''}`}
            >
              <span className="flex min-w-0 items-center gap-1.5 px-1 py-0.5">
                <PersonAvatar login={a.login} />
                <span className="min-w-0 truncate font-medium text-text">{a.login}</span>
                {/* A space keeps the login and "(you)" apart when read aloud; the flex layout drops it. */}
                {a.mine && (
                  <>
                    {' '}
                    <span className="shrink-0 text-muted">(you)</span>
                  </>
                )}
              </span>
              {editing !== undefined && !a.mine && (
                <RowAction
                  label={`Unassign ${a.login}`}
                  title="Unassign"
                  disabled={busy !== ''}
                  onClick={() => {
                    void quick(a.login, [], [a.login])
                  }}
                >
                  <XIcon className="size-3" />
                </RowAction>
              )}
              {onUnassignMe !== undefined && a.mine && (
                <RowAction
                  label="Unassign me"
                  title={`Take yourself off this ${noun} on GitHub. Uses one REST request.`}
                  disabled={busy !== ''}
                  onClick={onUnassignMe}
                >
                  <XIcon className="size-3" />
                </RowAction>
              )}
            </li>
          ))}
        </ul>
      )}
      {more > 0 && (
        <button
          type="button"
          onClick={() => {
            openExternal(url)
          }}
          className="text-xs text-muted hover:text-accent hover:underline"
        >
          and {String(more)} more on GitHub
        </button>
      )}
      {failed !== '' && (
        <p role="alert" className={small ? 'text-danger' : 'text-xs text-danger'}>
          {failed}
        </p>
      )}
      {picking && editing !== undefined && (
        <AssigneePicker
          itemId={itemId}
          repo={repo}
          noun={noun}
          assigned={assignees}
          candidates={candidates.data}
          onLearned={(learned) => {
            candidates.set(() => learned)
          }}
          reads={editing.reads}
          onUnassignMe={onUnassignMe}
          ownLock={editing.ownLock}
          onEdit={async (add, remove) => {
            const why = await editing.onEdit(add, remove)

            void candidates.refetch()

            return why
          }}
          onClose={() => {
            setPicking(false)
          }}
        />
      )}
    </section>
  )
}
