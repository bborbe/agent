---
status: completed
spec: [061-a2a-push-notification-extension-point]
summary: 'Recorded the A2A push-notification extension point in docs/interactive-service.md, CHANGELOG.md and README.md: the ServiceOption seam and WithPushNotifications in the construction section, the derived card capability, push-configuration methods, fail-loud rule and mid-turn working status in the A2A surface, corrected the two falsified claims, added the push-config contract-table row and card-row capability, a ## Unreleased feat bullet, and extended the README interactive/ row.'
execution_id: agent-a2a-push-exec-237-spec-061-frozen-contract-and-changelog
dark-factory-version: v0.198.7
created: "2026-10-17T18:03:47Z"
queued: "2026-10-17T19:58:27Z"
started: "2026-10-17T20:44:14Z"
completed: "2026-10-17T20:52:08Z"
---

<summary>
- The interactive service's frozen contract document now describes the push-notification option, the Agent Card capability derived from it, and the rule that a half-supplied option fails at construction.
- The document stops claiming that push notifications are not built here, and still says task polling is not built here.
- The document describes the mid-turn working status a running turn emits, including that it never lengthens the turn.
- The changelog gains an entry for the extension point under an Unreleased heading.
- The README's `interactive/` package row names the optional push wiring, or points at the contract document for it.
- Nothing in the package's behaviour changes: this prompt writes down what prompts 1 and 2 already shipped.
</summary>

<objective>
Record the extension point in the three places the definition of done requires — the interactive service's frozen contract document, the changelog, and the README's package row — so the contract a consumer reads matches the code that shipped, and the release has an entry to cut. All three are part of this spec's change, not follow-up work.
</objective>

<context>
Read `CLAUDE.md` (repo root) for the project conventions before you change anything.

Read the linked spec in full: `specs/in-progress/061-a2a-push-notification-extension-point.md`. Its
`## Desired Behavior` items 1-7 and its `## Constraints` are what you are recording; write down what
shipped, not what was planned.

This prompt runs after the two implementation prompts of this spec. Read the shipped code first and
describe what it does:

- `interactive/push.go` — the `ServiceOption` type, `WithPushNotifications`, the unexported
  `serviceOptions` / `pushConfig` pair, and the panic messages.
- `interactive/service.go` — the trailing `opts ...ServiceOption` on `NewService` and
  `NewServiceWithPermissions`.
- `interactive/a2a.go` — `newAgentCard(config CardConfig, pushNotifications bool)`, which derives the
  card's capability.
- `interactive/a2a-handler.go` — `handlerOptions`, the `midTurnStatusDelay` constant,
  `midTurnStatusText`, `midTurnStatusEvent` and `runTurn`.
- `interactive/a2a_push_test.go` — the specs, if you need to confirm a detail you are about to write
  down.

Read the three documents you are editing, in full, before editing them:

- `docs/interactive-service.md` — `## Constructing the service` (the code block and the parameter
  table), `## The contract, row by row` (the per-request table), and `## A2A surface` (the last
  section, which contains the two sentences this prompt must stop being true). Its own preamble says
  it is the frozen contract and does not change without a spec; this spec is that spec.
- `CHANGELOG.md` — its preamble and its first version heading. There is no `## Unreleased` section
  today: the first version heading is `## v0.102.0`.
- `README.md` — its package table, whose `interactive/` row summarises the package's HTTP surface.

Read `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` before writing the
changelog entry, and `docs/dod.md` for the definition of done the validator applies.
</context>

