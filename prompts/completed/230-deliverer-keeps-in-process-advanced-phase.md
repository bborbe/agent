---
status: completed
summary: 'Kafka and file result deliverers now remember the last non-terminal phase advanced to in-process and republish it on every later phase-preserving save, so a Job that walks planning→execution no longer has its phase clobbered back to the Job-start phase; verified by 7 new Kafka specs (incl. a real two-phase Agent end-to-end run) plus a file-deliverer spec, all red before the fix and green after. Note: bare `make precommit` cannot run in this container because hideGit leaves ROOTDIR empty (`git rev-parse --show-toplevel` fails), so golangci-lint looks for `/.golangci.yml`; the identical gate run as `make precommit ROOTDIR=/workspace` exits 0 with `ready to commit`.'
execution_id: agent-inprocess-phase-exec-230-deliverer-keeps-in-process-advanced-phase
dark-factory-version: dev
created: "2026-10-06T07:40:08Z"
queued: "2026-10-06T07:47:57Z"
started: "2026-10-06T07:48:35Z"
completed: "2026-10-06T07:59:06Z"
---

# Keep the phase an Agent advanced to in-process across later in-place saves

<summary>
- A multi-phase agent that walks from one phase into the next inside a single Job no longer loses that advance when a later step of the same Job saves in place
- Today the next in-place save writes the phase the Job started at back onto the task; the re-triggered Job then skips every step of the old phase and the task parks forever (observed live 2026-10-06 on the trading hypothesis agent)
- The result deliverer now remembers the last non-terminal phase it advanced to in this Job and publishes it on every later save that would otherwise keep the incoming phase
- That covers in-place saves, progress saves, needs-input escalations and transient failures, so retries and operator resumes also land on the advanced phase
- Terminal advances (`done`) are never remembered; a Job that never advances behaves exactly as before
- Status, assignee clearing, previous-assignee recording, retry semantics and target-vault carry-over are unchanged
- The local file deliverer gets the same guard, because with the passthrough generator it has the same defect
- Covered by deliverer-level specs plus an end-to-end spec that runs a real two-phase Agent through the real Kafka deliverer
- CHANGELOG gains a patch-level `fix:` entry
</summary>

<objective>
Stop `kafkaResultDeliverer` (and `fileResultDeliverer`) from clobbering an in-process phase advance: once a delivery in this Job has published `Done` + a non-terminal `NextPhase`, every later phase-preserving delivery from the same deliverer must publish that advanced phase instead of the Job-start phase. After this prompt, a planning step that advances to `execution` followed by an in-place save in `execution` leaves the task record at `phase: execution`, so the next Job resumes in `execution` instead of parking at `planning`.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1; `delivery/` is a package in that root module). Paths below are repo-relative.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done (doc comments, `github.com/bborbe/errors`, Ginkgo/Gomega + Counterfeiter, CHANGELOG entry under `## Unreleased`).

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape and `## Unreleased` placement.

Files to read IN FULL before editing:
- `delivery/result-deliverer.go` — the only production file this prompt changes. Key symbols: `fileResultDeliverer` + its `DeliverResult` (~line 50–75), `NewKafkaResultDelivererWithSender` (~line 101), `kafkaResultDeliverer` struct (~line 117), `kafkaResultDeliverer.DeliverResult` (~line 125), `applyResultFrontmatter` (~line 199), `resolveNextPhase` (~line 344).
- `delivery/result-deliverer_test.go` — the test file this prompt extends. Existing `Describe("FileResultDeliverer", ...)` and `Describe("KafkaResultDeliverer", ...)` blocks; the Kafka block builds the deliverer in `JustBeforeEach` from `sender` (`*cqrsmocks.CDBCommandObjectSender`), `taskID`, `originalContent`, `generator` (`*libmocks.AgentContentGenerator`), `clock`; published frontmatter is read via `sender.SendCommandObjectArgsForCall(i)` → `cmdObj.Command.Data["frontmatter"].(map[string]interface{})`. See the existing `Context("real passthrough generator end-to-end (spec 052 reproduction)", ...)` for the real-generator pattern.
- `delivery/content-generator.go` — `applyStatusFrontmatter` and `NewPassthroughContentGenerator`. The passthrough generator IGNORES `originalContent` and builds from `result.Output`.
- `agent_agent.go` — `Agent.Run`, the loop after the `// Exit conditions` comment: on `Done` + a `NextPhase` that names another phase of the same Agent, it runs that phase's steps in the same process, with the SAME `deliverer` and the SAME `*Markdown`.
- `agent_runner.go` — `StepRunner.Run`: after every step it calls `md.Marshal(ctx)` and `r.deliverer.DeliverResult(ctx, AgentResultInfo{Status, Output: newContent, Message, NextPhase, ContinueToNext, AgentTurns, InteractionCount})`.

