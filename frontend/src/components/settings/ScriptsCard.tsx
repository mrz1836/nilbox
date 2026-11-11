import { useId, useState } from 'react'

import { scriptsOn } from '../../lib/scriptsTab'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  setIntegrationEnabled,
  setScriptSources,
  type IntegrationSwitch,
  type IntegrationTools,
  type ScriptSources,
  type Settings,
} from '../../platform'
import { Switch } from '../controls'
import { cardMatchClass, headingMatchClass, useSetting } from './match'
import { ErrorText, joinIds } from './rows'

// switchCaption says what the Scripts switch means: on or off as the user set it, or,
// while automatic, on or off with mage-x and go-pre-commit, once the tools are known.
function switchCaption(set: IntegrationSwitch, on: boolean, known: boolean): string {
  if (set === 'auto') {
    if (!known) {
      return 'Automatic'
    }

    return on ? 'On · automatic' : 'Off · automatic'
  }

  return set === 'on' ? 'On' : 'Off'
}

// sourceChoices are the sources Repo scripts reads besides the user's own scripts, each
// a checkbox.
const sourceChoices: readonly { key: keyof ScriptSources; label: string }[] = [
  { key: 'magexTargets', label: 'Show magex targets' },
  { key: 'repoFiles', label: 'Read t3.json and VS Code tasks' },
]

export interface ScriptsCardProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // tools are the tools as the last look found them, or null until listed: mage-x or
  // go-pre-commit on turns Scripts on while its switch is automatic.
  tools: IntegrationTools | null
}

// ScriptsCard is the Scripts integration's card: what it is for, its switch (automatic,
// the default, is on while mage-x or go-pre-commit is), and which sources Repo scripts
// reads: magex's targets, and the commands in a repository's own files. Each change is
// saved as soon as it is made.
export function ScriptsCard({ settings: stored, onSaved, tools }: ScriptsCardProps) {
  const id = useId()
  const [marks, match] = useSetting('Scripts')
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  // changed is what was changed last, beside which its refusal shows.
  const [changed, setChanged] = useState<'switch' | 'sources'>('switch')
  const { integrations } = settings
  const set = integrations.switches.scripts ?? 'auto'
  const on = scriptsOn(tools, set)
  // known reports what the switch shows is so: while automatic, that is only once the
  // tools are listed, and until then the switch waits, so a click never stores the
  // opposite of what the user meant.
  const known = set !== 'auto' || tools !== null
  const sources = integrations.scriptSources
  const switchError = changed === 'switch' ? error : ''
  const sourcesError = changed === 'sources' ? error : ''

  // flip stores the opposite of what the switch shows, for good: on or off, never
  // automatic again. It is only offered while what the switch shows is known.
  const flip = () => {
    const next: IntegrationSwitch = on ? 'off' : 'on'

    setChanged('switch')
    void save({ integrations: { ...integrations, switches: { ...integrations.switches, scripts: next } } }, () =>
      setIntegrationEnabled('scripts', next),
    )
  }

  const choose = (key: keyof ScriptSources, checked: boolean) => {
    const next = { ...sources, [key]: checked }

    setChanged('sources')
    void save({ integrations: { ...integrations, scriptSources: next } }, () =>
      setScriptSources(next.magexTargets, next.repoFiles),
    )
  }

  return (
    <section
      {...marks}
      aria-labelledby={`${id}-name`}
      className={`rounded-lg border border-border bg-surface ${cardMatchClass}`}
    >
      <div className="flex items-start justify-between gap-6 px-4 pt-3.5">
        <div className="min-w-0">
          <h3 id={`${id}-name`} className={`text-sm font-semibold ${headingMatchClass(match)}`}>
            <label htmlFor={`${id}-switch`}>Scripts</label>
          </h3>
          <p id={`${id}-purpose`} className="mt-0.5 text-xs text-muted">
            Run your repositories’ t3.json scripts, VS Code tasks, and magex targets from the Scripts tab.
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <span id={`${id}-caption`} className="text-xs text-muted">
            {switchCaption(set, on, known)}
          </span>
          <Switch
            id={`${id}-switch`}
            checked={on}
            disabled={!known}
            title={known ? undefined : 'Waiting for the tools to be listed'}
            describedBy={joinIds(`${id}-caption`, `${id}-purpose`, switchError !== '' && `${id}-switch-error`)}
            onChange={flip}
          />
        </div>
      </div>
      <div className="flex flex-col gap-1.5 px-4 pt-2.5 pb-3">
        <ErrorText id={`${id}-switch-error`} error={switchError} />
        {set === 'auto' && (
          <p className="text-xs text-muted">Automatic turns it on while mage-x or go-pre-commit is on.</p>
        )}
        <div
          role="group"
          aria-label="Repo scripts lists"
          aria-describedby={joinIds(sourcesError !== '' && `${id}-sources-error`)}
          className="flex flex-wrap gap-x-6 gap-y-1.5"
        >
          {sourceChoices.map((choice) => (
            <label key={choice.key} className="flex cursor-pointer items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={sources[choice.key]}
                onChange={(e) => {
                  choose(choice.key, e.target.checked)
                }}
                className="checkbox"
              />
              {choice.label}
            </label>
          ))}
        </div>
        <ErrorText id={`${id}-sources-error`} error={sourcesError} />
      </div>
    </section>
  )
}