<requirements>
1. **Document the option in `docs/interactive-service.md` under `## Constructing the service`.** Show
   it in the existing code block — add the two calls the consumer is written against, keeping the two
   existing lines:

   ```go
   svc := interactive.NewService(..., interactive.WithPushNotifications(store, sender))
   svc := interactive.NewServiceWithPermissions(..., interactive.WithPushNotifications(store, sender))
   ```

   Then add a paragraph after the `maxSessions` paragraph and before the `Service.Handler()` /
   `Service.Run(ctx)` paragraph (`docs/interactive-service.md:57-58`), after the existing `card`,
   `sessionIdleTimeout` and `maxSessions` paragraphs, covering, in prose:
   - the trailing `opts ...ServiceOption` on both constructors is the seam for an optional capability;
     passing none is the documented default and produces exactly the service described today;
   - `interactive.WithPushNotifications(store push.ConfigStore, sender push.Sender)` is the one option
     today, and supplying it *is* the opt-in — there is no second flag that enables or disables push;
   - the option carries both halves or it fails: a nil store or a nil sender panics at construction
     with a message naming the missing component, because a half-supplied option treated as "not
     configured" would hand a caller a service whose card advertises no push while the caller
     believes the channel is wired;
   - the library constructs neither a store nor a sender and overrides no field on the sender, so the
     SSRF guard's posture, the client timeout, `FailOnError` and the redirect cap are the caller's
     settings, passed through untouched.

2. **Document the push behaviour in `docs/interactive-service.md` under `## A2A surface`.** Add a
   block covering:
   - the Agent Card's `capabilities.pushNotifications` is derived from whether the caller supplied the
     option and is never hardcoded — an unconfigured service marshals `"pushNotifications": false`,
     which is the correct wire value;
   - the card's capability and the handler's acceptance of a registration are driven by that one
     decision, so a card that advertises support while the handler refuses a registration is not a
     reachable state;
   - with the option supplied, the service's A2A handler is built through the SDK's
     `a2asrv.WithPushNotifications` with the caller's store and sender, and the SDK's own push path
     then delivers one push per task event to every config registered for that task;
   - the push-configuration methods (`CreateTaskPushNotificationConfig`,
     `GetTaskPushNotificationConfig`, `ListTaskPushNotificationConfigs`,
     `DeleteTaskPushNotificationConfig`) are dispatched by the existing `/a2a` binding behind the
     existing bearer-token gate — no new route is added — and a registration naming a task the
     service does not hold is refused;
   - the payload is the SDK's task-update shape and the registered token travels as the SDK's
     `A2A-Notification-Token` header; the card carries no credential and no push config, callback URL
     or token is logged;
   - a running turn yields one mid-turn status update in the `working` state carrying non-empty text,
     after a fixed ten-second delay measured from the turn's start, at most once per turn and only
     while the turn is still running — a turn that finishes first emits none, and the turn's
     terminal status is unaffected; the delay is a fixed constant and is not configurable;
   - the mid-turn event rides alongside the turn's work rather than blocking it, so it never lengthens
     the turn; and the emitter honours the same stop signal the turn's other events do, so no
     goroutine or timer outlives the turn.

   `Streaming` stays `true` and is unchanged: the mid-turn signal is a push event, not a stream.

3. **Correct both falsified claims in `## A2A surface`.** Two sentences in that paragraph are made
   false by this change; correct both, and leave the rest of the paragraph saying what it says.

   First, the clause ending `... and push notifications and task polling are not built here.` must no
   longer claim that push notifications are absent. Reword it so it says push notifications are
   available when the caller wires them with the option, while task polling is still not built here.

   Second, the sentence that follows is falsified too, and for the same reason. It reads:

   > The SDK's handler additionally dispatches the rest of the A2A 1.0 method set — task lookup,
   > cancellation, push-configuration management and the extended card — against its own default
   > in-memory stores; none of those is part of this contract either.

   Requirement 2 documents the push-configuration methods as part of this surface, and with the
   option supplied they run against the caller's store, not the SDK's default — so leaving that
   sentence makes the document self-contradictory, which is exactly what AC11 exists to prevent.
   Drop `push-configuration management` from the list of methods dispatched "against its own default
   in-memory stores", and state that the push-configuration methods are served against the caller's
   store when the option is supplied.

   Leave the SSE sentence saying what it says. Do not reword any other existing line, and remove
   nothing.

