import { useState, type RefObject } from 'react'

import { brand } from '../../generated/brand'
import { backupFiles } from '../../lib/backup'
import { attempt, type Result } from '../../lib/call'
import { pickFileToOpen } from '../../lib/dialogs'
import { cameBack } from '../../lib/restored'
import { welcomeText } from '../../lib/setup'
import type { Theme } from '../../lib/theme'
import { getSettings, type Info, type Settings } from '../../platform'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../controls'
import { ChevronRightIcon, ImportIcon } from '../icons'
import { LogoMark } from '../Logo'
import { ImportBackupDialog } from '../settings/ImportBackupDialog'
import { ThemePicker } from './ThemePicker'

export interface WelcomeStepProps {
  // headingId and headingRef are the setup's heading, which this step draws.
  headingId: string
  headingRef: RefObject<HTMLHeadingElement | null>
  // info says what happened to the data at startup, null until it loads; rerun is a setup
  // run again from Settings; tools whether the Tools step shows.
  info: Info | null
  rerun: boolean
  tools: boolean
  theme: Theme
  onThemeChange: (theme: Theme) => Promise<Result<void>>
  onStart: () => void
  onSkip: () => void
  // onRestored hands on the settings a backup brought back, once the window read them
  // again, and resolves once the prompts and launch defaults are read again too.
  onRestored: (settings: Settings) => Promise<void>
  // onReview walks the steps after a backup came back, and onFinishNow goes to Finish.
  onReview: () => void
  onFinishNow: () => void
}

// capitalized is text with its first letter upper case.
function capitalized(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

// Variant is how Welcome greets the user: a new install, a fresh start they asked for, a
// fresh start the app had to make, or a setup run again.
type Variant = 'new' | 'fresh' | 'damaged' | 'rerun'

// variantOf is the greeting for info and rerun.
function variantOf(info: Info | null, rerun: boolean): Variant {
  if (rerun) {
    return 'rerun'
  }

  const reason = info?.dataReset?.reason

  if (reason === 'requested') {
    return 'fresh'
  }

  return reason === undefined ? 'new' : 'damaged'
}

// titles are each greeting's heading.
const titles: Readonly<Record<Variant, string>> = {
  new: `Welcome to ${brand.name}`,
  fresh: 'Fresh start',
  damaged: `Welcome back to ${brand.name}`,
  rerun: 'Welcome back',
}

// Greeting says what the greeting adds beneath the heading: what came back after a reset,
// what happened to data that couldn't be read, or that a setup run again shows what is set.
function Greeting({ variant, info }: { variant: Variant; info: Info | null }) {
  const restored = info?.restored ?? null

  switch (variant) {
    case 'fresh':
      return (
        <p className="mt-3 text-sm text-pretty text-muted">
          {restored === null
            ? 'Everything starts fresh.'
            : `${capitalized(cameBack(restored))} came back. Everything else starts fresh.`}
        </p>
      )
    case 'damaged':
      return (
        <p className="mt-3 text-sm text-pretty text-muted">
          {brand.name} couldn’t read its data, so it set it aside at{' '}
          <code className="font-mono text-xs break-all">{info?.dataReset?.path}</code> and started fresh.
          {restored !== null && ` It brought back ${cameBack(restored)}.`}
        </p>
      )
    case 'rerun':
      return (
        <p className="mt-3 text-sm text-pretty text-muted">
          Every step shows what is set already, so this doubles as a check of your setup.
        </p>
      )
    case 'new':
      return null
  }
}

// WelcomeStep opens the setup: the mark, pulsing, a greeting, what lies ahead, a theme
// picker to start with, and Let’s go; a backup to restore, as on a new Mac; and Skip setup.
// Once a backup is back, it offers to walk the steps anyway or to finish now.
export function WelcomeStep({
  headingId,
  headingRef,
  info,
  rerun,
  tools,
  theme,
  onThemeChange,
  onStart,
  onSkip,
  onRestored,
  onReview,
  onFinishNow,
}: WelcomeStepProps) {
  const variant = variantOf(info, rerun)
  // opened is the backup the import dialog shows, or ''; restored is set once one came
  // back, and failure says why the file dialog couldn't open.
  const [opened, setOpened] = useState('')
  const [restored, setRestored] = useState(false)
  const [failure, setFailure] = useState('')

  const open = async () => {
    setFailure('')

    const chosen = await pickFileToOpen({ title: 'Restore from a backup', button: 'Open', filters: backupFiles })

    if (!chosen.ok) {
      setFailure(chosen.error)
    } else if (chosen.data !== '') {
      setOpened(chosen.data)
    }
  }

  const imported = async () => {
    setOpened('')

    const reread = await attempt(getSettings())

    if (reread.ok) {
      if (reread.data.theme !== theme) {
        void onThemeChange(reread.data.theme)
      }

      await onRestored(reread.data)
    }

    setRestored(true)
  }

  return (
    <section className="flex flex-col items-center text-center">
      <span aria-hidden="true" className="nil-mark relative grid place-items-center">
        <span className="nil-ring" />
        <span className="nil-ring nil-ring-late" />
        <LogoMark className="relative h-16 w-auto" />
      </span>

      <h2
        ref={headingRef}
        id={headingId}
        tabIndex={-1}
        className="nil-rise mt-8 text-3xl font-bold tracking-tight outline-none"
      >
        {titles[variant]}
      </h2>
      <p className="nil-rise mt-2 text-base text-muted" style={{ animationDelay: '100ms' }}>
        {brand.tagline}
      </p>
      <Greeting variant={variant} info={info} />

      {restored ? (
        <div className="nil-rise mt-8 flex w-full max-w-md flex-col items-center gap-4 rounded-lg border border-success/50 bg-success/10 px-5 py-4">
          <p role="status" className="text-sm font-medium">
            Your setup is back: review the steps, or finish now.
          </p>
          <div className="flex gap-2">
            <button type="button" onClick={onReview} className={buttonClass}>
              Review the steps
            </button>
            <button type="button" onClick={onFinishNow} className={primaryButtonClass}>
              Finish now
              <ChevronRightIcon className="size-3.5" />
            </button>
          </div>
        </div>
      ) : (
        <>
          <p className="nil-rise mt-6 font-mono text-xs tracking-wide text-muted" style={{ animationDelay: '180ms' }}>
            {welcomeText(tools)}
          </p>

          <div className="nil-rise mt-6 w-full max-w-md" style={{ animationDelay: '240ms' }}>
            <ThemePicker theme={theme} onThemeChange={onThemeChange} />
          </div>

          <div
            className="nil-rise mt-8 flex flex-wrap items-center justify-center gap-3"
            style={{ animationDelay: '320ms' }}
          >
            <button
              type="button"
              onClick={() => {
                void open()
              }}
              className={buttonClass}
            >
              <ImportIcon className="size-3.5 text-muted" />
              Restore from a backup…
            </button>
            <button type="button" onClick={onStart} className={`${primaryButtonClass} px-5 py-2 text-base`}>
              Let’s go
              <ChevronRightIcon className="size-4" />
            </button>
          </div>
          <button type="button" onClick={onSkip} className={`${quietButtonClass} mt-4`}>
            Skip setup
          </button>
          {failure !== '' && (
            <p role="alert" className="mt-3 text-xs text-danger">
              {failure}
            </p>
          )}
        </>
      )}

      {opened !== '' && (
        <ImportBackupDialog
          path={opened}
          onCancel={() => {
            setOpened('')
          }}
          onImported={() => {
            void imported()
          }}
        />
      )}
    </section>
  )
}
