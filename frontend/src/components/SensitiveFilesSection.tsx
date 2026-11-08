import { useEffect, useId, useState, type SubmitEvent } from 'react'

import { brand } from '../generated/brand'
import { counted, errorMessage } from '../lib/format'
import {
  matchSensitivePath,
  refreshInbox,
  setSensitiveCategory,
  setSensitivePatterns,
  setSensitiveScope,
  type SensitiveCategory,
  type SensitiveLevel,
  type SensitivePathMatch,
  type SensitiveScope,
  type Settings,
} from '../platform'
import { buttonClass, fieldClass, Segmented, switchClass } from './controls'
import { XIcon } from './icons'

export interface SensitiveFilesSectionProps {
  settings: Settings
  // onSaved receives the settings after a change was stored.
  onSaved: (settings: Settings) => void
}

// tryDelay is how long the path tester waits after a keystroke before asking.
const tryDelay = 200

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

const levelText: Readonly<Record<SensitiveLevel, string>> = {
  danger: 'text-danger',
  warning: 'text-warning',
  '': 'text-text',
}

interface CategoryRowProps {
  category: SensitiveCategory
  on: boolean
  busy: boolean
  onChange: (on: boolean) => void
}

// CategoryRow is one built-in category: its switch, why its files deserve a look, and its
// patterns behind a disclosure. A locked category's switch stays on.
function CategoryRow({ category, on, busy, onChange }: CategoryRowProps) {
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
      <input
        id={id}
        type="checkbox"
        role="switch"
        aria-describedby={`${id}-help`}
        checked={on || category.locked}
        disabled={busy || category.locked}
        title={category.locked ? 'Always on: launches are guarded by it' : undefined}
        onChange={(e) => {
          onChange(e.target.checked)
        }}
        className={`${switchClass} mt-0.5`}
      />
    </li>
  )
}

