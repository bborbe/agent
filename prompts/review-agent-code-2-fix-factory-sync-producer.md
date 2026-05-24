---
status: draft
created: "2026-05-24T12:00:00Z"
---

<summary>
- Removes error wrapping from `CreateSyncProducer` factory — pure one-line pass-through
- Removes the unused `errors` import from factory.go
- Uses `agentName` from main.go (passed as parameter) instead of hardcoded `serviceName` constant
</summary>

<objective>
Refactor `CreateSyncProducer` to be a pure pass-through with zero business logic. The error wrapping currently in the factory is moved to the single caller in `main.go`. After this change, the factory is a single-line constructor wrapper with no conditional, no error handling, and no context propagation beyond passing ctx to the underlying constructor.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- agent/code/pkg/factory/factory.go — CreateSyncProducer at lines 26-36
- agent/code/main.go — caller at lines 96-101 where CreateSyncProducer is used and error is wrapped
</context>

<requirements>

## 1. Refactor CreateSyncProducer to pure pass-through

Read `agent/code/pkg/factory/factory.go` before editing.

Replace the `CreateSyncProducer` function and the `serviceName` constant with a version that accepts `agentName` as a parameter and returns the result directly without error wrapping:

Replace lines 24-36:

Before:
```go
const serviceName = "agent-code"

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
	agentName string,
) (libkafka.SyncProducer, error) {
	return libkafka.NewSyncProducerWithName(ctx, brokers, agentName)
}
```

## 2. Remove unused `errors` import

After the refactor above, verify that `errors` is no longer used in factory.go:
```bash
grep -n "errors\." agent/code/pkg/factory/factory.go
```

If no matches, remove `"github.com/bborbe/errors"` from the imports.

## 3. Update main.go call site

Read `agent/code/main.go` before editing.

Update the `CreateSyncProducer` call at lines 96-101 to pass `agentName` and move error wrapping to the caller:

Replace lines 96-101:
```go
syncProducer, err := factory.CreateSyncProducer(ctx, a.KafkaBrokers)
if err != nil {
	jobMetrics.RecordRun(agentlib.AgentStatusFailed)
	jobMetrics.RecordDuration(time.Since(start))
	return errors.Wrap(ctx, err, "create sync producer")
}
```

After:
```go
syncProducer, err := factory.CreateSyncProducer(ctx, a.KafkaBrokers, agentName)
if err != nil {
	jobMetrics.RecordRun(agentlib.AgentStatusFailed)
	jobMetrics.RecordDuration(time.Since(start))
	return errors.Wrap(ctx, err, "create sync producer")
}
```

Note: the caller already wraps with "create sync producer" — no change to the error message needed.

## 4. Verify factory has no remaining business logic

After editing, confirm the factory is clean:
```bash
grep -n "errors\." agent/code/pkg/factory/factory.go || echo "No errors import usage"
grep -n "if err" agent/code/pkg/factory/factory.go || echo "No if err in factory"
```

Expected: both return "not found" or echo output showing no matches.

## 5. Run make test then make precommit

```bash
cd agent/code && make test
```
Expected: exit 0.

```bash
cd agent/code && make precommit
```
Expected: exit 0.

</requirements>

<constraints>
- Only change files in `agent/code/`
- Do NOT commit — dark-factory handles git
- The factory function must remain a single return statement with no conditional
- Follow project conventions: error wrapping with `github.com/bborbe/errors`, never `fmt.Errorf`
</constraints>

<verification>
cd agent/code && make precommit
</verification>
