---
status: inbox
created: "2026-04-03T16:30:00Z"
---

<summary>
- JobSpawner uses bborbe/k8s builders (JobBuilder, PodSpecBuilder, ContainerBuilder, EnvBuilder) instead of hand-crafted structs
- Completed/failed jobs auto-delete after 10 minutes via TTLSecondsAfterFinished
- Jobs get proper labels (app=agent, component=task-identifier) for monitoring
- ImagePullSecrets configured via PodSpecBuilder instead of hardcoded LocalObjectReference
- EnvBuilder replaces manual EnvVar slice construction
- Builder validation catches misconfiguration before K8s API call
</summary>

<objective>
Replace the hand-built batchv1.Job struct in JobSpawner with the fluent bborbe/k8s builder pattern. This adds TTL-based auto-cleanup, labels, and validation while reducing code and matching the established builder pattern used across other bborbe services.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Current state:** `task/executor/pkg/spawner/job_spawner.go` constructs a `batchv1.Job` manually (~30 lines of struct literals). Missing: TTL cleanup, labels, TypeMeta, CompletionMode, PodReplacementPolicy. ImagePullSecrets was recently added as a hardcoded fix.

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

**Reference:** See builder source at `~/Documents/workspaces/k8s/k8s_job-builder.go` and tests at `~/Documents/workspaces/k8s/k8s_job-builder_test.go`.

Files to read before making changes:
- `task/executor/pkg/spawner/job_spawner.go` — current implementation
- `task/executor/pkg/spawner/job_spawner_test.go` — current tests
- `task/executor/pkg/factory/factory.go` — where spawner is wired
- `~/Documents/workspaces/k8s/k8s_job-builder.go` — JobBuilder interface
- `~/Documents/workspaces/k8s/k8s_podspec-builder.go` — PodSpecBuilder interface
- `~/Documents/workspaces/k8s/k8s_container-builder.go` — ContainerBuilder interface
- `~/Documents/workspaces/k8s/k8s_env-builder.go` — EnvBuilder interface
- `~/Documents/workspaces/k8s/k8s_objectmeta-builder.go` — ObjectMetaBuilder interface
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

       containerBuilder := k8s.NewContainerBuilder()
       containerBuilder.SetName("agent")
       containerBuilder.SetImage(image)
       containerBuilder.SetEnvBuilder(envBuilder)

       containersBuilder := k8s.NewContainersBuilder()
       containersBuilder.SetContainerBuilders([]k8s.ContainerBuilder{containerBuilder})

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
   - Same env vars: TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH
   - Same job naming: `agent-{first-8-chars}`
   - Same already-exists handling (log + return nil)

4. **New behavior from builder defaults:**
   - `TTLSecondsAfterFinished: 600` — auto-cleanup after 10 min
   - `Labels: {app: agent, component: <task-identifier>}` on pod template
   - `TypeMeta: {Kind: Job, APIVersion: batch/v1}` auto-set
   - `CompletionMode: NonIndexed`
   - `PodReplacementPolicy: TerminatingOrFailed`

5. **Update tests:**
   - Verify TTLSecondsAfterFinished is set on created jobs
   - Verify labels are set on pod template
   - Verify ImagePullSecrets is set
   - Existing test assertions (env vars, image, namespace) must still pass

6. **Run tests and precommit:**
   ```bash
   cd task/executor && make test
   cd task/executor && make precommit
   ```
</requirements>

<constraints>
- Do NOT change the JobSpawner interface — same `SpawnJob(ctx, task, image) error` signature
- Do NOT change job naming logic (`jobNameFromTask`)
- Do NOT change already-exists handling
- Do NOT remove any existing env vars
- Use `github.com/bborbe/k8s` builders, not hand-built structs
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT update CHANGELOG.md
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

Run precommit:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.
</verification>
