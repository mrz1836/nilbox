// The Worktrees page's layout: every class its rows, drawers, and history are drawn with.
// The page's clones share one grid, and each clone's section, its list, and each row are
// subgrids of it, so the columns line up down the whole page, from one clone to the next.
// The rows follow the grid's width, never the window's: under 56rem (@max-4xl) the item
// and the work move to a second line under the branch, and under 36rem (@max-xl) a row is a
// five-column grid of its own whose second line holds the item, the work, the agent, and
// the age (metaClass, display: contents while they sit in the row's columns). Nothing here
// clips (no overflow on the grid, a section, or a list): the clone headings stick in the
// page's scroll, and a menu's panel hangs out of its row.

import type { LaunchPlace } from '../LaunchControls'
import type { Tone } from '../../lib/worktreeParts'

// worktreeGridClass is the page's list of clones: the container its rows' layout follows,
// and the grid of its nine columns: the state, the branch, the item, the work, the agent,
// the age, the verb, the tools (›_ and ⋯), and the chevron. The verb has a column of its
// own, so a launch's control never widens the tools. The branch column is as wide as the
// longest branch listed, up to 16rem; the item takes what's left.
export const worktreeGridClass =
  '@container grid grid-cols-[auto_fit-content(16rem)_minmax(0,1fr)_fit-content(10rem)_fit-content(14rem)_auto_auto_auto_auto] gap-x-3 gap-y-3.5'

// cloneSectionClass is one clone's section: its heading, then its rows.
export const cloneSectionClass = 'col-span-full grid grid-cols-subgrid content-start gap-y-0.5'

// rowListClass is a clone's list: each row, its note, and its drawer.
export const rowListClass = 'col-span-full grid grid-cols-subgrid'

// rowClass is a row: a subgrid of the page's columns, every cell placed in it by its own
// classes, and in a narrow grid a grid of its own, five columns wide. Its left padding is
// the room of its edge.
export const rowClass =
  'relative col-span-full grid min-h-7.5 grid-cols-subgrid items-center rounded-md py-0.75 pr-0.5 pl-3.5 transition-opacity hover:bg-raised/60 focus-within:bg-raised/60 data-open:bg-raised/75 @max-4xl:py-1.25 @max-xl:grid-cols-[auto_minmax(0,1fr)_auto_auto_auto] @max-xl:gap-x-2'

// dimClass dims a row a change takes, until the list is read again.
export const dimClass = 'opacity-55'

// edgeClass is a row's edge, the bar down its left side colored by its lane, across both
// its lines.
export const edgeClass = 'absolute top-1.5 bottom-1.5 left-0.75 w-0.75 rounded-sm'

// stateCellClass holds the state's one mark.
export const stateCellClass = 'col-start-1 row-start-1 flex w-3.5 min-w-0 justify-center'

// branchCellClass holds the branch: its prefix gives way first, so the name stays readable.
export const branchCellClass =
  'col-start-2 row-start-1 flex min-w-0 items-center overflow-hidden text-[0.84375rem] whitespace-nowrap'

// metaClass holds the item, the work, the agent, and the age: no box of its own while they
// sit in the row's columns, and in a narrow grid the second line they share.
export const metaClass =
  'contents @max-xl:col-[2/-1] @max-xl:row-start-2 @max-xl:flex @max-xl:min-w-0 @max-xl:items-center @max-xl:gap-2.25 @max-xl:pr-1.5'

export const itemCellClass =
  'col-start-3 row-start-1 flex min-w-0 items-center gap-1.5 overflow-hidden text-[0.78125rem] whitespace-nowrap @max-4xl:col-[2/4] @max-4xl:row-start-2 @max-xl:flex-auto'

export const workCellClass =
  'col-start-4 row-start-1 flex min-w-0 items-center gap-2.25 text-xs whitespace-nowrap tabular-nums @max-4xl:col-[4/9] @max-4xl:row-start-2 @max-4xl:justify-end @max-4xl:pr-1.5 @max-xl:shrink-0 @max-xl:pr-0'

// agentCellClass holds the agent. Its padding is room for the ping of its dot, which its
// overflow would otherwise cut.
export const agentCellClass =
  'col-start-5 row-start-1 flex min-w-0 items-center gap-1.5 overflow-hidden py-0.5 pl-[0.4375rem] text-xs whitespace-nowrap text-muted @max-xl:max-w-[42%] @max-xl:shrink'

export const ageCellClass =
  'col-start-6 row-start-1 min-w-0 text-right text-xs whitespace-nowrap text-muted tabular-nums @max-xl:shrink-0'

// verbCellClass holds the row's verb, against the tools.
export const verbCellClass = 'col-start-7 row-start-1 min-w-0 justify-self-end @max-xl:col-start-3'

// toolsCellClass holds ›_ and ⋯.
export const toolsCellClass = 'col-start-8 row-start-1 flex min-w-0 items-center gap-0.5 @max-xl:col-start-4'

export const chevronCellClass = 'col-start-9 row-start-1 flex min-w-0 @max-xl:col-start-5'

// workMarkClass says what a change taking the row is doing, in place of its verb and its
// tools.
export const workMarkClass =
  'col-[7/9] row-start-1 inline-flex items-center gap-1.5 justify-self-end px-1.5 text-xs font-semibold whitespace-nowrap @max-xl:col-[3/5]'

// noteClass is the line under a row a change left in place, or whose action failed.
export const noteClass = 'col-[2/-1] flex flex-col gap-0.5 pb-1.25 text-xs'

// launchPlace places a launch's control in the verb's column, and its outcome under the
// row, from the branch to the row's end (LaunchControls' row variant).
export const launchPlace: LaunchPlace = { boxClass: verbCellClass, outcomeClass: 'col-[2/-1] pb-1' }

