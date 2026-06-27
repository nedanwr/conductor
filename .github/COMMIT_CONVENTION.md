# Git Commit Message Convention

> Adapted from [Angular's commit
> convention](https://github.com/angular/angular/blob/main/contributing-docs/commit-message-guidelines.md),
> with one deliberate change: the scope is the **application**, not a single
> feature. Because this codebase is a polyglot monorepo that will grow large,
> scoping by application keeps history legible and changelogs groupable by product
> surface.

## Message format

All commit messages must match:

```
/^(revert: )?(feat|fix|docs|style|refactor|perf|test|build|ci|chore|types)\(.+\)!?: .{1,72}/
```

Full structure:

```
<type>(<scope>): <subject>
<BLANK LINE>
<body>
<BLANK LINE>
<footer>
```

> [!IMPORTANT]
> The header (`<type>(<scope>): <subject>`) is mandatory, and so is the scope.
> Body and footer are optional. Repo-wide and root changes use the `monorepo`
> scope.

## Type

Required. Determines changelog categorization.

| Type       | Use for                                 | In changelog |
| ---------- | --------------------------------------- | ------------ |
| `feat`     | A new feature                           | ✅           |
| `fix`      | A bug fix                               | ✅           |
| `perf`     | A performance improvement               | ✅           |
| `docs`     | Documentation only                      | —            |
| `style`    | Formatting/whitespace, no logic change  | —            |
| `refactor` | Code change that neither fixes nor adds | —            |
| `test`     | Adding or correcting tests              | —            |
| `build`    | Build system or dependencies            | —            |
| `ci`       | CI configuration and scripts            | —            |
| `chore`    | Other maintenance                       | —            |
| `types`    | Type definition changes                 | —            |

> [!NOTE]
> Breaking changes always appear in the changelog regardless of type.

## Scope — the application

Required. The scope names the application or package the change belongs to.
Current scopes:

- `git-backend` — the from-scratch git server (Go module)
- `kernel` — the orchestrator kernel (`packages/kernel`)
- `shared-types` — shared kernel types (`packages/shared-types`)
- `ui` — shared component library (`packages/ui`)
- `desktop` — desktop orchestrator app (`apps/desktop`)
- `web` — Git Web + Platform API (`apps/web`)
- `monorepo` — repo-wide tooling, config, or root files

Add a new scope here when a new application or package is introduced. A commit
that genuinely spans multiple applications uses the `monorepo` scope, but prefer
splitting it into per-application commits where practical.

## Subject

Required. Maximum 72 characters.

- Imperative, present tense: "add", not "added" or "adds".
- No capitalization of the first letter.
- No trailing punctuation.

## Body

Optional. Imperative, present tense. Explain the motivation for the change and
contrast it with previous behavior.

## Footer

Optional. Use it to:

- Reference closed issues, e.g. `Closes #123`.
- Document breaking changes (see below).

## Breaking changes

Add `!` before the colon in the header and describe the change in the footer:

```
feat(git-backend)!: change placement resolution to be repo-UUID keyed

BREAKING CHANGE: the registry placement API now takes a repo UUID instead of
an owner/name pair. Callers must resolve the UUID before calling.
```

## Reverts

A revert commit begins with `revert: ` followed by the header of the reverted
commit. The body should state which commit is reverted:

```
revert: feat(desktop): add run timeline view

This reverts commit <hash>.
```

## Examples

```
feat(web): add issue-to-task dispatch endpoint
fix(kernel): release provider session on hard interrupt
perf(git-backend): stream pack output instead of buffering
docs(monorepo): document the branch model in the README
refactor(git-backend): extract ref-update locking into its own type
test(kernel): add replay-equivalence property test for the task graph
chore(monorepo): bump Go toolchain to the pinned version
types(shared-types): brand EventSeq as a distinct numeric type
feat(desktop)!: drop legacy local-state schema
```
