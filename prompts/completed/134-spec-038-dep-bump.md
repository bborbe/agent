---
status: completed
spec: [038-rename-task-status-phase-taxonomy]
summary: Bumped vault-cli to v0.64.3 across all 6 modules, updated TaskFrontmatter.Phase()/Status() to normalize legacy values via NormalizeTaskPhase/NormalizeTaskStatus, fixed resolveNextPhase to use NormalizeTaskPhase (preventing 'in_progress' from failing canonical validation), updated executor defaultTriggerPhases/knownPhases to TaskPhaseExecution, replaced all TaskPhaseInProgress in executor test file with TaskPhaseExecution, updated Phase flag defaults and usage strings in agent/claude/gemini/code, and updated CRD Trigger doc comment.
container: agent-exec-134-spec-038-dep-bump
dark-factory-version: v0.162.0
created: "2026-05-20T17:00:00Z"
queued: "2026-05-20T17:19:49Z"
started: "2026-05-20T17:47:59Z"
completed: "2026-05-20T18:04:29Z"
branch: dark-factory/rename-task-status-phase-taxonomy
---

<summary>
- vault-cli dependency is bumped to the first published version that exposes TaskStatusNext ("next") and TaskPhaseExecution ("execution") across lib/, task/controller/, task/executor/, agent/claude/, agent/gemini/, and agent/code/
- lib/agent_task-frontmatter.go Phase() and Status() accessors call NormalizeTaskPhase / NormalizeTaskStatus so legacy values ("in_progress", "todo") transparently resolve to the new canonical during task evaluation
- task/executor/pkg/handler/task_event_handler.go defaultTriggerPhases and knownPhases are updated to use domain.TaskPhaseExecution instead of domain.TaskPhaseInProgress
- task/executor tests that reference domain.TaskPhaseInProgress are updated to domain.TaskPhaseExecution so they compile and produce the new canonical phase value
- agent/claude/main.go and agent/claude/cmd/run-task/main.go Phase flag defaults change from "in_progress" to "execution" with updated usage strings
- task/executor/k8s/apis CRD Trigger doc comment is updated to reference "execution" instead of "in_progress"
- make precommit exits 0 in lib/, task/executor/, agent/claude/, agent/gemini/, agent/code/
- task/controller test literal updates and make precommit are deferred to the companion prompt (2-spec-038-test-updates)
</summary>

<objective>
Propagate vault-cli's rename from canonical status "todo" → "next" and canonical phase "in_progress" → "execution" through all non-test Go code in the agent repo: dependency version, lib frontmatter accessors, executor trigger-phase constants, and agent startup flag defaults. After this prompt, all newly published or transitioned tasks will emit the new canonical values, and in-flight tasks with legacy values continue to load correctly via vault-cli's normalize functions.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `go-enum-type-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — enum constants, Available* lists, Validate(), Contains()
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface/struct patterns, accessor methods
- `go-mod-dependency-fix-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — go get / go mod tidy workflow
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — entry format and `## Unreleased` rules
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — phase lifecycle, executor respawn gates, terminal-phase contract
- `docs/agent-job-lifecycle.md` — phase values used during job spawning

