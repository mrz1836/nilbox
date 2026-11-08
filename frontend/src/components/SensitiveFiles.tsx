import { useId, useLayoutEffect, useState } from 'react'

import { formatAgo } from '../lib/age'
import { counted, errorMessage, formatCount } from '../lib/format'
import {
  changeText,
  changeTone,
  filesChip,
  filesOf,
  groupsOf,
  sensitiveCount,
  shortHead,
  type FilesChip,
} from '../lib/sensitive'
import { toneText } from '../lib/signals'
import { usePopover } from '../lib/usePopover'
import {
  checkItemFiles,
  openInBrowser,
  type FileCheck,
  type SensitiveFile,
  type SensitiveGroup,
  type SensitiveLevel,
} from '../platform'
import { quietButtonClass } from './controls'
import { ExternalIcon, RefreshIcon, ShieldAlertIcon, ShieldCheckIcon } from './icons'

export interface SensitiveFilesProps {
  // itemId is the pull request the check is for, for checking it again.
  itemId: number
  check: FileCheck
  now: Date
  // onSettings opens Settings → Sensitive files; without it, the panel names the page.
  onSettings?: () => void
}

// chipClass is each tone's chip: a tinted pill for a finding, a bare word otherwise.
const chipClass: Readonly<Record<FilesChip['tone'], string>> = {
  danger: 'rounded-full border border-danger/45 bg-danger/10 px-1.5 text-danger hover:bg-danger/18',
  warning: 'rounded-full border border-warning/45 bg-warning/10 px-1.5 text-warning hover:bg-warning/18',
  success: 'rounded-full px-1 text-success hover:bg-raised',
  muted: 'rounded-full px-1 text-muted hover:bg-raised hover:text-text',
}

// panelGap is the room kept between the panel and the edge of the area it scrolls in, and
// minPanelHeight the least height it is given however little room there is.
const panelGap = 12
const minPanelHeight = 160

// scrollBounds returns the top and bottom of the nearest ancestor that scrolls, which clips
// the panel, or of the window.
function scrollBounds(el: HTMLElement): { top: number; bottom: number } {
  for (let node = el.parentElement; node !== null; node = node.parentElement) {
    const overflow = getComputedStyle(node).overflowY

    if (overflow === 'auto' || overflow === 'scroll') {
      const rect = node.getBoundingClientRect()

      return { top: rect.top, bottom: rect.bottom }
    }
  }

  return { top: 0, bottom: window.innerHeight }
}

// levelDot is a category's level, as a dot beside its name.
const levelDot: Readonly<Record<SensitiveLevel, string>> = {
  danger: 'bg-danger',
  warning: 'bg-warning',
  '': 'bg-muted',
}

function openDiff(url: string) {
  openInBrowser(url).catch((err: unknown) => {
    console.error('could not open the file in the browser', err)
  })
}

// FileRow is one sensitive file: its path, which opens its diff on GitHub, and how it
// changed.
function FileRow({ file }: { file: SensitiveFile }) {
  const change = changeText(file)

  return (
    <li className="flex min-w-0 items-baseline gap-2 py-0.5">
      {file.url === '' ? (
        <span className="min-w-0 truncate font-mono text-xs text-text" title={file.path}>
          {file.path}
        </span>
      ) : (
        <button
          type="button"
          title={`Open the diff of ${file.path} on GitHub`}
          onClick={() => {
            openDiff(file.url)
          }}
          className="group/file inline-flex min-w-0 items-baseline gap-1 text-left font-mono text-xs text-text hover:text-accent hover:underline"
        >
          <span className="truncate">{file.path}</span>
          <ExternalIcon className="size-3 shrink-0 self-center text-muted group-hover/file:text-accent" />
        </button>
      )}
      <span
        title={change}
        className={`ml-auto min-w-0 shrink truncate text-[11px] ${toneText[changeTone(file.change)]}`}
      >
        {change}
      </span>
    </li>
  )
}

