# internal/bump

`internal/bump/` reads a dependency update from a bot's pull request title into an `Update`,
for the review sheet and the `.Dependency` prompt variables, and which bot's branch a
head branch is and the ecosystem a Dependabot branch names (`ParseBranch`, the
`dependabot.yml` words, `Ecosystems`), for Autopilot's conditions. It is pure and imports
no service.
