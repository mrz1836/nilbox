import type { ReactElement } from 'react'

import type { AutopilotDraft, DescribeAttachRef } from '../../../platform'

// DescribeRequest is what opens Describe it from elsewhere in the window: a message to send
// at once (the empty tab's composer), or what to attach to the next one (Change with
// Claude… attaches a routine, Describe a routine for this… an item). seq tells one request
// from the next, so the same one asked again acts again.
export interface DescribeRequest {
  seq: number
  text?: string
  attach?: DescribeAttachRef[]
}

// DescribeViewProps are what the Autopilot tab hands Describe it.
export interface DescribeViewProps {
  // request is the latest request from elsewhere, undefined for none.
  request?: DescribeRequest

  // onEditInBuilder opens the builder on a draft, as a template's Customize does
  // (AutopilotView's newFrom).
  onEditInBuilder: (draft: AutopilotDraft) => void

  // onShowRoutine selects one of the user's routines in the tab: a card's Show it.
  onShowRoutine: (id: number) => void
}

// DescribeView is Describe it: the conversation with Claude Code in the Autopilot tab, the
// cards from the user's own Claude Code above it, and the composer. Until it is built, it
// shows its heading alone.
export function DescribeView({ request }: DescribeViewProps): ReactElement {
  return (
    <section aria-label="Describe it" data-request={request?.seq ?? 0}>
      <h2 className="text-lg font-semibold">Describe it</h2>
    </section>
  )
}
