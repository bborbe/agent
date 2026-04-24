---
status: committing
summary: Injected PHASE env var into spawned agent Jobs sourced from task frontmatter phase field, with empty string when absent; extracted taskPhaseString helper to stay within funlen limit; extended existing test and added new empty-phase test case.
container: agent-073-inject-phase-env-var
dark-factory-version: v0.132.0
created: "2026-04-24T11:50:00Z"
queued: "2026-04-24T11:44:56Z"
started: "2026-04-24T11:44:58Z"
---

<summary>
- The executor now injects a `PHASE` environment variable into every spawned agent Job
- Agents can read `PHASE` to dispatch per-phase logic (planning, in_progress, ai_review) without parsing the task frontmatter themselves
- Phase-unaware agents ignore the variable — no behavioral change for them
- When `phase` is missing from frontmatter, the variable is set to the empty string (agents treat empty as "no phase dispatch")
- A unit test asserts the variable is present in the spawned Job's container env, populated from the task frontmatter `phase` field
- Minimal change — one line added to the env-builder, one assertion added to the existing job-spawner test
</summary>

<objective>
Extend `jobSpawner.SpawnJob` to inject `PHASE` into the agent container's env, sourced from the task frontmatter's `phase` field. After this change, agents can branch on the current phase without re-parsing `TASK_CONTENT` frontmatter. Phase-unaware agents continue to work unchanged because they never read `PHASE`.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `github.com/bborbe/errors`, never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2, external test packages, `Expect`/`Equal`

**Key files to read in full before editing:**

- `task/executor/pkg/spawner/job_spawner.go` — contains `SpawnJob`. The env-builder section (currently ~line 76-83) is the only production edit site:
  ```go
  envBuilder := k8s.NewEnvBuilder()
  envBuilder.Add("TASK_CONTENT", string(task.Content))
  envBuilder.Add("TASK_ID", string(task.TaskIdentifier))
  envBuilder.Add("KAFKA_BROKERS", s.kafkaBrokers)
  envBuilder.Add("BRANCH", s.branch)
  for key, value := range config.Env {
      envBuilder.Add(key, value)
  }
  ```
- `task/executor/pkg/spawner/job_spawner_test.go` — existing Ginkgo tests. The `It("creates a job with correct name and env vars", ...)` block at ~line 61 already asserts `TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`, `GEMINI_API_KEY` via `envMap`. Extend this test (do NOT add a new parallel one).
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter.Phase()` returns `*domain.TaskPhase` (pointer). **Nil when absent.** The executor must dereference safely:
  ```go
  phase := ""
  if p := task.Frontmatter.Phase(); p != nil {
      phase = string(*p)
  }
  envBuilder.Add("PHASE", phase)
  ```
- `github.com/bborbe/vault-cli/pkg/domain.TaskPhase` — `TaskPhase` string type (accessible via `go doc github.com/bborbe/vault-cli/pkg/domain.TaskPhase`) with constants `TaskPhasePlanning`, `TaskPhaseInProgress`, `TaskPhaseAIReview`, `TaskPhaseHumanReview`, `TaskPhaseDone`, `TaskPhaseTodo`. No changes needed — reference only.

**Why empty string when phase is absent:**
- The env-builder adds the variable unconditionally, even empty, so the agent container's env surface is stable (predictable for test assertions, debugging).
- An agent that checks `os.Getenv("PHASE") == ""` can treat this as "task has no phase; run single-phase logic" without distinguishing absent-var from empty-string.
- Matches the pattern used for `KAFKA_BROKERS` (always injected, may be empty).

**Design rationale (outside container — for the human reviewer, not the agent):**
This change implements the "PHASE env var injection" from the Agent Phase Dispatch Guide (§Required Infrastructure Changes #1). The agent running this prompt does not need to read that guide — the requirements below are self-contained.

Grep before editing (all paths repo-relative, container-safe):
```bash
grep -n "envBuilder.Add" task/executor/pkg/spawner/job_spawner.go
grep -n "envMap\[" task/executor/pkg/spawner/job_spawner_test.go | head -20
```
</context>

<requirements>

1. **Add `PHASE` injection in `task/executor/pkg/spawner/job_spawner.go`**

   In `SpawnJob`, immediately after the existing `envBuilder.Add("BRANCH", s.branch)` line, insert:

   ```go
   phase := ""
   if p := task.Frontmatter.Phase(); p != nil {
       phase = string(*p)
   }
   envBuilder.Add("PHASE", phase)
   ```

   Do NOT reorder the existing `Add` calls. The `PHASE` line goes AFTER `BRANCH` and BEFORE the `for key, value := range config.Env` loop — so per-agent Env from Config CRD can override `PHASE` if absolutely needed (unlikely, but consistent with the existing precedence).

   No new imports required — `task.Frontmatter.Phase()` and the `domain.TaskPhase` type are already reachable via the existing `lib "github.com/bborbe/agent/lib"` import.

2. **Extend the env-var test in `task/executor/pkg/spawner/job_spawner_test.go`**

   Find the `It("creates a job with correct name and env vars", ...)` block (~line 61). The test builds a task and config, calls `SpawnJob`, reads back the Job's container env into `envMap`, and asserts on known keys.

   Modify the `Task` frontmatter to include a phase:

   ```go
   task := lib.Task{
       TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
       Frontmatter: lib.TaskFrontmatter{
           "assignee": "claude",
           "phase":    "planning",
       },
       Content: lib.TaskContent("do the work"),
   }
   ```

   Add an assertion after the existing env checks (after the `GEMINI_API_KEY` line at ~107):

   ```go
   Expect(envMap["PHASE"]).To(Equal("planning"))
   ```

3. **Add a second test case: phase absent → empty string**

   Immediately below the amended `It(...)` block, add a new `It` case:

   ```go
   It("injects empty PHASE when task frontmatter has no phase", func() {
       task := lib.Task{
           TaskIdentifier: lib.TaskIdentifier("no-phase-task"),
           Frontmatter: lib.TaskFrontmatter{
               "assignee": "claude",
           },
           Content: lib.TaskContent("do the work"),
       }
       config := pkg.AgentConfiguration{
           Assignee: "claude",
           Image:    "my-image:latest",
           Env:      map[string]string{},
       }
       _, err := jobSpawner.SpawnJob(ctx, task, config)
       Expect(err).To(BeNil())

       jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
       Expect(err).To(BeNil())
       Expect(jobs.Items).To(HaveLen(1))

       envMap := make(map[string]string)
       for _, e := range jobs.Items[0].Spec.Template.Spec.Containers[0].Env {
           envMap[e.Name] = e.Value
       }
       Expect(envMap).To(HaveKey("PHASE"))
       Expect(envMap["PHASE"]).To(Equal(""))
   })
   ```

   `HaveKey` + `Equal("")` asserts the variable is present AND empty — distinguishes from the "not added at all" case.

4. **Do NOT change `lib/agent_task-frontmatter.go`** — `Phase()` already returns `*domain.TaskPhase` (nil when absent). Correct signature.

5. **Do NOT change `result_publisher.go`, `job_watcher.go`, `task_event_handler.go`, any controller code, any Config CRD, or any Kafka schema**. This is a one-file production change plus test.

6. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased` (create the section if absent; do not touch released sections):

   ```markdown
   - feat(executor): inject `PHASE` env var into spawned agent Jobs, sourced from task frontmatter `phase` field (empty string when absent); enables per-phase dispatch in phase-aware agents without parsing `TASK_CONTENT` frontmatter
   ```

