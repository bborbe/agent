---
status: executing
container: agent-039-executor-agent-configuration
dark-factory-version: v0.108.0-dirty
created: "2026-04-11T10:39:36Z"
queued: "2026-04-11T10:56:16Z"
started: "2026-04-11T10:56:17Z"
---

<summary>
- Each agent type gets its own configuration with image and environment variables
- Agent configurations replace the flat assignee-to-image mapping
- Per-agent secrets replace the shared API key passed to all agents
- Backtest agent receives only the Gemini key, trade-analysis agent receives only the Anthropic key
- New Kubernetes secret entry for the Anthropic API key
- Shared environment variables (task content, Kafka, branch) remain in the job spawner
- Lookup by assignee name routes tasks to the correct agent configuration
- Image tagging with branch name preserved for dev/prod separation
</summary>

<objective>
Enable per-agent environment variables so each agent type receives only the API keys
it needs, instead of sharing a single key across all agents. The executor resolves
assignee to a typed configuration (image + env vars) rather than a flat image string.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Key files to read before making changes:
- `task/executor/main.go` — `assigneeImages` map (line 33), `GeminiAPIKey` field, `taggedImages` construction, `CreateConsumer` call
- `task/executor/pkg/handler/task_event_handler.go` — `assigneeImages map[string]string` field, image lookup by assignee
- `task/executor/pkg/handler/task_event_handler_test.go` — `assigneeImages` construction in BeforeEach, handler creation
- `task/executor/pkg/factory/factory.go` — `CreateConsumer` signature with `assigneeImages` and `geminiAPIKey` params
- `task/executor/pkg/spawner/job_spawner.go` — `geminiAPIKey` field, hardcoded `envBuilder.Add("GEMINI_API_KEY", ...)` in SpawnJob
- `task/executor/pkg/spawner/job_spawner_test.go` — `NewJobSpawner` calls with `geminiAPIKey` param
- `task/executor/pkg/metrics/metrics.go` — existing metric labels

Important facts:
- `base.Branch` from `github.com/bborbe/cqrs/base` is a string type
- Handler already has a `branch` field (from prompt 038)
- `SpawnJob(ctx, task, image)` is the current interface — this changes to `SpawnJob(ctx, task, config)`
- `k8s.EnvBuilder.Add(key, value)` is the established pattern for env vars
- Handler currently does `image, ok := h.assigneeImages[string(task.Frontmatter.Assignee())]`
</context>

<requirements>

1. **Create `task/executor/pkg/agent_configuration.go`**

   New file with these types:

   ```go
   package pkg

   // AgentConfiguration defines the container image and environment for one agent type.
   type AgentConfiguration struct {
       // Assignee is the task frontmatter assignee value that routes to this agent.
       Assignee string
       // Image is the container image base name (without tag). Tag is appended at runtime from branch.
       Image string
       // Env holds per-agent environment variables (e.g. API keys, config).
       // These are merged with shared env vars (TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH)
       // when spawning the K8s Job.
       Env map[string]string
   }

   // AgentConfigurations is a list of agent configurations.
   type AgentConfigurations []AgentConfiguration

   // FindByAssignee returns the configuration for the given assignee name.
   // Returns the config and true if found, zero value and false otherwise.
   func (a AgentConfigurations) FindByAssignee(assignee string) (AgentConfiguration, bool) {
       for _, c := range a {
           if c.Assignee == assignee {
               return c, true
           }
       }
       return AgentConfiguration{}, false
   }

   // TaggedConfigurations returns a new AgentConfigurations with the branch appended
   // to each image as a tag (e.g. "registry/image" + ":" + "dev" → "registry/image:dev").
   func (a AgentConfigurations) TaggedConfigurations(branch string) AgentConfigurations {
       result := make(AgentConfigurations, len(a))
       for i, c := range a {
           result[i] = AgentConfiguration{
               Assignee: c.Assignee,
               Image:    c.Image + ":" + branch,
               Env:      c.Env,
           }
       }
       return result
   }
   ```

2. **Create `task/executor/pkg/agent_configuration_test.go`**

   Ginkgo test file matching existing test style. Test:

   a. `FindByAssignee` — returns config when found, returns false when not found
   b. `TaggedConfigurations` — appends branch as tag to all images, preserves env

