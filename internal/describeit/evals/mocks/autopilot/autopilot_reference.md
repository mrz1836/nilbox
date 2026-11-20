---
type: fixed
---

# The proposal, and a routine's definition

A proposal is one JSON object, given as `proposal` to `autopilot_check` and `autopilot_propose`:

```
Proposal { summary: string, prompts?: NewPrompt[], routines?: RoutineChange[], settings?: SettingsChange, rules?: RulesChange }
RoutineChange { id?: number, base?: string, name: string, description?: string, mode?: "dryRun" | "ask" | "auto", template?: string, spec: Spec }
Spec { version: 1, watch: { sources: [{ lists: ("reviewRequested" | "authored" | "assigned")[] }] }, trigger: { event: string }, conditions: { match: "all" | "any", of: [] }, steps: Step[], limits: {} }
Step { notify?: { note?: string } } | { pin?: {} } | { snooze?: { days?: number } } | { agent?: { prompt: { starter?: string, nickname?: string } } }
```

A new routine has no `id`; its `mode` is only a hint, and it arrives switched off. A proposal has no field for a routine's switch, Autopilot's own switch, or Pause.

## A worked example

```json
{
  "summary": "Pins your pull requests when their checks fail, and tells you.",
  "routines": [
    {
      "name": "Pin my PRs when checks fail",
      "mode": "auto",
      "spec": {
        "version": 1,
        "watch": { "sources": [{ "lists": ["authored"] }] },
        "trigger": { "event": "checks_failed" },
        "steps": [{ "pin": {} }, { "notify": { "note": "Its checks failed" } }]
      }
    }
  ]
}
```