7. **Verification commands**

   Must exit 0:
   ```bash
   cd task/executor && make precommit
   ```

   Spot checks:
   ```bash
   grep -n 'envBuilder.Add("PHASE"' \
     task/executor/pkg/spawner/job_spawner.go
   ```
   Must show exactly one match.

   ```bash
   grep -n 'envMap\["PHASE"\]' \
     task/executor/pkg/spawner/job_spawner_test.go
   ```
   Must show at least two matches (populated-phase test and empty-phase test).

</requirements>

<constraints>
- Only edit `task/executor/pkg/spawner/job_spawner.go`, `task/executor/pkg/spawner/job_spawner_test.go`, and `CHANGELOG.md`. No other production files.
- `lib.TaskFrontmatter.Phase()` returns `*domain.TaskPhase` (pointer, nil when absent). Dereference with a nil check — do NOT call `string(task.Frontmatter.Phase())` (won't compile).
- Set `PHASE` to empty string when `phase` is absent — never skip adding the variable. Consumers rely on its presence.
- Do NOT rename or reorder existing `envBuilder.Add` calls. Insert `PHASE` between `BRANCH` and the per-agent env loop.
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this prompt introduces none).
- Ginkgo v2 only. External test package (`package spawner_test`) — follow the existing file's package declaration.
- All existing tests must pass after the change.
- Do NOT commit — dark-factory handles git.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify the injection is in place:
```bash
grep -nA1 'envBuilder.Add("BRANCH"' \
  task/executor/pkg/spawner/job_spawner.go
```
Must show the `BRANCH` line followed by the new `phase := "" / if p := ... / envBuilder.Add("PHASE", phase)` block within the next ~5 lines.

Verify the test assertions:
```bash
grep -nB1 -A1 'envMap\["PHASE"\]' \
  task/executor/pkg/spawner/job_spawner_test.go
```
Must show at least two `envMap["PHASE"]` assertions: one `Equal("planning")` and one `Equal("")`.

Run the focused tests:
```bash
cd task/executor && go test -v ./pkg/spawner/...
```
Must exit 0. Output must include PASS lines for both the amended "creates a job with correct name and env vars" test and the new "injects empty PHASE when task frontmatter has no phase" test.

Run full precommit:
```bash
cd task/executor && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "PHASE env var\|phase.*dispatch" \
  CHANGELOG.md
```
Must show the Unreleased entry.
</verification>
