---
status: approved
created: "2026-04-03T16:30:00Z"
queued: "2026-04-03T17:54:49Z"
---

<summary>
- Job spawner uses fluent builder pattern instead of hand-crafted structs
- Completed and failed jobs auto-delete after 10 minutes
- Jobs get proper labels for monitoring
- Image pull secrets configured via builder instead of hardcoded reference
- Environment variable construction uses builder pattern
- Builder validation catches misconfiguration before API call
</summary>

<objective>
Replace the hand-built batchv1.Job struct in JobSpawner with the fluent bborbe/k8s builder pattern. This adds TTL-based auto-cleanup, labels, and validation while reducing code and matching the established builder pattern used across other bborbe services.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Current state:** `task/executor/pkg/spawner/job_spawner.go` constructs a `batchv1.Job` manually (~30 lines of struct literals). Missing: TTL cleanup, labels, TypeMeta, CompletionMode, PodReplacementPolicy.

**Target state:** Use `github.com/bborbe/k8s` builders:
- `k8s.NewJobBuilder()` — job-level config (backoff, TTL, labels, completions)
- `k8s.NewObjectMetaBuilder()` — name, namespace
- `k8s.NewPodSpecBuilder()` — restart policy, image pull secrets
- `k8s.NewContainersBuilder()` + `k8s.NewContainerBuilder()` — image, env
- `k8s.NewEnvBuilder()` — env vars with fluent `.Add("KEY", "val")`

**Builder defaults that help us:**
- `TTLSecondsAfterFinished: 600` — jobs auto-delete after 10 min (currently they accumulate forever!)
- `BackoffLimit: 4` — we override to 0
- `CompletionMode: NonIndexed`, `PodReplacementPolicy: TerminatingOrFailed` — sane defaults
- Validates restart policy (rejects `Always`)

**Builder defaults that change behavior (must address):**
- `ContainerBuilder` sets default resource limits: cpu 50m/20m, memory 50Mi/20Mi — current code has NO resource limits. Override to match current (no limits) or accept the defaults.
- `ContainerBuilder.Build()` sets `ImagePullPolicy: PullAlways` — current code doesn't set this. Acceptable since deploy yaml already uses `imagePullPolicy: Always`.

**Important type signatures (differs from naive usage):**
- `ContainerBuilder.SetName()` takes `k8s.Name`, not `string` — use `k8s.Name("agent")`
- `ContainersBuilder.SetContainerBuilders()` takes `[]k8s.HasBuildContainer`, not `[]k8s.ContainerBuilder`
- `JobBuilder.SetPodSpecBuilder()` takes `k8s.HasBuildPodSpec`
- `JobBuilder.SetObjectMetaBuild()` takes `k8s.HasBuildObjectMeta`
- `PodSpecBuilder.SetContainersBuilder()` takes `k8s.HasBuildContainers`

**Reference:** Builder source is vendored at `task/executor/vendor/github.com/bborbe/k8s/` after `go mod vendor`. Key files:
- `k8s_job-builder.go` — JobBuilder interface
- `k8s_podspec-builder.go` — PodSpecBuilder interface
- `k8s_container-builder.go` — ContainerBuilder interface (note `HasBuildContainer` type)
- `k8s_containers-builder.go` — ContainersBuilder interface (note `HasBuildContainers` type)
- `k8s_env-builder.go` — EnvBuilder interface
- `k8s_objectmeta-builder.go` — ObjectMetaBuilder interface

Files to read before making changes:
- `task/executor/pkg/spawner/job_spawner.go` — current implementation (5 env vars including GEMINI_API_KEY)
- `task/executor/pkg/spawner/job_spawner_test.go` — current tests
- `task/executor/pkg/factory/factory.go` — where spawner is wired
</context>

<requirements>
1. **Add `github.com/bborbe/k8s` dependency:**
   ```bash
   cd task/executor && go get github.com/bborbe/k8s@latest
   go mod tidy && go mod vendor
   ```

