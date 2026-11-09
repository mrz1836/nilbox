import { startTransition, useActionState, useId, useState } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { setBranchRules, setWorktreeAutoCleanup, sweepWorktrees, type BranchRules, type Settings } from '../../platform'
import { quietButtonClass, Switch } from '../controls'
import { ErrorText, Group, joinIds, Note, rowDescribedBy, SectionIntro, SettingRow } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'

// Field is where a refusal shows: under the cleanup switch, under the branch switches, or
// under the protected branch field.
type Field = 'cleanup' | 'rules' | 'protected'

// Refusal is why a change was refused, where it shows, and the draft typed when it was:
// typing again clears a refused pattern.
interface Refusal {
  field: Field
  draft: string
  message: string
}

// Change is a change to the worktree settings: how it shows at once, the call that
// stores it, where a refusal shows, the draft typed when it was made, and what to do once
// it is stored.
interface Change {
  shows: Partial<Settings>
  call: () => PromiseLike<Settings>
  field: Field
  draft: string
  then?: () => void
}

const noRefusal: Refusal = { field: 'rules', draft: '', message: '' }

// sameList reports whether two lists hold the same patterns in the same order.
function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

// sweepNow runs the automatic cleanup at once rather than at its next check.
function sweepNow(): void {
  sweepWorktrees().catch((err: unknown) => {
    console.error('could not sweep the worktrees', err)
  })
}

interface RuleSwitch {
  key: 'own' | 'pullRequest' | 'merged'
  label: string
  description: string
}

// ruleSwitches are the branch rules turned on and off.
const ruleSwitches: readonly RuleSwitch[] = [
  {
    key: 'own',
    label: 'Delete the worktree’s own branch',
    description:
      'The worktree-pr-… or worktree-issue-… branch made for it. Starts ticked when you remove or clean up a worktree, and the automatic cleanup deletes it.',
  },
  {
    key: 'pullRequest',
    label: 'Delete the pull request’s branch',
    description:
      'The pull request’s own branch, such as feature-x, when a session checked it out in the worktree. Starts ticked, and the automatic cleanup deletes it.',
  },
  {
    key: 'merged',
    label: 'Tick merged branches',
    description:
      'In Clean up merged branches…, on the Worktrees page, each local branch whose pull request merged starts ticked.',
  },
]

