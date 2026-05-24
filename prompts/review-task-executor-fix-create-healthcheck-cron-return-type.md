---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Introduces CronScheduler interface in pkg/
- Changes CreateHealthcheckCron to return interface
- Updates main.go to use the new interface type
</summary>

<objective>
CreateHealthcheckCron in factory.go returns run.Runnable (concrete type) instead of an interface. This leaks the concrete type libcron.ExpressionCron and makes testing harder. After this change, the factory returns a CronScheduler interface.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/factory/factory.go (~line 128, CreateHealthcheckCron)
- task/executor/main.go (where CreateHealthcheckCron is called)
</context>

<requirements>
### 1. Define CronScheduler interface in pkg/

Create a new file or add to an existing file:

```go
// CronScheduler runs a cron expression on a schedule.
type CronScheduler interface {
    Run(ctx context.Context) error
}
```

### 2. Update CreateHealthcheckCron return type

```go
func CreateHealthcheckCron(
    expression libcron.Expression,
    runner probe.HealthcheckRunner,
) CronScheduler {
    return libcron.NewExpressionCron(expression, runner)
}
```

### 3. Update main.go to use CronScheduler interface

In main.go, change the variable type that receives the result of CreateHealthcheckCron to use the interface type.

### 4. Run make build and make test

```bash
cd task/executor && make build && make test
```
</requirements>

<constraints>
- Only change files in `task/executor/`
- Do NOT commit — dark-factory handles git
- Factory functions must have zero business logic
</constraints>

<verification>
cd task/executor && make precommit
</verification>
