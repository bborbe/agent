---
status: completed
summary: Renamed MyVault/MY_VAULT/myVault/--my-vault to VaultName/VAULT_NAME/vaultName/--vault-name across task/controller code, tests, factory, docs, and CHANGELOG; spec preserved as historical record; make precommit passes with exit code 0
container: agent-rename-myvault-exec-203-rename-myvault-to-vaultname
dark-factory-version: v0.177.1
created: "2026-06-15T01:30:00Z"
queued: "2026-06-15T15:19:48Z"
started: "2026-06-15T15:20:34Z"
completed: "2026-06-15T15:33:15Z"
---

<summary>
- Rename the task-controller's vault-identity configuration from `MyVault` / `MY_VAULT` to `VaultName` / `VAULT_NAME` everywhere it appears in code, docs, and CHANGELOG
- Pure semantic-rename change — no behavior change, no new tests, no API surface added or removed
- Each renamed identifier moves consistently: Go field `MyVault` → `VaultName`, parameter `myVault` → `vaultName`, function `ValidateMyVault` → `ValidateVaultName`, env var `MY_VAULT` → `VAULT_NAME`, CLI flag `--my-vault` → `--vault-name`
- Existing tests in `routing/`, `command/task_create_task_executor_test.go`, and `main_internal_test.go` are updated to match the new names; test count and assertions are unchanged
- `docs/controller-design.md` and `CHANGELOG.md` references updated to use the new names
- Historical `specs/in-progress/044-multi-vault-routing.md` is NOT modified — it records the design at land time and the spec text stays as a historical record
- `make precommit` passes in `task/controller/`
- No k8s manifest changes in this prompt — k8s yaml `MY_VAULT` env on the open PR #19 (`feature/task-controller-personal`) will be renamed in a follow-up rebase after this lands on master
</summary>

<objective>
Make the controller's vault-identity configuration read as ops-conventional `VAULT_NAME` (matching `NAMESPACE`, `BRANCH`, `KAFKA_BROKERS`, etc.) instead of the informal `MY_VAULT`. End state: `grep -rn 'MyVault\|MY_VAULT\|myVault\|my-vault' task/ docs/ CHANGELOG.md` returns zero matches outside `specs/` (historical record).
</objective>

<context>
- `/workspace/CLAUDE.md` — project conventions
- `/workspace/task/controller/main.go` — defines the field, validates at startup, passes to `CreateCommandConsumer`
- `/workspace/task/controller/pkg/command/task_create_task_executor.go` — receives `myVault` parameter, uses it in `routing.ShouldProcess` + skip-log
- `/workspace/task/controller/pkg/routing/routing.go` — `ValidateMyVault` function, `ShouldProcess(cmd, myVault)` signature
- `/workspace/task/controller/main_internal_test.go` — `TestApplicationMyVaultFieldExists` reflect test
- `/workspace/task/controller/pkg/command/task_create_task_executor_test.go` — `BeforeEach` constructs executor with `myVault` arg
- `/workspace/task/controller/pkg/routing/routing_test.go` — `ValidateMyVault` + `ShouldProcess` matrix tests
- `/workspace/docs/controller-design.md` — narrative description of the routing predicate
- `/workspace/CHANGELOG.md` — `## v0.66.0` entry references `MY_VAULT`
</context>

<requirements>

1. **Rename the application struct field in `/workspace/task/controller/main.go`**

   Current (lines ~62-64):
   ```go
   MyVault         string            `required:"true"  arg:"my-vault"          env:"MY_VAULT"          usage:"vault slug this controller serves (e.g. openclaw, personal); legacy empty targetVault defaults to openclaw"`
   ```

   Replace with:
   ```go
   VaultName       string            `required:"true"  arg:"vault-name"        env:"VAULT_NAME"        usage:"vault slug this controller serves (e.g. openclaw, personal); legacy empty targetVault defaults to openclaw"`
   ```

   Field name `MyVault` → `VaultName`. Tags: `arg:"my-vault"` → `arg:"vault-name"`, `env:"MY_VAULT"` → `env:"VAULT_NAME"`. Usage string unchanged. Align field tags with the surrounding struct (the rest of the file uses aligned tag columns — preserve that visual style).

