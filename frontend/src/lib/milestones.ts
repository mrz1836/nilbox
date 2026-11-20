import type { Achievement, NilMoment } from '../platform'

// The inbox's milestones as the party shows them: what each achievement is called, the
// chip that says what a time at nil was worth, and the one-liners that fit the moment.
// internal/milestones decides what each time unlocks, and whether confetti flies for it.

// achievementTitle is what the party calls an achievement.
export function achievementTitle(a: Achievement): string {
  switch (a.kind) {
    case 'streak':
      return `${String(a.n)}-day streak`
    case 'count':
      return `${String(a.n)} nils`
    case 'first':
      return 'First nil'
    case 'sweep':
      return 'Clean sweep'
    case 'early':
      return 'Early bird'
    case 'friday':
      return 'Friday finish'
    case 'weekend':
      return 'Weekend warrior'
    case 'owl':
      return 'Night owl'
  }
}

// NilChip is the chip beside the party's heading: an achievement unlocked, with the most
// significant one's title, how many more came with it, and every title for the tooltip;
// or a plain note of the streak or the nil's number.
export type NilChip =
  { kind: 'unlocked'; title: string; more: number; titles: string[] } | { kind: 'note'; text: string }

// nilChip is the chip for a moment: an unlock shows its achievement, an inbox the user
// emptied with nothing unlocked shows its streak from the second day on, else its number,
// and one found empty shows the streak from the second day on. null shows none.
export function nilChip(moment: NilMoment | null): NilChip | null {
  if (moment === null) {
    return null
  }

  const [top, ...rest] = moment.unlocked

  if (top !== undefined) {
    return {
      kind: 'unlocked',
      title: achievementTitle(top),
      more: rest.length,
      titles: moment.unlocked.map(achievementTitle),
    }
  }

  if (moment.streak >= 2) {
    return { kind: 'note', text: `Day ${String(moment.streak)} at nil` }
  }

  if (moment.earned && moment.count > 0) {
    return { kind: 'note', text: `Nil #${String(moment.count)}` }
  }

  return null
}

// standupHour is when a weekday morning's standup starts, before which nil makes for an
// easy update.
const standupHour = 10

// momentQuips are the one-liners that fit an inbox the user emptied at now, the most
// significant first: a sweep, a new best streak, a streak, a Friday, a weekend, before
// standup, and after dark.
export function momentQuips(moment: NilMoment, now: Date): string[] {
  const out: string[] = []
  const { cleared, streak, best } = moment
  const hour = now.getHours()
  const day = now.getDay()
  const weekend = day === 0 || day === 6

  if (cleared >= 10) {
    out.push(`${String(cleared)} down, 0 to go.`)
  }

  if (streak >= 3 && streak === best) {
    out.push(`${String(streak)} days running. A new personal best.`)
  } else if (streak >= 2) {
    out.push(`Day ${String(streak)} at nil. Don’t break the chain.`)
  }

  if (day === 5) {
    out.push('Nil on a Friday. The weekend starts now.')
  }

  if (weekend) {
    out.push('Nil on a weekend. Nobody asked, and you did it anyway.')
  }

  if (!weekend && hour >= 5 && hour < standupHour) {
    out.push('Nil before standup. Your update writes itself.')
  }

  if (hour >= 20 || hour < 5) {
    out.push('Nil after dark. The bugs can sleep tonight.')
  }

  return out
}

// momentQuip is the one-liner that fits a moment the user earned at now, or null when none
// does or the inbox was found empty: the most significant when it unlocked something, so the
// quip speaks to the achievement, else one of those that fit, at random.
export function momentQuip(moment: NilMoment, now: Date, random: () => number = Math.random): string | null {
  if (!moment.earned) {
    return null
  }

  const fit = momentQuips(moment, now)

  if (moment.unlocked.length > 0) {
    return fit[0] ?? null
  }

  return fit[Math.min(Math.floor(random() * fit.length), fit.length - 1)] ?? null
}