Root cause (verified against the source):
1. Within one Job, `Agent.Run` walks planning → execution in-process using one deliverer instance.
2. The frontmatter the deliverer receives from its generator always carries the **Job-start** phase: the fallback/section generators build from `d.originalContent` (task markdown as of Job start); the passthrough generator builds from `result.Output`, the re-serialized in-memory `*Markdown`, whose frontmatter `phase` the in-process walk never advances.
3. `applyResultFrontmatter` sets `phase` only on `Done` + non-empty `NextPhase`. Every other branch — `Done` + empty `NextPhase`, `AgentStatusNeedsInput`, `AgentStatusInProgress`, `AgentStatusFailed`, `default` — preserves the incoming phase.
4. So: step A delivers `Done` + `NextPhase: execution` → record phase `execution`; an `execution` step then delivers `Done` + `""` (or `in_progress`) → deliverer publishes `phase: planning` → the re-triggered Job starts at `planning`, every planning step's `ShouldRun` is false, `Agent.Run` returns nil, the task parks forever. Observed live 2026-10-06 on `bborbe/trading`'s hypothesis agent (task 74270983).

Concurrency (verified): within this repo `DeliverResult` is only called sequentially (`StepRunner.Run` loop, `Agent.Run` loop, `Agent.unsupportedPhase`, and the single-shot `claude/task-runner.go` `deliver` via `resultDelivererAdapter`). The deliverer is constructed per Job with that Job's `originalContent`, so per-instance state is the correct scope. Because the constructors are exported and consumed by other repositories, and `make test` runs with `-race`, the new field is guarded by a `sync.Mutex` held for the whole `DeliverResult` call.

Other `agentlib.ResultDeliverer` implementations in this repo: `noopResultDeliverer` (writes nothing — no change) and `fileResultDeliverer` (writes the generated content, phase included; with the passthrough generator it has the same defect — fixed here). `mocks/agent-result-deliverer.go` is a generated fake — do not touch.

`.dark-factory.yaml`: `workflow: direct`, `testCommand: make test`, no `hideGit`. No `git` command is used in this prompt's verification. This is a bug fix → patch release.
</context>

<requirements>

1. **Write the failing tests FIRST (red).** Implement steps 5 and 6 below before touching production code. Then run:

   ```bash
   cd /workspace && go test -mod=mod -race ./delivery/ -ginkgo.focus='advanced in-process' -ginkgo.v 2>&1 | tee /tmp/red.log
   ```

   Confirm spec 5a (`Done` + `NextPhase: execution` then `Done` + `""`) FAILS with Gomega reporting the actual value `planning` against the expected `execution`. Record that failure line in your completion summary. Only then proceed to steps 2–4.

