# Conductor

An AI-native development platform — a coordination layer that lets developers
orchestrate multiple AI coding agents across multiple projects at once, using the
AI subscriptions they already pay for.

Conductor is built around two products that share one foundation and one design
language, but each stands on its own:

- **Orchestrator** — a multi-agent coding workflow tool. Bring your own AI
  subscription (BYOS); Conductor runs multiple agents in parallel across isolated
  git worktrees, curates per-task context for each invocation, and captures the
  reasoning behind every decision as a first-class, queryable artifact.
- **Git platform** — a git host purpose-built for AI-assisted and AI-generated
  code, backed by a from-scratch git server. It stores not just _what_ changed
  (the diff) but _why_ (the intent and reasoning chain behind it).

## Status

Early development. This is the `main` branch; active work happens on `develop`.

## Repository layout

This is a polyglot monorepo:

```
.
├── packages/      # TypeScript libraries (Bun workspaces)
├── apps/          # TypeScript applications (Bun workspaces)
└── git-server/    # Go module — the git server (outside the Bun workspaces)
```

The TypeScript side uses [Bun](https://bun.sh) workspaces; the git server is a
self-contained Go module built with the Go toolchain.
