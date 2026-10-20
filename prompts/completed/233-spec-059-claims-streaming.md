---
status: completed
spec: [059-interactive-service-claims-streaming]
execution_id: agent-contract-streaming-exec-233-spec-059-claims-streaming
dark-factory-version: v0.196.0
created: "2026-10-11T18:37:27Z"
queued: "2026-10-11T18:48:40Z"
started: "2026-10-11T20:47:37Z"
completed: "2026-10-11T21:05:36Z"
---

<summary>
- The service's frozen contract now claims that it serves the streaming A2A method, instead of disclaiming it.
- The contract states the streaming method runs over the same JSON-RPC binding and through the same executor as the one-shot method, so the two are documented as one surface.
- Every other disclaimer in that part of the contract survives untouched: push notifications, task polling, task lookup, cancellation, push-configuration management and the extended card all stay outside the contract.
- The Agent Card the service publishes now advertises the streaming capability as available, so a standards-compliant A2A client can discover the route.
- Push notifications stay advertised as unavailable, and no other card field changes.
- A test reads the capability values out of the served card JSON — the actual wire response — rather than from source, so a serialization change cannot pass unnoticed.
- The one-shot method's behavior is untouched: its response shapes, session reuse, id validation, body cap and per-request locking are unchanged.
- The public posture is unchanged: the card stays credential-free and the execution route stays behind the existing bearer token.
- Nothing new is built: the streaming route already exists in the SDK; this change only makes the contract and the card tell the truth about it.
</summary>

<objective>
Make the frozen contract and the Agent Card tell the truth about a capability the service already serves: the contract's A2A section must claim the streaming method — over the same JSON-RPC binding and the same executor as the one-shot method — while keeping every other disclaimer, and the card must advertise the streaming capability as available while push notifications stay unavailable. Nothing else moves: the one-shot bridge, the bearer gate, the card's credential-free posture and every other disclaimed A2A method are unchanged.
</objective>

<context>
Paths below are repo-relative. Single Go module `github.com/bborbe/agent`; `interactive/` is a package in that root module, not a separate module.

Read before editing:

- `CLAUDE.md` (repo root) for project conventions.
- `docs/dod.md` — the Definition of Done this change is validated against (note its `CHANGELOG.md` requirement).
- `docs/interactive-service.md` — the frozen contract. Read the whole "A2A surface" section. Its disclaimer paragraph sits at the end of that section and is the ONLY part of the document this prompt changes.
- `interactive/a2a.go` — read `newAgentCard` in full. The `Capabilities` field of the returned `*a2a.AgentCard` is the one code line this prompt changes.
- `interactive/a2a_test.go` — read the `Describe("Agent card", ...)` block and its helpers `newA2ATestServer` and `getAgentCard`; the new capability spec reuses them.
- `interactive/a2a-handler.go` — read it to confirm what the streaming method actually does before claiming it in the contract. `a2aExecutor.Execute` is the single executor for both wire methods and yields the submitted task, then the artifact, then the terminal status.
- `CHANGELOG.md` — the top of the file. The newest released heading is `## v0.99.3`, and there is currently no `## Unreleased` heading.
- `specs/in-progress/059-interactive-service-claims-streaming.md` — the spec this prompt implements. § Desired Behavior 1-6, § Constraints and § Acceptance Criteria are the source of truth; § Suggested Decomposition states that a single prompt suffices.

Coding-plugin docs (paths as they exist INSIDE the build container):

- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape, the frozen preamble, and the conventional-prefix rule.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega conventions used by this repo's suite.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — the README / CHANGELOG requirements.

Load-bearing facts, verified against this worktree and the A2A SDK source at `$(go env GOMODCACHE)/github.com/a2aproject/a2a-go/v2@v2.6.0`:

