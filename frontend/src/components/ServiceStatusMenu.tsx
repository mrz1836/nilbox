import { useId } from 'react'

import {
  affectsWords,
  othersWords,
  stageLabel,
  stateLabel,
  statusLabel,
  toneFill,
  toneOf,
  toneText,
  troubled,
  worst,
  type Tone,
} from '../lib/serviceStatus'
import type { ServiceStatusState } from '../lib/useServiceStatus'
import { usePopover } from '../lib/usePopover'
import { useNowAs } from '../lib/useNow'
import { formatAgo } from '../lib/time'
import { openExternal, type ServiceProvider } from '../platform'
import { buttonClass } from './controls'
import { AlertIcon, ExternalIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'

export interface ServiceStatusMenuProps {
  service: ServiceStatusState
  // onChooseWatch opens Settings → Service status, where the user chooses what to watch;
  // without it the popover leaves the link out.
  onChooseWatch?: (() => void) | undefined
}

// StateBadge is a state in the window's words, with its tone's dot.
function StateBadge({ tone, children }: { tone: Tone; children: string }) {
  return (
    <span className={`inline-flex shrink-0 items-center gap-1.5 text-xs font-medium ${toneText[tone]}`}>
      <span aria-hidden="true" className={`size-1.5 rounded-full ${toneFill[tone]}`} />
      {children}
    </span>
  )
}

// ProviderCard is what one provider in trouble says: its state, the parts watched it
// affects, the incidents that touch them, each a link to its page, how many others its page
// lists, its status page, and why its last check failed, if it did.
function ProviderCard({ provider }: { provider: ServiceProvider }) {
  const id = useId()
  const tone = toneOf(provider.state) ?? 'warning'
  const affects = affectsWords(provider)
  const others = othersWords(provider.others)

  return (
    <section aria-labelledby={`${id}-name`} className="flex flex-col gap-1.5 border-t border-border px-4 py-3">
      <div className="flex items-center justify-between gap-4">
        <h3 id={`${id}-name`} className="text-sm font-semibold">
          {provider.name}
        </h3>
        <StateBadge tone={tone}>{stateLabel(provider.state)}</StateBadge>
      </div>
      {affects !== '' && <p className="text-xs text-muted">{affects}</p>}
      {provider.incidents.length > 0 && (
        <ul aria-label={`${provider.name} incidents`} className="flex flex-col gap-1.5">
          {provider.incidents.map((incident) => {
            const stage = stageLabel(incident.stage)

            return (
              <li key={`${incident.url} ${incident.name}`}>
                <button
                  type="button"
                  onClick={() => {
                    openExternal(incident.url)
                  }}
                  className="text-left text-sm font-medium hover:underline"
                >
                  {incident.name}
                </button>
                {(stage !== '' || incident.maintenance) && (
                  <p className="text-xs text-muted">
                    {[incident.maintenance ? 'Maintenance' : '', stage].filter((w) => w !== '').join(' · ')}
                  </p>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {provider.error !== '' && (
        <p role="status" className="text-xs text-warning">
          {provider.error} This is the last reading.
        </p>
      )}
      <p className="flex flex-wrap items-center gap-x-1.5 text-xs text-muted">
        {others !== '' && (
          <>
            <span>{others}</span>
            <span aria-hidden="true">·</span>
          </>
        )}
        <button
          type="button"
          onClick={() => {
            openExternal(provider.url)
          }}
          className="inline-flex items-center gap-1 hover:text-text hover:underline"
        >
          Status page
          <ExternalIcon className="size-3" />
        </button>
      </p>
    </section>
  )
}

// ServiceStatusMenu is the toolbar's warning that a service the user watches has an
// incident (GitHub, Claude, or OpenAI's Codex), as its own status page says: shown only
// while one does, amber for degraded performance or maintenance and red for an outage,
// so the inbox, reviews, checks, or agents lagging or failing has a reason in sight. Only
// the parts the user ticked count. A click opens one card per provider in trouble, when
// the pages were checked, Check again, and a way to choose what to watch.
export function ServiceStatusMenu({ service, onChooseWatch }: ServiceStatusMenuProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, close, triggerRef } = popover
  const { status, checking, check } = service
  const checkedAt = status?.checkedAt ?? ''
  const checked = useNowAs((now) => (checkedAt === '' ? '' : formatAgo(now, checkedAt)))
  const providers = troubled(status)

  if (providers.length === 0) {
    return null
  }

  const state = worst(providers)
  const tone = toneOf(state) ?? 'warning'
  const label = statusLabel(providers)

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={`flex size-8 items-center justify-center rounded-md transition-colors hover:bg-raised aria-expanded:bg-raised ${toneText[tone]}`}
      >
        <AlertIcon className="size-4" />
      </button>
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        className="z-40 w-80 shadow-black/20 outline-none"
      >
        <div className="flex flex-col">
          <div className="flex items-center justify-between gap-4 px-4 pt-3.5">
            <h2 id={`${id}-title`} className="text-sm font-semibold">
              Service status
            </h2>
            <StateBadge tone={tone}>{stateLabel(state)}</StateBadge>
          </div>
          <p className="px-4 pt-0.5 pb-3 text-xs text-muted">
            A part you watch reports a problem, so the inbox, reviews, checks, or agents may lag or fail.
          </p>
          {providers.map((provider) => (
            <ProviderCard key={provider.id} provider={provider} />
          ))}
          <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-2.5">
            <span className="text-xs text-muted">{checked === '' ? '' : `Checked ${checked}`}</span>
            <div className="flex items-center gap-2">
              <button type="button" disabled={checking} onClick={check} className={buttonClass}>
                {checking ? 'Checking…' : 'Check again'}
              </button>
              {onChooseWatch !== undefined && (
                <button
                  type="button"
                  onClick={() => {
                    close()
                    onChooseWatch()
                  }}
                  className={buttonClass}
                >
                  Choose what to watch
                </button>
              )}
            </div>
          </div>
        </div>
      </PopoverPanel>
    </div>
  )
}