3. **Update `task/executor/main.go`**

   a. Replace `assigneeImages` map with `AgentConfigurations`:

   ```go
   var agentConfigs = pkg.AgentConfigurations{
       {
           Assignee: "claude",
           Image:    "docker.quant.benjamin-borbe.de:443/agent-claude",
           Env:      map[string]string{},
       },
       {
           Assignee: "backtest-agent",
           Image:    "docker.quant.benjamin-borbe.de:443/agent-backtest",
           Env:      map[string]string{"GEMINI_API_KEY": ""},
       },
       {
           Assignee: "trade-analysis-agent",
           Image:    "docker.quant.benjamin-borbe.de:443/agent-trade-analysis",
           Env:      map[string]string{"ANTHROPIC_API_KEY": ""},
       },
   }
   ```

   b. Add `AnthropicAPIKey` field to application struct:
   ```go
   AnthropicAPIKey string `required:"false" arg:"anthropic-api-key" env:"ANTHROPIC_API_KEY" usage:"Anthropic API key for Claude-based agents" display:"length"`
   ```

   c. In `Run()`, replace the `taggedImages` construction with runtime env injection + tagging.
   **Important:** Do NOT mutate `agentConfigs` directly — its `Env` maps are shared. Build new configs with fresh maps:
   ```go
   // Build configs with runtime secrets injected (do not mutate package-level agentConfigs)
   secretMap := map[string]string{
       "GEMINI_API_KEY":    a.GeminiAPIKey,
       "ANTHROPIC_API_KEY": a.AnthropicAPIKey,
   }
   configs := make(pkg.AgentConfigurations, len(agentConfigs))
   for i, ac := range agentConfigs {
       env := make(map[string]string, len(ac.Env))
       for k, v := range ac.Env {
           if secret, ok := secretMap[k]; ok && v == "" {
               env[k] = secret
           } else {
               env[k] = v
           }
       }
       configs[i] = pkg.AgentConfiguration{
           Assignee: ac.Assignee,
           Image:    ac.Image,
           Env:      env,
       }
   }
   taggedConfigs := configs.TaggedConfigurations(string(a.Branch))
   ```

   d. Update `CreateConsumer` call — replace `taggedImages` and `a.GeminiAPIKey` with `taggedConfigs`:
   ```go
   consumer := factory.CreateConsumer(
       saramaClient, a.Branch, kubeClient, a.Namespace,
       a.KafkaBrokers, taggedConfigs, log.DefaultSamplerFactory,
       currentDateTimeGetter,
   )
   ```
   Note: `geminiAPIKey` parameter is removed from `CreateConsumer` — it's now in `taggedConfigs`.

   e. Add import for the executor pkg package.

4. **Update `task/executor/pkg/factory/factory.go`**

   a. Change `CreateConsumer` signature — replace `assigneeImages map[string]string` and `geminiAPIKey string` with single `agentConfigs pkg.AgentConfigurations`:
   ```go
   func CreateConsumer(
       saramaClient sarama.Client,
       branch base.Branch,
       kubeClient kubernetes.Interface,
       namespace string,
       kafkaBrokers string,
       agentConfigs pkg.AgentConfigurations,
       logSamplerFactory log.SamplerFactory,
       currentDateTimeGetter libtime.CurrentDateTimeGetter,
   ) libkafka.Consumer
   ```

   b. Remove `geminiAPIKey` from `NewJobSpawner` call — spawner no longer needs it.

   c. Pass `agentConfigs` to handler:
   ```go
   taskEventHandler := handler.NewTaskEventHandler(jobSpawner, branch, agentConfigs)
   ```

5. **Update `task/executor/pkg/handler/task_event_handler.go`**

   a. Change `assigneeImages map[string]string` to `agentConfigs pkg.AgentConfigurations` in struct field, constructor param, and constructor body.

   b. Change the assignee lookup in `ConsumeMessage` from:
   ```go
   image, ok := h.assigneeImages[string(task.Frontmatter.Assignee())]
   ```
   to:
   ```go
   config, ok := h.agentConfigs.FindByAssignee(string(task.Frontmatter.Assignee()))
   ```

   c. Change `SpawnJob` call from `SpawnJob(ctx, task, image)` to `SpawnJob(ctx, task, config)`.

   d. Update the spawn log line to use `config.Image` instead of `image`.

