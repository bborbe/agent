---
status: prompted
approved: "2026-10-11T18:31:08Z"
generating: "2026-10-11T18:44:30Z"
prompted: "2026-10-11T18:44:30Z"
branch: dark-factory/interactive-service-claims-streaming
---

## Summary

- The `interactive` service's frozen contract currently **disclaims** A2A streaming, and the Agent Card faithfully encodes that disclaimer as `streaming: false`.
- A probe against the deployed pod shows the streaming method **already works** — the A2A SDK inherits the route and the same executor serves it — so the card is under-declaring a capability the service really has.
- This spec amends the frozen contract to **claim streaming** and flips the card's capability to `true`, so a standards-compliant A2A client can discover and use the route.
- Nothing else moves: the one-shot `SendMessage` behavior, push notifications (still `false`), task polling, task lookup, cancellation and the extended card stay exactly as they are.
- Because the contract is frozen, **this spec is the only legitimate route to amend it**.

## Problem

The `interactive` package serves an A2A JSON-RPC binding whose wire surface is described by `docs/interactive-service.md`, a document that states in its own opening that it "does not change without a spec". Its A2A section authors only the one-shot `SendMessage` bridge and explicitly disclaims the SDK's inherited SSE route, so the Agent Card — built from that contract — advertises `"streaming": false`. That is a faithful encoding of the contract, not a bug in the card. But the contract is now inaccurate about the running system: the A2A SDK's JSON-RPC handler already routes the streaming method through the same executor, and a probe against the deployed pod measured a working SSE response. A conforming A2A client reads `"streaming": false` and therefore never attempts the route at all. The gap is on the contract side — the contract must claim the capability it actually serves, and the card must then advertise it truthfully.

## Goal

After this work, the frozen contract states that the `interactive` service serves the A2A streaming method over the same binding and the same executor as `SendMessage`, the Agent Card advertises `capabilities.streaming: true` while `capabilities.pushNotifications` stays `false`, and a deployed pod answers a streaming request with an ordered SSE response and serves a card that says so. The one-shot bridge, the bearer gate, the card's public-and-credential-free posture, and every other disclaimed A2A method are unchanged.

## Non-goals

- **This spec supersedes spec 058's "Streaming and push notifications" Non-goal for streaming only.** 058 disclaimed both; the streaming half is lifted here. Push notifications and task polling remain out — task polling has its own separate task.
- **Not building a streaming surface.** The SDK already routes the method. This spec *claims* it and makes the card truthful; it adds no new route, no bespoke SSE handler, no new executor, and no change to the executor's event sequence.
- **Push notifications stay out** — `capabilities.pushNotifications` stays `false`, and `tasks/pushNotificationConfig/set` stays disclaimed.
- **Task lookup, cancellation, push-configuration management and the extended card stay disclaimed** — the rewrite removes only the streaming exclusion.
- **The consumer release chain** — the `bborbe/agent` tag, the `agent-claude` dependency bump, the image build and the nuke pin — is executed by the operator, not authored here. This spec's post-deploy criterion *observes* that chain; it does not perform it.
- **No change to any existing route's contract** — `/prompt`, `/permission`, `/readiness`, `/metrics` and the existing `/a2a` rows keep their current behavior and policy.

## Acceptance Criteria

