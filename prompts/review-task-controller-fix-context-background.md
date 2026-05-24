---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Adds `ctx context.Context` parameter to `injectTaskIdentifier` so errors carry structured context
- Propagates `ctx` from `processFile` through `injectAndStore` to `injectTaskIdentifier`
- Removes `context.Background()` from production business logic in `pkg/scanner/`
</summary>

<objective>
The `injectTaskIdentifier` function in `pkg/scanner/vault_scanner.go` calls `errors.Errorf(context.Background(), ...)` at line 400, violating the project rule that `context.Background()` must not appear in production business logic (`pkg/`). The error loses structured context that would help operators debug malformed frontmatter during scanning. After this fix, all errors from the frontmatter injection pipeline carry the caller's context.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- `task/controller/pkg/scanner/vault_scanner.go` — `injectTaskIdentifier` at line 391, `injectAndStore` at line 314, `processFile` at line 229
</context>

<requirements>

1. **Add `ctx` parameter to `injectTaskIdentifier`** at `vault_scanner.go:391`

   Change the function signature from:
   ```go
   func injectTaskIdentifier(content []byte, id string) ([]byte, error) {
   ```
   To:
   ```go
   func injectTaskIdentifier(ctx context.Context, content []byte, id string) ([]byte, error) {
   ```

   Update the error at line 399-402 from:
   ```go
   return nil, errors.Errorf(
       context.Background(),
       "content does not start with frontmatter delimiter",
   )
   ```
   To:
   ```go
   return nil, errors.Errorf(
       ctx,
       "content does not start with frontmatter delimiter",
   )
   ```

2. **Update `injectAndStore`** at `vault_scanner.go:314` to pass `ctx` to `injectTaskIdentifier`

   Find the call to `injectTaskIdentifier` inside `injectAndStore`. Add `ctx` as the first argument.

3. **Update the call site inside `processFile`** at `vault_scanner.go:229` where `injectAndStore` is called — ensure `ctx` is passed through the call chain.

   The full propagation path is: `runCycle` → `scanFiles` → `processFile` → `injectAndStore` → `injectTaskIdentifier`. Verify that `ctx` flows through each step and is passed correctly.

4. **Verify no other uses of `context.Background()` remain in `pkg/scanner/`**

   Run:
   ```bash
   grep -n "context.Background()" task/controller/pkg/scanner/
   ```
   Expected: zero matches in non-test files.

5. **Run tests:**
   ```bash
   cd task/controller && make test
   ```
   All tests must pass.

6. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change `pkg/scanner/vault_scanner.go`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`)
</constraints>

<verification>
cd task/controller && make precommit
</verification>
