import { useId } from 'react'

import type { ReleaseBump, ReleaseBumpOffer } from '../../platform'
import { quietButtonClass } from '../controls'

// bumpNames are each bump's name and what it is for, as its card says them.
const bumpNames: Readonly<Record<ReleaseBump, { label: string; purpose: string }>> = {
  patch: { label: 'Patch', purpose: 'Fixes' },
  minor: { label: 'Minor', purpose: 'New features' },
  major: { label: 'Major', purpose: 'Breaking changes' },
}

export interface BumpChoiceProps {
  bumps: readonly ReleaseBumpOffer[]
  value: ReleaseBump
  onChange: (bump: ReleaseBump) => void
  // disabled holds every card while the sheet can't be changed, as while it starts.
  disabled?: boolean
  // onUpdate opens Settings → Integrations, for a card that needs a newer magex.
  onUpdate: () => void
}

// BumpChoice is the release's three cards, Patch, Minor, and Major, each with the version
// it makes. Patch is chosen as the sheet opens, every time. A card that isn't offered is
// off, and says why: Major needs a magex that can be told which version to make, and
// offers to update it in Settings. They are radio buttons, so the arrow keys move between
// them.
export function BumpChoice({ bumps, value, onChange, disabled = false, onUpdate }: BumpChoiceProps) {
  const name = useId()

  return (
    <div role="radiogroup" aria-label="Version" className="grid grid-cols-3 gap-2">
      {bumps.map((offer) => {
        const { label, purpose } = bumpNames[offer.bump]
        const chosen = offer.bump === value
        const noteId = `${name}-${offer.bump}-note`

        const choosable = offer.offered && !disabled

        // The whole card is the radio's label, so a click anywhere on it chooses the bump,
        // and selects none of its text. Update sits outside it: a label holds no button.
        return (
          <div
            key={offer.bump}
            className={`flex flex-col rounded-lg border transition-colors ${chosen ? 'border-accent/60 bg-accent/8' : 'border-border bg-surface'} ${choosable && !chosen ? 'hover:bg-raised' : ''} ${offer.offered ? '' : 'opacity-75'}`}
          >
            <label
              className={`flex flex-1 flex-col gap-1 px-3 py-2.5 select-none ${choosable ? 'cursor-pointer' : ''}`}
            >
              <span className="flex items-center gap-2 text-sm">
                <input
                  type="radio"
                  name={name}
                  value={offer.bump}
                  checked={chosen}
                  disabled={disabled || !offer.offered}
                  aria-describedby={offer.note === '' ? undefined : noteId}
                  onChange={() => {
                    onChange(offer.bump)
                  }}
                  className="radio"
                />
                <span className="font-medium">{label}</span>
                <span className="ml-auto font-mono text-xs text-muted">{offer.version}</span>
              </span>
              <span className="pl-6 text-xs text-muted">{purpose}</span>
              {offer.note !== '' && (
                <span id={noteId} className="pl-6 text-xs text-muted">
                  {offer.note}
                </span>
              )}
            </label>
            {offer.fixes.includes('update') && (
              <button type="button" onClick={onUpdate} className={`${quietButtonClass} mx-3 mb-2.5 ml-8 self-start`}>
                Update magex in Settings
              </button>
            )}
          </div>
        )
      })}
    </div>
  )
}
