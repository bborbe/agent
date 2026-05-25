---
status: draft
created: "2026-05-24T11:10:00Z"
queued: "2026-05-25T22:23:08Z"
---

<summary>
- CreateSyncProducer factory in pkg/factory/factory.go performs error wrapping internally, violating the zero-business-logic factory rule
- Error wrapping is moved to main.go where it belongs, per the factory pattern guide
- Error-path test added to factory_test.go to cover broker-unreachable scenario
</summary>

<objective>
Refactor CreateSyncProducer to be a pure pass-through with zero business logic. The error wrapping currently in the factory is moved to the single caller in main.go. After this change, the factory is a single-line constructor wrapper with no conditional, no error handling, and no context propagation.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- agent/claude/pkg/factory/factory.go — CreateSyncProducer at line 46-56
- agent/claude/main.go — caller at line ~119 where CreateSyncProducer is used
- agent/claude/pkg/factory/factory_test.go — existing factory tests
</context>

<requirements>

## 1. Refactor CreateSyncProducer to pure pass-through

Read `agent/claude/pkg/factory/factory.go` before editing.

Replace the current CreateSyncProducer (lines 46-56):

Before:
```go
// CreateSyncProducer creates a Kafka sync producer.
func CreateSyncProducer(
	ctx context.Context,
	brokers libkafka.Brokers,
) (libkafka.SyncProducer, error) {
	producer, err := libkafka.NewSyncProducerWithName(ctx, brokers, serviceName)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "create sync producer failed")
	}
	return producer, nil
}
```

After:
```go
// CreateSyncProducer creates a Kafka sync producer.
func CreateSyncProducer(
	ctx context.Context,
	brokers libkafka.Brokers,
) (libkafka.SyncProducer, error) {
	return libkafka.NewSyncProducerWithName(ctx, brokers, serviceName)
}
```

The function is now a pure pass-through. Remove the `errors` import if it becomes unused.

Verify the `errors` import is still needed by other functions in the file. If not, remove it:
```bash
grep -n "errors\." agent/claude/pkg/factory/factory.go
```

## 2. Add error-path test for CreateSyncProducer

Read `agent/claude/pkg/factory/factory_test.go` before editing.

Add a test case that exercises the error path when broker is unreachable. Follow the same pattern as the existing factory error-path tests in this file (search for `It("returns an error"`).

```go
It("returns an error when broker is unreachable", func(ctx context.Context) {
    // Empty brokers should cause NewSyncProducerWithName to fail
    producer, err := CreateSyncProducer(ctx, libkafka.Brokers{})
    Expect(producer).To(BeNil())
    Expect(err).NotTo(BeNil())
    Expect(err.Error()).To(ContainSubstring("create sync producer"))
})
```

Note: The exact error message depends on what libkafka returns for empty brokers. Adjust the `ContainSubstring` argument based on actual behavior. If empty brokers returns nil error (no connection attempted), use a different invalid input.

Run tests to verify:
```bash
cd agent/claude && go test ./pkg/factory/... -v 2>&1 | grep -E "PASS|FAIL|broker"
```

## 3. Update main.go error handling for CreateSyncProducer

Read `agent/claude/main.go` before editing.

The call site at ~line 119 currently looks like:
```go
syncProducer, err := factory.CreateSyncProducer(ctx, a.KafkaBrokers)
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "create sync producer")
}
```

If the caller site already wraps with "create sync producer" message, no change is needed. Verify the exact message at the call site and adjust if necessary to avoid duplicate wrapping.

## 4. Run make test then make precommit

```bash
cd agent/claude && make test
```
Expected: exit 0.

```bash
cd agent/claude && make precommit
```
Expected: exit 0.

</requirements>

<constraints>
- Only change files in `agent/claude/`
- Do NOT commit — dark-factory handles git
- The factory function must remain a single return statement with no conditional
- Follow project conventions: error wrapping with `github.com/bborbe/errors`, never `fmt.Errorf`
</constraints>

<verification>
cd agent/claude && make precommit
</verification>
