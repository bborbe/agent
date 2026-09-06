---
status: completed
spec: [052-bug-kafka-result-stub-drops-target-vault]
summary: 'Stamped target_vault into published Kafka task frontmatter from originalContent when the generated content lacks it, with AC1-AC3 and end-to-end passthrough test rows plus a fix: changelog entry'
execution_id: agent-target-vault-stub-exec-215-spec-052-kafka-result-stub-target-vault
dark-factory-version: dev
created: "2026-09-06T16:02:11Z"
queued: "2026-09-06T16:05:46Z"
started: "2026-09-06T16:05:48Z"
completed: "2026-09-06T16:10:16Z"
branch: dark-factory/bug-kafka-result-stub-drops-target-vault
---

<summary>
- Fixes a live-observed routing regression: when an agent returns a stub result (failed / needs_input / unsupported-phase with empty or body-only `Output`), the published Kafka task-update message drops the task's `target_vault` from its frontmatter
- The controller's routing guard decides vault ownership from the result message's frontmatter; a message without `target_vault` falls through to legacy routing, so the non-owning controller scans its own vault, misses the file, and fires the `AgentControllerResultNotFound` alert (bborbe/nuke #140) — noise that masks real drops
- The Kafka result deliverer already holds the full original task markdown (which includes `target_vault`); the fix stamps `target_vault` into the published frontmatter from that original content only when the generated content lacks it
- A full result whose generated content already carries `target_vault` publishes byte-for-byte unchanged — the existing value is never overwritten, never duplicated
- Legacy tasks and direct CLI runs whose original content has no `target_vault` (or no frontmatter at all) are untouched — no key is invented, and their routing stays exactly as today
- One production change in the shared `delivery` library, so every agent that publishes results benefits; no new config fields, flags, or thresholds
- Spec AC1-AC3 are proven with Ginkgo rows in the existing deliverer test suite, plus one end-to-end row driving the real passthrough generator through the deliverer (the exact reproduction from the spec)
- Changelog gains a `fix:` entry under a new `## Unreleased` section; `make precommit` must exit 0
</summary>

<objective>
Make every published result message — stub or full — carry the task's `target_vault` in its frontmatter, so the controller's routing guard always decides ownership from the message and the non-owning controller skips instead of scanning-and-dropping. The fix lands in `kafkaResultDeliverer.DeliverResult` in the shared `delivery` package: stamp `target_vault` from `originalContent`'s frontmatter when the generated content lacks it, and change nothing else. This is a single-layer change in one production file plus its tests and a changelog entry — one prompt, no split.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single-module repo at `/workspace`, `module github.com/bborbe/agent`; `make precommit` / `make test` run from `/workspace`, never from a subdir).

Coding-plugin docs (read before editing, paths as they exist INSIDE the YOLO container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega suites, counterfeiter mocks, external test packages (`*_test`), coverage ≥80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions in the file you edit.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments for the new helper.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — `## Unreleased` placement and entry format; there is currently NO `## Unreleased` section in `/workspace/CHANGELOG.md` (top is `## v0.87.1`), so one must be created directly above the highest `## vX.Y.Z` heading, after the preamble header block.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing:
- `/workspace/delivery/result-deliverer.go` — THE production file this prompt changes. `kafkaResultDeliverer.DeliverResult` (lines 117-170) builds the published `agentlib.Task.Frontmatter` from `ParseMarkdownFrontmatter(generated)` plus `applyResultFrontmatter` overlays; the new stamp goes here. `applyResultFrontmatter` (lines 187-253) is the in-package precedent for a helper that mutates the frontmatter map in place.
- `/workspace/delivery/result-deliverer_test.go` — THE test file this prompt changes. The `Describe("KafkaResultDeliverer")` block (lines 100-667) has a shared `BeforeEach` (mocks `sender`/`clock`/`generator`, sets `originalContent` and `taskID`) and a `JustBeforeEach` that builds `deliverer = delivery.NewKafkaResultDelivererWithSender(sender, taskID, originalContent, generator, clock)`. New `Context` blocks slot in before the closing `})` at line 667.
- `/workspace/delivery/markdown.go` — `ParseMarkdownFrontmatter(content string) (map[string]any, string)` (line 112): splits a markdown document with YAML frontmatter into a typed map and body; returns an empty map and full content when there is no frontmatter; returns an empty map on YAML parse failure.
- `/workspace/agent_status.go` — `AgentResultInfo` struct (lines 33-51: `Status AgentStatus`, `Output string`, `Message string`, `NextPhase string`, `ContinueToNext bool`) and the `AgentStatus*` constants.
- `/workspace/agent_task-frontmatter.go` — `TaskFrontmatter` is `map[string]interface{}` (line 16); `target_vault` is an ordinary string key read via `frontmatter["target_vault"]` — there is no typed accessor for it, do NOT invent one.
- `/workspace/CHANGELOG.md` — read the top 15 lines to confirm the header block and the highest `## vX.Y.Z` heading before inserting `## Unreleased`.

Existing primitives to pin behavior to (do NOT invent new ones):
- `ParseMarkdownFrontmatter(content string) (map[string]any, string)` in `/workspace/delivery/markdown.go` — already used at `DeliverResult` line 126; reuse it to read `target_vault` from `d.originalContent`'s frontmatter.
- The mock `libmocks.AgentContentGenerator` (`/workspace/mocks/delivery-content-generator.go`) with `GenerateReturns(content string, err error)` — already used throughout `result-deliverer_test.go`; reuse it, do not hand-write mocks.
- The mock `cqrsmocks.CDBCommandObjectSender` with `SendCommandObjectReturns(err error)` and the published-command accessor `_, cmdObj := sender.SendCommandObjectArgsForCall(0)` followed by `fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})` — the established pattern for asserting on the published frontmatter (the JSON round-trip through `base.ParseEvent`).
- The REAL `delivery.NewPassthroughContentGenerator()` (defined in `/workspace/delivery/content-generator.go`, lines 184-205) — for the end-to-end reproduction test. On `AgentStatusFailed` with empty `Output` it produces status-only frontmatter (the observed 1-key stub) plus a `## Failure` body.

Spec being implemented: `specs/in-progress/052-bug-kafka-result-stub-drops-target-vault.md`. The fix location, the `target_vault` key spelling (lowercase with underscore, value e.g. `personal`), the Desired Behavior, the three Acceptance Criteria, and the Failure Modes table are all spelled out there. The controller (`routing.ShouldProcessResult`) is in a different repo and is NOT touched by this prompt.

Background on the key spelling: `target_vault` is the markdown-frontmatter key in the task files (observed verbatim in the spec's logs: `skipped vault mismatch target_vault="personal" vault="openclaw"`). Do NOT confuse it with the camelCase JSON `targetVault` field used by `command/task` commands — that is a different struct, out of scope. In `agentlib.TaskFrontmatter` the key is used as the raw literal `"target_vault"`, matching the existing raw-literal style of `applyResultFrontmatter` (`frontmatter["status"]`, `frontmatter["phase"]`, `frontmatter["assignee"]`, `frontmatter["previous_assignee"]`). No new constant is needed.
</context>

<requirements>

## 1. Add the `stampTargetVault` helper to `/workspace/delivery/result-deliverer.go`

Insert a new unexported free function immediately AFTER the `applyResultFrontmatter` method's closing brace (currently line 253), BEFORE the `resolveNextPhase` doc-comment block (currently line 255). Do NOT change `applyResultFrontmatter` or `resolveNextPhase` in any way.

```go
// stampTargetVault adds target_vault to the task frontmatter from the original
// task content when the generated content lacks it. Stub results (failed /
// needs_input / unsupported-phase, empty or body-only Output) produce
// status-only generated frontmatter; without target_vault the controller's
// routing guard (routing.ShouldProcessResult) falls through to legacy routing
// and the non-owning controller scans-and-drops the result (spec 052). No-op
// when the generated content already carries target_vault (full results echo
// unchanged), or when originalContent has no frontmatter / no target_vault
// (legacy tasks and direct CLI runs keep today's routing).
func stampTargetVault(frontmatter agentlib.TaskFrontmatter, originalContent string) {
	if _, ok := frontmatter["target_vault"]; ok {
		return
	}
	originalFM, _ := ParseMarkdownFrontmatter(originalContent)
	if tv, ok := originalFM["target_vault"].(string); ok && tv != "" {
		frontmatter["target_vault"] = tv
	}
}
```

Behavior contract (the spec's Desired Behavior — do not deviate):

- Generated content already carries `target_vault` → early return, existing value preserved, no overwrite, no duplicate (map write never happens). This is Desired Behavior 2 and Failure Modes rows 2-3.
- Generated content lacks `target_vault` AND `originalContent` frontmatter has a non-empty string `target_vault` → `frontmatter["target_vault"]` is set to that value. This is Desired Behavior 1.
- `originalContent` has no frontmatter, or frontmatter without `target_vault`, or an empty/non-string `target_vault` → no-op, no bogus key, nothing else changes. This is Desired Behavior 3 and Failure Modes row 1.
- The type assertion `.(string)` guards against a YAML non-string `target_vault` value; the `tv != ""` guard skips an empty value. `ParseMarkdownFrontmatter` never panics on malformed input (it returns an empty map), so this path cannot panic.

## 2. Call `stampTargetVault` from `DeliverResult`

In `kafkaResultDeliverer.DeliverResult` (lines 117-170), add exactly one call line immediately AFTER the existing `d.applyResultFrontmatter(frontmatter, result)` line (currently line 133), so the status/phase overlays run first and the target_vault echo stamp is the final frontmatter step before serialization:

```go
	d.applyResultFrontmatter(frontmatter, result)

	stampTargetVault(frontmatter, d.originalContent)
```

Do NOT change any other line in `DeliverResult`. `d.originalContent` is the full original task markdown already stored on the deliverer (the `TASK_CONTENT` env echo described in the spec) — it includes `target_vault` for every task created through the controller's create path. No new imports are needed: `ParseMarkdownFrontmatter` is same-package (`delivery/markdown.go`) and `agentlib` is already imported. The function stays under the funlen limit (~56 lines; the stamp is a single call).

## 3. Add the AC1-AC3 test contexts to `Describe("KafkaResultDeliverer")` in `/workspace/delivery/result-deliverer_test.go`

Insert a new top-level `Context("target_vault echo from originalContent (spec 052)", ...)` block immediately before the closing `})` of the existing `Describe("KafkaResultDeliverer")` block (line 667), after the last existing `Context("AgentStatusFailed with incoming phase: ai_review", ...)` block. The new context overrides the shared `originalContent` `BeforeEach` variable, so the deliverer is rebuilt by the outer `JustBeforeEach` with the new content. Use the existing mock generator for these rows (this is the spec's named evidence shape):

```go
	Context("target_vault echo from originalContent (spec 052)", func() {
		BeforeEach(func() {
			originalContent = "---\ntitle: Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05\nstatus: in_progress\ntarget_vault: personal\n---\n\nBody.\n"
		})

		It("AC1: stamps target_vault on a stub result (empty Output, failed status)", func() {
			// Stub result: the generator produces status-only frontmatter (the
			// observed frontmatter keys=1 case from the spec) and the result
			// has empty Output.
			generator.GenerateReturns(
				"---\nstatus: in_progress\n---\n\nBody.\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Output:  "",
				Message: "claude step failed",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["target_vault"]).To(Equal("personal"))
		})

		Context("AC2: full result already carries target_vault in the generated content", func() {
			BeforeEach(func() {
				// Stale originalContent value must never clobber the generated
				// value — this is what makes the "no overwrite" assertion
				// meaningful (Failure Modes row 2).
				originalContent = "---\ntitle: Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05\nstatus: in_progress\ntarget_vault: openclaw\n---\n\nBody.\n"
			})

			It("preserves the existing value exactly once (no overwrite, no duplicate)", func() {
				// Full echo: the generated content itself carries target_vault.
				generator.GenerateReturns(
					"---\nstatus: completed\nphase: done\ntarget_vault: personal\n---\n\nBody.\n\n## Result\n\nok\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:    agentlib.AgentStatusDone,
					NextPhase: "done",
				})
				Expect(err).NotTo(HaveOccurred())
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				// The generated value (personal) wins; the stale openclaw from
				// originalContent is never stamped. A Go map cannot hold the key
				// twice, so "personal" present with the stale value absent proves
				// no overwrite and no duplicate.
				Expect(fm["target_vault"]).To(Equal("personal"))
			})
		})

		Context("AC3: originalContent without target_vault", func() {
			BeforeEach(func() {
				originalContent = "---\ntitle: Legacy task\nstatus: in_progress\n---\n\nBody.\n"
			})

			It("adds no target_vault key", func() {
				generator.GenerateReturns(
					"---\nstatus: in_progress\n---\n\nBody.\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status: agentlib.AgentStatusFailed,
					Output: "",
				})
				Expect(err).NotTo(HaveOccurred())
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(fm).NotTo(HaveKey("target_vault"))
			})
		})

		Context("AC3: originalContent without frontmatter", func() {
			BeforeEach(func() {
				originalContent = "Just body text with no frontmatter delimiters.\n"
			})

			It("adds no target_vault key", func() {
				generator.GenerateReturns(
					"---\nstatus: in_progress\n---\n\nBody.\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status: agentlib.AgentStatusFailed,
					Output: "",
				})
				Expect(err).NotTo(HaveOccurred())
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(fm).NotTo(HaveKey("target_vault"))
			})
		})
	})
```

These rows are the spec's AC evidence: AC1 asserts `published.Frontmatter["target_vault"] == "personal"` on a failed/empty-Output stub; AC2 asserts the generated value present exactly once with the stale originalContent value absent; AC3 (two rows, matching "without frontmatter (or without target_vault)") asserts no `target_vault` key. The `cmdObj.Command.Data["frontmatter"]` access traverses the real cqrs `base.ParseEvent` JSON serialization boundary — the same path production traffic takes.

## 4. Add the end-to-end reproduction test with the REAL passthrough generator

Add a second new top-level `Context("real passthrough generator end-to-end (spec 052 reproduction)", ...)` block immediately after the context added in requirement 3 (still before the closing `})` of `Describe("KafkaResultDeliverer")`). This is the exact reproduction path from the spec: `DeliverResult` running the real passthrough generator over an empty `Output`. It is NOT satisfied by requirement 3's mock-generator rows alone — those prove the deliverer's stamping, this one proves the real production generator + deliverer combination ends with `target_vault` on the wire:

```go
	Context("real passthrough generator end-to-end (spec 052 reproduction)", func() {
		It("publishes target_vault on a failed stub with empty Output", func() {
			originalContent := "---\ntitle: Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05\nstatus: in_progress\ntarget_vault: personal\n---\n\nAnalyze the sentry issue.\n"
			passthrough := delivery.NewKafkaResultDelivererWithSender(
				sender,
				taskID,
				originalContent,
				delivery.NewPassthroughContentGenerator(),
				clock,
			)
			err := passthrough.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Output:  "",
				Message: "claude step failed",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			// The real passthrough generator drops target_vault (it ignores
			// originalContent); the deliverer's stamp must restore it.
			Expect(fm["target_vault"]).To(Equal("personal"))
		})
	})
```

`delivery.NewPassthroughContentGenerator()`, `delivery.NewKafkaResultDelivererWithSender`, `sender`, `taskID`, and `clock` are all already available (the latter three from the shared `BeforeEach`); no new imports or mocks are needed.

## 5. Add the CHANGELOG entry

`/workspace/CHANGELOG.md` currently has NO `## Unreleased` section (top is `## v0.87.1`). Per the changelog guide, insert a new `## Unreleased` section directly above the highest `## vX.Y.Z` heading (immediately after the preamble header block, i.e. between the `* PATCH version ...` bullet and `## v0.87.1`) with exactly one bullet:

```markdown
## Unreleased

- fix: `delivery` `kafkaResultDeliverer` now stamps `target_vault` into the published task frontmatter from the original task content when the generated content lacks it — stub results (failed / needs_input / unsupported-phase with empty or body-only output) no longer drop `target_vault`, so the controller's routing guard skips non-owning results instead of scanning-and-dropping and firing `AgentControllerResultNotFound` (spec 052)
```

Do NOT rename any version heading, do NOT move or alter the preamble header block, and do NOT add any other bullet. (This is a `fix:` → patch bump per the changelog guide.)

## 6. Scope containment

Edit ONLY these three files:
- `/workspace/delivery/result-deliverer.go`
- `/workspace/delivery/result-deliverer_test.go`
- `/workspace/CHANGELOG.md`

Do NOT touch: `/workspace/delivery/content-generator.go` (the passthrough generator stays as-is — it is a library API other repos consume; the deliverer owns the echo), `/workspace/delivery/markdown.go`, `agent_task-frontmatter.go` (no typed `TargetVault()` accessor), `mocks/`, any `command/task` file (the camelCase `TargetVault` JSON field there is unrelated), `agent-task-controller` / `routing.ShouldProcessResult` (different repo, spec Non-goal), or the alert expression (spec Non-goal). `fileResultDeliverer` (same file) is untouched by design — it writes to the owning vault's file directly; routing is a Kafka-message concern (spec Goal/ACs).

## 7. Failure-mode self-check (walk the spec table before finishing)

Each spec Failure Modes row must be satisfied by the code and tests you wrote:
- Stub published while `originalContent` is empty or has no `target_vault` (legacy task / direct CLI run) → `stampTargetVault` is a no-op (empty map + type-assertion miss), no panic, no bogus key → AC3 rows (requirement 3).
- Full echo that already carries `target_vault` → early return, not clobbered by a stale `originalContent` value, no duplicate → AC2 row (requirement 3).
- `TaskFrontmatter` map already holds `target_vault` → stamp is a no-op (the `if _, ok := frontmatter["target_vault"]; ok { return }` guard) → AC2 row.

</requirements>

<constraints>
- Kafka message schema otherwise untouched — only the missing-`target_vault` case is stamped (a pre-existing frontmatter key added back; no new field shape).
- Legacy unstamped tasks keep routing as today — a message without `target_vault` still falls through the guard unchanged (spec Non-goals: do NOT change `ShouldProcessResult`, do NOT add a controller-side fallback, do NOT change the alert).
- Full-result echo behavior preserved exactly — generated `target_vault` is never overwritten or duplicated.
- No new config fields, flags, thresholds, metrics, or constants. The key is the raw literal `"target_vault"` (matches the existing `frontmatter["status"]` / `frontmatter["phase"]` literal style in this same file).
- The `target_vault` key spelling is lowercase-with-underscore and is read/written as a plain `map[string]interface{}` key via `frontmatter["target_vault"]` — do NOT add a typed accessor to `TaskFrontmatter`.
- Ginkgo v2 / Gomega; counterfeiter mocks only (reuse `libmocks.AgentContentGenerator` and `cqrsmocks.CDBCommandObjectSender`, never hand-write mocks); the test file stays `package delivery_test`.
- Error wrapping in `result-deliverer.go` follows `github.com/bborbe/errors` (unchanged — the new code adds no error paths).
- Coverage: the new `stampTargetVault` helper must be ≥80% statement-covered by the requirement-3 rows (the exercised branches: present → return; absent + present-in-original → set; absent-in-original → no-op (the empty-string guard is defense-in-depth; add a `target_vault: ""` row if you want it covered)).
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass. None of the existing `result-deliverer_test.go` rows set `target_vault` in `originalContent` or in their generated outputs, so the stamp is a no-op for all of them — the new code must not change their assertions.
- This prompt is the whole spec — there is deliberately one prompt, matching the spec's single-layer fix ("One change, benefits every agent using the deliverer").
</constraints>

<verification>
Run from `/workspace` (single-module repo; the Makefile and go.mod are at root — there is no `delivery/Makefile`).

```bash
cd /workspace
# 1. Targeted delivery package tests (fast iteration — run after each edit):
go test -mod=mod ./delivery/ -v 2>&1 | tee /tmp/delivery-test.log
# Must exit 0. The new Ginkgo rows must appear in the output (grep each It description):
grep -E "AC1: stamps target_vault|preserves the existing value exactly once|adds no target_vault key|real passthrough generator end-to-end" /tmp/delivery-test.log
# Each pattern must match at least one line.

# 2. Root module still green:
cd /workspace && make test
# Must exit 0.

# 3. Full pipeline (AC4 — must exit 0; runs ensure + format + generate + test + lint + license):
cd /workspace && ROOTDIR=/workspace make precommit
# ROOTDIR is overridden because the Makefile derives it via `git rev-parse --show-toplevel`,
# and .git is masked/unavailable in the container — without the override the golangci-lint
# --config path resolves to a nonexistent root and `make precommit` fails on an unrelated cause.

# 4. Scope check — only the three intended files carry changes:
# (.git is unavailable in the container, so confirm manually instead of git status.)
# Expected: /workspace/delivery/result-deliverer.go, /workspace/delivery/result-deliverer_test.go,
# /workspace/CHANGELOG.md. `make precommit`'s `go mod tidy`/`rm -rf vendor` may touch
# go.sum/go.mod as a side effect of the ensure target — that is expected and not a scope violation.
```
</verification>
