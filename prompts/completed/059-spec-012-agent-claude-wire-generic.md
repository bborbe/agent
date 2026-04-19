---
status: completed
spec: [012-generic-claude-task-runner]
container: agent-059-spec-012-agent-claude-wire-generic
dark-factory-version: v0.128.1-3-gf1cfca3-dirty
created: "2026-04-19T18:00:00Z"
queued: "2026-04-19T20:03:37Z"
started: "2026-04-19T20:12:08Z"
completed: "2026-04-19T20:35:59Z"
branch: dark-factory/generic-claude-task-runner
---

<summary>
- Updates `agent/claude/pkg/factory/factory.go` to use the new generic `claude.TaskRunner[claude.AgentResult]` and `claude.ResultDeliverer[claude.AgentResult]` types from the lib module
- `CreateTaskRunner` return type changes from `claudelib.TaskRunner` (non-generic) to `claudelib.TaskRunner[claudelib.AgentResult]`
- `CreateKafkaResultDeliverer` return type changes from `claudelib.ResultDeliverer` (non-generic) to `claudelib.ResultDeliverer[claudelib.AgentResult]`
- `NewResultDelivererAdapter` call gains explicit type parameter `[claudelib.AgentResult]`
- `NewTaskRunner` call gains explicit type parameter `[claudelib.AgentResult]`
- `agent/claude/main.go` updated if the return-type changes require it (factory return types changed)
- `agent/claude/mocks/` regenerated if any mocked interfaces changed
- Runtime behavior is byte-identical: the generic parameter is erased at compile time
- `cd agent/claude && make precommit` passes
</summary>

<objective>
Wire the newly-generic `lib/claude.TaskRunner[T]` and `ResultDeliverer[T]` into the `agent/claude` binary. The only change is adding explicit `[claudelib.AgentResult]` type parameters at the two factory call sites. Runtime behavior is unchanged — the generic parameter is a compile-time construct. This completes the migration for the `agent/claude` module. The `trade-analysis` module migration happens separately (prompt 3).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — factory function conventions, zero logic, Create* prefix

**This prompt depends on prompt 1 (lib generic task-runner) being complete.** The `lib/` module is referenced via a local `replace` directive in `agent/claude/go.mod`:
```
replace github.com/bborbe/agent/lib => ../../lib
```
So no `go.mod` bump is needed — the workspace `replace` already picks up the lib changes.

**Key files to read before editing:**

