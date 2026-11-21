// The theme as the stylesheet spells it, for tests that check a name against it.

// NodeFS is the one call of Node's fs these tests make.
interface NodeFS {
  readFileSync(path: URL, encoding: 'utf8'): string
}

// NodeProcess is Node's process, reached without Node's types, which the window's own
// TypeScript leaves out.
interface NodeProcess {
  getBuiltinModule(id: 'node:fs'): NodeFS
}

// here is this file's own URL. It stays apart from the new URL below, which Vite would
// otherwise rewrite into an address on its own server.
const here = import.meta.url

// themeColors are the theme's color tokens, the --color-* custom properties the generated
// theme.css defines, in its order: "--color-background", "--color-surface", and so on.
// Vitest hands a test an empty string for any stylesheet it imports, ?raw included, so they
// are read from the file itself.
export function themeColors(): string[] {
  const { process } = globalThis as unknown as { process: NodeProcess }
  const css = process.getBuiltinModule('node:fs').readFileSync(new URL('../generated/theme.css', here), 'utf8')

  return [...css.matchAll(/--color-[a-z0-9-]+(?=:)/g)].map((m) => m[0])
}