**Current state (grep-verified at spec creation time):**
- All 6 modules currently use vault-cli v0.64.1 or v0.64.2; neither has TaskStatusNext or TaskPhaseExecution. The rename ships in **v0.64.3** (must be tagged before this prompt can succeed).
- `lib/agent_task-frontmatter.go:18` — `Status()` converts raw string to `domain.TaskStatus` without normalize
- `lib/agent_task-frontmatter.go:23` — `Phase()` converts raw string to `domain.TaskPhase` without normalize
- `task/executor/pkg/handler/task_event_handler.go:31` — `defaultTriggerPhases` uses `domain.TaskPhaseInProgress`
- `task/executor/pkg/handler/task_event_handler.go:70` — `knownPhases` uses `domain.TaskPhaseInProgress`
- `agent/claude/main.go:84` — Phase flag: `default:"in_progress"` and `usage:"Agent phase: planning | in_progress | ai_review"`
- `agent/claude/cmd/run-task/main.go:68` — Phase flag: `default:"in_progress"` and `usage:"Agent phase: planning | in_progress | ai_review"`
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go:39` — Trigger doc comment: "phases: planning/in_progress/ai_review; statuses: in_progress"
- `task/executor/pkg/handler/task_event_handler_test.go` — ~30 occurrences of `domain.TaskPhaseInProgress`

**Files to read in full before editing:**
- `lib/agent_task-frontmatter.go` — all typed accessors; Phase() at line 23, Status() at line 18
- `lib/agent_task_test.go` — existing tests for Phase/Status accessors
- `task/executor/pkg/handler/task_event_handler.go` — full file; defaultTriggerPhases (line ~26), knownPhases (line ~66)
- `task/executor/pkg/handler/task_event_handler_test.go` — full file (>1000 lines; use chunked reads with offset/limit); all domain.TaskPhaseInProgress occurrences
- `agent/claude/main.go` — Phase field struct tag at line 84
- `agent/claude/cmd/run-task/main.go` — Phase field struct tag at line 68
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` — Trigger struct doc comment
</context>

<requirements>

## 1. Verify vault-cli ≥ v0.64.3 is published — STOP if not

The rename was implemented in vault-cli `master` and queued for release as **v0.64.3** (see `https://github.com/bborbe/vault-cli/blob/master/CHANGELOG.md` — the `## v0.64.3` entry introduces `TaskStatusNext`, `TaskPhaseExecution`, `IsValidTaskPhase`, and `NormalizeTaskPhase`). The minimum acceptable version is **v0.64.3**.

Check the latest published tag:

```bash
go list -m -versions github.com/bborbe/vault-cli 2>/dev/null | tr ' ' '\n' | sort -V | tail -5
```

The list MUST include `v0.64.3` or higher. If the highest tag is still `v0.64.x`, the release has not yet been cut. STOP immediately and report `status: failed` with message: "vault-cli v0.64.3 not yet published — latest tag is <HIGHEST>. Re-run after vault-cli v0.64.3 is tagged."

If v0.64.3+ is available, pick the highest published tag as `TARGET_VERSION` and verify the constants are present:

```bash
# Replace vX.Y.Z with TARGET_VERSION
go mod download github.com/bborbe/vault-cli@vX.Y.Z 2>/dev/null
grep -n "TaskStatusNext" \
  $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@vX.Y.Z/pkg/domain/task_status.go 2>/dev/null
grep -n "TaskPhaseExecution" \
  $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@vX.Y.Z/pkg/domain/task_phase.go 2>/dev/null
```

Both greps MUST return ≥1 line for the version to qualify. If only one of the two constants is present at TARGET_VERSION, STOP with `status: failed` and report the missing constant.

**If found:** Record TARGET_VERSION (e.g. `v0.64.3`). Continue to step 2.

After finding the target version, read the new API fully to understand exact function signatures:
```bash
cat $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_status.go
cat $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_phase.go
```

Check whether old constant names still exist (needed to decide scope of code changes):
```bash
grep -n "TaskStatusTodo\|TaskPhaseInProgress\|TaskPhaseTodo" \
  $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_status.go \
  $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_phase.go 2>/dev/null
```

Record per-constant presence:
- `TaskStatusTodo` present in new vault-cli? (yes/no)
- `TaskPhaseInProgress` present in new vault-cli? (yes/no)
- `TaskPhaseTodo` present in new vault-cli? (yes/no — `knownPhases` in step 5b references it)

If `TaskPhaseTodo` was removed, step 5b's `knownPhases` map must drop that entry — otherwise the file will not compile.

Confirm NormalizeTaskPhase function exists and its signature:
```bash
grep -n "func NormalizeTask" \
  $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_status.go \
  $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_phase.go 2>/dev/null
```

Record all findings before proceeding — exact constant names and function signatures determine the code changes in steps 3–5.

## 2. Bump vault-cli in all 6 go.mod files

