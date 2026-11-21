import { useEffect, useEffectEvent, useRef } from 'react'

import { logFailure } from '../../lib/call'
import { toolsReason } from '../../lib/setup'
import type { IntegrationsState } from '../../lib/useIntegrations'
import { setIntegrationsAnnounced, type Settings, type SetupSignals } from '../../platform'
import { ErrorBanner } from '../ErrorBanner'
import { IntegrationCard } from '../settings/IntegrationCard'
import { Spinner } from '../Spinner'

export interface ToolsStepProps {
  // settings is null until it loads.
  settings: Settings | null
  onSaved: (settings: Settings) => void
  integrations: IntegrationsState
  // signals is what the user's clones say about the tools, null until counted.
  signals: SetupSignals | null
}

// ToolsStep offers the tools the app drives, when they fit: why they are suggested, and
// each tool's card as Settings → Integrations shows it, to install, update, or look at.
// It is always skippable. Reaching it counts as the tools being announced, so the toast
// that would announce the Scripts tab never shows on top of what the setup just said.
export function ToolsStep({ settings, onSaved, integrations, signals }: ToolsStepProps) {
  const { tools, error, checking, reload, replace } = integrations
  const reason = toolsReason(tools, signals)

  const announced = useRef(false)
  const announce = useEffectEvent(() => {
    if (announced.current || settings === null || settings.integrations.announced) {
      return
    }

    announced.current = true
    logFailure(setIntegrationsAnnounced(true), 'could not remember that the tools were announced:', onSaved)
  })

  useEffect(() => {
    announce()
  }, [settings])

  return (
    <div className="flex flex-col gap-4">
      {reason !== '' && (
        <p className="rounded-md border border-accent/45 bg-accent/5 px-3 py-2 text-sm text-text">{reason}</p>
      )}
      {error !== '' && <ErrorBanner>Couldn’t list the tools: {error}</ErrorBanner>}
      {tools === null
        ? error === '' && (
            <p role="status" className="flex items-center gap-2 text-sm text-muted">
              <Spinner />
              Looking for your tools…
            </p>
          )
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
      <p className="text-xs text-muted">
        Not now? Skip: every tool waits in Settings → Integrations, and the Scripts tab appears once one is on.
      </p>
    </div>
  )
}
