import { useId } from 'react'

import { formatAgo } from '../lib/time'
import { counted, formatCount } from '../lib/format'
import {
  changeText,
  changeTone,
  filesChip,
  levelDot,
  sensitiveCount,
  shortHead,
  type FilesChip,
} from '../lib/sensitive'
import { toneText } from '../lib/signals'
import { useMutation } from '../lib/useMutation'
import { usePopover } from '../lib/usePopover'
import { checkItemFiles, openExternal, type FileCheck, type SensitiveFile, type SensitiveGroup } from '../platform'
import { quietButtonClass } from './controls'
import { ExternalIcon, RefreshIcon, ShieldAlertIcon, ShieldCheckIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'
import { RelativeTime } from './RelativeTime'

export interface SensitiveFilesProps {
  // itemId is the pull request the check is for, for checking it again.
  itemId: number
  check: FileCheck
  // onSettings opens Settings → Sensitive files; without it, the panel names the page.
  onSettings?: (() => void) | undefined
}

// chipClass is each tone's chip: a tinted pill for a finding, a bare word otherwise.
const chipClass: Readonly<Record<FilesChip['tone'], string>> = {
  danger: 'rounded-full border border-danger/45 bg-danger/10 px-1.5 text-danger hover:bg-danger/18',
  warning: 'rounded-full border border-warning/45 bg-warning/10 px-1.5 text-warning hover:bg-warning/18',
  success: 'rounded-full px-1 text-success hover:bg-raised',
  muted: 'rounded-full px-1 text-muted hover:bg-raised hover:text-text',
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
            openExternal(file.url)
          }}
          className="group/file inline-flex min-w-0 items-baseline gap-1 text-left font-mono text-xs text-text hover:text-accent hover:underline"
        >
          <span className="truncate">{file.path}</span>
          <ExternalIcon className="size-3 shrink-0 self-center text-muted group-hover/file:text-accent" />
        </button>
      )}
      <span
        title={change}
        className={`ml-auto min-w-0 shrink truncate text-[0.6875rem] ${toneText[changeTone(file.change)]}`}
      >
        {change}
      </span>
    </li>
  )
}

// GroupSection is one category's files, with why they deserve a look.
function GroupSection({ group }: { group: SensitiveGroup }) {
  const files = group.files

  return (
    <section aria-label={group.label} className="flex flex-col gap-1">
      <h4 className="flex items-center gap-1.5 text-xs font-semibold text-text">
        <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${levelDot[group.level]}`} />
        {group.label}
        <span className="font-normal text-muted">{formatCount(files.length)}</span>
      </h4>
      <p className="text-[0.6875rem] leading-snug text-muted">{group.description}</p>
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
export function SensitiveFiles({ itemId, check, onSettings }: SensitiveFilesProps) {
  const id = useId()
  // The panel opens upward when it fits better above the chip, and keeps within the area it
  // scrolls in, scrolling inside itself when the files run longer.
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef } = popover
  const checker = useMutation(() => checkItemFiles(itemId))

  const chip = filesChip(check)
  const groups = check.groups
  const agent = groups.some((g) => g.category === 'agent')
  const unchecked = !check.complete && check.status !== 'failed' && check.status !== 'pending'

  const checking = checker.pending
  const error = checker.error

  const checkNow = () => {
    void checker.run()
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
        className={`inline-flex max-w-64 items-center gap-1 py-px text-[0.6875rem] leading-4 font-medium transition-colors aria-expanded:ring-1 aria-expanded:ring-border ${chipClass[chip.tone]}`}
      >
        {icon}
        {chip.short !== '' && <span className="truncate">{chip.short}</span>}
      </button>
      <PopoverPanel
        state={popover}
        align="left"
        id={id}
        role="dialog"
        aria-label="Sensitive files"
        className="z-20 flex max-h-[calc(100vh-8rem)] w-[26rem] flex-col gap-3 overflow-y-auto p-3.5 text-left shadow-black/25"
      >
        <div>
          <h3 className="text-sm font-semibold text-text">Sensitive files</h3>
          {check.head !== '' && (
            <p className="mt-0.5 text-[0.6875rem] text-muted">
              At commit <span className="font-mono">{shortHead(check.head)}</span>
              {check.checkedAt !== '' && (
                <>
                  {' · checked '}
                  <RelativeTime iso={check.checkedAt} show={formatAgo} />
                </>
              )}
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
            starts, even in plan or read-only mode.
          </p>
        )}
        <div className="flex items-center justify-between gap-3 border-t border-border pt-2.5">
          <button type="button" disabled={checking} onClick={checkNow} className={quietButtonClass}>
            <RefreshIcon className={`size-3.5 ${checking ? 'motion-safe:animate-spin' : ''}`} />
            {checking ? 'Checking…' : 'Check now'}
          </button>
          {onSettings === undefined ? (
            <span className="text-[0.6875rem] text-muted">Settings → Sensitive files</span>
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
      </PopoverPanel>
    </span>
  )
}