// iconButtonClass is a quiet icon button: ›_, ⋯, and the chevron.
export const iconButtonClass =
  'inline-grid size-6 shrink-0 place-items-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text disabled:opacity-50 aria-expanded:bg-raised aria-expanded:text-text'

// buttonClass is a bordered button: the row's verb, and the drawer's Remove… and Force
// remove…; cleanUpClass is Clean up's, in green.
const buttonShape =
  'inline-flex h-6 items-center gap-1.25 rounded-md border px-2.25 text-xs font-semibold whitespace-nowrap transition-colors disabled:opacity-55'

export const verbButtonClass = `${buttonShape} border-border bg-surface text-text hover:bg-raised disabled:hover:bg-surface`

export const cleanUpButtonClass = `${buttonShape} border-success/50 bg-success/8 text-success hover:bg-success/15 disabled:hover:bg-success/8`

export const dangerVerbButtonClass = `${buttonShape} border-border bg-surface text-danger hover:bg-raised disabled:hover:bg-surface`

// tagClass is a small tag beside an item or a folder: draft, or Claude Code.
export const tagClass =
  'shrink-0 cursor-help rounded-full border border-border bg-raised px-1.5 text-[0.65625rem] leading-[0.9375rem] font-semibold text-muted'

// drawerClass is a row's drawer, under it, from the branch's column to the row's end, on a
// raised background; in a narrow grid, across the row under the state's column. It is the
// container its columns follow.
export const drawerClass =
  '@container col-[2/-1] mt-0.5 mr-0.5 mb-2.5 overflow-hidden rounded-lg border border-border bg-raised/40 shadow-md shadow-black/10 @max-xl:col-span-full @max-xl:ml-5.5'

// drawerColumnsClass sets the drawer's three columns side by side, and under 48rem one
// above another.
export const drawerColumnsClass = 'grid grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)_minmax(0,1.25fr)] @max-3xl:grid-cols-1'

export const drawerColumnClass =
  'min-w-0 border-l border-border/70 px-3 pt-2.25 pb-2.5 first:border-l-0 @max-3xl:border-t @max-3xl:border-l-0 @max-3xl:first:border-t-0'

// columnHeadingClass is a drawer column's heading, in capitals.
export const columnHeadingClass =
  'mb-1.5 flex min-h-5 items-center gap-1.5 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase'

// columnNoneClass says a column has nothing to list.
export const columnNoneClass = 'flex items-center gap-1.5 text-xs text-muted'

// drawerFooterClass holds the drawer's actions, in words.
export const drawerFooterClass =
  'flex flex-wrap items-center gap-0.5 border-t border-border/70 bg-surface/45 px-2 py-1.25'

// skeletonClass stands in for a word still being read.
export const skeletonClass = 'inline-block h-2.25 rounded-sm bg-raised align-middle motion-safe:animate-pulse'

// historyListClass is the recently cleaned-up list: a grid of its own, of eight columns
// its lines take as subgrids (the mark, where it was, what it was for, who cleaned it up,
// the branch that went with it, the commit, the age, and the actions), and the container
// they follow. Under 56rem who cleaned it up, the branch, and the commit move to a second
// line; under 36rem what it was for and the branch give way.
export const historyListClass =
  '@container grid grid-cols-[auto_fit-content(16rem)_minmax(0,1fr)_auto_fit-content(14rem)_auto_auto_auto] gap-x-3'

export const historyRowClass =
  'group/cleanup col-span-full grid min-h-7 grid-cols-subgrid items-center rounded-md py-0.5 pr-1 pl-2 text-[0.78125rem] hover:bg-raised/60 focus-within:bg-raised/60 @max-4xl:py-1'

// historyCell is what every cell of a cleaned-up line shares: one line, cut short.
const historyCell = 'row-start-1 min-w-0 truncate'

export const historyIconClass = `${historyCell} col-start-1 flex text-success`

export const historyWhereClass = `${historyCell} col-start-2`

export const historyItemClass = `${historyCell} col-start-3 @max-xl:hidden`

export const historyHowClass = `${historyCell} col-start-4 text-xs text-muted @max-4xl:col-[2/5] @max-4xl:row-start-2 @max-xl:col-[2/6]`

export const historyBranchClass = `${historyCell} col-start-5 text-xs text-muted @max-4xl:col-[5/7] @max-4xl:row-start-2 @max-xl:hidden`

export const historyCommitClass = `${historyCell} col-start-6 cursor-help font-mono text-[0.71875rem] text-muted @max-4xl:col-[7/-1] @max-4xl:row-start-2 @max-4xl:justify-self-end @max-4xl:pr-1.5`

export const historyAgeClass = `${historyCell} col-start-7 text-right text-xs text-muted tabular-nums`

// historyActionsClass holds a line's Copy restore command and Open, shown while the line
// is pointed at or holds the focus.
export const historyActionsClass =
  'col-start-8 row-start-1 flex items-center gap-0.5 justify-self-end opacity-0 transition-opacity group-focus-within/cleanup:opacity-100 group-hover/cleanup:opacity-100'

// toneText and toneBg color a part of the page by its tone, spelled out in full so
// Tailwind finds them.
export const toneText: Readonly<Record<Tone, string>> = {
  info: 'text-info',
  warning: 'text-warning',
  caution: 'text-caution',
  success: 'text-success',
  danger: 'text-danger',
  muted: 'text-muted',
}

export const toneBg: Readonly<Record<Tone, string>> = {
  info: 'bg-info',
  warning: 'bg-warning',
  caution: 'bg-caution',
  success: 'bg-success',
  danger: 'bg-danger',
  muted: 'bg-muted',
}
