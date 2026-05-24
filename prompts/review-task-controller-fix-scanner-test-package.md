---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Changes `pkg/scanner/vault_scanner_test.go` from `package scanner` to `package scanner_test`
- Adds import for the scanner package to access exported types
- Documents the manual `testGitClient` test double rationale in the file header
</summary>

<objective>
`vault_scanner_test.go` currently declares `package scanner` (the internal implementation package), which means tests access private fields like `hashes`, `ops`, `trigger`, `pollInterval` directly. Per project convention, tests should use external test packages (`package scanner_test`) to test only the public API. This change makes the test package consistent with all other packages in the project.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner_test.go` — full file, especially lines 1-80 (package, imports, testGitClient struct)
- `task/controller/pkg/scanner/vault_scanner.go` — exported interface `VaultScanner` and `NewGitRestVaultScanner` constructor
</context>

<requirements>

1. **Change package declaration** from `package scanner` to `package scanner_test` at the top of `vault_scanner_test.go`.

2. **Add import for the scanner package** to access exported types:
   ```go
   import (
       "github.com/bborbe/agent/task/controller/pkg/scanner"
       // ... existing imports
   )
   ```

3. **Update references to private fields** — change `scanner.` prefix where needed for types like `*vaultScanner`. The exported `VaultScanner` interface and `NewGitRestVaultScanner` constructor should be accessible via the package import.

   Note: The `vaultScanner` struct (lowercase) is private and cannot be accessed from `scanner_test`. Tests that currently construct `&vaultScanner{...}` must be refactored to use the exported constructor `NewGitRestVaultScanner` or `NewVaultScanner` instead, or use the exported interface type.

4. **Document the `testGitClient` exception** — add a header comment explaining why the manual test double is used:
   ```go
   // NOTE: testGitClient is a hand-written test double rather than a Counterfeiter
   // mock because importing the mocks package would create an import cycle:
   // mocks/ imports scanner for ScanResult, so scanner cannot import mocks.
   // This is a documented exception to the Counterfeiter-mocks rule.
   ```

5. **Run tests iteratively** to fix any compilation errors from the package change:
   ```bash
   cd task/controller && make test
   ```

6. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change `task/controller/pkg/scanner/vault_scanner_test.go`
- Do NOT commit — dark-factory handles git
- Follow project test conventions: Ginkgo v2, external test package
</constraints>

<verification>
cd task/controller && make precommit
</verification>