4. **Add the push-configuration row to `## The contract, row by row`** in
   `docs/interactive-service.md`. That table is the per-request summary the rest of the document
   reads against, and without a row for this surface it becomes the only part of the document that
   does not reflect the change. Add one row:

   | `POST /a2a`, `CreateTaskPushNotificationConfig`, option supplied | `200` with the registered `url` and `token`; with the option absent, a JSON-RPC error saying push is not supported |

   Then add `capabilities.pushNotifications` to the existing `GET /.well-known/agent-card.json`
   row that describes the card's `name` (`docs/interactive-service.md:130` — the first of the two
   such rows; the second, at `:131`, is the `A2A_PUBLIC_URL` row and keeps its current text),
   noting it is `true` only when the caller wired the option and `false` otherwise. Match the
   table's existing style: one line per row, backticked request names, the expected value in the
   second column.

5. **Add the changelog entry.** `CHANGELOG.md` has no `## Unreleased` section today, so insert one
   immediately after the preamble (after the MAJOR/MINOR/PATCH bullets) and directly above
   `## v0.102.0`, containing one `feat:` bullet naming the extension point: the optional
   push-notification wiring on both constructors, the card capability derived from it, the fail-loud
   half-supplied rule, and the mid-turn working status event. The bullet must name the option
   literally as `WithPushNotifications` — the verification greps for that token, because `push`
   alone already matches ten pre-existing lines. Follow
   `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`: the preamble and the
   `# Changelog` header are frozen and nothing may be inserted above or inside them; one bullet for
   the one logical change; no `### Added` category; no attribution line. If a `## Unreleased` section
   already exists by the time you run, append to it instead of adding a second one.

6. **Extend the `interactive/` row in `README.md`.** `docs/dod.md` requires the README to be
   updated when a change affects usage, configuration or setup, and this change adds public API.
   `README.md`'s package table carries an `interactive/` row summarising the package's HTTP surface.
   Extend that row with the optional push wiring: that both constructors take an optional
   `interactive.WithPushNotifications(store, sender)` and that the Agent Card's push capability is
   derived from it. If the row is deliberately a summary rather than the contract, still name the
   push wiring and point at `docs/interactive-service.md` for the detailed contract. Do not
   restructure the table and do not touch another row.

7. **Change no code.** This prompt edits three documents and nothing else —
   `docs/interactive-service.md`, `CHANGELOG.md` and `README.md`. Do not touch `interactive/`,
   `CHANGELOG.md`'s existing entries, or any other document.
</requirements>

<constraints>
- **`docs/interactive-service.md` is a frozen contract.** Its own text: *"This document is the frozen
  contract. Both consumers honour the interface described here, and it does not change without a
  spec."* This spec is that spec. Requirement 3 is the only place a falsified claim is corrected —
  the two falsified sentences — and every other edit either adds text or extends an existing table
  row (requirement 4).
- **Do not document a knob that does not exist.** There is no configurable mid-turn delay, no opt-out
  flag that disables push while a store and sender are configured, and no library-owned default store
  or sender. The document must not imply any of them, and must state the delay is fixed.
- **The `## Unreleased` entry is required by `docs/dod.md`** and is what the release cuts from. This
  repository is a single Go module (`module github.com/bborbe/agent`); there is no `lib/` module, so
  the release is a single root tag and the changelog entry is the only version-bearing artifact.
- **Repo is dark-factory `workflow: direct`, `autoRelease: false`.** Commits land directly; the
  release tag is cut from the changelog header rather than by the pipeline. Do not add a version
  heading — `## Unreleased` only.
- Do NOT commit — dark-factory handles git. Do not run any `git` command at all: the daemon does
  not check verification exit codes, so a `git` invocation that fails reads as a pass and names
  evidence nothing can produce.
- Existing tests must still pass unchanged. This prompt changes no code, so `make test` passing
  afterwards is a regression check on the tree, not on the documents.
</constraints>