2. **Update the startup validation call in `main.go`**

   Current (~line 68):
   ```go
   if err := routing.ValidateMyVault(ctx, a.MyVault); err != nil {
   ```

   Replace with:
   ```go
   if err := routing.ValidateVaultName(ctx, a.VaultName); err != nil {
   ```

3. **Update the consumer wiring in `main.go`**

   Current (~line 149):
   ```go
   a.MyVault,
   ```

   Replace with:
   ```go
   a.VaultName,
   ```

   (This is the argument passed to `factory.CreateCommandConsumer` or equivalent — the wiring callsite, not the struct field again.)

4. **Rename the routing function in `/workspace/task/controller/pkg/routing/routing.go`**

   - Function `ValidateMyVault(ctx context.Context, myVault string) error` → `ValidateVaultName(ctx context.Context, vaultName string) error`
   - Parameter `myVault` → `vaultName` everywhere inside the function body and inside `ShouldProcess`
   - Doc comments: replace `myVault` with `vaultName`, replace `MY_VAULT` with `VAULT_NAME`
   - Error messages: replace `MY_VAULT` with `VAULT_NAME` in any `errors.Wrap` / `errors.Wrapf` format strings (the validation error format)

   Use `errors.Wrap`/`Wrapf` with 3-arg form (`ctx, err, msg`) — preserve the existing wrapping pattern; do not switch to `fmt.Errorf`.

5. **Update the executor in `/workspace/task/controller/pkg/command/task_create_task_executor.go`**

   - Rename parameter `myVault string` (line ~37) → `vaultName string`
   - Update every reference inside the executor body (including the `routing.ShouldProcess(cmd, myVault)` call and the skip-log format string)
   - In the skip-log line, change `my=%q` → `vault=%q` so the structured log key matches the new identifier:
     ```
     create-task: skipped vault mismatch target=%q effective=%q vault=%q task=%s
     ```
   - Update the doc comment on the executor that mentions `myVault`

6. **Update `/workspace/task/controller/main_internal_test.go`**

   - Rename `TestApplicationMyVaultFieldExists` → `TestApplicationVaultNameFieldExists`
   - Inside the test: `typ.FieldByName("MyVault")` → `typ.FieldByName("VaultName")`
   - Update tag-presence assertions: `MY_VAULT` → `VAULT_NAME`, `my-vault` → `vault-name`
   - Update any error messages emitted by the test to use the new names

7. **Update `/workspace/task/controller/pkg/command/task_create_task_executor_test.go`**

   - Locate the `BeforeEach` block that constructs the executor (`executor = command.NewCreateTaskExecutor(fakeGit, taskDir, ...)`). Update the third argument's variable / literal from `myVault` / `"openclaw"` style to `vaultName` / `"openclaw"`.
   - In the `Context("vault routing", ...)` block from spec 044, rename any local variable `myVault` → `vaultName`. The test descriptions ("AC 13 evidence", etc.) remain factually correct under the new name and do not need changes.
   - Update the skip-log grep test or any test that asserts the log substring — must match the new format `vault=` (was `my=`).

8. **Update `/workspace/task/controller/pkg/routing/routing_test.go`**

   - Rename `Describe("ValidateMyVault", ...)` → `Describe("ValidateVaultName", ...)`
   - Rename any `It` description mentioning `MY_VAULT` to mention `VAULT_NAME`
   - Update `ValidateMyVault` calls → `ValidateVaultName`
   - Update `ShouldProcess(cmd, myVault)` table arguments: rename the local variable `myVault` → `vaultName`
   - Update error-substring assertions that match `MY_VAULT` → match `VAULT_NAME`

