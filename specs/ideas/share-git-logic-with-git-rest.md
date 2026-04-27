---
status: idea
---

# Share git logic with git-rest

## Idea

Extract the git plumbing from `git-rest` into a shared Go library and have `task/controller` import it instead of embedding its own git client. Same Go process — NO runtime HTTP dependency on git-rest. The shared code becomes a reusable Go package consumed by both `git-rest` (HTTP wrapper) and `task/controller` (Kafka-driven mutator).

## Why

`task/controller` and `git-rest` solve the same problem (manage a local git clone with periodic pull, auto-commit + push on writes, optional SSH key, conflict handling). They currently maintain duplicate implementations. Centralizing in one Go package means:

- **One source of truth** for git auth, retry, conflict-handling semantics
- **One place to fix bugs** (current bugs in either are silently inherited)
- **Smaller controller binary** — drop direct dependency on `go-git` (or whatever git-rest uses)
- **Reusable for future services** — any K8s service that needs to manage a git clone can import the same package

Different from the architectural pivot we explicitly **rejected** (using git-rest as a runtime HTTP dependency from controller). That would have introduced a new failure surface (git-rest down → controller down). This idea is purely a code-sharing refactor — same in-process semantics, just shared implementation.

## Sketch

- Carve out `git-rest`'s git layer into an internal package, e.g. `pkg/gitrepo/` (clone, pull, write, commit, push, conflict-handling)
- Move it to a shared location — either a third repo (`bborbe/gitrepo`?) or kept in `git-rest` and imported by `task/controller` as a library dep
- `task/controller` swaps its embedded git plumbing for the library
- `git-rest`'s HTTP layer stays in `git-rest` repo, just wraps the library
- No behavior change for either service externally

## Risks / Open questions

- Where does the shared package live? Options: (a) new repo `bborbe/gitrepo`, (b) keep in `git-rest` and import `git-rest/pkg/gitrepo`, (c) put in `bborbe/agent/lib`. (a) is cleanest for reuse outside the agent ecosystem; (b) requires git-rest to expose stable internal API; (c) mismatches scope (lib is agent-specific).
- Versioning coordination — every consumer pins a version. Breaking changes in the shared package = coordinated bumps.
- Controller's current git plumbing may have agent-specific quirks (e.g. how it handles concurrent Kafka commands writing to same file) — verify these survive the refactor.
- Worth doing only if a third consumer emerges (Rule of Three). Today it's just controller + git-rest. With pr-watcher (and future watchers) NOT touching git directly via the Kafka-command-to-controller path, the controller is the only non-git-rest git consumer.

## Out of scope (explicitly rejected, separate decision)

- Using git-rest as a runtime HTTP dependency from controller — rejected because it adds a synchronous failure surface and runtime infra coupling. The current Kafka-command-to-controller architecture stays.

## When to revisit

When a third service needs to manage a git clone in-process (i.e., not just talk to one over HTTP). Or when controller's git plumbing accrues a bug that we'd rather fix once than twice.

## Related

- `~/Documents/workspaces/git-rest/` — current implementation
- `~/Documents/workspaces/agent/task/controller/pkg/` — current controller git plumbing
- `~/Documents/workspaces/agent/specs/in-progress/017-create-task-command.md` — the controller-side change in flight that surfaced this observation