1. `a2a.AgentCapabilities` (in `a2a/agent.go`) declares `Streaming bool` with JSON tag `streaming` and `PushNotifications bool` with JSON tag `pushNotifications`. NEITHER tag carries `omitempty`, so both keys are always present in the served card JSON and a zero value serializes as `false`.
2. The same `a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(&a2aExecutor{cache: s.cache}))` handler serves both `SendMessage` and `SendStreamingMessage`; there is exactly one executor and one event sequence (submitted task → artifact → terminal `completed` status). No new route, handler or executor is needed or wanted.
3. ⚠️ **This project runs dark-factory from a git worktree, so the container has NO usable git.** `dark-factory` refuses to start from a worktree unless `hideGit=true`, and with it set the container's `.git` is masked so `/workspace` is treated as a non-git directory — every `git` command inside fails. Do **not** use `git` for any check in this prompt; AC3's structural guard is content-based (SHA-256) instead. See `dark-factory/docs/troubleshooting.md` § "Running dark-factory from a git worktree or submodule".
4. `.maintainer.yaml` sets `release.autoRelease: true`, and `docs/dod.md` requires a `CHANGELOG.md` entry under `## Unreleased`.
5. The spec's AC3 constrains this change to exactly ONE non-test file under `interactive/`: `interactive/a2a.go`. There are 10 non-test `.go` files there; the other 9 must stay byte-identical to master.
</context>

<requirements>

## 1. Amend the frozen contract — `docs/interactive-service.md`

Replace the disclaimer paragraph at the end of the "A2A surface" section. It currently reads, verbatim:

```
This repository authors only the one-shot `SendMessage` bridge. The SDK's JSON-RPC handler
also routes `SendStreamingMessage` (SSE) through the same executor; that inherited method
is not part of this contract, and push notifications and task polling are not built here.
The SDK's handler additionally dispatches the rest of the A2A 1.0 method set — task
lookup, cancellation, push-configuration management and the extended card — against its
own default in-memory stores; none of those is part of this contract either. Only
`SendMessage` is authored and documented here.
```

Replace it with exactly this text:

```
This repository serves the one-shot `SendMessage` bridge, and the same JSON-RPC binding
serves `SendStreamingMessage` (SSE); the SDK's handler routes both through the same
executor, so a streaming request runs the same turn as a one-shot request and delivers
the same event sequence as an ordered SSE response, and
push notifications and task polling are not built here. The SDK's handler additionally
dispatches the rest of the A2A 1.0 method set — task lookup, cancellation,
push-configuration management and the extended card — against its own default in-memory
stores; none of those is part of this contract either.
```

Rules for this edit — each is measured by the spec's AC1 / AC5 evidence:

