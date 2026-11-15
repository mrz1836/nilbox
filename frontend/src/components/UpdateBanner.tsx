import type { ReactNode } from 'react'

import { brand } from '../generated/brand'
import { latestReleasePage, locationNote, type Banner } from '../lib/update'
import type { UpdateControl } from '../lib/useUpdate'
import { openExternal, type UpdateStatus } from '../platform'
import { buttonClass, primaryButtonClass } from './controls'
import { AlertIcon, ExternalIcon, UpdateIcon, XIcon } from './icons'
import { Spinner } from './Spinner'

// linkButtonClass is a quiet text button in the banner, such as What's new and Later,
// sized so the row lines up with the bordered buttons beside it.
const linkButtonClass =
  'inline-flex items-center gap-1 rounded-md px-2 py-1.5 text-sm font-medium text-muted transition-colors hover:bg-raised hover:text-text [&>svg]:size-3.5'

// closeClass is the banner's close button, as on the app's other notices.
const closeClass =
  'flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text'

export interface UpdateBannerProps {
  update: UpdateControl
  // onDetails opens Settings → About, which says why an update didn't install.
  onDetails: () => void
}

// Shell lays out one look of the banner: the icon, the words, and the buttons, in a row
// as tall as a button whatever it holds, so moving from one look to the next never moves
// the page; a failure's longer words wrap. A download's progress runs along its foot.
function Shell({
  tone,
  icon,
  children,
  actions,
  progress = null,
}: {
  tone: 'offer' | 'warning'
  icon: ReactNode
  children: ReactNode
  actions?: ReactNode
  progress?: number | null
}) {
  const toneClass = tone === 'warning' ? 'border-warning' : 'border-border bg-surface'

  return (
    <div
      className={`relative mx-4 mt-4 flex items-start gap-2.5 overflow-hidden rounded-md border py-1.5 pr-1.5 pl-3 text-sm ${toneClass}`}
    >
      <span className="flex h-[2.125rem] shrink-0 items-center">{icon}</span>
      <p className="min-w-0 flex-1 py-[0.4375rem] leading-5">{children}</p>
      {actions !== undefined && <div className="flex min-h-[2.125rem] shrink-0 items-center gap-1">{actions}</div>}
      {progress !== null && (
        <div
          role="progressbar"
          aria-label="Download"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progress}
          className="absolute inset-x-0 bottom-0 h-0.5 bg-accent/15"
        >
          <div
            className="h-full bg-accent transition-[width] duration-300 ease-out motion-reduce:transition-none"
            style={{ width: `${String(progress)}%` }}
          />
        </div>
      )}
    </div>
  )
}

// WhatsNew opens the newer version's release page.
function WhatsNew({ status }: { status: UpdateStatus | null }) {
  return (
    <button
      type="button"
      onClick={() => {
        openExternal(status?.releaseUrl ?? '')
      }}
      className={linkButtonClass}
    >
      What’s new
      <ExternalIcon />
    </button>
  )
}

// DownloadManually opens the newest release's page, which holds the disk image to install
// by hand.
function DownloadManually({ status }: { status: UpdateStatus | null }) {
  return (
    <button
      type="button"
      onClick={() => {
        openExternal(latestReleasePage(status?.releaseUrl ?? ''))
      }}
      className={linkButtonClass}
    >
      Download manually
      <ExternalIcon />
    </button>
  )
}

// Later hides the banner for a day.
function Later({ onLater }: { onLater: () => void }) {
  return (
    <button type="button" title="Hide this for a day" onClick={onLater} className={linkButtonClass}>
      Later
    </button>
  )
}

// Dismiss puts a failure away for this session.
function Dismiss({ onDismiss }: { onDismiss: () => void }) {
  return (
    <button type="button" aria-label="Dismiss" title="Dismiss" onClick={onDismiss} className={closeClass}>
      <XIcon className="size-3.5" />
    </button>
  )
}

