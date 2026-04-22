---
status: completed
spec: [013-agent-concurrency-via-priority-class]
summary: Wired PriorityClassName from AgentConfiguration onto spawned Job PodTemplates with guard, added two Ginkgo tests covering set and unset cases, created PriorityClass and ResourceQuota manifests for dev/prod, updated agent-claude Config CR with priorityClassName, and updated CHANGELOG.md
container: agent-063-spec-013-executor-spawner-tests-manifests
dark-factory-version: v0.132.0
created: "2026-04-22T00:00:00Z"
queued: "2026-04-22T05:25:31Z"
started: "2026-04-22T05:31:56Z"
completed: "2026-04-22T05:34:43Z"
branch: dark-factory/agent-concurrency-via-priority-class
---

<summary>
- Job spawner stamps `job.Spec.Template.Spec.PriorityClassName` from `config.PriorityClassName` when the field is non-empty
- When `config.PriorityClassName` is empty the field is omitted from the PodTemplate entirely (zero value, pre-spec behavior)
- Two new Ginkgo tests in `job_spawner_test.go` cover both cases (set and unset)
- `agent/claude/k8s/priorityclass.yaml` created: cluster-scoped PriorityClass named `agent-claude`, value 500, `preemptionPolicy: Never`
- `agent/claude/k8s/resource-quota-dev.yaml` created: namespace-scoped ResourceQuota in `dev`, `pods: "1"`, scoped to PriorityClass `agent-claude`
- `agent/claude/k8s/resource-quota-prod.yaml` created: same shape in `prod` namespace
- `agent/claude/k8s/agent-claude.yaml` (existing Config CR) updated to add `spec.priorityClassName: agent-claude`
- `CHANGELOG.md` at repo root updated with `## Unreleased` entry
- `cd task/executor && make precommit` passes
</summary>

<objective>
Wire `AgentConfiguration.PriorityClassName` (added in prompt 1) into the K8s Job PodTemplate inside the job spawner. When the executor creates a Job for an agent whose Config has `spec.priorityClassName` set, that value is copied onto `job.Spec.Template.Spec.PriorityClassName`. This is the enforcement hook — K8s then matches the pod against the `ResourceQuota` scoped to that PriorityClass. Also create the four-file K8s bundle for `agent-claude` so the production agent immediately benefits from a quota of one concurrent pod.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, counterfeiter mocks, external test packages

**This prompt depends on prompt 1 being complete.** `AgentConfiguration.PriorityClassName` must exist before editing the spawner.

Verify prompt 1 is done:
```bash
grep -n "PriorityClassName" task/executor/pkg/agent_configuration.go
```
If the field is absent, stop — prompt 1 has not been applied.

**Key files to read before editing:**

- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob` function; find where `job.Spec.Template.Spec` is assembled and add the `PriorityClassName` assignment
- `task/executor/pkg/spawner/job_spawner_test.go` — existing test structure; add two `It` blocks following the same style
- `agent/claude/k8s/agent-claude.yaml` — existing Config CR; add `spec.priorityClassName: agent-claude`
- `CHANGELOG.md` — top-level file; check for existing `## Unreleased` before writing
</context>

<requirements>

1. **Update `task/executor/pkg/spawner/job_spawner.go` — stamp priorityClassName on the Job PodTemplate**

   In `SpawnJob`, after the `job` variable is built (after `jobBuilder.Build(ctx)` or equivalent final builder call), add:
   ```go
   if config.PriorityClassName != "" {
       job.Spec.Template.Spec.PriorityClassName = config.PriorityClassName
   }
   ```

   Place this assignment immediately before `return jobName, nil` (or before the `CreateJob` / `k8sClient.BatchV1().Jobs(namespace).Create(...)` call — after the full job is assembled). Read the file to find the exact location and variable names used.

   No other changes to `SpawnJob` are needed. The assignment is deliberately outside any builder method — it is a direct patch on the assembled `*batchv1.Job`.

2. **Add two Ginkgo tests to `task/executor/pkg/spawner/job_spawner_test.go`**

   Read the existing test file to understand the setup (`BeforeEach`, helper constructors, fake clients). Match the style exactly.

   Add two `It` blocks in the existing `Describe` / `Context` block that covers `SpawnJob`:

   a. **priorityClassName is stamped when config has it set:**
   ```go
   It("stamps priorityClassName on the spawned Job when config has it set", func() {
       config := pkg.AgentConfiguration{
           Assignee:          "claude-agent",
           Image:             "example/image:latest",
           PriorityClassName: "agent-claude",
       }
       task := lib.Task{
           TaskIdentifier: lib.TaskIdentifier("test-task-uuid-1234"),
           Frontmatter: lib.TaskFrontmatter{
               "status":   "in_progress",
               "phase":    "ai_review",
               "assignee": "claude-agent",
               "stage":    "prod",
           },
       }
       jobName, err := spawner.SpawnJob(ctx, task, config)
       Expect(err).To(BeNil())
       Expect(jobName).NotTo(BeEmpty())

       // Verify the Job was created with the correct priorityClassName
       jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
       Expect(err).To(BeNil())
       Expect(jobs.Items).To(HaveLen(1))
       Expect(jobs.Items[0].Spec.Template.Spec.PriorityClassName).To(Equal("agent-claude"))
   })
   ```

   b. **priorityClassName is omitted when config leaves it unset:**
   ```go
   It("omits priorityClassName from the spawned Job when config has none", func() {
       config := pkg.AgentConfiguration{
           Assignee: "claude-agent",
           Image:    "example/image:latest",
           // PriorityClassName intentionally not set
       }
       task := lib.Task{
           TaskIdentifier: lib.TaskIdentifier("test-task-uuid-5678"),
           Frontmatter: lib.TaskFrontmatter{
               "status":   "in_progress",
               "phase":    "ai_review",
               "assignee": "claude-agent",
               "stage":    "prod",
           },
       }
       jobName, err := spawner.SpawnJob(ctx, task, config)
       Expect(err).To(BeNil())
       Expect(jobName).NotTo(BeEmpty())

       jobs, err := fakeClient.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
       Expect(err).To(BeNil())
       Expect(jobs.Items).To(HaveLen(1))
       Expect(jobs.Items[0].Spec.Template.Spec.PriorityClassName).To(BeEmpty())
   })
   ```

   Adjust variable names (`spawner`, `fakeClient`, `namespace`, `ctx`) to match the actual names used in the existing test file. If the existing tests use a different pattern for listing created jobs (e.g., a `fakeKubernetesClient`), follow the same approach.