1. The three disclaiming clauses must not survive anywhere in the document: the opener `This repository authors only the one-shot`, the streaming clause's phrase `inherited method`, and the closer `is authored and documented here`.
2. The replacement MUST contain the phrase ``serves `SendStreamingMessage` `` — a document that merely deletes the disclaimer and says nothing about the route fails AC1.
3. These three clauses must survive VERBATIM, and the first two must each remain on a single source line (the spec's AC5 greps for them are line-based, so do not wrap inside them):
   - `push notifications and task polling are not built here`
   - `none of those is part of this contract either`
   - `task lookup, cancellation, push-configuration management and the extended card` (this one is checked newline-insensitively, so its wrapping may differ)
4. No single source line may contain both `SendStreamingMessage` and `not part of this contract`.
5. Change nothing else in the document. Do NOT touch the Agent Card paragraph above it, the routes table, the row-by-row contract table, the Authentication section, or any other section. The rewrite removes ONLY the streaming exclusion.

## 2. Advertise streaming in the Agent Card — `interactive/a2a.go`

In `newAgentCard`, change the `Capabilities` field of the returned `*a2a.AgentCard` from:

```go
Capabilities:       a2a.AgentCapabilities{},
```

to:

```go
Capabilities:       a2a.AgentCapabilities{Streaming: true},
```

The exact run of spaces before `a2a.AgentCapabilities` is gofmt's column alignment — keep the line however `make precommit`'s format step writes it.

- Do NOT add a `PushNotifications` field. `grep -c 'PushNotifications' interactive/a2a.go` must return `0`: the field is left at its zero value and the SDK's JSON tag (no `omitempty`) makes it serialize as `false`.
- Change nothing else in the file, and nothing else under `interactive/` except `interactive/a2a_test.go` (requirement 3). `interactive/a2a.go` is the only non-test file under `interactive/` that may differ from master.
- Do not change the card's name, version, description, supported interfaces, default modes, skills, or the `A2APublicURLFromEnv` accessor.

## 3. Assert the capability in the SERVED card JSON — `interactive/a2a_test.go`

Add ONE new spec inside the existing `Describe("Agent card", ...)` block, using the existing helpers `newA2ATestServer` and `getAgentCard`. Do not add new fixtures, and do not call unexported helpers. The spec must read the capability values from the raw JSON body — not from a decoded `a2a.AgentCard` — so that a renamed serialized key or a newly-added `omitempty` fails the test:

```go
It("advertises streaming and not push notifications", func() {
	server := newA2ATestServer(
		testPublicURL,
		interactive.NewAuthToken(authTestToken),
		newAuthMockFactory(),
	)
	defer server.Close()

	status, body, err := getAgentCard(server.URL)
	Expect(err).To(BeNil())
	Expect(status).To(Equal(http.StatusOK))

	var card map[string]any
	Expect(json.Unmarshal([]byte(body), &card)).To(Succeed())

	capabilities, ok := card["capabilities"].(map[string]any)
	Expect(ok).To(BeTrue(), "the served card must carry a capabilities object")
	Expect(capabilities["streaming"]).To(Equal(true))
	Expect(capabilities["pushNotifications"]).To(Equal(false))
})
```

Why the raw map and not `a2a.AgentCard`: decoding into the SDK struct uses the same struct tags that produced the JSON, so it would pass even if the SDK renamed the key or added `omitempty` — which is precisely the failure mode this assertion exists to catch. `Equal(true)` rather than `BeTrue()` additionally fails if the value serializes as a string instead of a JSON boolean.

The existing card specs — including "serves the agent card without a credential", which asserts `card["name"]` — must keep passing unchanged.

## 4. CHANGELOG entry — `CHANGELOG.md`

Create a `## Unreleased` heading immediately below the frozen SemVer preamble block (after the last MAJOR/MINOR/PATCH bullet and its blank line) and directly above `## v0.99.3`. Never above or inside any line of the preamble. Add ONE bullet whose line begins with exactly `- feat: ` — no scope suffix such as `feat(interactive):`, because `changelog-guide.md`'s bump regex is `^- ([a-z]+:)` and verification step 8 matches `^- feat:` literally.

The bullet must state: the `interactive` service's frozen contract now claims the A2A streaming method (`SendStreamingMessage`, SSE) over the same JSON-RPC binding and the same executor as the one-shot `SendMessage`, and the Agent Card now advertises `capabilities.streaming` as `true` while `capabilities.pushNotifications` stays `false`. Name that nothing else moved — the one-shot bridge, the bearer gate, the card's credential-free posture, and the other disclaimed A2A methods (push notifications, task polling, task lookup, cancellation, push-configuration management, the extended card) are unchanged — and that the route is inherited from the SDK rather than newly built. Do not describe verification; describe what changed.

</requirements>

<constraints>
- **`docs/interactive-service.md` is a frozen contract** — its own words are that it does not change without a spec. Spec 059 is that spec. The ONLY contract edit in scope is the disclaimer-paragraph rewrite in requirement 1.
- **The rewrite removes only the streaming exclusion.** Push notifications, task polling, task lookup, cancellation, push-configuration management and the extended card stay explicitly outside the contract. Do not tidy, reword or reflow the surviving clauses; use the replacement text given, verbatim.
- **`SendMessage`'s behavior must not change.** Same completed/failed task shapes, same artifact, same `contextId` validation against the anchored session-id regex, same 1 MiB body cap, same session cache and per-session lock. Do not edit `interactive/a2a-handler.go`.
- **Streaming is claimed, not built.** Add no route, no bespoke SSE handler, no second executor, and no change to the executor's event sequence.
- **`pushNotifications` stays `false`** — do not set the field in source; leave it at its zero value.
- **Exactly one non-test file under `interactive/` changes**: `interactive/a2a.go`. Test files under `interactive/` may change, but only `interactive/a2a_test.go`. No other non-test file under `interactive/` may be touched — that includes `interactive/a2a-handler.go`, which carries the one-shot bridge this change must protect.
- **The card stays credential-free and public**, and the `/a2a` route stays gated by the existing bearer token. Do not add a second credential, a new scheme or an exempt path.
- **No new dependency and no `go.mod` / `go.sum` change.** `github.com/a2aproject/a2a-go/v2` is already required at v2.6.0.
- **No `fmt.Errorf`** in Go code — this repo uses `github.com/bborbe/errors`. This prompt adds no error path; the rule is restated because `make precommit` lints for it.
- **Do not edit `README.md`** — this change adds no route, no configuration and no setup step.
- **Do not edit `specs/` or any other document.**
- **Do not commit** — dark-factory handles git.
</constraints>

<verification>
Run from the repository root. Every command must produce the annotated result.

```bash
# 1. The contract no longer disclaims streaming — AC1 negatives, each must print 0.
grep -c 'This repository authors only the one-shot' docs/interactive-service.md
grep -c 'inherited method' docs/interactive-service.md
grep -c 'is authored and documented here' docs/interactive-service.md
grep -n 'SendStreamingMessage' docs/interactive-service.md | grep -c 'not part of this contract'   # prints 0 pre- and post-fix; AC1 completeness only — not falsifiable alone

# 2. The contract now CLAIMS streaming — must print 1 (or more).
tr '\n' ' ' < docs/interactive-service.md | grep -cE 'serves .SendStreamingMessage.'

# 3. The other disclaimers survived — AC5, each must print 1 (or more).
grep -c 'push notifications and task polling are not built here' docs/interactive-service.md
grep -c 'none of those is part of this contract either' docs/interactive-service.md
tr '\n' ' ' < docs/interactive-service.md | grep -c 'task lookup, cancellation, push-configuration management and the extended card'

# 4. The card advertises streaming (AC2, must print 1 line) and does NOT set push notifications (AC4, must print 0).
grep -nE 'Streaming:\s+true' interactive/a2a.go
grep -c 'PushNotifications' interactive/a2a.go

# 5. The new served-JSON assertions exist in the test file.
grep -n 'capabilities\["streaming"\]' interactive/a2a_test.go
grep -n 'capabilities\["pushNotifications"\]' interactive/a2a_test.go

# 6. AC3 — exactly ONE non-test file under interactive/ may change: interactive/a2a.go.
# The container has NO usable git (this project runs dark-factory from a git worktree, where
# hideGit=true is mandatory and masks .git, so /workspace is a non-git directory). This guard is
# therefore CONTENT-based, not git-based. Verify the 9 other non-test files are byte-identical
# to their pinned pre-change digests:
HASH=$(command -v sha256sum || echo 'shasum -a 256'); $HASH \
  interactive/a2a-handler.go interactive/auth.go interactive/permission-handler.go \
  interactive/permission.go interactive/prompt.go interactive/readiness.go \
  interactive/service.go interactive/session-cache.go interactive/session-id.go
# Expected output — EXACTLY these 9 lines, in this order. Any other digest, any missing line,
# or a `command not found` means AC3 FAILS and you must revert whatever touched those files:
#   b665869611aeb20610dbdd3b3e31631709d8bbf19471ebeb618787f1a4386b99  interactive/a2a-handler.go
#   3068912863b808bf578cb5c46150cd2ff5276bfc4383c41a3189a3ecc064cacd  interactive/auth.go
#   f9c86824312519604aaa7ba2ea1b79199c21ab9f39cb115d8bf1a6c81456d3e9  interactive/permission-handler.go
#   f7eae88140fc75db9d96ab95270519ff36f11b4312209f7f44469445032509d6  interactive/permission.go
#   6bf7aba9f3d2747bcac1aaa33b0dfb61ca3d99c6d39c67394e19faf4f5c0363a  interactive/prompt.go
#   ae02c3c503e113dc33b21a2cb4d8d8e945f19146e557597d66f3b4ce08b27ae2  interactive/readiness.go
#   78319647472f87b6a8676c1919ea87609434213481570c9d1cd70f1c26451f78  interactive/service.go
#   b0b85725d3972c624bba5b4b7c6b6a0c1821bab4e04a2f079f6d76cfeddf80c6  interactive/session-cache.go
#   894ab998e16bba8276030f1c208197d89b465249d42afc31a8542f1e2823ccb6  interactive/session-id.go

# 7. Non-git cross-check for step 6 — the non-test file count under interactive/ is unchanged (must print 10).
find interactive -maxdepth 1 -name '*.go' -not -name '*_test.go' | wc -l

# 8. CHANGELOG entry.
grep -n '## Unreleased' CHANGELOG.md
awk '/^## /{sec=$0} sec=="## Unreleased" && /^- feat:/{found=1} END{exit !found}' CHANGELOG.md && echo "CHANGELOG feat OK"
```

Then run the full gate:

```bash
make precommit
make test
```

`make precommit` (ensure → format → generate → test → check → addlicense) must exit `0` — it is the repo's DoD gate, and its `format` step re-runs `gofmt`/`golines`, which decides the exact spacing on the changed `Capabilities:` line. `make test` must exit `0` with the new "advertises streaming and not push notifications" spec passing and every existing `SendMessage` round-trip spec passing unchanged.

**Self-check before finishing.** Re-run the block above and confirm each result. Then walk the spec's Desired Behavior 1-6 against your change: (1) the contract claims streaming; (2) the other disclaimers survive; (3) the card advertises streaming and not push notifications, with no other card field changed; (4) the served JSON carries the claim; (5) `SendMessage` is untouched; (6) the posture — public credential-free card, bearer-gated route — is unchanged. Desired Behavior 4's on-the-wire half and the spec's AC6 post-deploy criterion are operator-side: they require a deployed pod built from another repository's release chain and are NOT verifiable in this container, so they are deliberately not part of this prompt's verification.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **Decomposition.** One prompt, as the spec's § Suggested Decomposition prescribes ("A single prompt suffices"). The change is one prose reconciliation in a frozen document plus one capability field, with one test addition; there is no second layer to sequence and no second prompt to sequence against. The CHANGELOG entry rides along because `docs/dod.md` (the pipeline's `validationPrompt`) requires it and this is the change's only prompt.
- **The exact replacement text is pre-validated.** The old → new block in requirement 1 was measured against every grep the spec's AC1 and AC5 name, on a scratch copy of the document: baseline (unfixed) prints `1` / `1` / `1` for the three AC1 negatives and `0` for the `serves …` fragment; the replacement prints `0` / `0` / `0` and `1`; and all three AC5 clauses still print `1`. The wrap points in the replacement are chosen so that `push notifications and task polling are not built here` and `none of those is part of this contract either` each sit whole on one source line — AC5's greps for those two are line-based, so an executor that re-wraps inside them breaks the criterion even though the words survive.
- **Open question 1 — the card's capability is not documented in the contract.** Desired Behavior 3 says the card advertises streaming, but the spec's Constraint says "No other edit to the contract is in scope" and its Non-goals say "the rewrite removes only the streaming exclusion". The contract's Agent Card paragraph never enumerated capabilities in the first place, so this prompt leaves it untouched. If the reviewer wants the contract to also state `capabilities.streaming: true` / `capabilities.pushNotifications: false`, that is one added sentence — but it is new content, not a reconciliation, and no AC measures it.
- **Open question 2 — CHANGELOG is not named by any AC.** It is included because `docs/dod.md` requires an entry under `## Unreleased` and `.maintainer.yaml` has `release.autoRelease: true` (so: heading under `## Unreleased`, never a version heading or tag). Prefix `feat:` is chosen — a capability becomes discoverable, MINOR. `fix:` is defensible if the reviewer reads the under-declaration itself as the defect. Delete requirement 4 if the reviewer wants the change to be doc+code only.
- **Open question 3 — AC3's check is content-based, not git-based (RESOLVED).** `dark-factory` runs from this git worktree, where `hideGit=true` is mandatory and masks `.git`, so the container workspace is a **non-git directory** and no `git` check can run there. AC3's guard is therefore SHA-256 pinning of the 9 files that must not change (verification step 6); a typo'd path cannot silently defeat it, because a bad path makes the hashing command fail loudly. The spec's operator-side rung keeps the equivalent `git diff` check on the host, where git does work.
- **Open question 4 — one AC1 grep is not falsifiable on its own.** `grep -n 'SendStreamingMessage' | grep -c 'not part of this contract'` already prints `0` against the unfixed document (the disclaimer sentence wraps across two source lines), so it cannot distinguish pre-fix from post-fix. It is included because AC1 lists it; the falsifiable halves are the three negative greps and the `serves …` fragment, all measured above.
- **Operator-side rung (not in this prompt's verification).** The spec's post-deploy criterion (AC's Rung-2 / Desired Behavior 4's wire half) requires the `bborbe/agent` tag, the `agent-claude` dependency bump, the image build and the nuke dev pin — another repository's release chain, named out of scope by the spec's Non-goals. The card probe and the streaming `curl` probe live on the spec's Verification ladder, not here.
