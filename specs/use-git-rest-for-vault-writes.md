---
status: draft
---

# Use git-rest as runtime dependency for vault writes

## Idea

`task/controller` calls `git-rest` over HTTP for all git operations (clone state, write file, commit, push, pull) instead of embedding its own git client. Removes duplicate git plumbing from the controller; git-rest becomes THE service that manages git clones for the cluster.

**Direction reversed 2026-05-02**: previously this idea was framed as "share git code as a library, NO runtime HTTP dependency." That framing is obsolete — using git-rest at runtime is now the preferred direction because it eliminates duplicate logic operationally, not just at the code level.

## Why

`task/controller` and `git-rest` currently solve the same problem twice. Operational consequences:

- **Duplicate bugs** — when git auth, conflict resolution, or retry semantics drift between the two, the controller silently behaves differently from git-rest
- **Two clones to babysit** — controller maintains its own `/data/vault` clone with its own pull cycle; git-rest maintains its own; both can desync from the remote independently
- **No central audit point** — any service that wants to log/observe git ops touching the vault has to instrument both paths

Centralizing on git-rest (one process, one clone, one HTTP API) gives the cluster a single git surface. The controller becomes a Kafka consumer + git-rest client — much smaller responsibility.

## Sketch

- Define git-rest's HTTP API for the operations the controller needs: `write file at path`, `read file at path`, `list files`, `pull`, `branch state`. Some of this exists; gap-fill as needed.
- Controller drops `pkg/gitclient/` and switches to a thin git-rest HTTP client.
- Controller no longer mounts a vault PVC — git-rest holds the clone.
- Authentication for git-rest itself moves into the controller's request signing (mTLS / shared secret / etc.).

## Risks / Open questions

- **New synchronous failure surface**: git-rest down → controller stalls on every Kafka command. Was the previous reason this idea was rejected. Mitigation: K8s readiness/liveness on git-rest, controller retries-with-backoff, and the Kafka offset stays put on git-rest unavailability so messages aren't lost.
- **Latency**: HTTP round-trip per write vs in-process file write. Controller throughput is low (one Kafka command at a time); should be fine.
- **Clone ownership**: git-rest already manages a vault clone for other consumers. Controller switching to it = potential conflict between writers (HTTP API and any internal git-rest sync loop). Need to confirm git-rest's write semantics handle concurrent callers.
- **Migration**: dual-write phase first (controller writes both directly and via git-rest, compare outputs) before removing controller's git plumbing.

## Out of scope

- Replacing the Kafka-command pattern with synchronous HTTP from watcher to controller. That's a separate (larger) decision.

## Related

- `~/Documents/workspaces/git-rest/` — current implementation
- `~/Documents/workspaces/agent/task/controller/pkg/gitclient/` — code to be removed
- `~/Documents/workspaces/agent/specs/in-progress/017-create-task-command.md` — controller-side change in flight
- 2026-05-02 finding: schema drift between watcher's `lib v0.57.0` and deployed controller's `lib v0.53.3-5` caused silent message skip. Centralizing on git-rest reduces controller's dependency on `lib` (no in-process git semantics to keep in sync), making this class of drift smaller.

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
