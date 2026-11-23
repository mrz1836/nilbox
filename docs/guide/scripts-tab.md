# The Scripts tab

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/integrations`](../architecture/integrations.md), [The Scripts tab's pages (frontend)](../architecture/frontend/scripts-pages.md)

**Scripts** sits after Prompts while mage-x or go-pre-commit is on, or you turned Scripts on in
Settings → Integrations; with everything off, the tab goes away. When it first appears because a
tool was found while the window is open, a toast says so, once and never again (*Found magex and
go-pre-commit. They're in the new Scripts tab.*) with **Open Scripts**. The toolbar's filter
isn't shown on Scripts: each page searches in itself.

- **The rail** lists the pages: **Release** while mage-x is on, counting the repositories with
  unreleased changes; **Hooks** while go-pre-commit is on, counting the repositories that need
  attention, with an amber dot; **Repo scripts** while Scripts is on (automatically, while
  mage-x or go-pre-commit is); **Deps** while Scripts is on too, counting the clones whose
  dependencies are stale; and **Runs**, counting the runs in progress. Its foot lists each
  tool's status, and **Manage in Settings** opens Settings → Integrations.
- **It opens** on the page you picked last, even after a restart, or the first page that's on
  when that one is off now. The tab keeps its page and an open release sheet while you switch
  tabs.
- **Headings stay in view:** a group's heading on Release, Hooks, and Deps, and a section's on
  Repo scripts, stays at the top of the page while its rows scroll under it, so you can still see
  which group you're in, and fold it, from anywhere in its list.
- **A page whose tool can't be used** says why in place of its list (*magex isn't on this
  Mac.*), with the way to fix it in Settings.