// WorktreesSection is the settings page's Worktrees section: whether finished worktrees
// are cleaned up on their own, which branches go with a worktree or with the cleanup of
// merged branches, and the branches that never go.
export function WorktreesSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  const [draft, setDraft] = useState('')
  // invalid is why the draft cannot be added, found before asking the backend.
  const [invalid, setInvalid] = useState<Refusal>(noRefusal)
  // Every change runs through one action: one at a time, with the controls held while it
  // runs.
  const [refused, change, busy] = useActionState(async (_: Refusal, next: Change): Promise<Refusal> => {
    const result = await save(next.shows, next.call)

    if (!result.ok) {
      return { field: next.field, draft: next.draft, message: result.error }
    }

    next.then?.()

    return noRefusal
  }, noRefusal)

  const rules = settings.branches
  const defaults = settings.defaultProtectedBranches
  const refusal = invalid.message !== '' ? invalid : busy ? noRefusal : refused
  // errorOf is the refusal under one field, or ''.
  const errorOf = (field: Field) =>
    refusal.field !== field ? '' : field !== 'protected' || refusal.draft === draft ? refusal.message : ''

  // store makes a change through the action.
  const store = (next: Omit<Change, 'draft'>) => {
    setInvalid(noRefusal)
    startTransition(() => {
      change({ ...next, draft })
    })
  }

  // storeRules replaces the branch rules, showing a refusal under field.
  const storeRules = (next: BranchRules, field: Field, then?: () => void) => {
    store({
      shows: { branches: next },
      call: () => setBranchRules(next),
      field,
      ...(then === undefined ? {} : { then }),
    })
  }

  const add = () => {
    // The button is disabled while the field is blank, so the pattern is never empty.
    const pattern = draft.trim()

    if (rules.protected.includes(pattern)) {
      setInvalid({ field: 'protected', draft, message: `${pattern} is already protected.` })

      return
    }

    storeRules({ ...rules, protected: [...rules.protected, pattern] }, 'protected', () => {
      setDraft('')
    })
  }

  const rulesError = errorOf('rules')

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <SettingRow
          label="Clean up after merging"
          controlId={`${id}-cleanup`}
          description="Removes a session’s worktree once its pull request merges or its issue closes, if it has no uncommitted changes, all its commits are on GitHub, and no session runs in it, with the branches the switches below delete. Checked every few minutes; ignored files in it go with it."
          error={errorOf('cleanup')}
        >
          <Switch
            id={`${id}-cleanup`}
            checked={settings.worktreeAutoCleanup}
            disabled={busy}
            describedBy={rowDescribedBy(`${id}-cleanup`, errorOf('cleanup'))}
            onChange={(worktreeAutoCleanup) => {
              store({
                shows: { worktreeAutoCleanup },
                call: () => setWorktreeAutoCleanup(worktreeAutoCleanup),
                field: 'cleanup',
                // Turned on, it sweeps at once rather than at the next check.
                ...(worktreeAutoCleanup ? { then: sweepNow } : {}),
              })
            }}
          />
        </SettingRow>
      </Group>

      <section aria-labelledby={`${id}-branches`} className="flex flex-col gap-3">
        <SectionIntro id={`${id}-branches`} title="Branches">
          What goes with a worktree. Every removal and cleanup shows a box for each branch, ticked as these say, and you
          can change any of them before you confirm.
        </SectionIntro>
        <Group>
          {ruleSwitches.map((s) => (
            <SettingRow key={s.key} label={s.label} controlId={`${id}-${s.key}`} description={s.description}>
              <Switch
                id={`${id}-${s.key}`}
                checked={rules[s.key]}
                disabled={busy}
                describedBy={joinIds(`${id}-${s.key}-help`, rulesError !== '' && `${id}-rules-error`)}
                onChange={(on) => {
                  storeRules({ ...rules, [s.key]: on }, 'rules')
                }}
              />
            </SettingRow>
          ))}
        </Group>
        <ErrorText id={`${id}-rules-error`} error={rulesError} />
        <Note>
          A branch is deleted only when every commit on it is on GitHub, and only while it still points at the commit
          the confirmation showed, so no work is lost: it can be brought back from that commit.
        </Note>
      </section>

      <section aria-labelledby={`${id}-protected`} className="flex flex-col gap-3">
        <SectionIntro
          id={`${id}-protected`}
          title="Protected branches"
          action={
            !sameList(rules.protected, defaults) && (
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  storeRules({ ...rules, protected: defaults }, 'protected')
                }}
                className={`${quietButtonClass} shrink-0`}
              >
                Restore the defaults
              </button>
            )
          }
        >
          Never deleted, by any cleanup. Each repository’s default branch, and any branch checked out in a worktree, are
          always kept too.
        </SectionIntro>
        <StringListEditor
          label="Protected branches"
          items={rules.protected}
          empty="No branch is protected but each repository’s default branch."
          removeLabel={(pattern) => `Stop protecting ${pattern}`}
          onRemove={(pattern) => {
            storeRules({ ...rules, protected: rules.protected.filter((p) => p !== pattern) }, 'protected')
          }}
          extra={(pattern) => defaults.includes(pattern) && <span className="text-xs text-muted">default</span>}
          inputLabel="Branch to protect"
          placeholder="A branch, or a pattern such as release/*"
          submitLabel="Protect branch"
          draft={draft}
          onDraftChange={setDraft}
          onAdd={add}
          error={errorOf('protected')}
          describedBy={`${id}-syntax`}
          busy={busy}
        />
        <Note id={`${id}-syntax`}>
          A pattern matches whole branch names: <code className="font-mono">*</code> matches any run of characters but{' '}
          <code className="font-mono">/</code>, and <code className="font-mono">?</code> any one, so{' '}
          <code className="font-mono">release/*</code> protects release/1.2 but not release/1.2/fix.
        </Note>
      </section>
    </div>
  )
}