<verification>
Run in the repo root. Pass `ROOTDIR=/workspace` explicitly on every `make` target: the Makefile
derives `ROOTDIR` from `git rev-parse --show-toplevel` (`Makefile:3`), which is not guaranteed to
resolve inside the container, so without `ROOTDIR=/workspace` the `lint` target resolves
`$(ROOTDIR)/.golangci.yml` against `/` and fails for a reason unrelated to your change. `lint`
(`Makefile:37`) is the target that genuinely needs it — it references `$(ROOTDIR)/.golangci.yml`
directly, with no CWD-relative fallback. `osv-scanner` (`Makefile:51`) and `trivy` (`Makefile:66`)
check the CWD-relative `.osv-scanner.toml` and `.trivyignore` first and fall back to `$(ROOTDIR)`
only when the local file is absent, so the explicit value is harmless there but not required.

```bash
make test ROOTDIR=/workspace
make precommit ROOTDIR=/workspace
```

Then verify the three documents, which is what AC11 reads:

```bash
# AC11: the changelog carries the new entry under ## Unreleased. The token is
# `WithPushNotifications`, not `push`: `grep -n 'push' CHANGELOG.md` already returns ten
# pre-existing matches, none of them under ## Unreleased, so it cannot distinguish before from after.
# The `test` form also catches an insertion above the preamble, which the line-number ordering test
# below does not.
grep -n '## Unreleased' CHANGELOG.md
grep -n 'WithPushNotifications' CHANGELOG.md
test "$(awk '/^## /{sec=$0} /WithPushNotifications/{print sec}' CHANGELOG.md)" = "## Unreleased"

# The Unreleased heading sits strictly above the newest version heading.
test "$(grep -n '^## Unreleased' CHANGELOG.md | cut -d: -f1)" \
     -lt "$(grep -n '^## v[0-9]' CHANGELOG.md | head -1 | cut -d: -f1)"

# AC11: the contract document describes push, and no longer denies it. Absence is asserted with
# ! grep -q, not with grep -c (grep -c exits 1 when the count is zero). The positive greps use
# tokens the new prose carries: `push notification` alone already matches today's line 454, and
# `mid-turn` alone already matches six pre-existing lines about permission requests and the session
# cache, so neither can distinguish before from after.
grep -n 'pushNotifications' docs/interactive-service.md
! grep -q 'push notifications and task polling' docs/interactive-service.md
! grep -q 'push notifications and task polling are not built here' docs/interactive-service.md
! grep -q 'push-configuration management and the extended card' docs/interactive-service.md

# The option, the fail-loud rule and the mid-turn status are documented where they belong.
grep -n 'WithPushNotifications' docs/interactive-service.md
grep -n 'ServiceOption' docs/interactive-service.md
grep -n 'mid-turn status' docs/interactive-service.md

# Requirement 4's new contract-table row and the caller's-store claim requirement 3 adds are
# positively asserted: an implementation that updates the card row but omits the push-config row,
# or drops the caller's-store claim, must fail here.
grep -n 'CreateTaskPushNotificationConfig' docs/interactive-service.md
grep -n "caller's store" docs/interactive-service.md

# The README row reflects the new surface: README.md carries no `push` today, so any match is new.
grep -n 'push' README.md

# The changelog preamble is intact: # Changelog is still the first content line, and the first
# `## ` heading is `## Unreleased` — which the ordering test above pins against the newest version
# heading.
test "$(head -1 CHANGELOG.md)" = "# Changelog"
test "$(grep -m1 '^## ' CHANGELOG.md)" = "## Unreleased"
```

`awk` is the section-walking form on purpose: a `grep -A` window over `## Unreleased` swallows a
neighbouring section and reports a count that is not the one under test.

Do **not** run `git diff`, `git status` or `git log` — the daemon does not check verification exit
codes, so a failing `git` invocation reads as a pass and names evidence nothing can produce. To
confirm no existing line of `docs/interactive-service.md` was removed, read the `## A2A surface`
section before and after your edit and confirm the only changed claims are the two push claims — the
"not built here" clause and the default-in-memory-stores sentence; `grep -c ''` on the file must show
a count that only increased.

Before you finish, re-run `<verification>` and confirm every command passes; then walk AC11 against
the change and state which line of which document is its evidence.
</verification>
