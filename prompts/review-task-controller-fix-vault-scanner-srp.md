---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- `task/controller/pkg/scanner/vault_scanner.go` is 509 lines and mixes 3 concerns: orchestration (scan loop), frontmatter parsing, and UUID/task-identifier injection
- Extract frontmatter helpers (`extractFrontmatter`, `extractBody`, `DeduplicateFrontmatter`) into `pkg/scanner/frontmatter.go`
- Extract task-identifier helpers (`InjectTaskIdentifier`, `removeTaskIdentifier`, `isValidUUID`, `isIdentifierUnique`) into `pkg/scanner/task_identifier.go`
- Pure code move — no signature changes, no behavior changes, only package-internal reorg
- `vault_scanner.go` retains: `VaultScanner` interface, `vaultScanner` struct, scan/poll orchestration methods, factories
</summary>

<objective>
Reduce `vault_scanner.go` from 509 lines to ~250 lines by moving two cohesive groups of helpers into their own files within the same package. Public API and behavior unchanged.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

File to read in full before changes:
- `task/controller/pkg/scanner/vault_scanner.go` (509 lines)

Current function layout (verified by `grep -nE "^func " vault_scanner.go`):

| Line | Symbol | Group |
|------|---|---|
| 64   | `newLocalFileOps(basePath string) fileOps` | orchestration (stay) |
| 97   | `NewVaultScanner(...)` | orchestration (stay) |
| 116  | `NewGitRestVaultScanner(...)` | orchestration (stay) |
| 136  | `(v *vaultScanner) Run(ctx, results) error` | orchestration (stay) |
| 153  | `(v *vaultScanner) RunCycle(ctx, results)` | orchestration (stay) |
| 176  | `(v *vaultScanner) scanFiles(...)` | orchestration (stay) |
| 216  | `(v *vaultScanner) processFile(...)` | orchestration (stay) |
| 303  | `isValidUUID(s string) bool` | task-identifier (MOVE) |
| 309  | `(v *vaultScanner) isIdentifierUnique(id, relPath string) bool` | task-identifier (MOVE — method, but still goes in same package) |
| 321  | `(v *vaultScanner) injectAndStore(...)` | orchestration (stay — uses fileOps) |
| 348  | `(v *vaultScanner) writeCounterReset(...)` | orchestration (stay) |
| 378  | `(v *vaultScanner) collectDeleted(...)` | orchestration (stay) |
| 399  | `removeTaskIdentifier(content []byte) []byte` | task-identifier (MOVE) |
| 414  | `InjectTaskIdentifier(ctx, content, id) ([]byte, error)` | task-identifier (MOVE — exported) |
| 429  | `DeduplicateFrontmatter(ctx, fmYAML) (string, bool, error)` | frontmatter (MOVE — exported) |
| 467  | `extractFrontmatter(ctx, content) (string, error)` | frontmatter (MOVE) |
| 486  | `extractBody(content []byte) string` | frontmatter (MOVE) |

Notes:
- `isIdentifierUnique` is a METHOD on `*vaultScanner` — moving its definition to `task_identifier.go` is legal in Go because both files are in the same package
- `InjectTaskIdentifier` and `DeduplicateFrontmatter` are EXPORTED (capitalized) — their callers in other packages must continue to find them at the same import path
- The `vault_scanner_test.go` file exists — verify tests still pass after move
</context>

<requirements>

1. **Create `task/controller/pkg/scanner/frontmatter.go`** with the standard header and move three functions verbatim:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package scanner

   import (
       // Add ONLY the imports actually used by the moved functions.
       // Read the function bodies to determine the exact import set.
   )

   // <body of DeduplicateFrontmatter from line 429>
   // <body of extractFrontmatter from line 467>
   // <body of extractBody from line 486>
   ```

   Preserve doc-comments and the function bodies exactly. Do NOT rename, do NOT change signatures.

2. **Create `task/controller/pkg/scanner/task_identifier.go`** with the standard header and move four functions verbatim:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package scanner

   import (
       // Add ONLY the imports actually used by the moved functions.
   )

   // <body of isValidUUID from line 303>
   // <body of (v *vaultScanner) isIdentifierUnique from line 309>
   // <body of removeTaskIdentifier from line 399>
   // <body of InjectTaskIdentifier from line 414>
   ```

3. **Remove the moved functions from `vault_scanner.go`**. After removal, `vault_scanner.go` should contain only:
   - Package declaration + header + imports (prune unused imports)
   - The `VaultScanner` interface and `vaultScanner` struct definition
   - `newLocalFileOps`, `NewVaultScanner`, `NewGitRestVaultScanner` factories
   - The methods: `Run`, `RunCycle`, `scanFiles`, `processFile`, `injectAndStore`, `writeCounterReset`, `collectDeleted`
   - `fileOps` related type/struct definitions

4. **Prune imports in all three files**:
   - `vault_scanner.go` may no longer need the imports used only by the moved code (e.g. yaml, regexp, uuid). Run `goimports`-style cleanup (handled by `make precommit`).
   - `frontmatter.go` and `task_identifier.go` import only what their bodies need.

5. **Run tests** to confirm nothing broke:
   ```bash
   cd task/controller && make test
   ```

6. **Run precommit**:
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only create: `task/controller/pkg/scanner/frontmatter.go`, `task/controller/pkg/scanner/task_identifier.go`
- Only edit: `task/controller/pkg/scanner/vault_scanner.go` (remove moved code, prune imports)
- Do NOT change ANY function signatures, return types, or doc comments
- Do NOT touch `vault_scanner_test.go` — existing tests must continue to pass against the moved code
- Do NOT change other files in `pkg/scanner/` or anywhere else in the repo
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit

# Confirm new files exist and have the expected functions:
grep -n "^func " task/controller/pkg/scanner/frontmatter.go
grep -n "^func " task/controller/pkg/scanner/task_identifier.go

# Confirm vault_scanner.go shrank:
wc -l task/controller/pkg/scanner/vault_scanner.go

# Confirm moved functions are GONE from vault_scanner.go:
! grep -nE "^func (extractFrontmatter|extractBody|DeduplicateFrontmatter|isValidUUID|removeTaskIdentifier|InjectTaskIdentifier)\b" task/controller/pkg/scanner/vault_scanner.go
! grep -nE "func \(v \*vaultScanner\) isIdentifierUnique\b" task/controller/pkg/scanner/vault_scanner.go
</verification>