2. **Kafka deliverer: add per-Job state** in `delivery/result-deliverer.go`. Add `"sync"` to the imports. Extend the struct (keep existing fields and order; append the two new fields):

   ```go
   type kafkaResultDeliverer struct {
   	commandObjectSender cdb.CommandObjectSender
   	taskID              agentlib.TaskIdentifier
   	originalContent     string
   	generator           ContentGenerator
   	currentDateTime     libtime.CurrentDateTimeGetter

   	// mu serialises DeliverResult so advancedPhase is read and written safely.
   	mu sync.Mutex
   	// advancedPhase is the last non-terminal phase this deliverer published on a
   	// Done + NextPhase result. A deliverer is constructed per Job (with that Job's
   	// originalContent), so this is per-Job state: Agent.Run advances phases
   	// in-process with one deliverer, and every later phase-preserving save in the
   	// same Job must publish this phase instead of the Job-start phase it was built
   	// from. Empty means "no advance yet — keep the incoming phase".
   	advancedPhase string
   }
   ```

   `NewKafkaResultDelivererWithSender` keeps returning `&kafkaResultDeliverer{...}` with the same five fields; the zero-value `sync.Mutex` and empty `advancedPhase` need no initialisation. Do not change either constructor's signature.

3. **Kafka deliverer: carry the advanced phase.**

   a. At the top of `kafkaResultDeliverer.DeliverResult`, before `d.generator.Generate(...)`, add:

   ```go
   d.mu.Lock()
   defer d.mu.Unlock()
   ```

   b. In `kafkaResultDeliverer.DeliverResult`, immediately after the existing `d.applyResultFrontmatter(frontmatter, result)` call and before `d.stampTargetVault(frontmatter)`, add `d.carryAdvancedPhase(frontmatter, result)`.

   c. Add this method (place it directly after `applyResultFrontmatter`):

   ```go
   // carryAdvancedPhase keeps an in-process phase advance from being clobbered by a
   // later save in the same Job. On Done + NextPhase it records the phase
   // applyResultFrontmatter just resolved — unless that phase is the terminal
   // "done", which is never recorded. On every other (phase-preserving) result it
   // overrides the incoming phase with the recorded one, if any. Status, assignee,
   // previous_assignee and every other key are left exactly as
   // applyResultFrontmatter set them. Caller must hold d.mu.
   func (d *kafkaResultDeliverer) carryAdvancedPhase(
   	frontmatter agentlib.TaskFrontmatter,
   	result agentlib.AgentResultInfo,
   ) {
   	if result.Status == agentlib.AgentStatusDone && result.NextPhase != "" {
   		if phase, _ := frontmatter["phase"].(string); phase != "" && phase != "done" {
   			d.advancedPhase = phase
   		}
   		return
   	}
   	if d.advancedPhase != "" {
   		frontmatter["phase"] = d.advancedPhase
   	}
   }
   ```

   Do NOT add the logic inside `applyResultFrontmatter`'s `switch` — keeping it in a separate method keeps `applyResultFrontmatter` under the repo's `funlen` (80 lines) and `gocognit` (20) limits. `human_review`, `planning`, `execution`, `ai_review` ARE recorded; an invalid `NextPhase` resolves to `done` via `resolveNextPhase` and is therefore NOT recorded. A later `Done` + `NextPhase: done` does not clear an earlier recorded phase (unreachable in practice: `Agent.Run` exits on `done`).

   d. Update the phase comments in `applyResultFrontmatter` so they no longer claim the incoming phase always wins. Replace each of these comments:
      - `// phase intentionally not modified — preserves incoming phase` (in the `Done` empty-`NextPhase` branch and the `AgentStatusInProgress` branch)
      - `// phase is preserved from incoming frontmatter (already copied from fmMap above)` (in the `AgentStatusNeedsInput`, `AgentStatusFailed` and `default` branches)

      with:

      ```go
      // phase not modified here — preserves the incoming phase, or the phase this
      // Job already advanced to (applied afterwards by carryAdvancedPhase)
      ```

      Also update the sentence in the `Done` empty-`NextPhase` branch comment `keep status: in_progress and preserve the phase from incoming frontmatter` to `keep status: in_progress and preserve the incoming phase (or the phase this Job already advanced to)`. In the function's doc comment, change `(assignee cleared, phase unchanged)` to `(assignee cleared, phase unchanged — see carryAdvancedPhase)`. Do not change any code inside the `switch`.