Run in sequence (each module independently; do NOT run at repo root):
```bash
(cd lib             && go get github.com/bborbe/vault-cli@${TARGET_VERSION} && go mod tidy)
(cd task/controller && go get github.com/bborbe/vault-cli@${TARGET_VERSION} && go mod tidy)
(cd task/executor   && go get github.com/bborbe/vault-cli@${TARGET_VERSION} && go mod tidy)
(cd agent/claude    && go get github.com/bborbe/vault-cli@${TARGET_VERSION} && go mod tidy)
(cd agent/gemini    && go get github.com/bborbe/vault-cli@${TARGET_VERSION} && go mod tidy)
(cd agent/code      && go get github.com/bborbe/vault-cli@${TARGET_VERSION} && go mod tidy)
```

Verify all modules are pinned to the same version:
```bash
grep -rn "vault-cli" lib/go.mod task/controller/go.mod task/executor/go.mod \
  agent/claude/go.mod agent/gemini/go.mod agent/code/go.mod | grep -v "^#"
```
Expected: all 6 entries show `github.com/bborbe/vault-cli ${TARGET_VERSION}`.

Quick compile check to surface constant-rename breakage early:
```bash
cd lib && go build ./... 2>&1 | head -20
cd task/executor && go build ./pkg/handler/... 2>&1 | head -20
cd agent/claude && go build ./... 2>&1 | head -20
```
If any module reports undefined: domain.TaskPhaseInProgress or similar, that constant was removed in the new vault-cli. Step 5 (executor handler) and step 6 (executor tests) fix these.

## 3. Update lib/agent_task-frontmatter.go Phase() and Status() to normalize

Read `lib/agent_task-frontmatter.go` in full before editing.

### 3a. Update Status() to normalize

Locate the current Status() method (line ~18):
```go
func (f TaskFrontmatter) Status() domain.TaskStatus {
	v, _ := f["status"].(string)
	return domain.TaskStatus(v)
}
```

Replace with (adapt the Normalize call to match the exact signature found in step 1):
```go
func (f TaskFrontmatter) Status() domain.TaskStatus {
	v, _ := f["status"].(string)
	if canonical, ok := domain.NormalizeTaskStatus(v); ok {
		return canonical
	}
	return domain.TaskStatus(v)
}
```

### 3b. Update Phase() to normalize

Locate the current Phase() method (line ~23):
```go
func (f TaskFrontmatter) Phase() *domain.TaskPhase {
	v, ok := f["phase"].(string)
	if !ok || v == "" {
		return nil
	}
	p := domain.TaskPhase(v)
	return &p
}
```

Replace with (adapt Normalize call to match the signature found in step 1 — likely returns `(TaskPhase, bool)`):
```go
func (f TaskFrontmatter) Phase() *domain.TaskPhase {
	v, ok := f["phase"].(string)
	if !ok || v == "" {
		return nil
	}
	if canonical, ok := domain.NormalizeTaskPhase(v); ok {
		return &canonical
	}
	p := domain.TaskPhase(v)
	return &p
}
```

If `NormalizeTaskPhase` does not exist in the new vault-cli (verify in step 1), skip the normalize call for Phase() and document this in the completion report under `## Improvements`.

Build check:
```bash
cd lib && go build ./...
```
Expected: exit 0.

### 3c. Add normalize behavior tests to lib/agent_task_test.go

Read `lib/agent_task_test.go` before editing. Locate the existing `Describe` blocks for Phase and Status accessors.

In the Status accessor's `Describe` block, add:
```go
It("normalizes legacy status 'todo' to TaskStatusNext", func() {
	fm := lib.TaskFrontmatter{"status": "todo"}
	Expect(fm.Status()).To(Equal(domain.TaskStatusNext))
})
```

In the Phase accessor's `Describe` block, add:
```go
It("normalizes legacy phase 'in_progress' to TaskPhaseExecution", func() {
	fm := lib.TaskFrontmatter{"phase": "in_progress"}
	p := fm.Phase()
	Expect(p).NotTo(BeNil())
	Expect(*p).To(Equal(domain.TaskPhaseExecution))
})

It("returns nil for absent phase (normalize path)", func() {
	fm := lib.TaskFrontmatter{}
	Expect(fm.Phase()).To(BeNil())
})
```

The `"returns nil for absent phase"` test guards that normalize does not break the nil-return contract.

