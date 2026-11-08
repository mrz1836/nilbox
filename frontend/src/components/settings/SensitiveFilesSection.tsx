import { startTransition, useActionState, useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { counted } from '../../lib/format'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  refreshInbox,
  setSensitiveCategory,
  setSensitivePatterns,
  setSensitiveScope,
  type SensitiveCategory,
  type SensitiveLevel,
  type SensitiveScope,
  type Settings,
} from '../../platform'
import { Segmented, Switch } from '../controls'
import { PathTester } from './PathTester'
import { ErrorText, Group, joinIds, Note, SectionIntro } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'

// Field is where a refusal shows: under the settings above the patterns, or under the
// pattern field.
type Field = 'settings' | 'patterns'

// Refusal is why a change was refused, where it shows, and the draft typed when it was:
// typing again clears a refused pattern.
interface Refusal {
  field: Field
  draft: string
  message: string
}

// Change is a change to the sensitive-file settings: how it shows at once, the call that
// stores it, where a refusal shows, the draft typed when it was made, and what to do once
// it is stored.
interface Change {
  shows: Partial<Settings>
  call: () => PromiseLike<Settings>
  field: Field
  draft: string
  then?: () => void
}

const noRefusal: Refusal = { field: 'settings', draft: '', message: '' }

const scopeOptions: readonly { value: SensitiveScope; label: string; title: string }[] = [
  {
    value: 'outside',
    label: 'Outside contributors',
    title: 'Forks, people outside the repository, and bots you don’t trust',
  },
  {
    value: 'collaborators',
    label: 'Also collaborators',
    title: 'Collaborators’ pull requests too: people with access who aren’t the owner or org members',
  },
]

// levelDot is a category's level, as a dot beside its name.
const levelDot: Readonly<Record<SensitiveLevel, string>> = {
  danger: 'bg-danger',
  warning: 'bg-warning',
  '': 'bg-muted',
}

interface CategoryRowProps {
  category: SensitiveCategory
  on: boolean
  busy: boolean
  onChange: (on: boolean) => void
  // describedBy is the id of why the last change was refused, while it shows.
  describedBy: string | false
}

