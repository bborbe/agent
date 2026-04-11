---
status: completed
summary: Added SecretName field to AgentConfiguration, injected envFrom secretRef in SpawnJob via applySecretEnvFrom helper, configured backtest-agent and trade-analysis-agent with their respective secret names, and added two new test cases verifying secret envFrom behavior.
container: agent-042-executor-secret-envfrom
dark-factory-version: v0.108.0-dirty
created: "2026-04-11T13:17:56Z"
queued: "2026-04-11T13:17:56Z"
started: "2026-04-11T13:19:38Z"
completed: "2026-04-11T14:08:19Z"
---

<summary>
- Each agent can reference a Kubernetes secret for environment variables
- Job spawner injects secret as envFrom on the agent container
- Agents without a secret configured continue to work unchanged
- Executor no longer needs to know or forward individual API keys
- Secret name is per-agent, set in the agent configuration
</summary>

<objective>
The job spawner mounts per-agent Kubernetes secrets via envFrom. Each agent
configuration can specify a secret name. The spawner adds envFrom to the Job
container so the agent pod receives secret values as environment variables.
Agents without a secret name are unaffected.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read coding plugin docs for Go patterns: `go-error-wrapping-guide.md`, `go-factory-pattern.md`, `go-testing-guide.md`.

Key files to read before making changes:
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct with Assignee, Image, Env, VolumeClaim, VolumeMountPath; `TaggedConfigurations` deep-copies all fields
- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob` builds K8s Job via builders, then calls `kubeClient.BatchV1().Jobs().Create()`. After `jobBuilder.Build(ctx)` returns `*batchv1.Job`, the job can be modified before Create
- `task/executor/main.go` — package-level `agentConfigs` with three agents; no longer has secret injection logic (removed)
- `task/executor/pkg/spawner/job_spawner_test.go` — existing tests for SpawnJob

Important facts:
- The k8s builder library does NOT have `SetEnvFrom` or `AddEnvFrom` — do NOT try to use it
- Instead, modify the built Job object directly: `job.Spec.Template.Spec.Containers[0].EnvFrom`
- `corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}}}` is the K8s type
- Secret keys must match env var names exactly (e.g., secret key `GEMINI_API_KEY` becomes env var `GEMINI_API_KEY`)
- Each agent's secret is created in the same namespace as the Jobs (managed separately in the trading repo)
- `TaggedConfigurations` and any other deep-copy site must include the new field
</context>

<requirements>

1. **Add `SecretName` field to `AgentConfiguration`**

   In `task/executor/pkg/agent_configuration.go`:
   ```go
   // SecretName is the name of a K8s Secret to mount as envFrom on the container.
   // Empty means no secret is mounted.
   SecretName string
   ```

2. **Update all deep-copy sites to include `SecretName`**

   a. `TaggedConfigurations` in `agent_configuration.go`: add `SecretName: c.SecretName` to the copy.

   b. Verify no other copy sites exist (the runtime secret injection loop was removed).

3. **Add envFrom to Job in `SpawnJob`**

   In `job_spawner.go`, after `job, err := jobBuilder.Build(ctx)` and before `kubeClient.Create()`:

   If `config.SecretName` is non-empty, append to the first container's EnvFrom:
   ```go
   if config.SecretName != "" {
       job.Spec.Template.Spec.Containers[0].EnvFrom = append(
           job.Spec.Template.Spec.Containers[0].EnvFrom,
           corev1.EnvFromSource{
               SecretRef: &corev1.SecretEnvSource{
                   LocalObjectReference: corev1.LocalObjectReference{
                       Name: config.SecretName,
                   },
               },
           },
       )
   }
   ```

4. **Configure agents with secret names**

   In `task/executor/main.go`, update agent configs:
   ```go
   {
       Assignee:   "backtest-agent",
       Image:      "docker.quant.benjamin-borbe.de:443/agent-backtest",
       Env:        map[string]string{},
       SecretName: "agent-backtest",
   },
   {
       Assignee:        "trade-analysis-agent",
       Image:           "docker.quant.benjamin-borbe.de:443/agent-trade-analysis",
       Env:             map[string]string{},
       SecretName:      "agent-trade-analysis",
       VolumeClaim:     "agent-trade-analysis",
       VolumeMountPath: "/home/claude/.claude",
   },
   ```

   Leave `claude` agent without SecretName (no secret needed).

5. **Update tests**

   Add test cases in `job_spawner_test.go`:
   - SpawnJob with SecretName set → verify Job container has envFrom with secretRef
   - SpawnJob without SecretName → verify Job container has no envFrom (existing behavior)

</requirements>

<constraints>
- Do NOT modify the k8s builder library — modify the Job object after Build()
- Do NOT change the JobSpawner interface signature
- Do NOT create K8s Secret resources — only reference them via envFrom
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Run precommit:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.

Verify SecretName in AgentConfiguration:

```bash
grep -n "SecretName" task/executor/pkg/agent_configuration.go
```
Must show the field and deep-copy.

Verify envFrom in SpawnJob:

```bash
grep -n "EnvFrom\|SecretRef\|SecretName" task/executor/pkg/spawner/job_spawner.go
```
Must show envFrom injection.

Verify agent configs have secret names:

```bash
grep -n "SecretName" task/executor/main.go
```
Must show secret names for backtest-agent and trade-analysis-agent.
</verification>
