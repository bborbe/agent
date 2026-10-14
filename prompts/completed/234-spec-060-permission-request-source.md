---
status: completed
spec: [060-a-permission-request-carries-its-raising-path]
summary: Added claude.PermissionSource (a closed set of /prompt and /a2a) carried per turn on the context, stamped onto claude.PermissionRequest.Source, set by both turn-serving routes, and documented in the frozen interactive-service contract.
execution_id: agent-permgate-exec-234-spec-060-permission-request-source
dark-factory-version: v0.198.7
created: "2026-10-14T15:40:43Z"
queued: "2026-10-14T15:55:11Z"
started: "2026-10-14T16:03:09Z"
completed: "2026-10-14T16:20:38Z"
branch: dark-factory/a-permission-request-carries-its-raising-path
---

<summary>
- A permission request raised mid-turn now says which of the service's two turn-serving routes raised it.
- A decider outside this library can read that value and answer an A2A-raised request differently from an interactive one.
- The value is set by the route that serves the turn and is never taken from the request, so a caller cannot choose it.
- Both routes are covered: a request raised on the interactive prompt route and one raised on the A2A route carry different, non-empty values.
- The value is carried per turn, not per session, so two routes interleaved on one cached conversation each see their own value.
- A decider written before this change keeps compiling and behaves exactly as it does today on both routes.
- The interactive service's frozen contract document now describes the new value and the closed set it can take.
- Nothing else moves: the interactive gate still blocks on the permission endpoint and still resumes on a posted verdict, a service built without a decider still fails an out-of-allowlist turn, and no route's wire shape changes.
- The library gains no new dependency, no new configuration knob and no new metric.
</summary>

<objective>
Add a discriminator to the permission seam so a `PermissionDecider` can tell, from the value it is handed, whether the request it is deciding was raised by the A2A route or by the `/prompt` route, and can therefore answer the two differently. The value is populated by the route that serves the turn, travels on the turn's context (the only carriage that does not change `Session.Prompt`'s signature), is stamped onto the request the decider receives, and is recorded in the interactive service's frozen contract document in the same change. `Session.Prompt` and `PermissionDecider.DecidePermission` keep their signatures and no existing consumer's behaviour changes.
</objective>

<context>
Read `CLAUDE.md` (repo root) for the project conventions before you change anything.

Read the linked spec in full: `specs/in-progress/060-a-permission-request-carries-its-raising-path.md`.
It is the authority for this prompt. Its `## Non-goals` section is a hard veto list — do not add a
publish path, a second decider method, a configurable source, a metric, or any change to the A2A
protocol, the attention store or the pod's HTTP routes.

Read these source files in full before editing (paths are relative to the repo root):

- `claude/claude-session.go` — the permission seam. `PermissionRequest` is the struct at the
  `// PermissionRequest describes a tool invocation` comment; `PermissionDecider` is the interface
  carrying `//counterfeiter:generate -o ../mocks/claude-permission-decider.go --fake-name
  ClaudePermissionDecider . PermissionDecider`; `answerPermission` is the method that builds the
  `PermissionRequest` and calls the decider; `sessionArgs` appends `--permission-prompt-tool stdio`
  and `--permission-mode manual` only when the decider is non-nil.
- `claude/claude-model.go` — the in-package precedent for a small typed-string value set
  (`ClaudeModel`): a typed `string`, a `const` block, a doc comment, and nothing else. Mirror this
  shape for the new type; do not add a `Validate()`, a `Contains()` or an `Available*` collection,
  because nothing in this change ranges over the set.
- `interactive/prompt.go` — `promptHandler`, the route that serves `POST /prompt`.
- `interactive/a2a-handler.go` — `a2aExecutor.Execute`, the route that serves `POST /a2a`.
- `interactive/permission.go` — `PermissionRegistry` / `PendingPermission`, the `/permission`
  registry and the JSON shape it renders. Confirm `PendingPermission` has no `Source` field and that
  this change does not add one.
- `interactive/service.go` — `Handler()` (the router) and `NewServiceWithPermissions`.
- `interactive/session-cache.go` — `sessionEntry.Prompt`, which is the one place a turn's context is
  forwarded to the backend session. The cache is shared by both routes.
