import { useId, useState } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { setRefreshInterval, type RefreshKind, type Settings } from '../../platform'
import { Group, Note, SectionIntro, SettingRow } from './rows'
import type { SectionProps } from './types'

// defaults are each interval a fresh database starts at, in seconds, which its select
// marks.
const defaults: Readonly<Record<RefreshKind, number>> = { inbox: 300, checks: 45, worktrees: 300, notifications: 300 }

// rows are the things the app reads again on its own, each with what a read costs.
const rows: readonly { kind: RefreshKind; label: string; description: string }[] = [
  {
    kind: 'inbox',
    label: 'Inbox',
    description:
      'Every item in your inbox, read again from GitHub. Each refresh reads every item, a few points; Refresh reads it at once.',
  },
  {
    kind: 'checks',
    label: 'Running checks',
    description:
      'The pull requests whose checks are still running, all in one request, about a point, until their checks end. Nothing is read while no checks run.',
  },
  {
    kind: 'worktrees',
    label: 'Worktrees',
    description:
      'Each clone’s worktrees, the pull requests and issues they belong to, and the automatic cleanup. Opening the Worktrees page reads them at once.',
  },
  {
    kind: 'notifications',
    label: 'GitHub notifications',
    description:
      'Your GitHub inbox, listed from GitHub: free when nothing changed, and never sooner than GitHub asks. The refresh button in the GitHub notifications panel checks at once.',
  },
]

// everyWords words an interval: "Every 45 seconds", "Every 5 minutes", "Every hour", or
// "Off".
export function everyWords(seconds: number): string {
  if (seconds <= 0) {
    return 'Off'
  }

  if (seconds < 60) {
    return `Every ${String(seconds)} seconds`
  }

  if (seconds === 60) {
    return 'Every minute'
  }

  if (seconds < 3600) {
    return `Every ${String(seconds / 60)} minutes`
  }

  return seconds === 3600 ? 'Every hour' : `Every ${String(seconds / 3600)} hours`
}

// RefreshingSection holds how often the app reads the inbox, the running checks, the
// worktrees, and the GitHub notifications again on its own, each a select of the intervals Go offers, saved as soon as
// it is chosen; what reads them again works its next wait out from the new interval at
// once.
export function RefreshingSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  // changed is the interval changed last, beside which a refusal shows.
  const [changed, setChanged] = useState<RefreshKind>('inbox')

  const choose = (kind: RefreshKind, seconds: number) => {
    setChanged(kind)

    const refresh: Settings['refresh'] = { ...settings.refresh, [kind]: seconds }

    void save({ refresh }, () => setRefreshInterval(kind, seconds))
  }

  return (
    <div className="flex flex-col gap-3">
      <SectionIntro title="How often">
        Shorter intervals keep the app closer to GitHub and spend more of your API budget, which the budget gauge in the
        toolbar shows.
      </SectionIntro>
      <Group>
        {rows.map((row) => {
          const controlId = `${id}-${row.kind}`

          return (
            <SettingRow
              key={row.kind}
              label={row.label}
              controlId={controlId}
              description={row.description}
              error={changed === row.kind ? error : ''}
            >
              <select
                id={controlId}
                value={settings.refresh[row.kind]}
                aria-describedby={`${controlId}-help`}
                onChange={(e) => {
                  choose(row.kind, Number(e.target.value))
                }}
                className="rounded-md border border-border bg-surface px-2 py-1 text-sm"
              >
                {settings.refreshChoices[row.kind].map((seconds) => (
                  <option key={seconds} value={seconds}>
                    {everyWords(seconds)}
                    {seconds === defaults[row.kind] ? ' (default)' : ''}
                  </option>
                ))}
              </select>
            </SettingRow>
          )
        })}
      </Group>
      <Note>A change applies at once: a refresh already due under the new interval starts straight away.</Note>
    </div>
  )
}
