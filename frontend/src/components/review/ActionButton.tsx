import { buttonClass, primaryButtonClass } from '../controls'
import { CheckIcon, ChevronDownIcon } from '../icons'
import { MenuButton, type MenuItem } from '../Menu'

// ButtonOffer is what the button needs of an action a sheet offers: the action, its
// words, whether it can run now and why not, and whether it asks first.
export interface ButtonOffer<A extends string> {
  action: A
  label: string
  enabled: boolean
  reason: string
  confirm: boolean
}

export interface ActionButtonProps<A extends string> {
  offers: readonly ButtonOffer<A>[]
  // selected is the action the button runs; the menu chooses another.
  selected: A
  onSelect: (action: A) => void
  onRun: (action: A) => void
  busy: boolean
  // blocked says why the window holds an action back, such as a comment with no text, or
  // ''.
  blocked: (action: A) => string
  // menuOnly reports an action that runs only when picked from the menu, such as a pull
  // request's close (menuOnly below); by default none does.
  menuOnly?: (action: A) => boolean
  // menuLabel names the menu's button.
  menuLabel?: string
  // extra are items set apart at the menu's foot, below the sheet's offers, such as one
  // that changes who the item is assigned to.
  extra?: readonly MenuItem[]
}

// noExtra is a menu with nothing set apart at its foot.
const noExtra: readonly MenuItem[] = []

// anyFromButton lets every action be the button.
const anyFromButton = () => false

// menuOnly reports an action that runs only when picked from the menu, on purpose: a pull
// request's close, and approving its held workflow runs, which the sheet's bar offers too.
// Picking it runs it (a close asks first), so it is never the button, never the action the
// sheet keeps chosen, never run by ⌘↩, and never counts as something to do.
export function menuOnly(action: string): boolean {
  return action === 'close' || action === 'approve_runs'
}

// ActionButton is a sheet's split button: the action it runs, labelled with exactly what
// happens, as in "Approve & squash-merge", and a menu of every action the sheet offers,
// with why each one that can't run now can't. With no action to run on its own (no
// primary, and none chosen) but some the user may pick, such as a write to the branch that
// is never the button by default, it reads "Choose an action" and only opens the menu. An
// action that runs only from the menu sits apart at the menu's foot and runs when picked,
// as the extra items below it do; with nothing else to do, the sheet says so and keeps
// them in a menu beside the words.
export function ActionButton<A extends string>({
  offers,
  selected,
  onSelect,
  onRun,
  busy,
  blocked,
  menuOnly: onlyFromMenu = anyFromButton,
  menuLabel = 'More review actions',
  extra = noExtra,
}: ActionButtonProps<A>) {
  const offer = onlyFromMenu(selected) ? undefined : offers.find((o) => o.action === selected)
  const items = (): MenuItem[] => [
    ...offers.map((o): MenuItem => {
      if (onlyFromMenu(o.action)) {
        const reason = o.enabled ? blocked(o.action) : o.reason

        return {
          label: o.label,
          icon: <span className="size-3.5" />,
          disabled: busy || !o.enabled || reason !== '',
          title: reason === '' ? undefined : reason,
          divided: true,
          onSelect: () => {
            onRun(o.action)
          },
        }
      }

      return {
        label: o.label,
        icon: o.action === selected ? <CheckIcon className="size-3.5 text-accent" /> : <span className="size-3.5" />,
        disabled: !o.enabled,
        title: o.enabled ? undefined : o.reason,
        onSelect: () => {
          onSelect(o.action)
        },
      }
    }),
    // The items set apart are held back while an action runs, as the actions run from
    // the menu are, and a line sets them apart from any offer above them.
    ...extra.map((item, i) => ({
      ...item,
      disabled: busy || item.disabled === true,
      divided: item.divided ?? (i === 0 && offers.length > 0),
    })),
  ]

  if (offer === undefined) {
    if (!offers.some((o) => o.enabled && !onlyFromMenu(o.action))) {
      return (
        <div className="flex items-center gap-2">
          <p className="text-xs text-muted">Nothing to do here now.</p>
          {(offers.some((o) => o.enabled && onlyFromMenu(o.action)) || extra.length > 0) && (
            <MenuButton label={menuLabel} items={items} className={buttonClass}>
              More <ChevronDownIcon className="size-3.5" />
            </MenuButton>
          )}
        </div>
      )
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
        label={menuLabel}
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
