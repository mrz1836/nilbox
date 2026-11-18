import { useId } from 'react'

import { filesChip } from '../../lib/sensitive'
import { usePopover } from '../../lib/usePopover'
import type { InboxItem } from '../../platform'
import { ShieldAlertIcon, ShieldCheckIcon } from '../icons'
import { SensitiveFilesPanel } from '../SensitiveFiles'

// trustTitle says why an item from someone the repository doesn't trust launches in plan
// or read-only mode: an untrusted bot's, or an outside contributor's. It is '' for an item
// from the repository's own people or a trusted bot.
export function trustTitle(item: InboxItem): string {
  if (item.trusted) {
    return ''
  }

  return item.authorIsBot
    ? "A bot that isn't trusted, so launches start in plan or read-only mode. Add it to your trusted bots in Settings → Trust."
    : 'Launches in plan or read-only mode unless you lift the restriction'
}

// tones are the chip's two tones: danger once a sensitive file is in a danger category,
// else warning.
const tones = {
  danger: 'border-danger/45 bg-danger/10 text-danger hover:bg-danger/18',
  warning: 'border-warning/45 bg-warning/10 text-warning hover:bg-warning/18',
} as const

const chipClass =
  'inline-flex max-w-64 items-center gap-1 rounded-full border px-1.5 py-px text-[0.6875rem] leading-4 font-medium transition-colors aria-expanded:ring-1 aria-expanded:ring-border'

export interface RiskChipProps {
  item: InboxItem
  // onFilesSettings opens Settings → Sensitive files from the sensitive-files panel.
  onFilesSettings?: (() => void) | undefined
}

// RiskChip is a card's one chip for what to be careful of: who opened the item, when the
// repository doesn't trust them ("Outside contributor", "Untrusted bot"), and the
// sensitive files it changes, or that its files couldn't all be checked. The worst tone
// wins; in a list narrower than 48rem, the shield alone says it. An outside contributor's files checked clean get a checked shield. It opens the
// sensitive-files panel when the files were checked. A trusted bot gets no chip: its square
// avatar says it, and neither does an item without a finding from someone trusted.
export function RiskChip({ item, onFilesSettings }: RiskChipProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef } = popover
  const check = item.files
  const files = check === null ? null : filesChip(check)
  const notable = files !== null && files.short !== ''

  if (item.trusted && !notable) {
    return null
  }

  const trust = item.trusted ? '' : item.authorIsBot ? 'Untrusted bot' : notable ? 'Outside' : 'Outside contributor'
  const words = [trust, notable ? files.short : ''].filter((part) => part !== '').join(' · ')
  const title = [trustTitle(item), files?.long ?? ''].filter((part) => part !== '').join('\n')
  const tone = check?.groups.some((g) => g.level === 'danger') === true ? 'danger' : 'warning'
  const clean = !item.trusted && files?.icon === 'clear'
  const content = (
    <>
      {clean ? <ShieldCheckIcon className="size-3.5 shrink-0" /> : <ShieldAlertIcon className="size-3.5 shrink-0" />}
      <span className="truncate @max-3xl:sr-only">{words}</span>
    </>
  )

  if (check === null) {
    return (
      <span title={title} data-tone={tone} className={`relative z-10 shrink-0 ${chipClass} ${tones[tone]}`}>
        {content}
      </span>
    )
  }

  return (
    <span className="relative z-10 inline-flex min-w-0 shrink-0 has-[[aria-expanded=true]]:z-20">
      <button
        ref={triggerRef}
        type="button"
        title={title}
        data-tone={tone}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={`${chipClass} ${tones[tone]}`}
      >
        {content}
      </button>
      <SensitiveFilesPanel itemId={item.id} check={check} onSettings={onFilesSettings} popover={popover} id={id} />
    </span>
  )
}