Verify domain is imported in test file:
```bash
grep -n '"github.com/bborbe/vault-cli/pkg/domain"' lib/agent_task_test.go
```
If absent, add to the import block.

Run iterative tests:
```bash
cd lib && go test ./... -v 2>&1 | grep -E "PASS|FAIL|normalize|Phase|Status" | head -20
```
Expected: exit 0; all 3 new tests appear as PASS.

## 4. Run make precommit in lib/

```bash
cd lib && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make errcheck`) and fix before retrying.

## 5. Update task/executor/pkg/handler/task_event_handler.go constants

Read the full file before editing.

### 5a. Update defaultTriggerPhases (line ~26–33)

Locate the `defaultTriggerPhases` var block. Change `domain.TaskPhaseInProgress` to `domain.TaskPhaseExecution`:

Before:
```go
var defaultTriggerPhases = domain.TaskPhases{
	domain.TaskPhasePlanning,
	domain.TaskPhaseInProgress,
	domain.TaskPhaseAIReview,
}
```

After:
```go
var defaultTriggerPhases = domain.TaskPhases{
	domain.TaskPhasePlanning,
	domain.TaskPhaseExecution,
	domain.TaskPhaseAIReview,
}
```

### 5b. Update knownPhases (line ~66–78)

Locate the `knownPhases` var block. Replace `domain.TaskPhaseInProgress` with `domain.TaskPhaseExecution`. If vault-cli still exports `TaskPhaseInProgress` as a deprecated constant (check: `grep -n TaskPhaseInProgress $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@${TARGET_VERSION}/pkg/domain/task_phase.go`), include it as a known alias to avoid false-positive unknown_phase log lines for in-flight tasks:

If `TaskPhaseInProgress` still exists in new vault-cli:
```go
var knownPhases = map[domain.TaskPhase]struct{}{
	domain.TaskPhaseTodo:        {},
	domain.TaskPhasePlanning:    {},
	domain.TaskPhaseExecution:   {}, // new canonical (was TaskPhaseInProgress)
	domain.TaskPhaseInProgress:  {}, // legacy alias — still a known phase string
	domain.TaskPhaseAIReview:    {},
	domain.TaskPhaseHumanReview: {},
	domain.TaskPhaseDone:        {},
}
```

If `TaskPhaseInProgress` was removed from vault-cli (and Phase() now normalizes "in_progress" → "execution" via step 3b):
```go
var knownPhases = map[domain.TaskPhase]struct{}{
	domain.TaskPhaseTodo:        {},
	domain.TaskPhasePlanning:    {},
	domain.TaskPhaseExecution:   {}, // new canonical (was in_progress)
	domain.TaskPhaseAIReview:    {},
	domain.TaskPhaseHumanReview: {},
	domain.TaskPhaseDone:        {},
}
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

Verify:
```bash
grep -n "TaskPhaseExecution\|TaskPhaseInProgress" task/executor/pkg/handler/task_event_handler.go
```
Expected: `TaskPhaseExecution` present in both `defaultTriggerPhases` and `knownPhases`.

## 6. Update task/executor/pkg/handler/task_event_handler_test.go

Read the full file before editing. It is >1000 lines — use chunked reads (e.g., `offset: 0, limit: 400`, then `offset: 400, limit: 400`, etc.) to read it fully before making changes.

Count the occurrences to update:
```bash
grep -c "domain\.TaskPhaseInProgress" task/executor/pkg/handler/task_event_handler_test.go
```

Replace ALL occurrences of `domain.TaskPhaseInProgress` with `domain.TaskPhaseExecution` in the test file. This includes:
- `"phase": string(domain.TaskPhaseInProgress)` — test frontmatter input fields
- `domain.TaskPhases{domain.TaskPhaseInProgress, ...}` — trigger phase slices
- DescribeTable Entry arguments that pass `domain.TaskPhaseInProgress` as the phase parameter

If `TaskPhaseInProgress` was removed from vault-cli (compile error in step 2), this replacement is mandatory to compile. If it still exists as deprecated, the replacement is still required by spec AC #5 (tests must use new canonical).

After editing, verify the replacement was complete:
```bash
grep -c "domain\.TaskPhaseInProgress" task/executor/pkg/handler/task_event_handler_test.go
```
Expected: 0.

Build check:
```bash
cd task/executor && go build ./...
```
Expected: exit 0.

Run iterative tests:
```bash
cd task/executor && go test ./pkg/handler/... -v 2>&1 | tail -30
```
Expected: exit 0. Fix any assertion failures before continuing.

## 7. Update Phase flag defaults in agent/claude/

Read `agent/claude/main.go` before editing.

At line 84, change the Phase field struct tag:

Before:
```go
Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | in_progress | ai_review" default:"in_progress"`
```

After:
```go
Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"execution"`
```