- `interactive/permission_test.go` — reuse its helpers: `permissionFactory`, `permissionSession`,
  `postPromptFull`, `newPermissionTestServer`, `getPermissions`, `postVerdict`.
- `interactive/a2a_handler_test.go` — reuse `postA2A` and `a2aSendBody`.
- `interactive/a2a_test.go` — `testPublicURL`, `newA2ATestServer`.
- `interactive/claude-session_test.go` — `claudeServiceShim` and `installClaudeShim`, the pattern for
  faking the `claude` CLI on `PATH`.
- `claude/claude-session_test.go` — `sessionShimPermission`, `sessionShimPermissionShort`,
  `writeSessionShim`, `sessionConfig` and `indexOf`: the pattern for driving a real session against
  a permission-raising shim. Note that each top-level `Describe` owns its own closures — the
  permission `Describe` has no `openSession` helper.
- `docs/interactive-service.md` — the frozen contract. Read the `## Permission endpoint` section in
  full; your additions go there.
- `docs/dod.md` — the definition of done the validator applies (coverage, doc comments, changelog).

Read these coding-plugin guides (in-container paths) before writing code or tests:

- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` (linter limits: funlen 80,
  nestif 4, golines 100 — wrap every doc comment at 100 columns)
</context>

<requirements>
1. **Create `claude/permission-source.go`** holding the discriminator type and its context carriage.
   Start the file with the repo's BSD license header exactly as the sibling files carry it
   (`// Copyright (c) 2026 Benjamin Borbe All rights reserved.` / `// Use of this source code is
   governed by a BSD-style` / `// license that can be found in the LICENSE file.`), then
   `package claude`. The file contains, and contains only:

   ```go
   import "context"

   // PermissionSource identifies which of the interactive service's turn-serving routes
   // raised a permission request. It is the closed set {PromptPermissionSource,
   // A2APermissionSource}: a turn is served by POST /prompt or by the A2A JSON-RPC binding,
   // and no third route serves a turn today. A future third route must add its own value
   // here and to docs/interactive-service.md.
   type PermissionSource string

   const (
       // PromptPermissionSource marks a request raised by a POST /prompt turn.
       PromptPermissionSource PermissionSource = "/prompt"
       // A2APermissionSource marks a request raised by a POST /a2a turn.
       A2APermissionSource PermissionSource = "/a2a"
   )

   // permissionSourceContextKey is the type of the context key a PermissionSource travels
   // under. It is an unexported empty struct, so no other package can construct an equal
   // key and no other package's value can be read back as a PermissionSource.
   type permissionSourceContextKey struct{}

   // WithPermissionSource returns a copy of ctx carrying source. The route that serves a
   // turn sets it before calling Session.Prompt; the session reads it back in
   // answerPermission and stamps it onto the PermissionRequest it hands the decider.
   //
   // A route must never populate it from request data. X-Session-Id is caller-controlled,
   // so a caller that could choose its own source could make a pane-answered /prompt gate
   // look like an A2A gate and route it to a channel the operator answers.
   func WithPermissionSource(ctx context.Context, source PermissionSource) context.Context {
       return context.WithValue(ctx, permissionSourceContextKey{}, source)
   }

   // PermissionSourceFromContext returns the PermissionSource carried by ctx, or the empty
   // PermissionSource when ctx carries none. The empty value is what a session whose
   // caller named no route sees — a caller outside the interactive service, or a test that
   // drives the session directly — and it is the value a decider written before this field
   // existed already ignores.
   func PermissionSourceFromContext(ctx context.Context) PermissionSource {
       source, _ := ctx.Value(permissionSourceContextKey{}).(PermissionSource)
       return source
   }
   ```

   Two constants and no `Validate`, `Contains`, `String` or `Available*` collection: the shape
   mirrors the package's existing `ClaudeModel` (`claude/claude-model.go`) and nothing in this
   change ranges over the set. Do not add a JSON tag anywhere in this file.

