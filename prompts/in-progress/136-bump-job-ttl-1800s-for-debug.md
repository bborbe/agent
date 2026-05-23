---
status: approved
created: "2026-05-23T22:18:11Z"
queued: "2026-05-23T22:18:11Z"
---

<summary>
- Task-executor spawns Kubernetes Jobs that inherit `bborbe/k8s` library's default `TTLSecondsAfterFinished=600` (10 min) — completed pods + logs disappear before operator can `kubectl logs` for diagnosis
- Override the default to 1800s (30 min) via explicit `SetTTLSecondsAfterFinished(1800)` call on the JobBuilder before `.Build()`
- Update the existing assertion `int32(600)` → `int32(1800)` in the spawner test
- Single-file code change in `task/executor/pkg/spawner/job_spawner.go` + one-line test fix
</summary>

<objective>
Every Job spawned by the agent task-executor must carry `spec.ttlSecondsAfterFinished == 1800`. Operators can `kubectl logs <pod>` up to 30 min after Job completion (vs. 10 min today). No new env var, no per-CR config knob, no library change — just an explicit override in the spawner.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Why now:** discovered 2026-05-23 while diagnosing `bborbe/trading#133` pr-reviewer routing to `human_review`. By the time the unexpected vault state surfaced and the operator tried `kubectl logs`, the pod was already gone (10-min TTL). 30 min gives ~2× headroom on observed operator response time.

**Files to read before implementing:**
- `task/executor/pkg/spawner/job_spawner.go` — the Job construction site; uses `k8s.NewJobBuilder()` at ~line 105. Identify the builder variable name; find where it's finalized (the call before `Build()` or `.Build()` equivalent).
- `task/executor/pkg/spawner/job_spawner_test.go` lines 88-89 — current assertion: `Expect(*job.Spec.TTLSecondsAfterFinished).To(Equal(int32(600)))`.

**Library API (already in vendored deps):**
- `github.com/bborbe/k8s@v1.14.2/k8s_job-builder.go:45,133` — `SetTTLSecondsAfterFinished(ttlSecondsAfterFinished int32) JobBuilder`
- Default `600` lives at `k8s_job-builder.go:56`: `ttlSecondsAfterFinished: collection.Ptr(int32(600))`. We override per-caller; do NOT modify the library.

**Why a named constant:** future tuning should be a one-line edit at a single source of truth, not a search-and-replace.
</context>

<requirements>

1. **Add a package-level constant** at the top of `task/executor/pkg/spawner/job_spawner.go` (or wherever existing package constants live):

   ```go
   // jobTTLSecondsAfterFinished controls how long completed Job pods survive
   // before Kubernetes' TTL controller garbage-collects them. 30 min gives
   // operators headroom to fetch logs after noticing an unexpected vault state.
   const jobTTLSecondsAfterFinished int32 = 1800
   ```

2. **Call the setter on the JobBuilder** before `.Build()`. The builder variable is created via `k8s.NewJobBuilder(...)` around line 105 of `job_spawner.go`. Identify the variable name (likely `jobBuilder` or similar), then:

   ```go
   jobBuilder.SetTTLSecondsAfterFinished(jobTTLSecondsAfterFinished)
   ```

   Place the call immediately after `SetBackoffLimit(0)` on line ~108 of `job_spawner.go`, before `SetApp(...)`. The other setter calls there are the canonical insertion point.

3. **Update the test** at `task/executor/pkg/spawner/job_spawner_test.go:89`:

   ```go
   // OLD:
   Expect(*job.Spec.TTLSecondsAfterFinished).To(Equal(int32(600)))
   // NEW:
   Expect(*job.Spec.TTLSecondsAfterFinished).To(Equal(int32(1800)))
   ```

   Do NOT loosen the `NotTo(BeNil())` check on line 88.

4. **Run `make precommit`** in `task/executor/`:

   ```bash
   cd task/executor && make precommit
   ```

   Must exit 0. Fix any failures.

5. **Add a CHANGELOG entry** in project root `CHANGELOG.md`. The project does not currently have a `## Unreleased` section — the top-of-file section is `## v0.62.24`. Insert a new `## Unreleased` section **immediately above `## v0.62.24`** containing:

   ```markdown
   ## Unreleased

   - chore(task/executor): bump spawned-Job `TTLSecondsAfterFinished` 600s → 1800s; completed pods + logs stay queryable for 30 min instead of 10, giving operators headroom for live debug
   ```

</requirements>

<constraints>
- Single-file Go change: only `task/executor/pkg/spawner/job_spawner.go`
- Single-line test change: only `task/executor/pkg/spawner/job_spawner_test.go:89`
- One CHANGELOG line under `## Unreleased`
- No new env var, no Config CR field, no library bump — just the constant + setter call
- The literal `1800` MUST appear exactly once in the spawner source (as the named constant)
- The literal `600` MUST NOT appear anywhere in `task/executor/pkg/spawner/` after the change
- Do NOT commit — dark-factory handles git
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf` (n/a here but stated for completeness)
</constraints>

<verification>
```bash
# AC1: setter is wired with the new value
grep -nE 'SetTTLSecondsAfterFinished\(jobTTLSecondsAfterFinished\)' task/executor/pkg/spawner/job_spawner.go
# Expected: ≥1 match

# AC2: named constant exists with value 1800
grep -nE 'jobTTLSecondsAfterFinished\s+int32\s*=\s*1800' task/executor/pkg/spawner/job_spawner.go
# Expected: exactly 1 match

# AC3: test updated to assert 1800
grep -nE 'Equal\(int32\(1800\)\)' task/executor/pkg/spawner/job_spawner_test.go
# Expected: ≥1 match

# AC4: no stale 600s in spawner pkg
grep -rnE 'int32\(600\)' task/executor/pkg/spawner/
# Expected: 0 matches

# AC5: precommit green
cd task/executor && make precommit
# Expected: exit 0

# AC6: CHANGELOG entry under Unreleased
grep -A5 '## Unreleased' CHANGELOG.md | grep -q 'TTLSecondsAfterFinished'
# Expected: match
```
</verification>
