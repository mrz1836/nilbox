import { useId, useState, type SubmitEvent } from 'react'

import { parseTarget, ruleRows, targetKey, withRepoRule, type RuleRow } from '../../lib/repoRules'
import { useQuery } from '../../lib/useQuery'
import type { SettingsSave } from '../../lib/useSettingsSave'
import { listLaunchTargets, setRepoRule, type LaunchTarget, type RepoRule, type Settings } from '../../platform'
import { buttonClass, fieldClass, Segmented, type SegmentOption } from '../controls'
import { MuteIcon, PinIcon, XIcon } from '../icons'
import { ErrorText, Group, joinIds, Note, SectionIntro } from './rows'

export interface RepoRulesProps {
  // settings are the settings as shown, with every change still being saved applied, and
  // save stores one change (useSettingsSave).
  settings: Settings
  save: SettingsSave['save']
}

// noTargets is the list of targets offered until it loads.
const noTargets: readonly LaunchTarget[] = []

// ruleOptions are a row's choices.
const ruleOptions: readonly SegmentOption<Exclude<RepoRule, ''>>[] = [
  { value: 'pinned', label: 'Pinned', icon: <PinIcon filled className="size-3" /> },
  { value: 'muted', label: 'Muted', icon: <MuteIcon className="size-3" /> },
]

// optionText writes a target the field offers: "acme-org" or "acme-org/api".
function optionText(t: LaunchTarget): string {
  return t.name === '' ? t.owner : `${t.owner}/${t.name}`
}

// RepoRules lists the pinned and muted repositories and orgs, each org first and then its
// repositories, each with its rule to switch and a button that takes it off, above a field
// that pins or mutes another. A pinned one's items stay at the top of every inbox list; a
// muted one's wait in each list's Muted tab, and the Release page lists it apart.
export function RepoRules({ settings, save }: RepoRulesProps) {
  const id = useId()
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const targets = useQuery(() => listLaunchTargets(), [], { log: 'list the repositories to pin or mute' })
  const rows = ruleRows(settings.repoRules)
  // orgs are the orgs with a rule of their own, whose repositories' rows sit under theirs.
  const orgs = new Set(rows.filter((r) => !r.target.includes('/')).map((r) => targetKey(r.target)))

  // set stores a target's rule, '' to take it off, showing it at once. It resolves with
  // whether it was stored; a refusal shows under the field.
  const set = async (target: string, rule: RepoRule): Promise<boolean> => {
    setBusy(true)
    setError('')

    const result = await save({ repoRules: withRepoRule(settings.repoRules, target, rule) }, () =>
      setRepoRule(target, rule),
    )

    setBusy(false)

    if (!result.ok) {
      setError(result.error)
    }

    return result.ok
  }

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()

    // Pin, the form's first button, is what Return in the field does.
    const submitter = e.nativeEvent.submitter
    const rule: RepoRule = submitter instanceof HTMLButtonElement && submitter.value === 'muted' ? 'muted' : 'pinned'
    const target = parseTarget(draft)

    if (target === null) {
      setError('Type an org, such as acme-org, or a repository, such as acme-org/example-repo.')

      return
    }

    void set(target, rule).then((stored) => {
      if (stored) {
        setDraft('')
      }
    })
  }

  return (
    <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-3">
      <SectionIntro id={`${id}-heading`} title="Pinned and muted">
        Pin an org or a repository to keep its pull requests and issues at the top of every inbox list. Mute one to move
        them into each list&apos;s Muted tab, out of the counts, the menu bar, and the notifications. A
        repository&apos;s own rule wins over its org&apos;s.
      </SectionIntro>
      <Group>
        {rows.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted">
            None yet. Each card&apos;s menu pins or mutes its repository or org too.
          </p>
        ) : (
          <ul aria-label="Pinned and muted repositories" className="divide-y divide-border">
            {rows.map((row) => (
              <RuleRowItem
                key={row.target}
                row={row}
                nested={row.target.includes('/') && orgs.has(targetKey(row.org))}
                busy={busy}
                onChange={(rule) => {
                  void set(row.target, rule)
                }}
                onRemove={() => {
                  void set(row.target, '')
                }}
              />
            ))}
          </ul>
        )}
        <form onSubmit={submit} className="flex items-start gap-2 px-4 py-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <label htmlFor={`${id}-add`} className="sr-only">
              Org or repository to pin or mute
            </label>
            <input
              id={`${id}-add`}
              list={`${id}-targets`}
              value={draft}
              placeholder="acme-org or acme-org/example-repo"
              spellCheck={false}
              autoComplete="off"
              aria-invalid={error !== ''}
              aria-describedby={joinIds(error !== '' && `${id}-error`)}
              onChange={(e) => {
                setDraft(e.target.value)
                setError('')
              }}
              className={`${fieldClass} w-full font-mono text-[0.8125rem]`}
            />
            <datalist id={`${id}-targets`}>
              {(targets.data ?? noTargets).map((t) => (
                <option key={optionText(t)} value={optionText(t)}>
                  {t.name === '' ? 'All repositories' : `${String(t.items)} in the inbox`}
                </option>
              ))}
            </datalist>
            <ErrorText id={`${id}-error`} error={error} />
          </div>
          <button type="submit" value="pinned" disabled={busy || draft.trim() === ''} className={buttonClass}>
            <PinIcon className="size-3.5" />
            Pin
          </button>
          <button type="submit" value="muted" disabled={busy || draft.trim() === ''} className={buttonClass}>
            <MuteIcon className="size-3.5" />
            Mute
          </button>
        </form>
      </Group>
      <Note>
        A muted repository&apos;s items are still read with the inbox, so they are up to date when you unmute it, but
        nothing follows their checks or checks their files. On Scripts → Release, a muted repository is listed apart,
        and GitHub isn&apos;t asked about it.
      </Note>
    </section>
  )
}

interface RuleRowItemProps {
  row: RuleRow
  // nested sets a repository's row under its org's.
  nested: boolean
  busy: boolean
  onChange: (rule: Exclude<RepoRule, ''>) => void
  onRemove: () => void
}

// RuleRowItem is one target's row: its name, what it covers, its rule, and the button that
// takes the rule off.
function RuleRowItem({ row, nested, busy, onChange, onRemove }: RuleRowItemProps) {
  const repository = row.target.includes('/')

  return (
    <li className={`flex items-center gap-3 py-2.5 pr-4 ${nested ? 'bg-background/40 pl-8' : 'pl-4'}`}>
      <span className="min-w-0 truncate font-mono text-[0.8125rem]">{row.target}</span>
      <span className="shrink-0 text-xs text-muted">{repository ? 'Repository' : 'All repositories'}</span>
      <span className="ml-auto">
        <Segmented
          label={`Rule for ${row.target}`}
          value={row.rule}
          options={ruleOptions}
          size="sm"
          disabled={busy}
          onChange={onChange}
        />
      </span>
      <button
        type="button"
        disabled={busy}
        aria-label={`Remove the rule for ${row.target}`}
        title={`Remove the rule for ${row.target}`}
        onClick={onRemove}
        className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-danger disabled:opacity-50"
      >
        <XIcon className="size-3.5" />
      </button>
    </li>
  )
}
