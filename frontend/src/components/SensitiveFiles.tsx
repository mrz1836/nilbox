import { useId } from 'react'

import { formatAgo } from '../lib/time'
import { counted, formatCount } from '../lib/format'
import { shortCommit } from '../lib/git'
import {
  changeText,
  changeTone,
  changeWords,
  filesChip,
  levelDot,
  sensitiveCount,
  type FilesChip,
} from '../lib/sensitive'
import { toneText } from '../lib/signals'
import { useMutation } from '../lib/useMutation'
import { usePopover, type Popover } from '../lib/usePopover'
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

// PathText is a file's path on one line in the width it is given: its folders give way
// first, cut short, so the file name stays readable, and the name is cut short only when
// it alone doesn't fit.
function PathText({ path, title, className = '' }: { path: string; title?: string; className?: string }) {
  const slash = path.lastIndexOf('/')

  return (
    <span title={title} className={`flex min-w-0 ${className}`}>
      <span className="min-w-0 truncate">{path.slice(0, slash + 1)}</span>
      <span className="max-w-full shrink-0 truncate">{path.slice(slash + 1)}</span>
    </span>
  )
}

// FileChange is how a file changed, its words never cut short; a moved or copied file's
// previous path, beside them, gives way as the row's own path does.
function FileChange({ file }: { file: SensitiveFile }) {
  const { words, from } = changeWords(file)

  return (
    <span
      title={changeText(file)}
      className={`ml-auto flex max-w-1/2 shrink-0 items-baseline gap-1 text-caption ${toneText[changeTone(file.change)]}`}
    >
      <span className="shrink-0 whitespace-nowrap">{words}</span>
      {from !== '' && (
        <>
          {' '}
          <PathText path={from} />
        </>
      )}
    </span>
  )
}

// FileRow is one sensitive file: its path, which opens its diff on GitHub, and how it
// changed. The path takes the room the change leaves it, and its title or its button's
// name keeps it whole.
function FileRow({ file }: { file: SensitiveFile }) {
  return (
    <li className="flex min-w-0 items-baseline gap-2 py-0.5">
      {file.url === '' ? (
        <PathText path={file.path} title={file.path} className="font-mono text-xs text-text" />
      ) : (
        <button
          type="button"
          title={`Open the diff of ${file.path} on GitHub`}
          onClick={() => {
            openExternal(file.url)
          }}
          className="group/file inline-flex min-w-0 items-baseline gap-1 text-left font-mono text-xs text-text hover:text-accent hover:underline"
        >
          <PathText path={file.path} />
          <ExternalIcon className="size-3 shrink-0 self-center text-muted group-hover/file:text-accent" />
        </button>
      )}
      <FileChange file={file} />
    </li>
  )
}

// SensitiveFilesGroup is one category's files, with why they deserve a look: in the
// panel, and in a card's peek.
export function SensitiveFilesGroup({ group }: { group: SensitiveGroup }) {
  const files = group.files

  return (
    <section aria-label={group.label} className="flex flex-col gap-1">
      <h4 className="flex items-center gap-1.5 text-xs font-semibold text-text">
        <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${levelDot[group.level]}`} />
        {group.label}
        <span className="font-normal text-muted">{formatCount(files.length)}</span>
      </h4>
      <p className="text-caption leading-snug text-muted">{group.description}</p>
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
    case 'flagged':
      return `${counted(sensitiveCount(check), 'file', 'files')} here deserve a close look before you review or open a session.`
  }
}

export interface SensitiveFilesPanelProps {
  // itemId is the pull request the check is for, for checking it again.
  itemId: number
  check: FileCheck
  // onSettings opens Settings → Sensitive files; without it, the panel names the page.
  onSettings?: (() => void) | undefined
  // popover is the state of the button that opens the panel, and id the panel's id, which
  // the button's aria-controls names while it is open.
  popover: Popover<HTMLButtonElement, HTMLDivElement>
  id: string
}

// SensitiveFilesPanel lists a pull request's sensitive files by category, each opening its
// diff on GitHub, with a button to check the files again now, in the panel a chip opens:
// the sensitive-file chip's own, or the card's risk chip (card/RiskChip). The check it
// runs outlasts the panel's closing. It wraps its own text: a card's line, which it opens
// in, doesn't wrap, and that would carry into the panel and run its sentences past its
// edge. It is never wider than the window, and its file rows fit its width.
export function SensitiveFilesPanel({ itemId, check, onSettings, popover, id }: SensitiveFilesPanelProps) {
  const checker = useMutation(() => checkItemFiles(itemId))

  const groups = check.groups
  const agent = groups.some((g) => g.category === 'agent')
  const unchecked = !check.complete && check.status !== 'failed' && check.status !== 'pending'

  const checking = checker.pending
  const error = checker.error

  const checkNow = () => {
    void checker.run()
  }

  return (
    <PopoverPanel
      state={popover}
      align="left"
      elevation="card"
      id={id}
      role="dialog"
      aria-label="Sensitive files"
      className="layer-popover flex max-h-[calc(100vh-8rem)] w-[26rem] max-w-[calc(100vw-2rem)] flex-col gap-3 overflow-y-auto p-3.5 text-left break-words whitespace-normal"
    >
      <div>
        <h3 className="text-sm font-semibold text-text">Sensitive files</h3>
        {check.head !== '' && (
          <p className="mt-0.5 text-caption text-muted">
            At commit <span className="font-mono">{shortCommit(check.head)}</span>
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
        <SensitiveFilesGroup key={group.category} group={group} />
      ))}
      {unchecked && (
        <p role="note" className="text-xs leading-relaxed text-warning">
          GitHub listed {formatCount(check.checked)} of {counted(check.total, 'changed file', 'changed files')}, so the
          rest weren’t checked. Review them on GitHub.
        </p>
      )}
      {agent && (
        <p className="rounded-md border border-danger/30 bg-danger/8 px-2.5 py-2 text-xs leading-relaxed text-text">
          Opening a session on it asks first: hooks in agent settings run commands on your Mac as soon as a session
          starts, even in plan or read-only mode.
        </p>
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-border pt-2.5">
        <button type="button" disabled={checking} onClick={checkNow} className={quietButtonClass}>
          <RefreshIcon className={`size-3.5 ${checking ? 'motion-safe:animate-spin' : ''}`} />
          {checking ? 'Checking…' : 'Check now'}
        </button>
        {onSettings === undefined ? (
          <span className="text-caption text-muted">Settings → Sensitive files</span>
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
  )
}

// SensitiveFiles is a pull request's sensitive-file chip: the categories of the sensitive
// files it changes, or that its files were checked and none is, or that they could not
// all be checked. It opens a panel listing each file by category (SensitiveFilesPanel).
export function SensitiveFiles({ itemId, check, onSettings }: SensitiveFilesProps) {
  const id = useId()
  // The panel opens upward when it fits better above the chip, and keeps within the area it
  // scrolls in, scrolling inside itself when the files run longer.
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef } = popover

  const chip = filesChip(check)

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
        className={`inline-flex max-w-64 items-center gap-1 py-px text-caption leading-4 font-medium transition-colors aria-expanded:ring-1 aria-expanded:ring-border ${chipClass[chip.tone]}`}
      >
        {icon}
        {chip.short !== '' && <span className="truncate">{chip.short}</span>}
      </button>
      <SensitiveFilesPanel itemId={itemId} check={check} onSettings={onSettings} popover={popover} id={id} />
    </span>
  )
}
