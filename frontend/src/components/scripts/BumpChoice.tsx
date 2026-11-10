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

        return (
          <div
            key={offer.bump}
            className={`flex flex-col gap-1 rounded-lg border px-3 py-2.5 transition-colors ${chosen ? 'border-accent/60 bg-accent/8' : 'border-border bg-surface'} ${offer.offered ? '' : 'opacity-75'}`}
          >
            <label className={`flex items-center gap-2 text-sm ${offer.offered && !disabled ? 'cursor-pointer' : ''}`}>
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
            </label>
            <p className="pl-6 text-xs text-muted">{purpose}</p>
            {offer.note !== '' && (
              <p id={noteId} className="pl-6 text-xs text-muted">
                {offer.note}
              </p>
            )}
            {offer.fixes.includes('update') && (
              <button type="button" onClick={onUpdate} className={`${quietButtonClass} ml-5 self-start`}>
                Update magex in Settings
              </button>
            )}
          </div>
        )
      })}
    </div>
  )
}
