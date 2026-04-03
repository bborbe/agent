---
status: draft
created: "2026-04-03T17:00:00Z"
---

<summary>
- Gemini API key stored as Kubernetes secret via teamvault
- Executor deployment receives the key from the secret
- Executor passes the key through to all spawned agent jobs
- Agent jobs (like backtest) can now access the Gemini API
- Existing environment variables passed to jobs are unchanged
- Gemini API key configured for dev and prod environments
</summary>

<objective>
Pass GEMINI_API_KEY from the task/executor's K8s Secret through to Jobs it spawns, so agent-backtest (and future agents needing Gemini) can parse task content. Currently agent-backtest fails with "Required field empty, define parameter gemini-api-key or define env GEMINI_API_KEY".
</objective>

<context>
Read CLAUDE.md for project conventions.

**Problem:** agent-backtest requires GEMINI_API_KEY but task/executor only passes TASK_CONTENT, TASK_ID, KAFKA_BROKERS, and BRANCH to spawned Jobs. The key needs to flow: K8s Secret → executor Deployment → executor code → spawned Job env.

**Current flow:**
1. `agent-task-executor-secret.yaml` has only `sentry-dsn`
2. `agent-task-executor-deploy.yaml` reads SENTRY_DSN from secret
3. `main.go` has no GeminiAPIKey field
4. `factory.go` passes kafkaBrokers and branch to JobSpawner
5. `job_spawner.go` injects 4 env vars into spawned containers

Files to read before making changes:
- `task/executor/k8s/agent-task-executor-secret.yaml` — K8s Secret
- `task/executor/k8s/agent-task-executor-deploy.yaml` — Deployment
- `task/executor/main.go` — application struct and wiring
- `task/executor/pkg/factory/factory.go` — factory wiring
- `task/executor/pkg/spawner/job_spawner.go` — Job creation with env vars
- `task/executor/pkg/spawner/job_spawner_test.go` — existing tests
</context>

<requirements>
1. **Add gemini-api-key to K8s Secret:**
   In `task/executor/k8s/agent-task-executor-secret.yaml`, add:
   ```yaml
   gemini-api-key: '{{ "GEMINI_API_KEY" | env | teamvaultUrl | base64 }}'
   ```

2. **Add GEMINI_API_KEY env var to Deployment:**
   In `task/executor/k8s/agent-task-executor-deploy.yaml`, add under `env:`:
   ```yaml
   - name: GEMINI_API_KEY
     valueFrom:
       secretKeyRef:
         key: gemini-api-key
         name: agent-task-executor
   ```

3. **Add GeminiAPIKey to application struct and wire through:**
   In `task/executor/main.go`:
   ```go
   GeminiAPIKey string `required:"true" arg:"gemini-api-key" env:"GEMINI_API_KEY" usage:"Gemini API key forwarded to spawned agents" display:"length"`
   ```
   Update the `CreateConsumer` call:
   ```go
   // old
   consumer := factory.CreateConsumer(saramaClient, a.Branch, kubeClient, a.Namespace, a.KafkaBrokers, taggedImages, log.DefaultSamplerFactory)
   // new
   consumer := factory.CreateConsumer(saramaClient, a.Branch, kubeClient, a.Namespace, a.KafkaBrokers, taggedImages, log.DefaultSamplerFactory, a.GeminiAPIKey)
   ```

4. **Update factory to accept and forward geminiAPIKey:**
   In `task/executor/pkg/factory/factory.go`, add `geminiAPIKey string` parameter to `CreateConsumer` and pass it to `spawner.NewJobSpawner`:
   ```go
   // old
   jobSpawner := spawner.NewJobSpawner(kubeClient, namespace, kafkaBrokers, string(branch))
   // new
   jobSpawner := spawner.NewJobSpawner(kubeClient, namespace, kafkaBrokers, string(branch), geminiAPIKey)
   ```

5. **Update JobSpawner to accept and inject geminiAPIKey:**
   In `task/executor/pkg/spawner/job_spawner.go`:
   - Add `geminiAPIKey string` field to `jobSpawner` struct
   - Add `geminiAPIKey string` parameter to `NewJobSpawner`
   - Add `{Name: "GEMINI_API_KEY", Value: s.geminiAPIKey}` to the container env vars in `SpawnJob`

6. **Add teamvault key to env files:**
   Add `export GEMINI_API_KEY=Qqap6L` to both `dev.env` and `prod.env` (same pattern as SENTRY_DSN_KEY).

7. **Update tests:**
   - Update `NewJobSpawner` calls in tests to include the new parameter, e.g. `spawner.NewJobSpawner(fakeClient, "test-ns", "kafka:9092", "develop", "test-gemini-key")`
   - Add assertion: `Expect(envMap["GEMINI_API_KEY"]).To(Equal("test-gemini-key"))`

8. **Run tests and precommit:**
   ```bash
   cd task/executor && make test
   cd task/executor && make precommit
   ```
</requirements>

<constraints>
- Do NOT change the JobSpawner interface — same `SpawnJob(ctx, task, image) error` signature
- Do NOT remove any existing env vars
- Do NOT change job naming logic or already-exists handling
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Run tests:

```bash
cd task/executor && make test
```
Must pass with exit code 0.

Verify GEMINI_API_KEY in spawner:

```bash
grep -n "GEMINI_API_KEY" task/executor/pkg/spawner/job_spawner.go
```
Must show the env var being added.

Verify secret yaml:

```bash
grep -n "gemini-api-key" task/executor/k8s/agent-task-executor-secret.yaml
```
Must show the secret key.

Verify deploy yaml:

```bash
grep -n "GEMINI_API_KEY" task/executor/k8s/agent-task-executor-deploy.yaml
```
Must show the env var reference.

Verify env files:

```bash
grep -n "GEMINI_API_KEY" dev.env prod.env
```
Must show the teamvault key in both files.

Run precommit:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.
</verification>
