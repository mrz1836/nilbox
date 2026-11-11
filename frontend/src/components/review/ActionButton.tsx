import type { ReviewAction, ReviewOffer } from '../../platform'
import { buttonClass, primaryButtonClass } from '../controls'
import { CheckIcon, ChevronDownIcon } from '../icons'
import { MenuButton, type MenuItem } from '../Menu'

export interface ActionButtonProps {
  offers: readonly ReviewOffer[]
  // selected is the action the button runs; the menu chooses another.
  selected: ReviewAction
  onSelect: (action: ReviewAction) => void
  onRun: (action: ReviewAction) => void
  busy: boolean
  // blocked says why the window holds an action back, such as a comment with no text, or
  // ''.
  blocked: (action: ReviewAction) => string
}

// ActionButton is the review sheet's split button: the action it runs, labelled with
// exactly what happens, as in "Approve & squash-merge", and a menu of every action the
// sheet offers, with why each one that can't run now can't. With no action to run on its
// own (no primary, and none chosen) but some the user may pick, such as a write to the
// branch that is never the button by default, it reads "Choose an action" and only opens
// the menu.
export function ActionButton({ offers, selected, onSelect, onRun, busy, blocked }: ActionButtonProps) {
  const offer = offers.find((o) => o.action === selected)
  const items = (): MenuItem[] =>
    offers.map((o) => ({
      label: o.label,
      icon: o.action === selected ? <CheckIcon className="size-3.5 text-accent" /> : <span className="size-3.5" />,
      disabled: !o.enabled,
      title: o.enabled ? undefined : o.reason,
      onSelect: () => {
        onSelect(o.action)
      },
    }))

  if (offer === undefined) {
    if (!offers.some((o) => o.enabled)) {
      return <p className="text-xs text-muted">Nothing to do here now.</p>
    }

    return (
      <MenuButton label="Choose an action" items={items} className={buttonClass}>
        Choose an action <ChevronDownIcon className="size-3.5" />
      </MenuButton>
    )
  }

  const reason = offer.enabled ? blocked(offer.action) : offer.reason

  return (
    <div className="flex items-stretch">
      <button
        type="button"
        disabled={busy || !offer.enabled || reason !== ''}
        title={reason === '' ? `${offer.label} (⌘↩)` : reason}
        onClick={() => {
          onRun(offer.action)
        }}
        className={`${primaryButtonClass} rounded-r-none`}
      >
        {busy ? 'Working…' : offer.label}
        {offer.confirm && !busy && '…'}
      </button>
      <MenuButton
        label="More review actions"
        items={items}
        // h-full: the menu wraps its button in a div, which the row stretches to the
        // label's height, so the button fills it rather than keep the icon's.
        className={`${primaryButtonClass} h-full rounded-l-none border-l-0 px-1.5`}
      >
        <ChevronDownIcon className="size-3.5" />
      </MenuButton>
    </div>
  )
}