4. **File deliverer: same guard.** `fileResultDeliverer` reads the task file from disk, but the passthrough generator ignores that content and rebuilds from `result.Output`, so it clobbers the advance identically.

   a. Extend the struct:

   ```go
   type fileResultDeliverer struct {
   	generator ContentGenerator
   	filePath  string

   	// mu serialises DeliverResult so advancedPhase is read and written safely.
   	mu sync.Mutex
   	// advancedPhase is the last non-terminal phase this deliverer wrote on a
   	// Done + NextPhase result; see kafkaResultDeliverer.advancedPhase.
   	advancedPhase string
   }
   ```

   `NewFileResultDeliverer` keeps its signature and keeps returning `&fileResultDeliverer{generator: generator, filePath: filePath}`.

   b. In `fileResultDeliverer.DeliverResult`: add `d.mu.Lock()` / `defer d.mu.Unlock()` as the first statements; after the `d.generator.Generate(...)` error check and before `os.WriteFile`, add `generated = d.carryAdvancedPhase(generated, result)`.

   c. Add:

   ```go
   // carryAdvancedPhase is the file-delivery twin of
   // kafkaResultDeliverer.carryAdvancedPhase, operating on the generated markdown.
   // Caller must hold d.mu.
   func (d *fileResultDeliverer) carryAdvancedPhase(
   	generated string,
   	result agentlib.AgentResultInfo,
   ) string {
   	if result.Status == agentlib.AgentStatusDone && result.NextPhase != "" {
   		fm, _ := ParseMarkdownFrontmatter(generated)
   		if phase, _ := fm["phase"].(string); phase != "" && phase != "done" {
   			d.advancedPhase = phase
   		}
   		return generated
   	}
   	if d.advancedPhase == "" {
   		return generated
   	}
   	return SetFrontmatterField(generated, "phase", d.advancedPhase)
   }
   ```

   `ParseMarkdownFrontmatter(content string) (map[string]any, string)` and `SetFrontmatterField(content, key, value string) string` already exist in `delivery/markdown.go`. Error paths in `fileResultDeliverer.DeliverResult` (read / generate / write) stay exactly as they are.

