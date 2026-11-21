import { useId } from 'react'

import { actionLabels } from '../../../lib/autopilot'
import { promptApplies } from '../../../lib/launch'
import { moved, problemsAt, problemsExactly } from '../../../lib/routineBuilder'
import type {
  AutopilotGitHubAction,
  AutopilotGitHubActionKind,
  AutopilotLabelsAction,
  AutopilotPromptRef,
  ItemKind,
  Prompt,
} from '../../../platform'
import { fieldClass, quietButtonClass } from '../../controls'
import { PlusIcon, TrashIcon } from '../../icons'
import { MenuButton, type MenuItem } from '../../Menu'
import { Notes, type BuilderKit } from './parts'
import { TokenField } from './TokenField'

export interface GitHubStepBlockProps {
  kit: BuilderKit
  // prompts are the prompt library, whose comment replies a reply action posts.
  prompts: readonly Prompt[]
  // path is where the step is in the steps: "[0]".
  path: string
  // kinds are the kinds of item the routine acts on, whose actions and replies fit.
  kinds: readonly ItemKind[]
  actions: readonly AutopilotGitHubAction[]
  onChange: (actions: AutopilotGitHubAction[]) => void
}

// actionKinds are the keys an action may set, one each, in the catalog's order.
const actionKinds: readonly AutopilotGitHubActionKind[] = ['rebase', 'updateBranch', 'reply', 'labels']

// actionKindOf is the kind of action an action is, or undefined for one that sets none.
function actionKindOf(a: AutopilotGitHubAction): AutopilotGitHubActionKind | undefined {
  return actionKinds.find((k) => a[k] !== undefined && a[k] !== null)
}

// isCommentReply reports a prompt a GitHub step may post: a reply written as a comment,
// never an approval's or a request for changes'.
export function isCommentReply(p: Pick<Prompt, 'purpose' | 'replyAction'>): boolean {
  return p.purpose === 'reply' && p.replyAction === 'comment'
}

// replyValue is a reply as the reply menu holds it.
function replyValue(ref: AutopilotPromptRef): string {
  return (ref.starter ?? '') !== '' ? `starter:${ref.starter ?? ''}` : `nickname:${ref.nickname ?? ''}`
}

// replyRef is the reply a menu value names.
function replyRef(value: string): AutopilotPromptRef {
  const at = value.indexOf(':')
  const name = value.slice(at + 1)

  return value.startsWith('starter:') ? { starter: name } : { nickname: name }
}

// promptRefOf is how a step names a library prompt: by its starter, else its nickname.
function promptRefOf(p: Prompt): AutopilotPromptRef {
  return p.starterKey !== '' ? { starter: p.starterKey } : { nickname: p.nickname }
}

// newAction is an action of kind as + Add an action adds it: a reply the first comment
// reply that fits, labels none yet.
function newAction(kind: AutopilotGitHubActionKind, replies: readonly Prompt[]): AutopilotGitHubAction {
  switch (kind) {
    case 'reply':
      return { reply: { prompt: replies[0] === undefined ? { nickname: '' } : promptRefOf(replies[0]) } }
    case 'labels':
      return { labels: { add: [] } }
    default:
      return { [kind]: {} }
  }
}

