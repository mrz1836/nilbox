import type { ReactNode } from 'react'

import type { Edge, Story, StoryIcon } from '../../lib/nextStep'
import { ArchiveIcon, AutopilotIcon, ClockIcon } from '../icons'
import { StatusDot } from '../StatusDot'

// edgeText is each edge's story color, and edgeDot its dot's, spelled out in full so
// Tailwind finds them.
const edgeText: Readonly<Record<Edge, string>> = {
  urgent: 'text-warning',
  blocked: 'text-danger',
  yours: 'text-accent',
  ready: 'text-success',
  agent: 'text-info',
  waiting: 'text-muted',
  none: 'text-muted',
}

const edgeDot: Readonly<Record<Edge, string>> = {
  urgent: 'bg-warning',
  blocked: 'bg-danger',
  yours: 'bg-accent',
  ready: 'bg-success',
  agent: 'bg-info',
  waiting: 'bg-muted',
  none: 'bg-muted',
}

// Mark is the story's icon: a dot in the edge's color, one with a ring swelling out of it
// for something at work or waiting now (StatusDot), or a clock, a bolt, or an archive box.
function Mark({ icon, edge }: { icon: StoryIcon; edge: Edge }) {
  switch (icon) {
    case 'dot':
      return <span aria-hidden="true" data-icon="dot" className={`size-1.5 shrink-0 rounded-full ${edgeDot[edge]}`} />
    case 'ping':
      return (
        <span data-icon="ping" className="inline-flex">
          <StatusDot color={edgeDot[edge]} ping />
        </span>
      )
    case 'clock':
      return (
        <span data-icon="clock" className="inline-flex">
          <ClockIcon className="size-3 shrink-0" />
        </span>
      )
    case 'bolt':
      return (
        <span data-icon="bolt" className="inline-flex">
          <AutopilotIcon className="size-3 shrink-0" />
        </span>
      )
    case 'archive':
      return (
        <span data-icon="archive" className="inline-flex">
          <ArchiveIcon className="size-3 shrink-0" />
        </span>
      )
  }
}

export interface CardStoryProps {
  story: Story
  edge: Edge
  // children is what a triage story shows in its place, in the Snoozed and Dismissed tabs:
  // when the item comes back, or when it was dismissed (the row's TriageTag).
  children?: ReactNode
}

// CardStory is a card's one sentence, at the start of its second line: its mark, its lead
// in the edge's color, and its detail after " · ", muted. It never shrinks; the byline
// after it gives way first. A triage story shows children instead.
export function CardStory({ story, edge, children }: CardStoryProps) {
  if (story.triage) {
    return children
  }

  return (
    <span className={`inline-flex shrink-0 items-center gap-1.5 font-semibold whitespace-nowrap ${edgeText[edge]}`}>
      <Mark icon={story.icon} edge={edge} />
      {story.text}
      {story.detail !== '' && <span className="font-medium text-muted">{` · ${story.detail}`}</span>}
    </span>
  )
}
