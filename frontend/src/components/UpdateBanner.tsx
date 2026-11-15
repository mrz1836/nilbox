import { useEffect, useRef, type ReactNode } from 'react'

import { brand } from '../generated/brand'
import { locationNote, type Banner, type Offer } from '../lib/update'
import type { UpdateControl } from '../lib/useUpdate'
import { openExternal, type UpdateStatus } from '../platform'
import { buttonClass, primaryButtonClass } from './controls'
import { AlertIcon, ClockIcon, ExternalIcon, UpdateIcon, XIcon } from './icons'
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
// the page; a failure's longer words wrap, and in a narrow window the buttons wrap under
// the words. A download's progress runs along its foot.
//
// focusFirst hands focus to the first button as the look appears, when nothing else has
// it: a restart that fails, or is refused, after the button or the dialog that started it
// went away, so a keyboard isn't left at the start of the page.
function Shell({
  tone,
  icon,
  children,
  actions,
  progress = null,
  focusFirst = false,
}: {
  tone: 'offer' | 'warning'
  icon: ReactNode
  children: ReactNode
  actions?: ReactNode
  progress?: number | null
  focusFirst?: boolean
}) {
  const toneClass = tone === 'warning' ? 'border-warning' : 'border-border bg-surface'
  const buttons = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const active = document.activeElement

    if (focusFirst && (active === null || active === document.body)) {
      buttons.current?.querySelector('button')?.focus()
    }
  }, [focusFirst])

  return (
    <div
      className={`relative mx-4 mt-4 flex flex-wrap items-start gap-x-2.5 overflow-hidden rounded-md border py-1.5 pr-1.5 pl-3 text-sm ${toneClass}`}
    >
      <span className="flex h-[2.125rem] shrink-0 items-center">{icon}</span>
      <p className="min-w-0 flex-[1_1_16rem] py-[0.4375rem] leading-5">{children}</p>
      {actions !== undefined && (
        <div ref={buttons} className="ml-auto flex min-h-[2.125rem] shrink-0 flex-wrap items-center justify-end gap-1">
          {actions}
        </div>
      )}
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

// DownloadManually opens a version's release page (page), which holds the disk image to
// install by hand.
function DownloadManually({ page }: { page: string }) {
  return (
    <button
      type="button"
      onClick={() => {
        openExternal(page)
      }}
      className={linkButtonClass}
    >
      Download manually
      <ExternalIcon />
    </button>
  )
}

// Later hides the banner, for a day unless hint says otherwise.
function Later({ onLater, hint = 'Hide this for a day' }: { onLater: () => void; hint?: string }) {
  return (
    <button type="button" title={hint} onClick={onLater} className={linkButtonClass}>
      Later
    </button>
  )
}

// OfferButtons are what the update offers (offerOf), as About offers it too: its action,
// with its words, and Download manually.
function OfferButtons({ offer, update }: { offer: Offer; update: UpdateControl }) {
  const plain = offer.action === 'retry'

  return (
    <>
      {offer.action !== null && (
        <button type="button" onClick={update.restart} className={`${plain ? buttonClass : primaryButtonClass} mr-1`}>
          {offer.label}
        </button>
      )}
      {offer.page !== '' && <DownloadManually page={offer.page} />}
    </>
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
  const { status, offer, later, dismiss } = update
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
              <OfferButtons offer={offer} update={update} />
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
      // A download can take a while: Later hides its banner while it goes on.
      return (
        <Shell
          tone="offer"
          icon={<Spinner className="size-4" />}
          progress={banner.progress}
          actions={banner.progress === null ? undefined : <Later onLater={later} hint="Hide this while it downloads" />}
        >
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
    case 'waiting':
      // A moment's wait, not a failure: calm, with nothing to click, and gone by itself.
      return (
        <Shell tone="offer" icon={<ClockIcon className="size-4 text-info" />}>
          {banner.message}
        </Shell>
      )
    case 'failed':
      return (
        <Shell
          tone="warning"
          icon={warningIcon}
          actions={
            <>
              <OfferButtons offer={offer} update={update} />
              <Dismiss onDismiss={dismiss} />
            </>
          }
          focusFirst
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
              <OfferButtons offer={offer} update={update} />
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
// with the download's progress (Later hides a download's), a restart that just failed or
// the one before this launch that didn't install (Try again · Download manually, and
// Details, or Download manually alone for an update that fails the same way every time), a
// moment's wait while the last update finishes, the app quitting on its own to finish an
// update whose quit was held, and an update the app can't install where it runs.
// What it shows is bannerOf's, and what it offers offerOf's (lib/update.ts), which About
// offers too.
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
            <Look key={banner.kind} banner={banner} update={update} onDetails={onDetails} />
          </div>
        </div>
      )}
    </div>
  )
}