2. **Rewrite `SpawnJob` to use builders:**
   ```go
   func (s *jobSpawner) SpawnJob(ctx context.Context, task lib.Task, image string) error {
       jobName := jobNameFromTask(task.TaskIdentifier)

       envBuilder := k8s.NewEnvBuilder()
       envBuilder.Add("TASK_CONTENT", string(task.Content))
       envBuilder.Add("TASK_ID", string(task.TaskIdentifier))
       envBuilder.Add("KAFKA_BROKERS", s.kafkaBrokers)
       envBuilder.Add("BRANCH", s.branch)
       envBuilder.Add("GEMINI_API_KEY", s.geminiAPIKey)

       containerBuilder := k8s.NewContainerBuilder()
       containerBuilder.SetName(k8s.Name("agent"))
       containerBuilder.SetImage(image)
       containerBuilder.SetEnvBuilder(envBuilder)

       containersBuilder := k8s.NewContainersBuilder()
       containersBuilder.SetContainerBuilders([]k8s.HasBuildContainer{containerBuilder})

       podSpecBuilder := k8s.NewPodSpecBuilder()
       podSpecBuilder.SetContainersBuilder(containersBuilder)
       podSpecBuilder.SetRestartPolicy(corev1.RestartPolicyNever)
       podSpecBuilder.SetImagePullSecrets([]string{"docker"})

       objectMetaBuilder := k8s.NewObjectMetaBuilder()
       objectMetaBuilder.SetName(jobName)
       objectMetaBuilder.SetNamespace(s.namespace)

       jobBuilder := k8s.NewJobBuilder()
       jobBuilder.SetObjectMetaBuild(objectMetaBuilder)
       jobBuilder.SetPodSpecBuilder(podSpecBuilder)
       jobBuilder.SetBackoffLimit(0)
       jobBuilder.SetApp("agent")
       jobBuilder.SetComponent(string(task.TaskIdentifier))

       job, err := jobBuilder.Build(ctx)
       if err != nil {
           return errors.Wrapf(ctx, err, "build job for task %s", task.TaskIdentifier)
       }

       // ... existing create + already-exists logic unchanged
   }
   ```

3. **Keep existing behavior unchanged:**
   - `BackoffLimit: 0` (no retries)
   - `RestartPolicy: Never`
   - `ImagePullSecrets: docker`
   - Same 5 env vars: TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH, GEMINI_API_KEY
   - Same job naming: `agent-{first-8-chars}`
   - Same already-exists handling (log + return nil)
   - Same `geminiAPIKey` field on struct, same constructor signature

4. **New behavior from builder defaults:**
   - `TTLSecondsAfterFinished: 600` — auto-cleanup after 10 min
   - `Labels: {app: agent, component: <task-identifier>}` on pod template
   - `TypeMeta: {Kind: Job, APIVersion: batch/v1}` auto-set
   - `CompletionMode: NonIndexed`
   - `PodReplacementPolicy: TerminatingOrFailed`
   - `ImagePullPolicy: PullAlways` (acceptable, matches executor deploy)
   - Resource limits: cpu 50m/20m, memory 50Mi/20Mi (from ContainerBuilder defaults — acceptable for now)

5. **Update tests:**
   - Verify TTLSecondsAfterFinished is set on created jobs
   - Verify labels are set on pod template
   - Verify ImagePullSecrets is set
   - Verify GEMINI_API_KEY is in env vars
   - Existing test assertions (env vars, image, namespace) must still pass

6. **Run tests and precommit:**
   ```bash
   cd task/executor && make test
   cd task/executor && make precommit
   ```
</requirements>

<constraints>
- Do NOT change the JobSpawner interface — same `SpawnJob(ctx, task, image) error` signature
- Do NOT change the `NewJobSpawner` constructor signature (5 params including geminiAPIKey)
- Do NOT change job naming logic (`jobNameFromTask`)
- Do NOT change already-exists handling
- Do NOT remove any existing env vars (there are 5: TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH, GEMINI_API_KEY)
- Use `github.com/bborbe/k8s` builders, not hand-built structs
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Run tests:

```bash
cd task/executor && make test
```
Must pass with exit code 0.

Verify builder usage:

```bash
grep -n "NewJobBuilder\|NewPodSpecBuilder\|NewContainerBuilder\|NewEnvBuilder" task/executor/pkg/spawner/job_spawner.go
```
Must show all four builder constructors.

Verify TTL is set:

```bash
grep -n "TTLSecondsAfterFinished\|SetTTLSecondsAfterFinished" task/executor/pkg/spawner/job_spawner.go
```
Must show TTL usage (or rely on builder default of 600).

Verify all 5 env vars:

```bash
grep -n "TASK_CONTENT\|TASK_ID\|KAFKA_BROKERS\|BRANCH\|GEMINI_API_KEY" task/executor/pkg/spawner/job_spawner.go
```
Must show all 5 env vars.

Run precommit:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.
</verification>