Read `agent/claude/cmd/run-task/main.go` before editing.

At line 67–68, change the Phase field and its comment:

Before:
```go
// Phase to run (defaults to in_progress; framework requires explicit phase)
Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | in_progress | ai_review" default:"in_progress"`
```

After:
```go
// Phase to run (defaults to execution; framework requires explicit phase)
Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"execution"`
```

Verify:
```bash
grep -n 'default:"in_progress"' agent/claude/main.go agent/claude/cmd/run-task/main.go
```
Expected: 0 lines.

```bash
grep -n 'default:"execution"' agent/claude/main.go agent/claude/cmd/run-task/main.go
```
Expected: ≥1 match in each file.

```bash
grep -rn 'planning | in_progress | ai_review' agent/ --include='*.go' --exclude-dir=vendor
```
Expected: 0 lines.

Build check:
```bash
cd agent/claude && go build ./...
```
Expected: exit 0.

## 8. Update CRD Trigger doc comment in types.go

Read `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` before editing.

Find the Trigger struct doc comment (line ~38–39):
```go
// Trigger declares the per-agent phase and status conditions under which the executor spawns a Job.
// Absent or empty lists fall back to the default allow-list (phases: planning/in_progress/ai_review; statuses: in_progress).
```

Update the second line:
```go
// Trigger declares the per-agent phase and status conditions under which the executor spawns a Job.
// Absent or empty lists fall back to the default allow-list (phases: planning/execution/ai_review; statuses: in_progress).
```

Verify:
```bash
grep -n "execution" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Expected: ≥1 match in the Trigger doc comment.

```bash
grep -n "in_progress/ai_review" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Expected: 0 lines (all occurrences updated).

## 9. Add CHANGELOG entry

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add (substitute `${TARGET_VERSION}` with the actual version string recorded in step 1 — do NOT leave the literal `${TARGET_VERSION}` placeholder in CHANGELOG.md):
```markdown
- chore(deps): bump `github.com/bborbe/vault-cli` to <ACTUAL_VERSION> across lib, task/controller, task/executor, agent/claude, agent/gemini, agent/code — exposes `TaskStatusNext` and `TaskPhaseExecution` constants
- refactor(lib): `TaskFrontmatter.Phase()` and `Status()` now call `NormalizeTaskPhase` / `NormalizeTaskStatus` so legacy phase `"in_progress"` and status `"todo"` transparently resolve to new canonical values on read
- refactor(task/executor): `defaultTriggerPhases` and `knownPhases` updated to reference `domain.TaskPhaseExecution` instead of `domain.TaskPhaseInProgress`
- refactor(agent/claude): Phase flag default changed from `"in_progress"` to `"execution"`; usage string updated to `planning | execution | ai_review`
```

After insertion, verify no placeholder leaked:
```bash
grep -n '\${TARGET_VERSION}\|<ACTUAL_VERSION>' CHANGELOG.md
```
Expected: 0 lines (literal placeholder must be replaced).

Verify:
```bash
grep -n "TaskPhaseExecution\|execution\|vault-cli" CHANGELOG.md | head -6
```
Expected: ≥4 matches under `## Unreleased`.

## 10. Run make test then make precommit in each changed module

Run in sequence (lib was verified in step 4; repeat for remaining modules):

```bash
cd task/executor && make test
```
Expected: exit 0.
```bash
cd task/executor && make precommit
```
Expected: exit 0.

```bash
cd agent/claude && make test
```
Expected: exit 0.
```bash
cd agent/claude && make precommit
```
Expected: exit 0.