6. **Update `task/executor/pkg/spawner/job_spawner.go`**

   a. Change `JobSpawner` interface: `SpawnJob(ctx context.Context, task lib.Task, config pkg.AgentConfiguration) error`

   b. Remove `geminiAPIKey` from `jobSpawner` struct and `NewJobSpawner` constructor.

   c. In `SpawnJob`, change image from parameter to `config.Image`.

   d. Replace the hardcoded `envBuilder.Add("GEMINI_API_KEY", s.geminiAPIKey)` with a loop over `config.Env`:
   ```go
   for key, value := range config.Env {
       envBuilder.Add(key, value)
   }
   ```
   Keep the 4 shared env vars (TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH) before the loop.

7. **Update `task/executor/pkg/spawner/job_spawner_test.go`**

   a. Remove `geminiAPIKey` from `NewJobSpawner` calls.

   b. Update `SpawnJob` calls to pass `pkg.AgentConfiguration{...}` instead of `"my-image:latest"`.

   c. Update env var assertions: instead of checking for hardcoded GEMINI_API_KEY, check that per-config env vars appear in the container spec.

   d. Add test: SpawnJob with config containing two env vars — verify both appear in container.

8. **Update `task/executor/pkg/handler/task_event_handler_test.go`**

   a. Replace `assigneeImages map[string]string` with `agentConfigs pkg.AgentConfigurations`:
   ```go
   agentConfigs = pkg.AgentConfigurations{
       {Assignee: "claude", Image: "my-image:latest", Env: map[string]string{}},
   }
   ```

   b. Update handler construction:
   ```go
   h = handler.NewTaskEventHandler(fakeSpawner, base.Branch("prod"), agentConfigs)
   ```

   c. Update all `localHandler` constructions in stage tests to use `agentConfigs`.

   d. Update `SpawnJobArgsForCall` assertions — second return is now `pkg.AgentConfiguration` not `string`:
   ```go
   _, spawnedTask, config := fakeSpawner.SpawnJobArgsForCall(0)
   Expect(config.Image).To(Equal("my-image:latest"))
   ```

9. **Regenerate mocks**

   The `JobSpawner` interface changed (new `SpawnJob` signature). Run:
   ```bash
   cd task/executor && go generate ./...
   ```

10. **Add ANTHROPIC_API_KEY to K8s secrets and deployment**

    a. In `task/executor/k8s/agent-task-executor-secret.yaml`, add (note: `teamvaultPassword` not `teamvaultUrl`, matching the existing `gemini-api-key` pattern):
    ```yaml
    anthropic-api-key: '{{ "ANTHROPIC_API_KEY_KEY" | env | teamvaultPassword | base64 }}'
    ```

    b. In `task/executor/k8s/agent-task-executor-deploy.yaml`, add env var:
    ```yaml
    - name: ANTHROPIC_API_KEY
      valueFrom:
        secretKeyRef:
          key: anthropic-api-key
          name: agent-task-executor
    ```

    c. Add `export ANTHROPIC_API_KEY_KEY=PLACEHOLDER` to both `dev.env` and `prod.env` (note: `_KEY` suffix matches the existing pattern, e.g. `GEMINI_API_KEY_KEY=Qqap6L`).
    The actual teamvault key ID will be configured separately — use PLACEHOLDER for now.

</requirements>

<constraints>
- Do NOT change the `TaskEventHandler` interface (only the concrete struct and constructor change)
- Do NOT modify `lib/agent_task-frontmatter.go` or any vault-cli types
- Do NOT change Kafka topic construction, job naming, or already-exists handling
- Do NOT rename existing metrics
- Keep shared env vars (TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH) in the spawner — only API keys move to per-agent config
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Follow existing file naming: `agent_configuration.go` (snake_case with prefix)
- All existing tests must still pass after updating assertions
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Regenerate mocks:

```bash
cd task/executor && go generate ./...
```

Run tests:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.

Verify AgentConfiguration type:

```bash
grep -rn "AgentConfiguration" task/executor/pkg/
```
Must show the new type and its usage in handler and spawner.

Verify geminiAPIKey removed from spawner:

```bash
grep -n "geminiAPIKey" task/executor/pkg/spawner/job_spawner.go
```
Must return no results.

Verify per-agent env loop in spawner:

```bash
grep -n "config.Env" task/executor/pkg/spawner/job_spawner.go
```
Must show the env loop.

Verify ANTHROPIC_API_KEY in K8s manifests:

```bash
grep -rn "ANTHROPIC_API_KEY\|anthropic-api-key" task/executor/k8s/
```
Must show secret and deployment entries.
</verification>
