import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { toolOf, usable } from '../../lib/scriptsTab'
import { terminalNames } from '../../lib/terminals'
import { useIntegrations } from '../../lib/useIntegrations'
import { useSettingsSave } from '../../lib/useSettingsSave'
import { setOfferAfterMerge, setRunsIn, type RunsIn, type Settings } from '../../platform'
import { buttonClass, quietButtonClass, Switch } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { RefreshIcon } from '../icons'
import { IntegrationCard } from './IntegrationCard'
import { rowMatchClass, useSetting } from './match'
import { ErrorText, Group, joinIds, rowDescribedBy, SettingRow } from './rows'
import type { SectionProps } from './types'

// RunsGroup holds where the runs go: in the app, or in the terminal chosen in Settings →
// Launching, named as it is set there; and whether a merge in the review sheet offers to
// release its repository, which needs mage-x usable. Each is saved as soon as it is
// chosen.
function RunsGroup({ settings: stored, onSaved, releases }: SectionProps & { releases: boolean }) {
  const id = useId()
  const { settings, save, error: saveError } = useSettingsSave(stored, onSaved)
  // changed is the setting changed last, beside which its refusal shows.
  const [changed, setChanged] = useState<'runsIn' | 'offer'>('runsIn')
  const error = changed === 'runsIn' ? saveError : ''
  const offerError = changed === 'offer' ? saveError : ''
  const [marks] = useSetting('Runs in')
  const choices: readonly { value: RunsIn; label: string }[] = [
    { value: 'app', label: brand.name },
    { value: 'terminal', label: `Terminal: ${terminalNames[settings.terminal]}, as set in Launching` },
  ]

  const choose = (runsIn: RunsIn) => {
    setChanged('runsIn')
    void save({ integrations: { ...settings.integrations, runsIn } }, () => setRunsIn(runsIn))
  }

  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-sm font-semibold">Runs</h3>
      <Group>
        <div {...marks} className={`flex items-start gap-8 px-4 py-3.5 ${rowMatchClass}`}>
          <p id={`${id}-label`} className="w-16 shrink-0 text-sm font-medium">
            Runs in
          </p>
          <div className="flex min-w-0 flex-col gap-2">
            <div
              role="radiogroup"
              aria-labelledby={`${id}-label`}
              aria-describedby={joinIds(`${id}-note`, error !== '' && `${id}-error`)}
              className="flex flex-wrap gap-x-6 gap-y-2"
            >
              {choices.map((choice) => (
                <label key={choice.value} className="flex cursor-pointer items-center gap-2 text-sm">
                  <input
                    type="radio"
                    name={`${id}-runs-in`}
                    value={choice.value}
                    checked={settings.integrations.runsIn === choice.value}
                    onChange={() => {
                      choose(choice.value)
                    }}
                    className="radio"
                  />
                  {choice.label}
                </label>
              ))}
            </div>
            <p id={`${id}-note`} className="text-xs text-muted">
              Runs in your terminal can ask for a passphrase. {brand.name} follows both.
            </p>
            <ErrorText id={`${id}-error`} error={error} />
          </div>
        </div>
        <SettingRow
          label="Offer a release after you merge a pull request"
          controlId={`${id}-offer`}
          description={
            releases
              ? 'The toast that says a pull request merged offers to release its repository. It never releases by itself.'
              : 'Needs mage-x: turn it on above, and make sure it can be used.'
          }
          error={offerError}
        >
          <Switch
            id={`${id}-offer`}
            checked={settings.integrations.offerAfterMerge}
            disabled={!releases}
            describedBy={rowDescribedBy(`${id}-offer`, offerError)}
            onChange={(offerAfterMerge) => {
              setChanged('offer')
              void save({ integrations: { ...settings.integrations, offerAfterMerge } }, () =>
                setOfferAfterMerge(offerAfterMerge),
              )
            }}
          />
        </SettingRow>
      </Group>
    </div>
  )
}

export interface IntegrationsSectionProps {
  // settings is null until it has loaded; the Runs group waits for it.
  settings: Settings | null
  // onSaved receives every setting as stored after a change, as the setter returned it.
  onSaved: (settings: Settings) => void
}

// IntegrationsSection is the settings page's Integrations section: the tools the app runs
// for the user, one card each, as the last look for them found them, with a way to look
// again, then where their runs go. It says so when the login shell's environment couldn't
// be read, since the tools are then looked for, and run, with a basic PATH.
export function IntegrationsSection({ settings, onSaved }: IntegrationsSectionProps) {
  const { tools, error, checking, checkError, checkAgain, reload, replace } = useIntegrations()
  const shellFailed = tools !== null && tools.shellChecked && !tools.shellRead

  const check = () => {
    void checkAgain()
  }

  return (
    <div className="flex flex-col gap-5">
      {shellFailed && (
        <div role="note" className="flex items-center justify-between gap-4 rounded-md border border-warning px-3 py-2">
          <p className="text-xs text-warning">
            Couldn’t read your shell’s environment, so runs use a basic PATH, and your ssh agent may be missing.
          </p>
          <button type="button" disabled={checking} onClick={check} className={`${quietButtonClass} shrink-0`}>
            Check again
          </button>
        </div>
      )}
      <div className="flex flex-col gap-1">
        <div className="flex items-start justify-between gap-6">
          <p className="text-sm text-muted">
            Tools {brand.name} runs for you. Each one is found on this Mac, or installed from its GitHub release.{' '}
            {brand.name} never uses go install or sudo.
          </p>
          <button type="button" disabled={checking} onClick={check} className={`${buttonClass} shrink-0`}>
            <RefreshIcon className={`size-3.5 text-muted ${checking ? 'motion-safe:animate-spin' : ''}`} />
            {checking ? 'Checking…' : 'Check again'}
          </button>
        </div>
        <ErrorText error={checkError} />
      </div>
      {error !== '' && <ErrorBanner>Couldn’t list the tools: {error}</ErrorBanner>}
      {tools === null
        ? error === '' && <p className="text-sm text-muted">Looking for the tools…</p>
        : tools.tools.map((tool) => (
            <IntegrationCard
              key={tool.id}
              tool={tool}
              checking={checking}
              onSaved={onSaved}
              reload={reload}
              onTools={replace}
              terminal={settings?.terminal ?? null}
            />
          ))}
      {settings !== null && (
        <RunsGroup settings={settings} onSaved={onSaved} releases={usable(toolOf(tools, 'mage-x'))} />
      )}
    </div>
  )
}