```bash
cd agent/gemini && make test
```
Expected: exit 0.
```bash
cd agent/gemini && make precommit
```
Expected: exit 0.

```bash
cd agent/code && make test
```
Expected: exit 0.
```bash
cd agent/code && make precommit
```
Expected: exit 0.

For any failing target, run only that target (`make lint`, `make gosec`, `make errcheck`) and fix before retrying full precommit.

NOTE: `task/controller/` make precommit is intentionally NOT run here — its test files use string literals ("todo", "in_progress") that require updates handled in the companion prompt `2-spec-038-test-updates`. Running task/controller precommit before those updates may produce test failures.

</requirements>

<constraints>
- **STOP if vault-cli ≥ v0.64.3 is not published.** Report `status: failed`. Do not attempt to modify code against v0.64.x, which lacks the new constants.
- **All 6 modules MUST use the same vault-cli version** — no version drift across modules. Pin to TARGET_VERSION in one pass.
- **No `go mod vendor` calls** — explicit spec constraint.
- **Add imports before running `go mod tidy`** to avoid transitive demotion.
- **Phase() must still return nil when "phase" key is absent or empty string** — the normalize call must not change this behavior.
- **Phase() must handle unknown phase values gracefully** — if NormalizeTaskPhase returns ok=false, return the raw domain.TaskPhase so downstream callers (knownPhases check) can detect unknown values.
- **Status() must handle empty string gracefully** — NormalizeTaskStatus("") likely returns ("", false); fall through to return domain.TaskStatus("").
- **defaultTriggerPhases MUST use domain.TaskPhaseExecution** — tasks in the vault with old "in_progress" phase are normalized by Phase() before reaching the trigger check, so there is no backward compat gap.
- **errors.Wrapf from github.com/bborbe/errors** for any new error wrapping — never fmt.Errorf.
- **Do NOT modify task/controller test files** — those are handled in prompt 2.
- **Do NOT commit.** dark-factory handles git.
- The Phase flag default MUST be `"execution"` — spec AC explicitly checks for zero `default:"in_progress"` lines.
</constraints>

<verification>

vault-cli version pinned consistently:
```bash
grep -rn "github.com/bborbe/vault-cli" lib/go.mod task/controller/go.mod task/executor/go.mod \
  agent/claude/go.mod agent/gemini/go.mod agent/code/go.mod | grep -v "^#"
```
Expected: all entries show the same version ≥ v0.64.3.

Phase() normalizes legacy value:
```bash
grep -n "NormalizeTaskPhase\|NormalizeTaskStatus" lib/agent_task-frontmatter.go
```
Expected: ≥1 match for each function in Phase() and Status() respectively.

Phase() nil-return contract preserved:
```bash
grep -B2 -A8 "func (f TaskFrontmatter) Phase" lib/agent_task-frontmatter.go
```
Expected: nil return on absent/empty phase is still present before the normalize call.

Flag defaults updated in agent/claude:
```bash
grep -n 'default:"in_progress"' agent/claude/main.go agent/claude/cmd/run-task/main.go
```
Expected: 0 lines.

```bash
grep -n 'default:"execution"' agent/claude/main.go agent/claude/cmd/run-task/main.go
```
Expected: ≥1 match in each file.

No legacy usage strings remaining:
```bash
grep -rn 'planning | in_progress | ai_review' agent/ --include='*.go' --exclude-dir=vendor
```
Expected: 0 lines.

CRD comment updated:
```bash
grep -n 'execution' task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Expected: ≥1 match in Trigger doc comment.

Executor trigger phases use new canonical:
```bash
grep -n "TaskPhaseExecution" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥2 matches (in defaultTriggerPhases and knownPhases).

Executor test file uses new canonical:
```bash
grep -c "domain\.TaskPhaseInProgress" task/executor/pkg/handler/task_event_handler_test.go
```
Expected: 0.

Full precommit in each changed module:
```bash
(cd lib           && make precommit)
(cd task/executor && make precommit)
(cd agent/claude  && make precommit)
(cd agent/gemini  && make precommit)
(cd agent/code    && make precommit)
```
Expected: all 5 exit 0.

</verification>