// GroupSection is one category's files, with why they deserve a look.
function GroupSection({ group }: { group: SensitiveGroup }) {
  const files = filesOf(group)

  return (
    <section aria-label={group.label} className="flex flex-col gap-1">
      <h4 className="flex items-center gap-1.5 text-xs font-semibold text-text">
        <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${levelDot[group.level]}`} />
        {group.label}
        <span className="font-normal text-muted">{formatCount(files.length)}</span>
      </h4>
      <p className="text-[11px] leading-snug text-muted">{group.description}</p>
      <ul className="mt-0.5 flex flex-col">
        {files.map((file) => (
          <FileRow key={file.path} file={file} />
        ))}
      </ul>
    </section>
  )
}

// summary is the panel's opening sentence for a check without findings, or for the
// files left unchecked.
function summary(check: FileCheck): string {
  switch (check.status) {
    case 'failed':
      return check.error === ''
        ? 'Couldn’t check this pull request’s files. The next refresh tries again.'
        : `Couldn’t check this pull request’s files: ${check.error}. The next refresh tries again.`
    case 'pending':
      return 'Not checked since its last push. The next refresh checks it, or check it now.'
    case 'partial':
    case 'clear':
      return `None of its ${counted(check.checked, 'checked file', 'checked files')} is sensitive.`
    default:
      return `${counted(sensitiveCount(check), 'file', 'files')} here deserve a close look before you review or open a session.`
  }
}

// SensitiveFiles is a pull request's sensitive-file chip: the categories of the sensitive
// files it changes, or that its files were checked and none is, or that they could not
// all be checked. It opens a panel listing each file by category, each opening its diff
// on GitHub, with a button to check the files again now.
export function SensitiveFiles({ itemId, check, now, onSettings }: SensitiveFilesProps) {
  const id = useId()
  const { open, toggle, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState('')
  const [above, setAbove] = useState(false)
  const [maxHeight, setMaxHeight] = useState<number | undefined>(undefined)

  const chip = filesChip(check)
  const groups = groupsOf(check)
  const agent = groups.some((g) => g.category === 'agent')
  const unchecked = !check.complete && check.status !== 'failed' && check.status !== 'pending'

  // Open the panel upward when it fits better above the chip, and keep it within the area
  // it scrolls in, scrolling inside itself when the files run longer.
  useLayoutEffect(() => {
    const trigger = triggerRef.current
    const panel = panelRef.current

    if (!open || trigger === null || panel === null) {
      return
    }

    const bounds = scrollBounds(trigger)
    const rect = trigger.getBoundingClientRect()
    const spaceBelow = bounds.bottom - rect.bottom - panelGap
    const spaceAbove = rect.top - bounds.top - panelGap
    const up = panel.scrollHeight > spaceBelow && spaceAbove > spaceBelow

    setAbove(up)
    setMaxHeight(Math.max(minPanelHeight, up ? spaceAbove : spaceBelow))
  }, [open, triggerRef, panelRef])

  const checkNow = () => {
    setChecking(true)
    setError('')

    checkItemFiles(itemId)
      .catch((err: unknown) => {
        setError(errorMessage(err))
      })
      .finally(() => {
        setChecking(false)
      })
  }

  const icon =
    chip.icon === 'alert' ? (
      <ShieldAlertIcon className="size-3.5 shrink-0" />
    ) : chip.icon === 'clear' ? (
      <ShieldCheckIcon className="size-3.5 shrink-0" />
    ) : null

  return (
    <span className="relative inline-flex min-w-0 shrink-0">
      <button
        ref={triggerRef}
        type="button"
        aria-label={chip.long}
        title={chip.long}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={`inline-flex max-w-64 items-center gap-1 py-px text-[11px] leading-4 font-medium transition-colors aria-expanded:ring-1 aria-expanded:ring-border ${chipClass[chip.tone]}`}
      >
        {icon}
        {chip.short !== '' && <span className="truncate">{chip.short}</span>}
      </button>
      {open && (
        <div
          ref={panelRef}
          id={id}
          role="dialog"
          aria-label="Sensitive files"
          style={{ maxHeight }}
          className={`absolute left-0 z-20 flex max-h-[calc(100vh-8rem)] w-[26rem] flex-col gap-3 overflow-y-auto rounded-lg border border-border bg-surface p-3.5 text-left shadow-xl shadow-black/25 motion-safe:animate-pop-in ${above ? 'bottom-full mb-1.5 origin-bottom-left' : 'top-full mt-1.5 origin-top-left'}`}
        >
          <div>
            <h3 className="text-sm font-semibold text-text">Sensitive files</h3>
            {check.head !== '' && (
              <p className="mt-0.5 text-[11px] text-muted">
                At commit <span className="font-mono">{shortHead(check.head)}</span>
                {check.checkedAt !== '' && ` · checked ${formatAgo(now, check.checkedAt)}`}
              </p>
            )}
          </div>
          <p className={`text-xs leading-relaxed ${check.status === 'failed' ? 'text-warning' : 'text-muted'}`}>
            {summary(check)}
          </p>
          {groups.map((group) => (
            <GroupSection key={group.category} group={group} />
          ))}
          {unchecked && (
            <p role="note" className="text-xs leading-relaxed text-warning">
              GitHub listed {formatCount(check.checked)} of {counted(check.total, 'changed file', 'changed files')}, so
              the rest weren’t checked. Review them on GitHub.
            </p>
          )}
          {agent && (
            <p className="rounded-md border border-danger/30 bg-danger/8 px-2.5 py-2 text-xs leading-relaxed text-text">
              Opening a session on it asks first: hooks in agent settings run commands on your Mac as soon as a session
              starts, even in plan mode.
            </p>
          )}
          <div className="flex items-center justify-between gap-3 border-t border-border pt-2.5">
            <button type="button" disabled={checking} onClick={checkNow} className={quietButtonClass}>
              <RefreshIcon className={`size-3.5 ${checking ? 'motion-safe:animate-spin' : ''}`} />
              {checking ? 'Checking…' : 'Check now'}
            </button>
            {onSettings === undefined ? (
              <span className="text-[11px] text-muted">Settings → Sensitive files</span>
            ) : (
              <button type="button" onClick={onSettings} className={quietButtonClass}>
                What counts as sensitive
              </button>
            )}
          </div>
          {error !== '' && (
            <p role="alert" className="-mt-1.5 text-xs text-danger">
              {error}
            </p>
          )}
        </div>
      )}
    </span>
  )
}