5. **Kafka deliverer specs** — in `delivery/result-deliverer_test.go`, inside `Describe("KafkaResultDeliverer", ...)`, add `Context("phase advanced in-process within one Job", ...)`. Use a local helper:

   ```go
   publishedFrontmatterAt := func(i int) map[string]interface{} {
   	_, cmdObj := sender.SendCommandObjectArgsForCall(i)
   	fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
   	Expect(ok).To(BeTrue())
   	return fm
   }
   ```

   Drive two deliveries on the SAME `deliverer` (built by the existing `JustBeforeEach`) with `generator.GenerateReturnsOnCall(i, content, nil)`. The generated content for every delivery after the first carries the Job-start phase, mirroring production:

   ```go
   advancedContent := "---\nstatus: in_progress\nphase: execution\nassignee: hypothesis-agent\n---\n\nBody.\n"
   jobStartContent := "---\nstatus: in_progress\nphase: planning\nassignee: hypothesis-agent\n---\n\nBody.\n"
   ```

   Specs (each asserts `sender.SendCommandObjectCallCount()` equals the number of deliveries):

   - **5a** `Done` + `NextPhase: "execution"` (generator call 0 → `advancedContent`), then `Done` + `NextPhase: ""` (call 1 → `jobStartContent`) → second published frontmatter has `phase: execution`, `status: in_progress`. This is the regression spec: it fails before the fix with `planning`.
   - **5b** same first delivery, then `AgentStatusInProgress` → second published `phase: execution`, `status: in_progress`, `assignee: hypothesis-agent`.
   - **5c** same first delivery, then `AgentStatusNeedsInput` with `Message: "need input"` → second published `phase: execution`, `status: in_progress`, `assignee: ""`, `previous_assignee: hypothesis-agent` (assignee clearing unchanged).
   - **5d** same first delivery, then `AgentStatusFailed` with `Message: "timeout"` (call 1 content = `jobStartContent` with `trigger_count: 1` added, below the default cap) → second published `phase: execution`, `assignee: hypothesis-agent` (retry stays routable on the advanced phase).
   - **5e** single `Done` + `NextPhase: ""` delivery with no prior advance (`GenerateReturns(jobStartContent, nil)`) → published `phase: planning` (unchanged behaviour control).
   - **5f** `Done` + `NextPhase: "done"` (call 0 → `"---\nstatus: completed\nphase: done\n---\n\nBody.\n"`), then `Done` + `NextPhase: ""` (call 1 → `jobStartContent`) → second published `phase: planning` (terminal `done` is never recorded as the advanced phase).
   - **5g (end-to-end, real seam)** — `Context` name must also contain `advanced in-process`. Build a real two-phase Agent and run it through the real Kafka deliverer with the real passthrough generator, reproducing the live incident:

     ```go
     jobContent := "---\ntitle: Hypothesis\nstatus: in_progress\nphase: planning\nassignee: hypothesis-agent\n---\n\nBody.\n"
     planStep := &libmocks.AgentStep{}
     planStep.NameReturns("plan")
     planStep.ShouldRunReturns(true, nil)
     planStep.RunReturns(&agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "execution"}, nil)
     execStep := &libmocks.AgentStep{}
     execStep.NameReturns("exec")
     execStep.ShouldRunReturns(true, nil)
     execStep.RunReturns(&agentlib.Result{Status: agentlib.AgentStatusInProgress}, nil)
     agent := agentlib.NewAgent(
     	agentlib.NewPhase(domain.TaskPhasePlanning, planStep),
     	agentlib.NewPhase(domain.TaskPhaseExecution, execStep),
     )
     realDeliverer := delivery.NewKafkaResultDelivererWithSender(
     	sender, taskID, jobContent, delivery.NewPassthroughContentGenerator(), clock,
     )
     _, err := agent.Run(ctx, domain.TaskPhasePlanning, jobContent, realDeliverer)
     ```

     (Use a local name such as `jobContent`, not `originalContent`, so the outer `originalContent` variable is not shadowed.) Assert `err` is nil, `planStep.RunCallCount()` and `execStep.RunCallCount()` are both 1, `sender.SendCommandObjectCallCount()` is 2, the first published `phase` is `execution`, and the second published `phase` is `execution` (before the fix: `planning`). Import `"github.com/bborbe/vault-cli/pkg/domain"` (already a module dependency; `domain.TaskPhasePlanning` = `"planning"`, `domain.TaskPhaseExecution` = `"execution"`). Signatures used: `agentlib.NewAgent(phases ...Phase) *Agent`, `agentlib.NewPhase(name domain.TaskPhase, steps ...Step) Phase`, `(*Agent).Run(ctx context.Context, phaseName domain.TaskPhase, taskContent string, deliverer ResultDeliverer) (*Result, error)`; `libmocks.AgentStep` is the existing counterfeiter fake in `mocks/agent-step.go`.

6. **File deliverer spec** — inside `Describe("FileResultDeliverer", ...)`, add `Context("phase advanced in-process within one Job (passthrough generator)", ...)` with one spec: build `delivery.NewFileResultDeliverer(delivery.NewPassthroughContentGenerator(), tmpFile.Name())`; deliver `Done` + `NextPhase: "execution"` with `Output: jobStartContent` (as defined in step 5), then `Done` + `NextPhase: ""` with `Output: jobStartContent`; read the file back, parse it with `delivery.ParseMarkdownFrontmatter`, and assert `phase` is `execution` (before the fix: `planning`). Reuse the existing `tmpFile` setup/teardown of that Describe.

7. **CHANGELOG** — `CHANGELOG.md` currently has no `## Unreleased` section (the newest section is `## v0.96.0`). If `## Unreleased` exists when you edit, append to it; otherwise insert `## Unreleased` immediately above the first `## v` heading (below the SemVer preamble, which is not modified). Add one bullet:

   ```
   - fix: keep the phase an `Agent` advanced to in-process when a later step of the same Job saves in place. The Kafka (and file) result deliverer rebuilt every save from the Job-start frontmatter, so after a step published `Done` + `NextPhase: execution` and `Agent.Run` walked into `execution` in the same Job, the next phase-preserving save (`Done` without `NextPhase`, `in_progress`, `needs_input`, `failed`) wrote the Job-start phase back; the re-triggered Job then skipped every step of the old phase and the task parked forever. The deliverer now remembers the last non-terminal phase it advanced to and publishes it on every later phase-preserving save in the same Job; `done` is never remembered, and status, assignee and retry semantics are unchanged.
   ```

