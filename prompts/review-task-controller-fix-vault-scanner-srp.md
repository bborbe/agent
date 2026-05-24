---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Extracts frontmatter parsing helpers from vault_scanner.go into dedicated files in pkg/scanner/
- Extracts UUID/task-identifier injection helpers into their own file
- Reduces vault_scanner.go from 485 lines to ~300 lines
- All extracted helpers retain their existing signatures — no API changes
</summary>

<objective>
`vault_scanner.go` contains three distinct semantic domains: (1) frontmatter/body parsing, (2) UUID/task-identifier injection, and (3) file-scanning and vault-polling orchestration. This violates the Single Responsibility Principle and makes the file difficult to test and maintain. After this fix, the file is split into focused units by semantic domain.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner.go` — full file, identify helper boundaries
</context>

<requirements>

1. **Extract frontmatter parsing helpers** into `pkg/scanner/frontmatter.go`:

   Move these functions from `vault_scanner.go` into a new file `pkg/scanner/frontmatter.go`:
   - `extractFrontmatter` (~line 405)
   - `deduplicateFrontmatter` (~line 405)
   - `extractBody` (~line 405)

   The new file should have the standard copyright header and package declaration:
   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package scanner
   ```

2. **Extract UUID injection helpers** into `pkg/scanner/task_identifier.go`:

   Move these functions from `vault_scanner.go` into a new file `pkg/scanner/task_identifier.go`:
   - `injectTaskIdentifier` (~line 391)
   - `removeTaskIdentifier` (~line 377)
   - `isValidUUID` (~line 289)
   - `isIdentifierUnique` (~line 289)

   Note: `injectTaskIdentifier` was fixed in a prior prompt to accept `ctx context.Context` — ensure the extracted version keeps that parameter.

3. **Keep in `vault_scanner.go`** the core scanning logic:
   - `VaultScanner` interface
   - `vaultScanner` struct and methods (`Run`, `runCycle`, `scanFiles`, `processFile`, `collectDeleted`)
   - `fileOps` struct and `newLocalFileOps`, `newGitRestVaultScanner` constructors
   - `NewVaultScanner`, `NewGitRestVaultScanner` factory functions

4. **Update imports in `vault_scanner.go`** — after extracting helpers, the remaining file should have no import changes needed since all helpers are within the same package.

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
- Only create new files in `pkg/scanner/` and edit `vault_scanner.go`
- Do NOT change function signatures — only move code
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit
</verification>