- `agent/claude/pkg/factory/factory.go` — `CreateTaskRunner` (returns `claudelib.TaskRunner`) and `CreateKafkaResultDeliverer` (returns `claudelib.ResultDeliverer`); both return types need the `[claudelib.AgentResult]` type parameter
- `agent/claude/main.go` — uses `factory.CreateTaskRunner(...)` and `factory.CreateKafkaResultDeliverer(...)`; check if type annotations need updating here (they typically don't when the variable is inferred)
- `agent/claude/mocks/mocks.go` — check if any mocked interfaces from `lib/claude` are referenced here; if so, verify they still compile after the lib changes
</context>

<requirements>

1. **Update `agent/claude/pkg/factory/factory.go` — `CreateTaskRunner` return type**

   Change the return type of `CreateTaskRunner` from `claudelib.TaskRunner` to `claudelib.TaskRunner[claudelib.AgentResult]`:

   ```go
   // CreateTaskRunner wires a complete TaskRunner with ClaudeRunner,
   // prompt assembly, and result delivery.
   func CreateTaskRunner(
       claudeConfigDir claudelib.ClaudeConfigDir,
       agentDir claudelib.AgentDir,
       allowedTools claudelib.AllowedTools,
       model claudelib.ClaudeModel,
       env map[string]string,
       envContext map[string]string,
       instructions claudelib.Instructions,
       deliverer claudelib.ResultDeliverer[claudelib.AgentResult],
   ) claudelib.TaskRunner[claudelib.AgentResult] {
       return claudelib.NewTaskRunner[claudelib.AgentResult](
           claudelib.NewClaudeRunner(claudelib.ClaudeRunnerConfig{
               ClaudeConfigDir:  claudeConfigDir,
               AllowedTools:     allowedTools,
               Model:            model,
               WorkingDirectory: agentDir,
               Env:              env,
           }),
           instructions,
           envContext,
           deliverer,
       )
   }
   ```

   Note: the `deliverer` parameter type also changes from `claudelib.ResultDeliverer` to `claudelib.ResultDeliverer[claudelib.AgentResult]`.

2. **Update `agent/claude/pkg/factory/factory.go` — `CreateKafkaResultDeliverer` return type**

   Change the return type from `claudelib.ResultDeliverer` to `claudelib.ResultDeliverer[claudelib.AgentResult]`:

   ```go
   // CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task updates to Kafka.
   func CreateKafkaResultDeliverer(
       syncProducer libkafka.SyncProducer,
       branch base.Branch,
       taskID agentlib.TaskIdentifier,
       taskContent string,
       currentDateTime libtime.CurrentDateTimeGetter,
   ) claudelib.ResultDeliverer[claudelib.AgentResult] {
       return claudelib.NewResultDelivererAdapter[claudelib.AgentResult](
           delivery.NewKafkaResultDeliverer(
               syncProducer,
               branch,
               taskID,
               taskContent,
               delivery.NewFallbackContentGenerator(),
               currentDateTime,
           ),
       )
   }
   ```

3. **Check `agent/claude/main.go` for compilation impact**

   Read `agent/claude/main.go` and locate all uses of `factory.CreateTaskRunner` and `factory.CreateKafkaResultDeliverer`. Because Go infers types from the return value, variable declarations like:
   ```go
   deliverer := factory.CreateKafkaResultDeliverer(...)
   taskRunner := factory.CreateTaskRunner(...)
   ```
   will continue to work without changes (the inferred type becomes the generic instantiation).

   However, if `main.go` has explicit type annotations such as:
   ```go
   var deliverer claudelib.ResultDeliverer = factory.CreateKafkaResultDeliverer(...)
   ```
   update them to:
   ```go
   var deliverer claudelib.ResultDeliverer[claudelib.AgentResult] = factory.CreateKafkaResultDeliverer(...)
   ```

   Similarly for `claudelib.TaskRunner` → `claudelib.TaskRunner[claudelib.AgentResult]`.

4. **Verify `agent/claude/mocks/` compiles**

   Run:
   ```bash
   cd agent/claude && go build ./...
   ```
   If any mock in `agent/claude/mocks/` references the old non-generic `claudelib.TaskRunner` or `claudelib.ResultDeliverer` interfaces, regenerate:
   ```bash
   cd agent/claude && make generate
   ```

5. **Verify tests still pass**

   ```bash
   cd agent/claude && make test
   ```
   Must exit 0.

6. **Update `CHANGELOG.md`** (the top-level file at `/workspace/CHANGELOG.md`)

   First check for existing `## Unreleased`:
   ```bash
   grep -n "Unreleased" CHANGELOG.md | head -3
   ```
   If `## Unreleased` already exists, APPEND to it. If not, INSERT immediately above the first `## v` heading.

   Add:
   ```markdown
   - feat: agent/claude: wire generic claude.TaskRunner[claude.AgentResult] and claude.ResultDeliverer[claude.AgentResult]
   ```

</requirements>

<constraints>
- Do NOT change any function signatures in `main.go` beyond updating type annotations — runtime behavior must be identical
- Do NOT change `delivery.NewKafkaResultDeliverer`, `delivery.NewFallbackContentGenerator`, or any `lib/delivery/` files
- Do NOT commit — dark-factory handles git
- Do NOT bump `go.mod` versions — the `replace` directive already points to the local `lib/` workspace
- All existing tests must pass
- `cd agent/claude && make precommit` must exit 0
</constraints>

<verification>
Verify factory return types are generic:
```bash
grep -n "TaskRunner\[claudelib\|ResultDeliverer\[claudelib" agent/claude/pkg/factory/factory.go
```
Must show both `TaskRunner[claudelib.AgentResult]` and `ResultDeliverer[claudelib.AgentResult]`.

Verify the binary compiles:
```bash
cd agent/claude && go build ./...
```
Must exit 0.

Run tests:
```bash
cd agent/claude && make test
```
Must exit 0.

Run precommit:
```bash
cd agent/claude && make precommit
```
Must exit 0.
</verification>
