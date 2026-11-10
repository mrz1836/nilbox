import { brand } from '../../generated/brand'
import { useIntegrations } from '../../lib/useIntegrations'
import type { Settings } from '../../platform'
import { buttonClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { RefreshIcon } from '../icons'
import { IntegrationCard } from './IntegrationCard'
import { ErrorText } from './rows'

export interface IntegrationsSectionProps {
  // onSaved receives every setting as stored after a change, as the setter returned it.
  onSaved: (settings: Settings) => void
}

// IntegrationsSection is the settings page's Integrations section: the tools the app runs
// for the user, one card each, as the last look for them found them, with a way to look
// again. It says so when the login shell's environment couldn't be read, since the tools
// are then looked for, and run, with a basic PATH.
export function IntegrationsSection({ onSaved }: IntegrationsSectionProps) {
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
            />
          ))}
    </div>
  )
}