3. **Create `agent/claude/k8s/priorityclass.yaml`**

   ```yaml
   apiVersion: scheduling.k8s.io/v1
   kind: PriorityClass
   metadata:
     name: agent-claude
   value: 500
   globalDefault: false
   preemptionPolicy: Never
   description: "Agent claude — namespace-local concurrency via matching ResourceQuota. Never preempts."
   ```

4. **Create `agent/claude/k8s/resource-quota-dev.yaml`**

   ```yaml
   apiVersion: v1
   kind: ResourceQuota
   metadata:
     name: agent-claude
     namespace: dev
   spec:
     hard:
       pods: "1"
     scopeSelector:
       matchExpressions:
         - scopeName: PriorityClass
           operator: In
           values: ["agent-claude"]
   ```

5. **Create `agent/claude/k8s/resource-quota-prod.yaml`**

   ```yaml
   apiVersion: v1
   kind: ResourceQuota
   metadata:
     name: agent-claude
     namespace: prod
   spec:
     hard:
       pods: "1"
     scopeSelector:
       matchExpressions:
         - scopeName: PriorityClass
           operator: In
           values: ["agent-claude"]
   ```

6. **Update `agent/claude/k8s/agent-claude.yaml` — add `spec.priorityClassName`**

   Read the file first. In the `spec:` block of the Config CR, add:
   ```yaml
   priorityClassName: agent-claude
   ```
   Place it after `assignee:` and before other spec fields. The exact position within spec is not important as long as indentation is consistent with the rest of the file.

7. **Update `CHANGELOG.md` at repo root**

   First check for existing `## Unreleased`:
   ```bash
   grep -n "^## Unreleased" CHANGELOG.md | head -3
   ```
   If `## Unreleased` already exists, APPEND the bullet to it. Otherwise INSERT a new section immediately above the first `## v` heading:

   ```markdown
   ## Unreleased

   - feat: priorityClassName field on Config CRD enables K8s-native concurrency cap via ResourceQuota; executor stamps value onto spawned Job PodTemplates; agent-claude bundle includes PriorityClass and per-env ResourceQuota manifests
   ```

8. **Run tests iteratively**

   After implementing step 1 + 2:
   ```bash
   cd task/executor && make test
   ```
   Fix any compilation or test failures before proceeding.

</requirements>

<constraints>
- `lib.Task` schema and `agent-task-v1-event` topic are unchanged — do NOT touch `lib/`
- Existing idempotency behaviour (`current_job` label guard described in `docs/task-flow-and-failure-semantics.md`) is untouched
- `retry_count` semantics from spec 011 are preserved — do NOT add new application-level retry paths
- Task controller is unaware of quota — do NOT touch `task/controller/`
- Do NOT add or modify any application-level gate, queue, or Kafka defer logic — K8s owns the limiting primitive
- Every PriorityClass must have `preemptionPolicy: Never` — agents must not evict other workloads
- `priorityClassName` assignment on the Job must be guarded by `!= ""` — unset Config must produce a Job with no `priorityClassName`
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- Use `github.com/bborbe/errors` for any error wrapping — never `fmt.Errorf`
- `cd task/executor && make precommit` must exit 0
</constraints>

<verification>
Verify spawner stamps the field:
```bash
grep -n "PriorityClassName" task/executor/pkg/spawner/job_spawner.go
```
Must show the guarded assignment: `if config.PriorityClassName != ""`.

Verify new tests exist:
```bash
grep -n "stamps priorityClassName\|omits priorityClassName" task/executor/pkg/spawner/job_spawner_test.go
```
Must show both test descriptions.

Verify K8s manifests exist:
```bash
ls agent/claude/k8s/priorityclass.yaml agent/claude/k8s/resource-quota-dev.yaml agent/claude/k8s/resource-quota-prod.yaml
```
All three must exist.

Verify agent-claude Config CR has priorityClassName:
```bash
grep "priorityClassName" agent/claude/k8s/agent-claude.yaml
```
Must show `priorityClassName: agent-claude`.

Verify preemptionPolicy is Never:
```bash
grep "preemptionPolicy" agent/claude/k8s/priorityclass.yaml
```
Must show `preemptionPolicy: Never`.

Run tests:
```bash
cd task/executor && make test
```
Must exit 0.

Run precommit:
```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