8. **Run the full suite (green)** — after steps 2–4, re-run the focused command from step 1 (all `advanced in-process` specs must pass, including 5a), then `make precommit`.

</requirements>

<constraints>
- Production change is confined to `delivery/result-deliverer.go`; tests to `delivery/result-deliverer_test.go`; plus `CHANGELOG.md`. No other file changes.
- Do NOT change any exported signature: `NewKafkaResultDeliverer`, `NewKafkaResultDelivererWithSender`, `NewFileResultDeliverer`, `NewNoopResultDeliverer`, and the `agentlib.ResultDeliverer` interface stay exactly as they are (consumed by other repositories).
- Do NOT change `Agent.Run`, `StepRunner`, any `ContentGenerator`, or `applyResultFrontmatter`'s `switch` logic — only its comments.
- Keep all other behaviour identical: status values, assignee clearing on `needs_input`/cap exhaustion/unknown status, `previous_assignee` recording, retry (`trigger_count`/`max_triggers`) semantics, `target_vault` carry-over, metrics keys.
- The terminal phase `done` is never recorded as the advanced phase. Phases are the canonical `vault-cli` values (`planning`, `execution`, `ai_review`, `human_review`, `done`) — never invent new values.
- `noopResultDeliverer` writes nothing and is not changed. Do not edit generated mocks in `mocks/`.
- Existing specs in `delivery/result-deliverer_test.go` must keep passing unchanged.
- Tests use Ginkgo v2 / Gomega with the existing Counterfeiter fakes; external test package `delivery_test`.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run every command in ONE shell from `/workspace`. Do not use `git` commands.

Red before green (done during step 1, before the production change): spec 5a must FAIL with the published phase `planning` where `execution` was expected; quote that Gomega failure line in your completion summary. After the change all specs below are green.

```bash
cd /workspace
grep -A3 '\[FAILED\] Expected' /tmp/red.log | grep -q 'planning' && echo RED-CONFIRMED   # must print RED-CONFIRMED (log written in step 1, before the production change)

# the new specs, focused — every spec must pass, 0 failed
go test -mod=mod -race ./delivery/ -ginkgo.focus='advanced in-process' -ginkgo.v 2>&1 | tee /tmp/green.log | tail -40
grep -E 'Ran ([8-9]|[1-9][0-9]) of' /tmp/green.log   # >= 8 specs ran
grep -E '\b0 Failed' /tmp/green.log                 # must print a line

# the new specs exist (Kafka context, end-to-end spec, file context)
grep -c 'advanced in-process' delivery/result-deliverer_test.go          # must print >= 2
grep -n 'agent.Run(' delivery/result-deliverer_test.go   # must print 1 line
grep -n 'GenerateReturnsOnCall' delivery/result-deliverer_test.go | head  # must print lines

# production change shape
grep -c 'advancedPhase' delivery/result-deliverer.go                     # must print >= 8
grep -n 'func (d \*kafkaResultDeliverer) carryAdvancedPhase' delivery/result-deliverer.go   # 1 line
grep -n 'func (d \*fileResultDeliverer) carryAdvancedPhase' delivery/result-deliverer.go    # 1 line
grep -c 'sync.Mutex' delivery/result-deliverer.go                        # must print 2
! grep -n 'preserves incoming phase$' delivery/result-deliverer.go       # stale comment gone

# changelog
awk '/^## /{print "first section: "$0; exit}' CHANGELOG.md              # must print "first section: ## Unreleased"
grep -c 'advanced to in-process' CHANGELOG.md                            # must print 1

# full gate
make precommit
```

`make precommit` must end with `ready to commit`. Then walk each `<constraints>` bullet against the diff of `delivery/result-deliverer.go` and confirm it holds.
</verification>