// Look is the banner's look for what it shows.
function Look({ banner, update, onDetails }: { banner: Exclude<Banner, { kind: 'none' }> } & UpdateBannerProps) {
  const { status, restart, later, dismiss } = update
  const offerIcon = <UpdateIcon className="size-4 text-accent" />
  const warningIcon = <AlertIcon className="size-4 text-warning" />

  switch (banner.kind) {
    case 'ready':
    case 'available':
      return (
        <Shell
          tone="offer"
          icon={offerIcon}
          actions={
            <>
              <button type="button" onClick={restart} className={`${primaryButtonClass} mr-1`}>
                {banner.kind === 'ready' ? 'Restart to update' : 'Update'}
              </button>
              <WhatsNew status={status} />
              <Later onLater={later} />
            </>
          }
        >
          <span className="font-medium">
            {brand.name} {banner.version} is {banner.kind}.
          </span>
        </Shell>
      )
    case 'location':
      return (
        <Shell
          tone="offer"
          icon={offerIcon}
          actions={
            <>
              <WhatsNew status={status} />
              <Later onLater={later} />
            </>
          }
        >
          <span className="font-medium">
            {brand.name} {banner.version} is {banner.ready ? 'ready' : 'available'}.
          </span>{' '}
          {status !== null && <span className="text-muted">{locationNote(status.location)}</span>}
        </Shell>
      )
    case 'working':
      return (
        <Shell tone="offer" icon={<Spinner className="size-4" />} progress={banner.progress}>
          {banner.text}
          {banner.progress !== null && (
            // The live region reads the words once; the bar says how far it has got.
            <span aria-hidden="true" className="text-muted tabular-nums">
              {' '}
              {banner.progress}%
            </span>
          )}
          {banner.detail !== '' && <span className="text-muted"> {banner.detail}</span>}
        </Shell>
      )
    case 'failed':
      return (
        <Shell
          tone="warning"
          icon={warningIcon}
          actions={
            <>
              <button type="button" onClick={restart} className={`${buttonClass} mr-1`}>
                Try again
              </button>
              <DownloadManually status={status} />
              <Dismiss onDismiss={dismiss} />
            </>
          }
        >
          {banner.message}
        </Shell>
      )
    case 'lastLaunch':
      return (
        <Shell
          tone="warning"
          icon={warningIcon}
          actions={
            <>
              <button type="button" onClick={restart} className={`${buttonClass} mr-1`}>
                Try again
              </button>
              <DownloadManually status={status} />
              <button type="button" onClick={onDetails} className={linkButtonClass}>
                Details
              </button>
              <Dismiss onDismiss={dismiss} />
            </>
          }
        >
          The update to {banner.version} didn’t install, so {brand.name} is still on {banner.current}.
        </Shell>
      )
  }
}

// UpdateBanner tells the user about the app's own update, above the page, where the data
// notice shows: a version ready to restart into (Restart to update · What's new · Later),
// one available to download and restart into as one click (Update), the restart under way
// with the download's progress, a restart that just failed or the one before this launch
// that didn't install (Try again · Download manually, and Details), and an update the app
// can't install where it runs. What it shows is bannerOf's (lib/update.ts).
//
// The live region stays mounted, so each change is announced, politely; a download's
// percentage is left out of it, so it isn't read out as it climbs. The banner makes its
// room gently as it appears, and at once with reduced motion.
export function UpdateBanner({ update, onDetails }: UpdateBannerProps) {
  const { banner } = update

  return (
    <div role="status" aria-live="polite" aria-label={`${brand.name} update`}>
      {banner.kind !== 'none' && (
        <div className="grid grid-rows-[1fr] motion-safe:animate-notice-in">
          <div className="min-h-0 overflow-hidden">
            <Look banner={banner} update={update} onDetails={onDetails} />
          </div>
        </div>
      )}
    </div>
  )
}
