# AGENTS.md

## 🎯 Purpose & Scope

This file defines the **baseline standards, workflows, and structure** for *all contributors and AI agents* operating within this repository. It serves as the root authority for engineering conduct, coding conventions, and collaborative norms.

It is designed to help AI assistants (e.g., Codex, Claude, Gemini) and human developers alike understand our practices, contribute clean and idiomatic code, and navigate the codebase confidently and effectively.

> Whether reading, writing, testing, or committing code, **you must adhere to the rules in this document.**

Additional `AGENTS.md` files **may exist in subdirectories** to provide more contextual or specialized guidance. These local agent files are allowed to **extend or override** the root rules to fit the needs of specific packages, services, or engineering domains—while still respecting the spirit of consistency and quality defined here.

<br><br>

## 🔒 Project Rules (Nilbox)

These rules are specific to this repository and override anything less strict elsewhere:

* **Open source, no private references.** Never write the names of employers, clients, customers, companies, private projects, or private repositories into this repo. Never write personal machine paths (absolute home directories) or private tracker or ticket IDs. This covers code, comments, tests, fixtures, docs, commit messages, branch names, and pull request titles and bodies. Use neutral placeholders such as `acme-org`, `example-repo`, and `~/projects`.
* **Never bypass git hooks.** No `--no-verify`, no `-n`. If a hook fails, fix the cause.
* **One home for the product name.** Product identity lives in `internal/brand`. Reference it; never hard-code the name elsewhere.
* **User data stays local.** The app talks only to GitHub's API; preferences, triage state, and caches never leave the user's machine.
* **The gate.** Run `magex format:fix && magex lint && magex test:race && go-pre-commit run --all-files`, then `magex frontend:gate`, before every pull request; on macOS also run `magex app:check` when the desktop shell, a bound service, or the frontend changed. See [docs/desktop.md](../docs/desktop.md).

<br><br>

## 📚 Technical Conventions

Our technical standards are organized into focused, portable documents in the `.github/tech-conventions/` directory:

### Core Development
* **[Go Essentials](tech-conventions/go-essentials.md)** - Context-first design, interfaces, goroutines, error handling
* **[Testing Standards](tech-conventions/testing-standards.md)** - Unit tests, coverage requirements, best practices
* **[Commenting & Documentation](tech-conventions/commenting-documentation.md)** - Code comments, package docs, markdown

### Version Control & Collaboration
* **[Commit & Branch Conventions](tech-conventions/commit-branch-conventions.md)** - Git workflow standards
* **[Pull Request Guidelines](tech-conventions/pull-request-guidelines.md)** - PR structure and review process
* **[Release Workflow & Versioning](tech-conventions/release-versioning.md)** - Semantic versioning and releases

### Project Management & Infrastructure
* **[Labeling Conventions](tech-conventions/labeling-conventions.md)** - GitHub label system
* **[Dependency Management](tech-conventions/dependency-management.md)** - Go modules and security
* **[Security Practices](tech-conventions/security-practices.md)** - Vulnerability reporting and secure coding
* **[GitHub Workflows Development](tech-conventions/github-workflows.md)** - Actions workflow best practices

> 💡 **Start with [tech-conventions/README.md](tech-conventions/README.md)** for a complete index with descriptions.

<br><br>

## 📁 Directory Structure

| Directory                   | Description                                             |
|-----------------------------|---------------------------------------------------------|
| `.github/`                  | Issue templates, workflows, and community documentation |
| `.github/actions/`          | GitHub composite actions for CI/CD and automation       |
| `.github/ISSUE_TEMPLATE/`   | Issue and pull request templates                        |
| `.github/tech-conventions/` | Technical conventions and standards for development     |
| `.github/workflows/`        | GitHub Actions workflows for CI/CD                      |
| `.vscode/`                  | VS Code settings and extensions for development         |
| `build/darwin/`             | Generated macOS bundle metadata (Info.plist)            |
| `cmd/nilbox/`               | Application entry point (headless; desktop with the `desktop` tag) |
| `docs/`                     | Desktop build guide and measured performance            |
| `frontend/`                 | React + TypeScript desktop frontend and its generated bindings |
| `internal/brand/`           | Product identity and design tokens                      |
| `internal/brandgen/`        | Generates frontend theme tokens and Info.plist from the brand |
| `internal/core/`            | Assembles the app and the services the window binds     |
| `internal/measure/`         | Startup, refresh, launch, and size measurements         |
| `internal/*/`               | The inbox, launcher, settings, profiles, worktrees, and data-layer packages |
| `magefiles/`                | Custom `magex` targets: `brand`, `frontend`, `app`, `measure` |
