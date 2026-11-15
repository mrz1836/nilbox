import { useId } from 'react'

import { headline, phrase, statusLabel, toneFill, toneOf, toneText, troubled } from '../lib/githubStatus'
import type { GitHubStatusState } from '../lib/useGitHubStatus'
import { usePopover } from '../lib/usePopover'
import { useNowAs } from '../lib/useNow'
import { formatAgo } from '../lib/time'
import { openExternal } from '../platform'
import { buttonClass, captionClass } from './controls'
import { AlertIcon, ExternalIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'

export interface GitHubStatusMenuProps {
  github: GitHubStatusState
}

// GitHubStatusMenu is the toolbar's warning that GitHub has an incident, as its own
// status page says: shown only while it does, amber for a minor incident or maintenance
// and red for an outage, so the inbox, reviews, or checks lagging or failing has a reason
// in sight. A click opens what the page says: the incidents, each a link to its page, the
// parts of GitHub affected, when it was checked, Check again, and the page itself.
export function GitHubStatusMenu({ github }: GitHubStatusMenuProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, triggerRef } = popover
  const { status, checking, check } = github
  const checkedAt = status?.checkedAt ?? ''
  const checked = useNowAs((now) => (checkedAt === '' ? '' : formatAgo(now, checkedAt)))

  if (!troubled(status)) {
    return null
  }

  const tone = toneOf(status.state) ?? 'warning'
  const label = statusLabel(status)

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
              GitHub status
            </h2>
            <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${toneText[tone]}`}>
              <span aria-hidden="true" className={`size-1.5 rounded-full ${toneFill[tone]}`} />
              {headline(status)}
            </span>
          </div>
          <p className="px-4 pt-0.5 text-xs text-muted">
            GitHub reports a problem, so the inbox, reviews, and checks may lag or fail.
            {checked !== '' && ` Checked ${checked}.`}
          </p>
          {status.incidents.length > 0 && (
            <section aria-labelledby={`${id}-incidents`} className="px-4 pt-3">
              <h3 id={`${id}-incidents`} className={captionClass}>
                Incidents
              </h3>
              <ul className="mt-1.5 flex flex-col gap-2">
                {status.incidents.map((incident) => (
                  <li key={incident.url}>
                    <button
                      type="button"
                      onClick={() => {
                        openExternal(incident.url)
                      }}
                      className="text-left text-sm font-medium hover:underline"
                    >
                      {incident.name}
                    </button>
                    {incident.status !== '' && <p className="text-xs text-muted">{phrase(incident.status)}</p>}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {status.components.length > 0 && (
            <section aria-labelledby={`${id}-affected`} className="px-4 pt-3">
              <h3 id={`${id}-affected`} className={captionClass}>
                Affected
              </h3>
              <ul className="mt-1.5 flex flex-col gap-1">
                {status.components.map((component) => (
                  <li key={component.name} className="flex items-baseline justify-between gap-4 text-sm">
                    <span className="min-w-0 truncate">{component.name}</span>
                    <span className="shrink-0 text-xs text-muted">{phrase(component.status)}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {status.error !== '' && (
            <p role="status" className="px-4 pt-3 text-xs text-warning">
              {status.error} This is the last reading.
            </p>
          )}
          <div className="mt-3.5 flex items-center justify-end gap-2 border-t border-border px-4 py-2.5">
            <button type="button" disabled={checking} onClick={check} className={buttonClass}>
              {checking ? 'Checking…' : 'Check again'}
            </button>
            <button
              type="button"
              onClick={() => {
                openExternal(status.url)
              }}
              className={buttonClass}
            >
              Status page
              <ExternalIcon className="size-3.5 text-muted" />
            </button>
          </div>
        </div>
      </PopoverPanel>
    </div>
  )
}