9. **Update `/workspace/docs/controller-design.md`**

   - Replace every `MY_VAULT` → `VAULT_NAME`
   - Replace `--my-vault` → `--vault-name`
   - Replace prose references to "the controller's `MY_VAULT`" / "`MY_VAULT` env var" with the new name
   - Preserve sentence structure; this is a search-and-replace, not a rewrite

10. **Update `/workspace/CHANGELOG.md`**

    - In the existing `## v0.66.0` entry (line ~13), edit the inline mention `MY_VAULT` → `VAULT_NAME` and `--my-vault` → `--vault-name`
    - Add a NEW `## Unreleased` entry above `## v0.66.0`:
      ```markdown
      ## Unreleased

      - refactor(task/controller): rename `MY_VAULT` env / `--my-vault` flag / `MyVault` field to `VAULT_NAME` / `--vault-name` / `VaultName` for consistency with surrounding ops conventions (`NAMESPACE`, `BRANCH`, `KAFKA_BROKERS`). No behavior change. The skip-log structured key `my=` is renamed to `vault=`.
      ```

11. **Do NOT modify `/workspace/specs/in-progress/044-multi-vault-routing.md`**

    The spec records the original `MY_VAULT` design at land time. It stays as a historical record. Modifying it would rewrite history and break the spec-completion verification trail.

12. **Verification — `make precommit` must pass**

    ```bash
    cd /workspace/task/controller && make precommit
    ```

    Verify the rename completeness:
    ```bash
    cd /workspace && grep -rn 'MyVault\|MY_VAULT\|myVault\|--my-vault' task/ docs/ CHANGELOG.md
    # Must return zero matches.
    ```

    Verify the new names appear:
    ```bash
    cd /workspace && grep -rcn 'VaultName\|VAULT_NAME\|vaultName\|--vault-name' task/controller/main.go task/controller/pkg/routing/routing.go task/controller/pkg/command/task_create_task_executor.go
    # Must return non-zero counts in all three files.
    ```

</requirements>

<constraints>
- Pure rename — no new behavior, no new tests, no test deletions, no API surface change beyond the rename
- Preserve `bborbe/errors` 3-arg `Wrap(ctx, err, msg)` and `Wrapf(ctx, err, fmt, args...)` patterns — do NOT switch to `fmt.Errorf`
- Preserve factory-function purity in `pkg/factory` — this prompt does not touch factories; if a factory references `MyVault` indirectly via a constructor parameter, update that parameter name only, no logic changes
- Do NOT change `specs/in-progress/044-multi-vault-routing.md` — historical record
- Do NOT change `task/controller/k8s/*.yaml` — those live on the open PR #19 branch (`feature/task-controller-personal`); they will be rebased separately
- Do NOT touch `lib/` — the rename is task-controller-internal; `lib/command/task/CreateCommand`'s `TargetVault` field is unrelated and stays as-is
- Do NOT add a config knob, feature flag, or backward-compat alias accepting both env-var names. This is a clean rename, not a deprecation
- Do NOT introduce `context.Background()` in business logic — keep the existing context flow
- Do NOT modify `vendor/`
</constraints>

<verification>
```bash
# 1. Tests pass
cd /workspace/task/controller && make precommit

# 2. Old names gone from active code
cd /workspace && ! grep -rn 'MyVault\|MY_VAULT\|myVault\|--my-vault' task/ docs/ CHANGELOG.md

# 3. New names present in the three key files
cd /workspace && grep -l 'VaultName' task/controller/main.go task/controller/pkg/routing/routing.go task/controller/pkg/command/task_create_task_executor.go

# 4. Spec untouched (history preserved)
cd /workspace && grep -c 'MY_VAULT' specs/in-progress/044-multi-vault-routing.md
# Must be > 0 — spec is the historical record, not rewritten

# 5. CHANGELOG has an Unreleased entry naming the rename
cd /workspace && grep -A 5 '## Unreleased' CHANGELOG.md | grep -E 'VAULT_NAME|--vault-name|VaultName'
```
</verification>
