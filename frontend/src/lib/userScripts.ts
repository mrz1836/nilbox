// The user's own scripts on Repo scripts: a new one as the editor starts it, how a row
// words where one shows and runs, the repositories the editor's Chosen list offers, and
// the variables every one runs with.

import { brand } from '../generated/brand'
import type { RunsIn, UserScript, UserScriptIcon, UserScriptRunsIn, UserScriptScope } from '../platform'

// newUserScript is a script as the editor starts a new one: for every repository, run
// where the setting sends runs, without asking first. ID 0 asks Save to create it.
export function newUserScript(): UserScript {
  return { id: 0, name: '', command: '', icon: 'play', scope: 'all', repos: [], runsIn: '', confirm: false }
}

// scopeLabels name each scope as the editor offers it.
export const scopeLabels: Readonly<Record<UserScriptScope, string>> = {
  all: 'All repositories',
  go: 'Go repositories',
  repos: 'Chosen…',
}

// iconLabels name each icon, t3.json's names, in the order the editor offers them.
export const iconLabels: Readonly<Record<UserScriptIcon, string>> = {
  play: 'Play',
  test: 'Test',
  lint: 'Lint',
  build: 'Build',
  configure: 'Configure',
}

// placeName names a place a run goes: the app, or the terminal chosen for launches.
export function placeName(place: RunsIn, terminalName: string): string {
  return place === 'app' ? brand.name : terminalName
}

// placeOf is where the script u runs: its own place, or else setting.
export function placeOf(u: UserScript, setting: RunsIn): RunsIn {
  return u.runsIn === '' ? setting : u.runsIn
}

// runsInLabel names a script's own place as the editor offers it.
export function runsInLabel(place: UserScriptRunsIn): string {
  switch (place) {
    case 'app':
      return brand.name
    case 'terminal':
      return 'Terminal'
    default:
      return 'As set in Settings'
  }
}

// scopeWords say which repositories u shows in: all, the Go ones, or those it names.
export function scopeWords(u: UserScript): string {
  switch (u.scope) {
    case 'all':
      return 'All repositories'
    case 'go':
      return 'Go repositories'
    default:
      return u.repos.join(', ')
  }
}

// aboutScript is the quiet line under one of the user's scripts: where it shows, where it
// runs when it says, and that it asks first.
export function aboutScript(u: UserScript, terminalName: string): string {
  return [
    scopeWords(u),
    ...(u.runsIn === '' ? [] : [`Runs in ${placeName(u.runsIn, terminalName)}`]),
    ...(u.confirm ? ['Asks before running'] : []),
  ].join(' · ')
}

// sameRepo compares two repositories as GitHub does, in any case.
function sameRepo(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase()
}

// repoChoices are the repositories the editor's Chosen list offers: the clones', and
// those already chosen, which may no longer be cloned, each once in any case, sorted.
export function repoChoices(names: readonly string[], chosen: readonly string[]): string[] {
  const out: string[] = []

  for (const name of [...chosen, ...names]) {
    if (!out.some((n) => sameRepo(n, name))) {
      out.push(name)
    }
  }

  return out.sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase()))
}

// toggleRepo chooses name, or unchooses it, in any case.
export function toggleRepo(chosen: readonly string[], name: string, on: boolean): string[] {
  const rest = chosen.filter((n) => !sameRepo(n, name))

  return on ? [...rest, name] : rest
}

// isChosen reports whether name is among chosen, in any case.
export function isChosen(chosen: readonly string[], name: string): boolean {
  return chosen.some((n) => sameRepo(n, name))
}

// scriptVariables name the variables each of the user's scripts runs with, from the
// product's slug: its repository, owner/name; its folder; and its default branch.
export const scriptVariables: readonly string[] = ['REPO', 'REPO_DIR', 'DEFAULT_BRANCH'].map(
  (v) => `${brand.slug.toUpperCase()}_${v}`,
)

// readyToSave reports whether the editor's script can be saved: it has a name and a
// command, and a script for chosen repositories has one chosen.
export function readyToSave(u: UserScript): boolean {
  return u.name.trim() !== '' && u.command.trim() !== '' && (u.scope !== 'repos' || u.repos.length > 0)
}