- [ ] **AC1 — the contract no longer disclaims streaming and now claims it.** Evidence: three negative greps against `docs/interactive-service.md`, each returning `0` — `grep -c 'This repository authors only the one-shot' docs/interactive-service.md`, `grep -c 'inherited method' docs/interactive-service.md`, and `grep -c 'is authored and documented here' docs/interactive-service.md` — **plus** positive evidence that the contract now *claims* the method rather than merely naming it: `tr '\n' ' ' < docs/interactive-service.md | grep -cE 'serves .SendStreamingMessage.'` returns ≥1 (the phrase Desired Behavior 1 requires the section to state; the `.` wildcards stand in for the backticks around the identifier, and the `tr` form is newline-insensitive because the prose wraps), and `grep -n 'SendStreamingMessage' docs/interactive-service.md | grep -c 'not part of this contract'` returns `0`. ⚠️ A bare `grep -c 'SendStreamingMessage'` proves nothing here: the **unfixed** document already returns `1`, because the disclaimer itself names the method. The `serves …` fragment is the half that fails a document which merely deletes the disclaimer and says nothing about the route. The three negative greps are the falsifiable half: each returns `1` against the unfixed document, so a no-op fails this criterion. ⚠️ Each pattern is single-line **by necessity**: the disclaimer sentence wraps across two source lines, so the whole-sentence form `grep -c 'inherited method is not part of this contract'` returns `0` against the **unfixed** document too and cannot distinguish pre-fix from post-fix. That form is not used.
- [ ] **AC2 — the Agent Card encodes streaming and serves it.** Evidence: `grep -nE 'Streaming:\s+true' interactive/a2a.go` returns ≥1 line (the capability is set in source — currently `0`; the `\s+` form is deliberate, since gofmt column-aligns struct-literal fields and a plain `'Streaming: true'` silently stops matching once a second field joins the literal), **and** `make test` exits `0` with the card assertion reading `capabilities.streaming` as `true` from the served JSON — the JSON key is `streaming` (the SDK field carries no `omitempty`, so the value is present rather than omitted). ⚠️ The served-JSON assertions named in AC2 and AC4 are **new test work**: `interactive/a2a_test.go`'s card test currently asserts only `card["name"]`, so the prompt must add the `capabilities.streaming` / `capabilities.pushNotifications` assertions.
- [ ] **AC3 — `SendMessage` behavior is unchanged (negative).** Evidence: the 9 non-test files under `interactive/` other than `interactive/a2a.go` (including `a2a-handler.go`, which carries the `SendMessage` bridge this criterion exists to protect) hash to their pinned pre-change SHA-256 digests — checked in-container by the prompt — **and** `make test` exits `0` with the existing `SendMessage` round-trip rows passing unchanged. ⚠️ **The container has no usable git**: dark-factory runs from a git worktree, where `hideGit=true` is mandatory and masks `.git`, so `/workspace` is a non-git directory. The structural guard is therefore content-based in-container; the equivalent host-side `git diff --name-only origin/master -- interactive/ ':!interactive/*_test.go'` (must print exactly `interactive/a2a.go`) runs on the operator rung. ⚠️ Do **not** restate the in-container guard as an enumerated filename list fed to `git diff`: `git diff` against a path that does not exist returns empty with exit `0`, so a single typo'd path passes silently and drops that file's guard entirely — which is exactly how this criterion read before the fix.
- [ ] **AC4 — `pushNotifications` stays `false` (negative).** Evidence: `grep -c 'PushNotifications' interactive/a2a.go` returns `0` (the field is left at its zero value) **and** the card assertion reads `capabilities.pushNotifications` as `false` from the served JSON.
- [ ] **AC5 — the other A2A disclaimers remain (negative — they must NOT be removed).** Evidence: three greps against `docs/interactive-service.md`, each returning ≥1 — `grep -c 'push notifications and task polling are not built here' docs/interactive-service.md`; `grep -c 'none of those is part of this contract either' docs/interactive-service.md`; and `tr '\n' ' ' < docs/interactive-service.md | grep -c 'task lookup, cancellation, push-configuration management and the extended card'`. The first is load-bearing: the push clause shares its **source line** with the streaming disclaimer, so an edit to that line's streaming clause can drop the push clause with it — AC5 catches that. ⚠️ The third uses a newline-insensitive form because the clause wraps across two source lines — a plain `grep -c` returns `0` even on the unfixed document.
- [ ] **Post-Deploy (Rung-2):** the deployed `claude-interactive` pod in nuke dev serves the streaming route and advertises it truthfully — evidence: (i) a JSON-RPC `message/stream` (the SDK's `SendStreamingMessage`) POST to the pod's advertised `/a2a` endpoint with the bearer token returns HTTP `200` with `Content-Type: text/event-stream`, and the SSE body carries, **in order**, a non-terminal frame (the submitted task, then the artifact) followed by the terminal `completed` status frame — the terminal frame is last; (ii) `GET /.well-known/agent-card.json` on the same pod returns JSON with `capabilities.streaming` `true` and `capabilities.pushNotifications` `false`. Evidence shape: HTTP response (status, content-type, ordered frames) plus JSON field values.
  - `deploy_check:` `kubectlnukedev -n dev get pods -l app=claude-interactive -o jsonpath='{.items[0].spec.containers[0].image}' | grep -Eqv ':v0\.17\.2$' && echo FRESH`
  - `deploy_target:` `FRESH`
  - **Why this target string:** the pre-fix pod is pinned at `bborbe/agent-claude:v0.17.2` (read from nuke dev on 2026-10-11), and the fix's own tag does not exist at authoring time — it comes out of the consumer release chain. The check therefore emits the literal marker `FRESH` verbatim when the deployed tag is **not** that known pre-fix tag, the same freshness-marker shape spec 053 uses for a rolling tag. Phase 0.5 compares the emitted `FRESH` against this literal and refuses verification against a stale pod.

**Scenario coverage — no new scenario.** Every behavior above is reachable by the in-process Ginkgo suite (the card assertions and the existing `SendMessage` rows) plus the operator-side cluster observation in the post-deploy criterion. No dark-factory scenario harness can reach a real cluster pod serving a real SSE response; the post-deploy criterion is the E2E layer for this change.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — exits `0`
- `make test` — the Ginkgo suite passes; the card assertions (`capabilities.streaming` `true`, `capabilities.pushNotifications` `false`) and the existing `SendMessage` rows pass
- `grep -c 'This repository authors only the one-shot' docs/interactive-service.md` — returns `0`
- `grep -c 'inherited method' docs/interactive-service.md` — returns `0`
- `grep -c 'is authored and documented here' docs/interactive-service.md` — returns `0`
- `grep -n 'SendStreamingMessage' docs/interactive-service.md | grep -c 'not part of this contract'` — returns `0`
- `tr '\n' ' ' < docs/interactive-service.md | grep -cE 'serves .SendStreamingMessage.'` — returns ≥1
- `grep -c 'push notifications and task polling are not built here' docs/interactive-service.md` — returns ≥1
- `grep -c 'none of those is part of this contract either' docs/interactive-service.md` — returns ≥1
- `tr '\n' ' ' < docs/interactive-service.md | grep -c 'task lookup, cancellation, push-configuration management and the extended card'` — returns ≥1
- `grep -nE 'Streaming:\s+true' interactive/a2a.go` — returns ≥1 line
- `grep -c 'PushNotifications' interactive/a2a.go` — returns `0`
- AC3's structural guard is **content-based, not git-based** — the container has no usable git (dark-factory runs from a git worktree, where `hideGit=true` is mandatory and masks `.git`, so `/workspace` is a non-git directory, per `dark-factory/docs/troubleshooting.md`). The 9 non-test files under `interactive/` other than `a2a.go` must hash to their pinned pre-change SHA-256 digests; the prompt carries the exact command and the expected 9-line output.

### Operator-executable (runs on the host after the release chain lands)

- AC3's host-side equivalent, where git does work: `git diff --name-only origin/master -- interactive/ ':!interactive/*_test.go'` — prints exactly one line: `interactive/a2a.go`

- The consumer release chain that makes the post-deploy criterion fire: tag `bborbe/agent`, bump `bborbe/agent-claude`'s dependency, build the image, pin it in the nuke dev manifest — the chain is the operator's, not this repo's.
- `kubectlnukedev -n dev get pods -l app=claude-interactive -o jsonpath='{.items[0].spec.containers[0].image}' | grep -Eqv ':v0\.17\.2$' && echo FRESH` — emits `FRESH`
- `curl -sS "$A2A_PUBLIC_URL/.well-known/agent-card.json" | jq -r '.capabilities.streaming, .capabilities.pushNotifications'` — prints `true` then `false`
- The streaming `curl` probe: `curl -sS -N -w '\n%{http_code} %{content_type}\n' -X POST "$A2A_PUBLIC_URL" -H "Authorization: Bearer $INTERACTIVE_AUTH_TOKEN" -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"message/stream","params":{"message":{"messageId":"probe","role":"user","parts":[{"kind":"text","text":"say ok"}],"contextId":"probe"}}}'` — prints `200 text/event-stream` and an SSE body whose terminal `completed` status frame is last

## Desired Behavior

1. **The frozen contract claims streaming.** Its A2A section states that the service serves `SendStreamingMessage` (SSE) over the same JSON-RPC binding and the same executor as `SendMessage`, and the three clauses that disclaimed it — the opener ("authors only the one-shot … bridge"), the streaming clause ("that inherited method is not part of this contract") and the closer ("Only `SendMessage` is authored and documented here") — are reconciled so that none of them excludes streaming.
2. **The other disclaimers survive the rewrite.** Push notifications, task polling, task lookup, cancellation, push-configuration management and the extended card remain explicitly outside the contract; the rewrite removes the streaming exclusion and nothing else.
3. **The Agent Card advertises streaming.** The card's `capabilities.streaming` is `true`; `capabilities.pushNotifications` remains `false`. No other card field changes — the name, version, supported interfaces, default modes and skills are untouched.
4. **The claim is truthful on the wire.** A deployed pod answers a streaming JSON-RPC request with an SSE response whose frames are ordered non-terminal-then-terminal, and serves a card whose `streaming` capability is `true`.
5. **`SendMessage` is untouched.** The one-shot bridge's response shapes, `contextId` validation, body cap, session reuse and per-session locking behave exactly as the contract already documents them.
6. **The posture is unchanged.** The card stays public and credential-free, and the `/a2a` route stays bearer-gated — the capability claim adds no authority, no new route and no second credential.

## Constraints

- **The frozen contract is amended only by this spec.** `docs/interactive-service.md` states it does not change without a spec; this is that spec. No other edit to the contract is in scope, and the routes table's existing rows are unchanged.
- **`SendMessage`'s existing behavior must not change.** Same completed/failed task shapes, same artifact, same `contextId` validation against the anchored regex, same 1 MiB cap, same session cache and lock.
- **The card stays credential-free and public**, and the card route stays exempt from the gate; `/a2a` stays gated by the same bearer token as every other non-exempt route. No second credential is introduced.
- **`pushNotifications` stays `false`**, and task lookup, cancellation, push-configuration management and the extended card stay disclaimed.
- **Streaming is claimed, not built.** The executor's event sequence is unchanged; no bespoke streaming handler is added.
- **Assumptions carried:** (a) the A2A SDK's JSON-RPC handler continues to route `SendStreamingMessage` through the same executor — measured working against the deployed pod on 2026-10-11; (b) the SDK's `AgentCapabilities` fields serialize without `omitempty`, so `streaming: true` appears in the served card; (c) a conforming A2A client reads `capabilities.streaming` to decide whether to call the streaming method.
- **Supersede note:** this spec supersedes spec 058's Non-goal "Streaming and push notifications — … out; one-shot `message/send` only" **for streaming only**. Push notifications and task polling remain out.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection |
|---|---|---|---|
| An SDK upgrade drops the inherited SSE routing while the card advertises `streaming: true` | the claim becomes false; a client that tries streaming gets an error | review or re-pin the SDK bump before release; the post-deploy criterion re-probes | the streaming probe returns non-`200` or no `text/event-stream` while the card still reads `true` |
| Partially-landed release chain — the library tag exists but the pod still runs the pre-fix tag | the card still advertises `streaming: false`; the pod serves the old build | finish the chain (dependency bump → image build → nuke pin) and re-run | the `deploy_check` emits no `FRESH`; the card reads `streaming: false` |
| The contract claims streaming while the deployed pod serves it not at all | contract and runtime disagree | resolve the SDK-regression or stale-deploy row before marking verified; never edit the contract to match a stale pod | the post-deploy criterion fails; the streaming probe returns a JSON-RPC error or `404` |
| `Streaming: true` is set but the SDK serializes the capability under a different key or with `omitempty` | the served card omits `streaming`; a client still never tries the route | assert the field in the **served JSON**, not only in source; pin the SDK version | the in-process card assertion fails; `jq '.capabilities.streaming'` returns `null` |
| Someone later re-adds the streaming disclaimer without a spec | the contract drifts back and the card claim becomes unsupported | revert; a contract change requires a spec | AC1's negative greps return non-zero |
| A client opens a long-lived SSE stream on one conversation | the conversation's session lock is held for the turn, serialising that id exactly as `/prompt` does | none needed — existing, documented exposure | a second request on the same id waits (the turn-boundary log pair) |

## Security / Abuse

- **The streaming route runs the same arbitrary prompts** with the same allowed tools and the same cluster credentials as `SendMessage` and `/prompt`. Its blast radius is exactly theirs; the bearer gate is the control, and the capability claim adds no authority.
- **The SSE route is gated exactly like `/a2a`.** No second credential, no new scheme, no exempt path. An unauthenticated streaming request is refused `401` before the handler runs.
- **`contextId` remains attacker-controlled.** The streaming path uses the same executor and therefore the same anchored-regex validation before any session is built; the claim must not be implemented in a way that bypasses it. A leading `-` would be read as a flag and `.` or `/` would escape a session directory.
- **The body cap still applies.** The streaming request is capped at 1 MiB by the same handler wrapper as `SendMessage`; an oversized body is refused.
- **The card stays credential-free.** Advertising `streaming: true` reveals no secret and exposes no new route; the card must still never embed the bearer token.
- **A denial-of-service surface exists and is not addressed here:** a long-lived SSE stream occupies its conversation's session lock for the duration of a turn, so a caller can serialise behind it. This matches `/prompt`'s existing exposure and is out of scope for this spec.
- **No credential value is logged or returned** — statuses, key names and counts only, as the existing service already does.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Amend the frozen contract's A2A section (claim streaming, keep the other disclaimers) and set the card's `streaming` capability, with the card and doc tests | 1-6 | 1-5 (AC6 is the operator-side post-deploy criterion, not prompt work) | — |

**A single prompt suffices.** The change is one prose reconciliation in a frozen document plus one capability field, with test additions in the existing Ginkgo suite; there is no second layer to sequence. The operator-side release chain and the post-deploy criterion are not prompt work.

## Do-Nothing Option

Do nothing and the card keeps under-declaring a capability the SDK already serves. A standards-compliant A2A client reads `"streaming": false` and never attempts the route, so a working surface stays undiscoverable — the capability exists but no conforming client can find it. The contract is also inaccurate in the other direction: it disclaims a method that works, so the document and the running system drift further apart with every SDK upgrade that keeps inheriting the route. The cost of doing nothing is not the code; it is that contract/implementation drift persists and a real capability remains invisible to the clients it was built for.
