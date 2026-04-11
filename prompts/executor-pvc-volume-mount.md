---
status: draft
---

<summary>
- Job spawner mounts an optional persistent volume into agent containers
- Each agent configuration can specify a volume claim and mount path
- Trade-analysis agent uses this for Claude Code OAuth credentials that refresh over time
- Agents without volume config continue to work unchanged
- PVC must be pre-created in the namespace, spawner only references it
</summary>

<objective>
The job spawner supports optional PVC mounts per agent. When an agent configuration
includes a volume claim name and mount path, the spawned K8s Job mounts that PVC
into the container. Agents without volume config are unaffected.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read coding plugin docs for Go patterns: `go-error-wrapping-guide.md`, `go-factory-pattern.md`, `go-testing-guide.md`.

Key files to read before making changes:
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct with Assignee, Image, Env fields; `TaggedConfigurations` deep-copies configs
- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob` builds K8s Job using `k8s.NewPodSpecBuilder`, `k8s.NewContainerBuilder`; currently has no volumes
- `task/executor/main.go` — package-level `agentConfigs` variable with three agents (claude, backtest-agent, trade-analysis-agent)
- `task/executor/pkg/spawner/job_spawner_test.go` — existing tests for SpawnJob

Important facts:
- `k8s.PodSpecBuilder` has `SetVolumes(volumes []corev1.Volume)` method
- `k8s.ContainerBuilder` has `AddVolumeMounts(volumeMounts ...corev1.VolumeMount)` method
- Both are from `github.com/bborbe/k8s` library already imported
- PVC access mode is ReadWriteOnce — only one Job pod at a time (executor already serializes per agent)
- The PVC itself is NOT created by the spawner — it must exist in the namespace (created via k8s manifest)
- `TaggedConfigurations` must also copy the new volume fields (it currently deep-copies Assignee, Image, Env)
</context>

<requirements>

1. **Add volume fields to `AgentConfiguration`**

   In `task/executor/pkg/agent_configuration.go`, add two optional fields:
   ```go
   // VolumeClaim is the name of an existing PVC to mount into the container.
   // Empty means no volume mount.
   VolumeClaim string
   // VolumeMountPath is the container path where the PVC is mounted.
   // Required when VolumeClaim is set.
   VolumeMountPath string
   ```

2. **Update all deep-copy sites to include volume fields**

   There are two places that copy `AgentConfiguration` — both must include the new fields:

   a. `TaggedConfigurations` in `agent_configuration.go` (line 38): add `VolumeClaim` and `VolumeMountPath` to the new `AgentConfiguration`.

   b. Runtime secret loop in `main.go` (line 106-110): add `VolumeClaim: ac.VolumeClaim` and `VolumeMountPath: ac.VolumeMountPath` to the deep-copy.

3. **Mount volume in `SpawnJob` when configured**

   In `task/executor/pkg/spawner/job_spawner.go`, after building the container:

   a. If `config.VolumeClaim` is non-empty:
      - Add a `corev1.VolumeMount` to the container builder:
        - Name: `"agent-data"`
        - MountPath: `config.VolumeMountPath`
      - Add a `corev1.Volume` to the pod spec builder:
        - Name: `"agent-data"`
        - VolumeSource: `PersistentVolumeClaimVolumeSource` with ClaimName `config.VolumeClaim`

   b. If `config.VolumeClaim` is empty: no volumes (current behavior).

4. **Configure trade-analysis agent with PVC**

   In `task/executor/main.go`, add volume fields to the existing trade-analysis-agent config. Do NOT change the existing `Env` map — only add the two new fields:
   ```go
   {
       Assignee:        "trade-analysis-agent",
       Image:           "docker.quant.benjamin-borbe.de:443/agent-trade-analysis",
       Env:             map[string]string{},
       VolumeClaim:     "claude-oauth",
       VolumeMountPath: "/home/claude/.claude",
   }
   ```

   Leave `claude` and `backtest-agent` configs unchanged (no VolumeClaim).

5. **Update tests**

   Add test case in `job_spawner_test.go`:
   - SpawnJob with VolumeClaim set → verify Job has volume and volume mount
   - SpawnJob without VolumeClaim → verify Job has no volumes (existing behavior preserved)

</requirements>

<constraints>
- Do NOT create the PVC resource — only reference it in the Job spec
- Do NOT change the JobSpawner interface signature
- Do NOT change existing agent configs (claude, backtest-agent) — only add fields to trade-analysis-agent
- Volume name in the Job spec is always `"agent-data"` (single volume per job is sufficient)
- If `VolumeClaim` is non-empty and `VolumeMountPath` is empty, `SpawnJob` must return an error
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Run precommit:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.

Verify volume fields in AgentConfiguration:

```bash
grep -n "VolumeClaim\|VolumeMountPath" task/executor/pkg/agent_configuration.go
```
Must show both fields.

Verify volume mount in SpawnJob:

```bash
grep -n "VolumeMount\|VolumeClaim\|agent-data" task/executor/pkg/spawner/job_spawner.go
```
Must show volume handling.

Verify trade-analysis config has PVC:

```bash
grep -A2 "trade-analysis" task/executor/main.go | grep -i "volume\|claude"
```
Must show VolumeClaim and VolumeMountPath.
</verification>
