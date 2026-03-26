---
status: created
spec: ["001"]
created: "2026-03-26T09:00:00Z"
branch: dark-factory/git-to-kafka-task-sync
---

<summary>
- task/controller gains four new CLI flags: vault-path, kafka-brokers, git-branch, and poll-interval
- Service fatals on startup if the vault path does not exist
- Service fatals on startup if the vault path exists but is not a git repository
- Poll interval defaults to 60 seconds
- All existing flags (sentry-dsn, sentry-proxy, listen) continue to work unchanged
- Existing HTTP server (healthz, readiness, metrics) continues to function unchanged
- New fields are added to the application struct and wired through Run() to a startup validator
</summary>

<objective>
Add the required CLI flags to task/controller and implement startup validation: if the configured vault path is missing or not a git repository, the service must refuse to start with a fatal error. This establishes the foundation that subsequent prompts (polling loop, Kafka publishing) will build on.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read `/home/node/.claude/docs/go-patterns.md` for interface/constructor/struct patterns.
Read `/home/node/.claude/docs/go-testing.md` for Ginkgo/Gomega and counterfeiter conventions.
Read `/home/node/.claude/docs/go-factory-pattern.md` for factory function rules (zero logic, Create* prefix).

Key files to read before making changes:
- `task/controller/main.go` — existing application struct, Run(), createHTTPServer()
- `lib/agent_cdb-schema.go` — TaskV1SchemaID definition
- `task/controller/Makefile` — build targets (make test, make precommit)
</context>

<requirements>
1. Add the following fields to the `application` struct in `task/controller/main.go`:

   ```go
   VaultPath    string        `required:"true"  arg:"vault-path"     env:"VAULT_PATH"     usage:"path to the Obsidian vault git repository"`
   KafkaBrokers string        `required:"true"  arg:"kafka-brokers"  env:"KAFKA_BROKERS"  usage:"comma-separated Kafka broker addresses"`
   GitBranch    string        `required:"false" arg:"git-branch"     env:"GIT_BRANCH"     usage:"git branch to track" default:"main"`
   PollInterval time.Duration `required:"false" arg:"poll-interval"  env:"POLL_INTERVAL"  usage:"vault polling interval" default:"60s"`
   ```

2. Create `task/controller/pkg/validator/vault_validator.go` with:
   - Interface:
     ```go
     //counterfeiter:generate -o ../../mocks/vault_validator.go --fake-name FakeVaultValidator . VaultValidator
     type VaultValidator interface {
         Validate(ctx context.Context) error
     }
     ```
   - Private struct `vaultValidator` with a `vaultPath string` field.
   - Constructor:
     ```go
     func NewVaultValidator(vaultPath string) VaultValidator
     ```
   - `Validate(ctx context.Context) error` implementation:
     - If `vaultPath` does not exist on disk: return `errors.Wrapf(ctx, err, "vault path does not exist: %s", vaultPath)`
     - If `vaultPath` exists but `.git/` subdirectory is absent: return `errors.Wrapf(ctx, validation.Error, "vault path is not a git repository: %s", vaultPath)`
     - Otherwise return nil.
   - Import paths: `github.com/bborbe/errors`, `github.com/bborbe/validation`

3. Create `task/controller/pkg/validator/vault_validator_test.go` with a Ginkgo suite:
   - Test suite bootstrap file `task/controller/pkg/validator/validator_suite_test.go`
   - Tests for:
     - Path does not exist → returns error
     - Path exists but no `.git/` subdirectory → returns error (use `os.TempDir()` + `os.MkdirTemp`)
     - Path exists and has `.git/` subdirectory → returns nil
   - Use `os.MkdirTemp` to create real temp dirs; clean up with `defer os.RemoveAll`.

4. In `task/controller/main.go`, update `Run()` to call `NewVaultValidator(a.VaultPath).Validate(ctx)` before starting the HTTP server. If validation fails, return the error immediately (the service framework will log it as fatal and exit).

5. Run `make generate` in `task/controller/` to regenerate mocks after adding the counterfeiter annotation.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib.Task`, `lib.TaskV1SchemaID`, or `lib.TaskIdentifier` — these are frozen
- Do NOT use `vault-cli/pkg/domain` types in this prompt — they are used in the scanner prompt
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- All new interfaces must have counterfeiter annotations; mocks must be generated
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Use external test packages (`package validator_test`)
- Existing healthz/readiness/metrics endpoints must continue to work unchanged
- `make test` must pass before declaring done
</constraints>

<verification>
Run `make test` in `task/controller/` — must pass.
Run `make precommit` in `task/controller/` — must pass with exit code 0.
</verification>