2. **Add the field to `claude.PermissionRequest`** in `claude/claude-session.go` (the struct with
   the `// PermissionRequest describes a tool invocation the CLI raised mid-turn` comment, whose
   existing fields are `ToolName`, `Description` and `InputPreview`). Add one field, after
   `InputPreview`, with a doc comment that names the closed set:

   ```go
   // Source identifies the interactive service route that raised the request — /prompt
   // or /a2a. It is the empty PermissionSource when the caller that built the session
   // named no route. It is read from the turn's context, never from the request's own
   // data.
   Source PermissionSource
   ```

   Carry no JSON tag: the three sibling fields carry none, and this struct is not a wire type.
   The field is additive, so every existing composite literal and every existing consumer keeps
   compiling.

3. **Stamp the field in `answerPermission`** (`claude/claude-session.go`, the method that builds the
   `PermissionRequest` and calls `s.decider.DecidePermission`). Add the `Source` entry to the
   existing struct literal, reading it from the same `ctx` the method already holds:

   ```go
   decision, err := s.decider.DecidePermission(ctx, PermissionRequest{
       ToolName:     event.Request.ToolName,
       Description:  event.Request.Description,
       InputPreview: previewOf(event.Request.Input),
       Source:       PermissionSourceFromContext(ctx),
   })
   ```

   Change nothing else in the method: the nil-decider guard, the error wrapping and the verdict
   write-back stay exactly as they are.

4. **Set the source on the `/prompt` route** in `interactive/prompt.go`. In `promptHandler`, derive
   the turn context from `r.Context()` and pass that derived context to both the cache lookup and
   the turn, so the value is per turn and never stored on the cached session:

   ```go
   ctx := claude.WithPermissionSource(r.Context(), claude.PromptPermissionSource)
   entry := s.cache.Get(ctx, sessionID)
   result, err := entry.Prompt(ctx, prompt)
   ```

   Add the import `"github.com/bborbe/agent/claude"`. Leave the rest of the handler untouched:
   the method check, `sessionIDFromRequest(r)` (which keeps using `r.Context()` for its error), the
   1 MiB `io.LimitReader` body cap, the digest log line, the `text/plain; charset=utf-8` header and
   the raw-body response all stay as they are.

5. **Set the source on the A2A route** in `interactive/a2a-handler.go`. In
   `(*a2aExecutor).Execute`, inside the returned closure, set the source on the incoming `ctx`
   before any session is resolved — before the session-id pattern check, so the value is on the
   context for the whole turn:

   ```go
   return func(yield func(a2a.Event, error) bool) {
       ctx = claude.WithPermissionSource(ctx, claude.A2APermissionSource)
       sessionID := execCtx.Message.ContextID
       ...
   ```

   Add the import `"github.com/bborbe/agent/claude"`. Change nothing else: the session-id regex
   check and its error, `promptFromMessage`, the `SubmittedTask` / `ArtifactEvent` /
   `StatusUpdateEvent` sequence, the failure log line and the `Cancel` implementation all stay as
   they are.

