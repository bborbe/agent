---
status: completed
summary: Removed CreateSyncProducer and CreateGeminiParser factory functions from agent/gemini/pkg/factory/factory.go and moved error-producing logic to call sites in main.go and cmd/run-task/main.go
container: agent-exec-179-review-agent-gemini-fix-factory-pattern
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-26T00:05:26Z"
completed: "2026-05-26T00:07:23Z"
---

<summary>
- Two factory functions (`CreateSyncProducer`, `CreateGeminiParser`) return `error`, violating the zero-business-logic rule
- Both factories have conditional error handling that belongs in the call site, not the factory
- After this change, these wrapper factories are removed and callers invoke constructors directly
</summary>

<objective>
Eliminate factory functions that return `error` by moving error-producing logic to call sites. Factories must be pure composition with zero conditionals.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- agent/gemini/pkg/factory/factory.go (~line 29-53, CreateSyncProducer and CreateGeminiParser)
- agent/gemini/main.go (~line 94-113, call sites for both)
- agent/gemini/cmd/run-task/main.go (~line 46-56, call sites)
</context>

<requirements>
1. **Remove `CreateSyncProducer`** from `agent/gemini/pkg/factory/factory.go`

   This factory has business logic (error handling conditional). Remove it entirely.

2. **Remove `CreateGeminiParser`** from `agent/gemini/pkg/factory/factory.go`

   This factory has business logic (error handling conditional). Remove it entirely.

3. **Update `main.go`** call sites

   Instead of:
   ```go
   geminiParser, err := factory.CreateGeminiParser(ctx, apiKey, model)
   if err != nil {
       jobMetrics.RecordRun(ctx, "parse_error")
       return errors.Wrap(ctx, err, "create gemini parser")
   }
   ```
   Use:
   ```go
   geminiParser, err := parser.New(ctx, apiKey, model)
   if err != nil {
       jobMetrics.RecordRun(ctx, "parse_error")
       return errors.Wrap(ctx, err, "create gemini parser")
   }
   ```

   Instead of:
   ```go
   syncProducer, err := factory.CreateSyncProducer(ctx, libkafka.Brokers(a.KafkaBrokers))
   if err != nil {
       jobMetrics.RecordRun(ctx, "producer_error")
       return errors.Wrap(ctx, err, "create sync producer")
   }
   ```
   Use:
   ```go
   syncProducer, err := libkafka.NewSyncProducerWithName(ctx, libkafka.Brokers(a.KafkaBrokers), factory.ServiceName)
   if err != nil {
       jobMetrics.RecordRun(ctx, "producer_error")
       return errors.Wrap(ctx, err, "create sync producer")
   }
   ```

4. **Update `cmd/run-task/main.go`** call sites

   Same pattern — replace `factory.CreateGeminiParser` with `parser.New` directly.

5. **Keep `ServiceName` constant** in `agent/gemini/pkg/factory/factory.go`

   The `ServiceName` constant is still needed by `main.go` for `libkafka.NewSyncProducerWithName`. Keep it.

6. **Update factory tests** in `agent/gemini/pkg/factory/factory_test.go`

   Remove any tests for `CreateSyncProducer` and `CreateGeminiParser` (which no longer exist). The remaining factories (`CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgentProvider`) require no test changes since they have zero logic.

7. **Run tests**
   ```bash
   cd agent/gemini && make test
   ```

8. **Run precommit**
   ```bash
   cd agent/gemini && make precommit
   ```
</requirements>

<constraints>
- Only change files in `agent/gemini/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/`
- `ServiceName` constant must remain exported in `factory.go` since it's used by `main.go`
- The `CreateAgent` factory (returns `*agentlib.Agent`) is out of scope for this fix
</constraints>

<verification>
cd agent/gemini && make precommit
</verification>