// GitHubStepBlock is a GitHub step's actions, in the order they are sent, each one its own
// write pinned to the head read right before it: Ask Dependabot to rebase, Update branch,
// Comment one of your replies (a comment reply of the library's that fits the items), and
// Add or remove labels; each with ↑ ↓ and 🗑, then + Add an action, which offers what the
// step hasn't and fits what the routine acts on.
export function GitHubStepBlock({ kit, prompts, path, kinds, actions, onChange }: GitHubStepBlockProps) {
  const id = useId()
  const field = `${path}.github`
  const replies = prompts.filter((p) => isCommentReply(p) && kinds.every((k) => promptApplies(p, k)))
  const have = actions.map(actionKindOf)
  const items: MenuItem[] = kit.catalog.githubActions
    .filter((a) => a.offered && !have.includes(a.kind) && kinds.every((k) => a.kinds.includes(k)))
    .map((a) => ({
      label: a.label,
      title: a.help,
      onSelect: () => {
        onChange([...actions, newAction(a.kind, replies)])
      },
    }))
  const set = (j: number, next: AutopilotGitHubAction) => {
    onChange(actions.map((a, k) => (k === j ? next : a)))
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-2">
      {actions.length === 0 && <p className="text-xs text-muted">No action yet: add what it sends.</p>}
      <ol className="flex flex-col gap-1.5">
        {actions.map((a, j) => {
          const kind = actionKindOf(a)
          const at = `${field}[${String(j)}]`
          const n = String(j + 1)
          const help = kit.catalog.githubActions.find((g) => g.kind === kind)?.help

          return (
            // Actions have no identity of their own: their place is what they are.
            <li key={j} className="flex flex-col gap-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm" title={help}>
                  {kind === undefined ? 'An action this version doesn’t know' : actionLabels[kind]}
                </span>
                {a.reply !== undefined && a.reply !== null && (
                  <ReplyPicker
                    id={`${id}-${n}-reply`}
                    n={n}
                    value={a.reply.prompt}
                    replies={replies}
                    prompts={prompts}
                    invalid={problemsAt(kit.problems, 'steps', `${at}.reply.prompt`).length > 0}
                    onChange={(prompt) => {
                      set(j, { reply: { prompt } })
                    }}
                  />
                )}
                <span className="ml-auto inline-flex items-center gap-0.5">
                  <button
                    type="button"
                    aria-label={`Move action ${n} up`}
                    title="Move up"
                    disabled={j === 0}
                    onClick={() => {
                      onChange(moved(actions, j, j - 1))
                    }}
                    className="rounded-md px-1.5 py-0.5 text-sm text-muted hover:bg-raised hover:text-text disabled:opacity-40"
                  >
                    ↑
                  </button>
                  <button
                    type="button"
                    aria-label={`Move action ${n} down`}
                    title="Move down"
                    disabled={j === actions.length - 1}
                    onClick={() => {
                      onChange(moved(actions, j, j + 1))
                    }}
                    className="rounded-md px-1.5 py-0.5 text-sm text-muted hover:bg-raised hover:text-text disabled:opacity-40"
                  >
                    ↓
                  </button>
                  <button
                    type="button"
                    aria-label={`Remove action ${n}`}
                    title="Remove"
                    onClick={() => {
                      onChange(actions.filter((_, k) => k !== j))
                    }}
                    className="rounded-md p-1 text-muted hover:bg-raised hover:text-danger"
                  >
                    <TrashIcon className="size-3.5" />
                  </button>
                </span>
              </div>
              {a.labels !== undefined && a.labels !== null && (
                <LabelsFields
                  id={`${id}-${n}`}
                  n={n}
                  kit={kit}
                  labels={a.labels}
                  onChange={(labels) => {
                    set(j, { labels })
                  }}
                />
              )}
              <Notes
                problems={problemsAt(kit.problems, 'steps', at)}
                warnings={problemsAt(kit.warnings, 'steps', at)}
              />
            </li>
          )
        })}
      </ol>
      <Notes problems={problemsExactly(kit.problems, 'steps', field)} />
      {items.length > 0 && (
        <MenuButton label="Add an action" items={items} className={`${quietButtonClass} self-start`}>
          <PlusIcon className="size-3.5" />
          Add an action
        </MenuButton>
      )}
    </div>
  )
}

interface ReplyPickerProps {
  id: string
  n: string
  value: AutopilotPromptRef
  replies: readonly Prompt[]
  prompts: readonly Prompt[]
  invalid: boolean
  onChange: (prompt: AutopilotPromptRef) => void
}

// ReplyPicker picks the comment reply an action posts. A reply the menu doesn't offer (one
// the library lost, or made an approval's since) is kept as itself, so its problem shows.
function ReplyPicker({ id, n, value, replies, prompts, invalid, onChange }: ReplyPickerProps) {
  const current = replyValue(value)
  const known = replies.some((p) => replyValue(promptRefOf(p)) === current)
  const unlisted =
    (value.starter ?? '') !== ''
      ? (prompts.find((p) => p.starterKey === value.starter)?.nickname ?? value.starter ?? '')
      : (value.nickname ?? '')

  return (
    <>
      <label htmlFor={id} className="sr-only">
        {`Action ${n}: the reply it comments`}
      </label>
      <select
        id={id}
        value={current}
        aria-invalid={invalid}
        onChange={(e) => {
          onChange(replyRef(e.target.value))
        }}
        className={`${fieldClass} py-0.5 text-xs`}
      >
        {!known && <option value={current}>{unlisted === '' ? 'Pick a comment reply' : unlisted}</option>}
        {replies.map((p) => (
          <option key={p.id} value={replyValue(promptRefOf(p))}>
            {p.nickname}
          </option>
        ))}
      </select>
    </>
  )
}

interface LabelsFieldsProps {
  id: string
  n: string
  kit: BuilderKit
  labels: AutopilotLabelsAction
  onChange: (labels: AutopilotLabelsAction) => void
}

// LabelsFields are a labels action's two lists: the labels it adds, and those it takes off.
function LabelsFields({ id, n, kit, labels, onChange }: LabelsFieldsProps) {
  const seen = kit.suggestions.labels.map((l) => ({ value: l, label: l }))

  return (
    <div className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-x-2 gap-y-1.5 pl-2">
      <label htmlFor={`${id}-add`} className="text-xs text-muted">
        Add
      </label>
      <TokenField
        id={`${id}-add`}
        label={`Action ${n}: labels to add`}
        placeholder="No label"
        tokens={labels.add ?? []}
        suggestions={seen}
        onChange={(add) => {
          onChange({ ...labels, add })
        }}
      />
      <label htmlFor={`${id}-remove`} className="text-xs text-muted">
        Take off
      </label>
      <TokenField
        id={`${id}-remove`}
        label={`Action ${n}: labels to take off`}
        placeholder="No label"
        tokens={labels.remove ?? []}
        suggestions={seen}
        onChange={(remove) => {
          onChange({ ...labels, remove })
        }}
      />
    </div>
  )
}
