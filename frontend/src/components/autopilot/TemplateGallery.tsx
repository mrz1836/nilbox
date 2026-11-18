import { useId } from 'react'

import { modeLabels } from '../../lib/autopilot'
import { suggestedFirst } from '../../lib/templates'
import type { AutopilotTemplate } from '../../platform'
import { captionClass } from '../controls'

export interface TemplateGalleryProps {
  templates: readonly AutopilotTemplate[]
  onPick: (template: AutopilotTemplate) => void
  // limit shows at most that many, the suggested first, as the empty tab does; every one
  // without it.
  limit?: number
}

// TemplateCard is one template: its icon in a tile, its name, which wraps rather than
// pushing the mode it suggests out of the card, and that mode as a quiet badge; one line
// on what it does and what this build can't run of it yet; and, set apart at the card's
// foot, why it is suggested. It is all a button that opens its quick setup. Every level
// down to the name may shrink (min-w-0), since a long name's nowrap width would otherwise
// widen the row past the card. A template whose step another part of the app still builds
// is listed all the same, never locked: its quick setup says what it needs.
function TemplateCard({ template: t, onPick }: { template: AutopilotTemplate; onPick: () => void }) {
  const id = useId()

  return (
    <li className="flex min-w-0">
      <button
        type="button"
        onClick={onPick}
        aria-describedby={`${id}-summary`}
        className="flex w-full min-w-0 flex-col gap-2.5 rounded-lg border border-border bg-surface p-4 text-left transition-colors hover:border-accent/50 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
      >
        <span className="flex w-full min-w-0 items-center gap-3">
          <span
            aria-hidden="true"
            className="flex size-8 shrink-0 items-center justify-center rounded-md bg-raised text-base leading-none"
          >
            {t.icon}
          </span>
          <span className="min-w-0 flex-1 text-sm leading-snug font-medium text-text">{t.name}</span>
          <span
            className="shrink-0 rounded-full border border-border px-2 text-[0.6875rem] leading-[1.125rem] font-medium whitespace-nowrap text-muted"
            title="The mode it suggests, a hint alone"
          >
            {modeLabels[t.suggests]}
          </span>
        </span>
        <span id={`${id}-summary`} className="text-[0.8125rem] leading-relaxed text-muted">
          {t.summary}
        </span>
        {t.needs.length > 0 && <span className="text-xs text-warning">Needs {t.needs.join(', ')}</span>}
        {t.suggested !== '' && (
          <span className="mt-auto flex items-start gap-2 border-t border-border pt-2.5 text-xs leading-relaxed text-muted">
            <span aria-hidden="true" className="mt-[0.4375rem] size-1.5 shrink-0 rounded-full bg-accent" />
            {t.suggested}
          </span>
        )}
      </button>
    </li>
  )
}

// TemplateGallery lists the templates, the ones that fit what this Mac already knows first,
// under Suggested for you, each with why; picking one opens its quick setup.
export function TemplateGallery({ templates, onPick, limit }: TemplateGalleryProps) {
  const id = useId()
  const { suggested, rest } = suggestedFirst(templates)
  const shownSuggested = limit === undefined ? suggested : suggested.slice(0, limit)
  const shownRest = limit === undefined ? rest : rest.slice(0, Math.max(0, limit - shownSuggested.length))

  const list = (items: AutopilotTemplate[], label: string) => (
    <ul aria-label={label} className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {items.map((t) => (
        <TemplateCard
          key={t.key}
          template={t}
          onPick={() => {
            onPick(t)
          }}
        />
      ))}
    </ul>
  )

  return (
    <div className="flex flex-col gap-4">
      {shownSuggested.length > 0 && (
        <section aria-labelledby={`${id}-suggested`} className="flex flex-col gap-2">
          <h3 id={`${id}-suggested`} className={captionClass}>
            Suggested for you
          </h3>
          {list(shownSuggested, 'Suggested templates')}
        </section>
      )}
      {shownRest.length > 0 && (
        <section
          aria-labelledby={shownSuggested.length > 0 ? `${id}-all` : undefined}
          aria-label={shownSuggested.length > 0 ? undefined : 'Templates'}
          className="flex flex-col gap-2"
        >
          {shownSuggested.length > 0 && (
            <h3 id={`${id}-all`} className={captionClass}>
              {limit === undefined ? 'More templates' : 'Templates'}
            </h3>
          )}
          {list(shownRest, 'Templates')}
        </section>
      )}
    </div>
  )
}