// CategoryRow is one built-in category: its switch, why its files deserve a look, and its
// patterns behind a disclosure. A locked category's switch stays on.
function CategoryRow({ category, on, busy, onChange, describedBy }: CategoryRowProps) {
  const id = useId()
  const patterns = category.patterns

  return (
    <li className="flex items-start gap-4 px-4 py-3.5">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${levelDot[category.level]}`} />
          <label htmlFor={id} className="text-sm font-medium">
            {category.label}
          </label>
          {category.locked && <span className="text-[11px] text-muted">Always on</span>}
        </div>
        <p id={`${id}-help`} className="mt-0.5 text-xs leading-relaxed text-muted">
          {category.description}
          {category.locked && ' A session on a pull request that changes these asks you first.'}
        </p>
        <details className="group/patterns mt-1.5">
          <summary className="cursor-pointer text-xs font-medium text-muted select-none hover:text-text">
            {counted(patterns.length, 'pattern', 'patterns')}
          </summary>
          <ul aria-label={`${category.label} patterns`} className="mt-1.5 flex flex-wrap gap-1">
            {patterns.map((p) => (
              <li key={p} className="rounded border border-border px-1.5 py-px font-mono text-[11px] text-text">
                {p}
              </li>
            ))}
          </ul>
        </details>
      </div>
      <Switch
        id={id}
        checked={on || category.locked}
        describedBy={joinIds(`${id}-help`, describedBy)}
        disabled={busy || category.locked}
        title={category.locked ? 'Always on: launches are guarded by it' : undefined}
        onChange={onChange}
        className="mt-0.5"
      />
    </li>
  )
}

// SensitiveFilesSection is the settings page's Sensitive files section: whose pull
// requests are checked, which built-in categories count, the user's own patterns, a path
// to try them on, and how the check works.
export function SensitiveFilesSection({ settings: stored, onSaved }: SectionProps) {
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

  const config = settings.sensitiveFiles
  const off = config.off
  const patterns = config.patterns
  const categories = settings.sensitiveCategories
  const refusal = invalid.message !== '' ? invalid : busy ? noRefusal : refused
  const error = refusal.field === 'settings' ? refusal.message : ''
  const patternError = refusal.field === 'patterns' && refusal.draft === draft ? refusal.message : ''
  const errorId = error !== '' && `${id}-error`

  // store makes a change through the action.
  const store = (next: Omit<Change, 'draft'>) => {
    setInvalid(noRefusal)
    startTransition(() => {
      change({ ...next, draft })
    })
  }

  // storePatterns replaces the user's own patterns.
  const storePatterns = (next: string[], then?: () => void) => {
    store({
      shows: { sensitiveFiles: { ...config, patterns: next } },
      call: () => setSensitivePatterns(next),
      field: 'patterns',
      ...(then === undefined ? {} : { then }),
    })
  }

  const chooseScope = (scope: SensitiveScope) => {
    store({
      shows: { sensitiveFiles: { ...config, scope } },
      call: () => setSensitiveScope(scope),
      field: 'settings',
      then: () => {
        // Collaborators' pull requests are checked by a refresh: run one now rather than
        // wait for the next.
        if (scope === 'collaborators') {
          refreshInbox().catch((err: unknown) => {
            console.error('could not refresh the inbox', err)
          })
        }
      },
    })
  }

  const add = () => {
    // The button is disabled while the field is blank, so the pattern is never empty.
    const pattern = draft.trim()

    if (patterns.some((p) => p.toLowerCase() === pattern.toLowerCase())) {
      setInvalid({ field: 'patterns', draft, message: `${pattern} is already on the list.` })

      return
    }

    storePatterns([...patterns, pattern], () => {
      setDraft('')
    })
  }

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-2">
        <Group>
          <div className="flex items-center justify-between gap-8 px-4 py-3.5">
            <div className="min-w-0">
              <p className="text-sm font-medium">Check pull requests from</p>
              <p className="mt-0.5 text-xs leading-relaxed text-muted">
                Forks, outside contributors, and bots you don’t trust always are. Collaborators have access to the
                repository without being its owner or org members.
              </p>
            </div>
            <Segmented
              label="Check pull requests from"
              value={config.scope === 'outside' ? 'outside' : 'collaborators'}
              options={scopeOptions}
              disabled={busy}
              describedBy={joinIds(errorId)}
              onChange={chooseScope}
            />
          </div>
        </Group>
        <ErrorText id={`${id}-error`} error={error} />
        <Note>
          Never checked: your own pull requests, and those of the repository’s owner, its org’s members, and the trusted
          bots in <span className="text-text">Settings → Trust</span>.
        </Note>
      </div>

      <section aria-labelledby={`${id}-categories`} className="flex flex-col gap-3">
        <SectionIntro id={`${id}-categories`} title="What counts as sensitive">
          A checked pull request’s card points out the files it changes in these categories: red for files that run or
          steer an agent, amber for files that change what the project trusts.
        </SectionIntro>
        <ul aria-label="Categories" className="divide-y divide-border rounded-lg border border-border bg-surface">
          {categories.map((category) => (
            <CategoryRow
              key={category.id}
              category={category}
              on={!off.includes(category.id)}
              busy={busy}
              describedBy={errorId}
              onChange={(on) => {
                store({
                  shows: {
                    sensitiveFiles: {
                      ...config,
                      off: on ? off.filter((c) => c !== category.id) : [...off, category.id],
                    },
                  },
                  call: () => setSensitiveCategory(category.id, on),
                  field: 'settings',
                })
              }}
            />
          ))}
        </ul>
      </section>

      <section aria-labelledby={`${id}-patterns`} className="flex flex-col gap-3">
        <SectionIntro id={`${id}-patterns`} title="Your patterns">
          Files and folders that matter in your projects, such as infrastructure or deploy scripts. They’re flagged in
          red.
        </SectionIntro>
        <StringListEditor
          label="Your patterns"
          items={patterns}
          empty="No patterns of your own yet."
          removeLabel={(pattern) => `Remove ${pattern}`}
          onRemove={(pattern) => {
            storePatterns(patterns.filter((p) => p !== pattern))
          }}
          inputLabel="Pattern to add"
          placeholder="A file or folder, such as deploy/ or *.tf"
          submitLabel="Add pattern"
          draft={draft}
          onDraftChange={setDraft}
          onAdd={add}
          error={patternError}
          describedBy={`${id}-syntax`}
          busy={busy}
        />
        <Note id={`${id}-syntax`}>
          Patterns work like <code className="font-mono">.gitignore</code>, ignoring case. A name alone, such as{' '}
          <code className="font-mono">*.tf</code>, matches at any depth; one with a slash, such as{' '}
          <code className="font-mono">infra/prod</code>, matches from the repository’s root; a trailing slash matches a
          folder’s contents; and <code className="font-mono">**</code> matches any number of folders.
        </Note>
      </section>

      <PathTester settings={stored} />

      <section aria-labelledby={`${id}-how`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-how`} title="How it works" />
        <ul className="flex list-disc flex-col gap-1.5 pl-4 text-xs leading-relaxed text-muted">
          <li>
            Each time a checked pull request is pushed to, {brand.name} reads the paths of the files it changes from
            GitHub, and where a moved file came from. It never downloads their contents.
          </li>
          <li>
            Opening a session on one that changes agent configuration, or whose files couldn’t all be checked, asks you
            first: hooks in <code className="font-mono">.claude/settings.json</code> run commands on your Mac as soon as
            Claude Code starts, even in plan mode. Its files are checked again at that moment.
          </li>
          <li>The session’s prompt names the sensitive files, so its review starts with them.</li>
          <li>
            Paths alone can’t show what a change does inside an ordinary file, such as a test that runs during a build.
            This is a first look, not a review.
          </li>
        </ul>
      </section>
    </div>
  )
}