// PathTester says whether a path counts as sensitive under the stored settings, as it is
// typed, and which category and pattern it matches.
function PathTester({ settings }: { settings: Settings }) {
  const id = useId()
  const [path, setPath] = useState('')
  const [result, setResult] = useState<{ path: string; match: SensitivePathMatch | null; error: string } | null>(null)

  // Ask again after typing stops, and whenever the settings change.
  useEffect(() => {
    const trimmed = path.trim()

    if (trimmed === '') {
      return undefined
    }

    let current = true
    const timer = setTimeout(() => {
      matchSensitivePath(trimmed)
        .then((match) => {
          if (current) {
            setResult({ path: trimmed, match, error: '' })
          }
        })
        .catch((err: unknown) => {
          if (current) {
            setResult({ path: trimmed, match: null, error: errorMessage(err) })
          }
        })
    }, tryDelay)

    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [path, settings])

  const shown = result !== null && result.path === path.trim() ? result : null

  return (
    <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-2">
      <div>
        <h3 id={`${id}-heading`} className="text-sm font-semibold">
          Try a path
        </h3>
        <p className="mt-0.5 text-xs text-muted">See whether a file in a pull request would be pointed out, and why.</p>
      </div>
      <label htmlFor={`${id}-path`} className="sr-only">
        Path to try
      </label>
      <input
        id={`${id}-path`}
        value={path}
        placeholder="A path in a repository, such as .github/workflows/release.yml"
        spellCheck={false}
        autoComplete="off"
        aria-describedby={`${id}-result`}
        onChange={(e) => {
          setPath(e.target.value)
        }}
        className={`${fieldClass} w-full font-mono text-[13px]`}
      />
      <p id={`${id}-result`} role="status" className="min-h-4 text-xs">
        {path.trim() !== '' && shown !== null && <TryResult result={shown} />}
      </p>
    </section>
  )
}

function TryResult({ result }: { result: { match: SensitivePathMatch | null; error: string } }) {
  if (result.error !== '') {
    return <span className="text-danger">{result.error}</span>
  }

  const match = result.match

  if (match === null || !match.sensitive) {
    return <span className="text-muted">Not sensitive with these settings.</span>
  }

  return (
    <span className={levelText[match.level]}>
      Sensitive: {match.label}, matched by <code className="font-mono">{match.pattern}</code>.
    </span>
  )
}

// SensitiveFilesSection is the settings page's Sensitive files section: whose pull
// requests are checked, which built-in categories count, the user's own patterns, a path
// to try them on, and how the check works.
export function SensitiveFilesSection({ settings, onSaved }: SensitiveFilesSectionProps) {
  const id = useId()
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const [patternError, setPatternError] = useState('')
  const [busy, setBusy] = useState(false)

  const config = settings.sensitiveFiles
  const off = config.off
  const patterns = config.patterns
  const categories = settings.sensitiveCategories

  // save stores a change, then reports the settings as stored; onError shows why it was
  // refused.
  const save = (call: Promise<Settings>, onError: (message: string) => void, after?: () => void) => {
    setBusy(true)
    setError('')
    setPatternError('')

    call
      .then((next) => {
        onSaved(next)
        after?.()
      })
      .catch((err: unknown) => {
        onError(errorMessage(err))
      })
      .finally(() => {
        setBusy(false)
      })
  }

  const chooseScope = (scope: SensitiveScope) => {
    save(setSensitiveScope(scope), setError, () => {
      // Collaborators' pull requests are checked by a refresh: run one now rather than
      // wait for the next.
      if (scope === 'collaborators') {
        refreshInbox().catch((err: unknown) => {
          console.error('could not refresh the inbox', err)
        })
      }
    })
  }

  const add = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()

    // The button is disabled while the field is blank, so the pattern is never empty.
    const pattern = draft.trim()

    if (patterns.some((p) => p.toLowerCase() === pattern.toLowerCase())) {
      setPatternError(`${pattern} is already on the list.`)

      return
    }

    save(setSensitivePatterns([...patterns, pattern]), setPatternError, () => {
      setDraft('')
    })
  }

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-2">
        <div className="divide-y divide-border rounded-lg border border-border bg-surface">
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
              onChange={chooseScope}
            />
          </div>
        </div>
        {error !== '' && (
          <p role="alert" className="text-xs text-danger">
            {error}
          </p>
        )}
        <p className="text-xs leading-relaxed text-muted">
          Never checked: your own pull requests, and those of the repository’s owner, its org’s members, and the trusted
          bots in <span className="text-text">Settings → Trust</span>.
        </p>
      </div>

      <section aria-labelledby={`${id}-categories`} className="flex flex-col gap-3">
        <div>
          <h3 id={`${id}-categories`} className="text-sm font-semibold">
            What counts as sensitive
          </h3>
          <p className="mt-0.5 text-xs text-muted">
            A checked pull request’s card points out the files it changes in these categories: red for files that run or
            steer an agent, amber for files that change what the project trusts.
          </p>
        </div>
        <ul aria-label="Categories" className="divide-y divide-border rounded-lg border border-border bg-surface">
          {categories.map((category) => (
            <CategoryRow
              key={category.id}
              category={category}
              on={!off.includes(category.id)}
              busy={busy}
              onChange={(on) => {
                save(setSensitiveCategory(category.id, on), setError)
              }}
            />
          ))}
        </ul>
      </section>

      <section aria-labelledby={`${id}-patterns`} className="flex flex-col gap-3">
        <div>
          <h3 id={`${id}-patterns`} className="text-sm font-semibold">
            Your patterns
          </h3>
          <p className="mt-0.5 text-xs text-muted">
            Files and folders that matter in your projects, such as infrastructure or deploy scripts. They’re flagged in
            red.
          </p>
        </div>
        <div className="divide-y divide-border rounded-lg border border-border bg-surface">
          {patterns.length === 0 ? (
            <p className="px-4 py-3 text-sm text-muted">No patterns of your own yet.</p>
          ) : (
            <ul aria-label="Your patterns" className="divide-y divide-border">
              {patterns.map((pattern) => (
                <li key={pattern} className="flex items-center gap-3 px-4 py-2.5">
                  <span className="min-w-0 truncate font-mono text-[13px]">{pattern}</span>
                  <button
                    type="button"
                    disabled={busy}
                    aria-label={`Remove ${pattern}`}
                    title={`Remove ${pattern}`}
                    onClick={() => {
                      save(setSensitivePatterns(patterns.filter((p) => p !== pattern)), setPatternError)
                    }}
                    className="ml-auto flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-danger disabled:opacity-50"
                  >
                    <XIcon className="size-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          )}
          <form onSubmit={add} className="flex items-start gap-2 px-4 py-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <label htmlFor={`${id}-add`} className="sr-only">
                Pattern to add
              </label>
              <input
                id={`${id}-add`}
                value={draft}
                placeholder="A file or folder, such as deploy/ or *.tf"
                spellCheck={false}
                autoComplete="off"
                aria-invalid={patternError !== ''}
                aria-describedby={patternError === '' ? `${id}-syntax` : `${id}-pattern-error ${id}-syntax`}
                onChange={(e) => {
                  setDraft(e.target.value)
                  setPatternError('')
                }}
                className={`${fieldClass} w-full font-mono text-[13px]`}
              />
              {patternError !== '' && (
                <p id={`${id}-pattern-error`} role="alert" className="text-xs text-danger">
                  {patternError}
                </p>
              )}
            </div>
            <button type="submit" disabled={busy || draft.trim() === ''} className={buttonClass}>
              Add pattern
            </button>
          </form>
        </div>
        <p id={`${id}-syntax`} className="text-xs leading-relaxed text-muted">
          Patterns work like <code className="font-mono">.gitignore</code>, ignoring case. A name alone, such as{' '}
          <code className="font-mono">*.tf</code>, matches at any depth; one with a slash, such as{' '}
          <code className="font-mono">infra/prod</code>, matches from the repository’s root; a trailing slash matches a
          folder’s contents; and <code className="font-mono">**</code> matches any number of folders.
        </p>
      </section>

      <PathTester settings={settings} />

      <section aria-labelledby={`${id}-how`} className="flex flex-col gap-2">
        <h3 id={`${id}-how`} className="text-sm font-semibold">
          How it works
        </h3>
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