6. **Do not change any of these.** `Session.Prompt` and `SessionFactory.Create` signatures;
   `PermissionDecider`'s method set and `DecidePermission`'s signature; `PermissionDecision`;
   `PendingPermission` and the `GET /permission` JSON body (it must still render one object per pending request carrying
   exactly `id`, `tool_name`, `description` and `input_preview` — the new value is a field of
   the in-process request the decider receives, not part of any route's wire shape);
   `PermissionRegistry`'s method set; `NewPermissionRegistry`; `sessionArgs` and the
   nil-decider flag behaviour (the `--permission-prompt-tool stdio` and `--permission-mode manual`
   flags are still appended only when the decider is non-nil); `NewSession`,
   `NewSessionFactory`, `NewService` and `NewServiceWithPermissions` signatures; the
   `//counterfeiter:generate -o ../mocks/claude-permission-decider.go --fake-name
   ClaudePermissionDecider . PermissionDecider` directive (the shortform
   `//counterfeiter:generate . Foo` is a silent no-op in this repo — do not switch to it; the
   interface signature is unchanged, so `mocks/claude-permission-decider.go` needs no edit).

7. **Test the seam end to end through both real routes** (AC1, AC2, and Failure Modes rows 3 and 4).
   Create `interactive/permission-source_test.go`, `package interactive_test`, with a Ginkgo spec
   named so its intent is greppable — e.g.
   `It("stamps the raising route onto the permission request it hands the decider", ...)`. The spec
   must use the real `claude` session, not a fake session that reads the context itself, so it
   crosses the real boundary (route sets the value → session reads it → decider receives it).

   - A long-lived permission-raising shim: a `#!/bin/sh` script named `claude` that loops, so one
     held process serves both turns on the shared session id. Each iteration reads one turn line,
     appends it to `$SHIM_DIR/stdin.log`, prints one `control_request` line with
     `"request_id"` unique per turn, `"request": {"subtype": "can_use_tool", "tool_name": "Bash",
     "input": {"command": "ls"}}`, reads the verdict line, appends it to `$SHIM_DIR/stdin.log`, then
     prints `{"type":"result","result":"reply-<n>"}`. Install it exactly as
     `installClaudeShim` in `interactive/claude-session_test.go` does: write it into
     `GinkgoT().TempDir()` as `claude` with mode `0755`, prepend that dir to `PATH`, and restore
     `PATH` in a `DeferCleanup`. The `claude` session is spawned by name, so the script must read
     `$SHIM_DIR` (handed through `claude.ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR":
     shimDir}}`) rather than `dirname "$0"`.
   - A recording decider: `decider := &mocks.ClaudePermissionDecider{}` with
     `decider.DecidePermissionReturns(claude.PermissionDecision{Allow: true}, nil)`. The counterfeiter
     fake records its arguments, so read them back with
     `_, first := decider.DecidePermissionArgsForCall(0)` and
     `_, second := decider.DecidePermissionArgsForCall(1)`. Assert
     `decider.DecidePermissionCallCount()` is 2.
   - The factory: `claude.NewSessionFactory(claude.ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR":
     shimDir}}, decider)`. Do not wrap it — the real session must read the context.
   - The service: `interactive.NewServiceWithPermissions(factory, ":0", "", prometheus.NewRegistry(),
     interactive.AuthDisabled, interactive.CardConfig{PublicURL: testPublicURL},
     interactive.NewPermissionRegistry(), interactive.DefaultSessionIdleTimeout,
     interactive.DefaultMaxSessions)`, served through `httptest.NewServer(svc.Handler())`. The
     constructor rejects a nil registry, and `AuthDisabled` keeps the spec free of a bearer token;
     the registry passed here is never consulted, because the turns resolve through the decider the
     factory holds.
   - Drive one turn through each route **on the same session id**: `postPromptFull(server.URL,
     "shared", "hi")` and then `postA2A(server.URL, "", a2aSendBody(1, "shared", "hi"))`. Assert both
     statuses are `200`. The same id makes both routes resolve the same cached entry and therefore
     the same held process, which is what makes a per-session (sticky) value fail this spec.
   - Assert: both recorded requests carry `ToolName == "Bash"`; `first.Source ==
     claude.PromptPermissionSource`; `second.Source == claude.A2APermissionSource`; both are
     non-empty; and `first.Source != second.Source`. Print both values with
     `GinkgoWriter.Printf("source prompt=%q a2a=%q\n", first.Source, second.Source)` so the two
     values are visible in `-v` output.
   - Cover the unpopulated path too: assert `claude.PermissionSourceFromContext(context.Background())`
     is the empty `claude.PermissionSource`, so a session whose caller named no route yields an empty
     source rather than a stale one.

8. **Unit-test the new context carriage and the stamping in the `claude` package** (level 1 for the
   new exported API and for the value crossing the session boundary).
   - Create `claude/permission-source_test.go`, `package claude_test`, asserting: the round trip
     `claude.PermissionSourceFromContext(claude.WithPermissionSource(ctx, claude.PromptPermissionSource))`
     equals `claude.PromptPermissionSource`, and the same for `claude.A2APermissionSource`; a bare
     `context.Background()` yields the empty `claude.PermissionSource`; and the two constants are
     non-empty and distinct.
   - Add one spec to the existing permission `Describe` in `claude/claude-session_test.go` — the
     `Describe("claudeSession failure and permission", ...)` block. That block owns its own
     `writeSessionShim` and `sessionConfig` closures but has **no** `openSession` helper, so build
     the session the way its neighbouring spec `"routes a permission request out and writes the
     verdict back"` does: `session := claude.NewSession(sessionConfig(shimDir), decider)` followed by
     `DeferCleanup(func() { _ = session.Close(context.Background()) })`. Use
     `shimDir := writeSessionShim(sessionShimPermissionShort)`,
     `decider := &mocks.ClaudePermissionDecider{}` with
     `decider.DecidePermissionReturns(claude.PermissionDecision{Allow: true}, nil)`, and prompt with
     `claude.WithPermissionSource(ctx, claude.PromptPermissionSource)` — the block's `ctx` is
     `context.Background()` from its `BeforeEach`. Assert `decider.DecidePermissionCallCount()` is 1
     and that `decider.DecidePermissionArgsForCall(0)`'s request carries
     `Source == claude.PromptPermissionSource`. Then add the mirror case in the same spec: a second
     session built from its own `writeSessionShim(sessionShimPermissionShort)` call and its own
     decider, prompted with the bare `ctx` and no `WithPermissionSource`, yields an empty `Source` at
     that decider — the unpopulated-path guard.

9. **Update the frozen contract** `docs/interactive-service.md`. Add to the `## Permission endpoint`
   section — between that heading and the next heading, `## Locking` — a short paragraph or list
   covering: (a) a request the CLI raises mid-turn now carries a `Source` value naming which route
   raised it, on the `claude.PermissionRequest` the decider is handed; (b) the closed set is
   `claude.PromptPermissionSource` (`/prompt`) and `claude.A2APermissionSource` (`/a2a`), and a
   future third turn-serving route must add its value to both the type and this document; (c) the
   value is set by the route that serves the turn and is never read from the request, because
   `X-Session-Id` is caller-controlled; (d) it is carried per turn, not per session, so two routes
   interleaved on one cached session id each see their own value; (e) a decider that ignores it
   behaves exactly as before; and (f) it is not part of any route's wire shape — it does not appear
   in the `GET /permission` JSON. This is an **additions-only** change: remove no line, reword no
   existing row, and change no code block already in the document. The document's own preamble says
   it is the frozen contract and changes only with a spec; this spec is that spec.

10. **Add a changelog entry.** `CHANGELOG.md` has no `## Unreleased` section today (its first
    version heading is `## v0.100.0`), so insert one immediately after the `# Changelog` preamble
    and before `## v0.100.0`, with a single `feat:` bullet naming the type, the two values, the
    carriage and the contract-document update. Follow
    `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — a `feat:` prefix
    (minor bump), one bullet for the one logical change, no bash comment, no prompt filename, and
    no `Generated with` / attribution line. If a `## Unreleased` section already exists by the time
    you run, append to it instead of adding a second one.

11. **Keep the change additive at the compile level.** After the edits, `go build ./...` must
    succeed with no other call site updated: the field is added, no parameter is added to an
    exported function, and no existing composite literal of `claude.PermissionRequest` needs a
    change to compile. Verify this by grepping every construction site of `claude.PermissionRequest`
    in the repo (production code and tests) before you declare the change done.

<constraints>
- `docs/interactive-service.md` is a frozen contract. Its own text: *"This document is the frozen
  contract. Both consumers honour the interface described here, and it does not change without a
  spec."* This spec is that spec; the document changes in the same prompt, **additions only**.
- `Session.Prompt(ctx, prompt)` keeps its signature. `agent-claude` and `agent-pi` both consume it;
  a signature change would break both for a value only the decider needs. The context is the
  carriage for exactly that reason.
- `PermissionDecider.DecidePermission(ctx, request) (PermissionDecision, error)` keeps its signature
  and its method set. Its shape is consumed by the counterfeiter mock
  `mocks/claude-permission-decider.go`, and `agent-claude` implements it. The seam gains
  information, not a second method. Do not regenerate or edit the mock.
- The existing `/permission` route keeps its contract: `GET` lists pending ids, `POST` delivers
  `{id, allow, message}`, an unknown id is refused with `404` and creates nothing, and nothing
  pending renders exactly `[]`. Do not add `Source` to `PendingPermission` or to that JSON.
- No new package dependency. The value must not require importing anything from the attention stack
  into this library, which knows nothing about it. `context` and the existing imports are the only
  additions.
- The new field is additive: a decider written before this change compiles and behaves exactly as
  today on both routes. No migration, no deprecation, no version guard.
- The value is set by the route that serves the turn and is **never** read from the request.
  `X-Session-Id` is caller-controlled: a caller that could choose its own source could make a
  pane-answered `/prompt` gate look like an A2A gate.
- The value is carried **per turn**, never on session state. The session cache is shared by both
  routes, so a sticky value would make a later turn read the previous turn's route.
- A `/prompt` gate keeps blocking on `/permission` and keeps resuming on a posted verdict; a
  service constructed without a decider still fails an out-of-allowlist turn rather than blocking
  on a decider nobody answers. Do not touch `sessionArgs`'s conditional flag append.
- No new config field, no opt-out flag, no tunable threshold, no Prometheus metric and no new HTTP
  route. The spec's Non-goals section forbids all of them; the observability for this change is the
  test's printed values, not a collector.
- Do NOT commit — dark-factory handles git. Do not run any `git` command at all (see
  `<verification>`: this repo's `.git` is masked in the container, so every `git` invocation fails).
- Existing tests must still pass unchanged, in particular the frozen-contract rows for `/prompt`,
  `/permission` and `/a2a` and the nil-decider argv spec in `claude/claude-session_test.go`.
- New files carry the repo's BSD license header (year 2026, "Benjamin Borbe"); wrap every line,
  including doc comments, at 100 columns (`golines --max-len=100` runs in `make precommit`).
</constraints>

<verification>
Run in the repo root. Pass `ROOTDIR=/workspace` explicitly on every `make` target: the Makefile
derives `ROOTDIR` from `git rev-parse --show-toplevel`, and this repo's `.git` is masked inside the
container (it is a character device, so every `git` command fails with
`fatal: not a git repository`). Without `ROOTDIR=/workspace` the lint, osv-scanner and trivy targets
resolve `$(ROOTDIR)/.golangci.yml`, `$(ROOTDIR)/.osv-scanner.toml` and `$(ROOTDIR)/.trivyignore`
against `/` and fail for a reason unrelated to your change.

```bash
make test
make precommit ROOTDIR=/workspace
```

`make test` must exit 0. `make precommit ROOTDIR=/workspace` must exit 0 — that is AC6. If it
fails, fix the failing target and re-run only that target (`make lint ROOTDIR=/workspace`,
`make vet ROOTDIR=/workspace`) until it passes, then run `make precommit ROOTDIR=/workspace` once
more at the end.

Then verify the two behaviours this change is about, and the document:

```bash
# The two recorded Source values, one per route, visible in the spec output (AC1, AC2).
go test -mod=mod -race -v -ginkgo.focus='raising route|carries the source|permission source' \
  ./interactive/... ./claude/...

# AC5: the new field is documented inside the Permission endpoint section.
grep -n 'Source' docs/interactive-service.md
grep -n '^## ' docs/interactive-service.md

# AC4: the permission flags are still appended only when the decider is non-nil.
grep -n 'permission-prompt-tool' claude/claude-session.go

# The value is set by the route, never read from the request.
grep -rn 'WithPermissionSource\|PermissionSourceFromContext' --include='*.go' .
```

`go test -run` selects Go test functions, not Ginkgo specs — this repo's suites are registered by
`TestInteractive` / `TestClaude`, so focus the specs with `-ginkgo.focus` as above (adjust the
focus regex to the spec names you actually write; the interactive spec must contain the phrase
"raising route").

Do **not** run `git diff`, `git status` or `git log` — they cannot pass here. To confirm the frozen
contract change is additions-only (AC3), read the `## Permission endpoint` section of
`docs/interactive-service.md` before and after the edit and confirm no existing line was removed or
reworded; `grep -c '' docs/interactive-service.md` must show a count that only increased. The
`git diff --stat docs/interactive-service.md` check in the spec's `## Verification` section is an
operator-side rung of the spec's ladder, not a container step — the container has no git.
</verification>
