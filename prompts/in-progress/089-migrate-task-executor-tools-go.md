---
status: approved
created: "2026-04-30T22:38:39Z"
queued: "2026-04-30T22:39:17Z"
---

# Migrate task/executor from tools.go to tools.env + Makefile @version pattern

<summary>
- This is a sub-module of the multi-module `bborbe/agent` repo. The migration applies ONLY to the `task/executor/` subdirectory.
- `task/executor/` has its own `go.mod`, `tools.go`, `Makefile` — they're independent of the rest of the repo.
- This sub-module references `agent/lib` via `replace github.com/bborbe/agent/lib => ../../lib` (local path); the `lib/` sub-module must already be migrated before this prompt runs (see prompt 1).
- All work is scoped to the `task/executor/` subdirectory: do NOT touch the repo root, `lib/`, other sub-modules, or shared `prompts/`/`docs/` at root.
</summary>

<objective>
Apply the canonical `tools.env` + Makefile `@version` pattern to the `task/executor/` sub-module of the agent repo. After completion: `task/executor/tools.go` is gone, `task/executor/tools.env` exists, `task/executor/Makefile` uses `@version`, `task/executor/go.mod` is dramatically smaller and references migrated bborbe deps.
</objective>

<context>
Reference guide: `~/Documents/workspaces/coding/docs/go-tools-versioning-guide.md`. Pattern validated on 24 bborbe libraries (errors v1.5.11 → cqrs v0.5.1) and the `agent/lib` sub-module. Same recipe: delete `tools.go`, add `tools.env`, switch Makefile to `@version`, slim `go.mod`, run `go mod tidy`. `osv-scanner` pinned to v2.3.1 (upstream v2.3.2+ broken).

The `replace github.com/bborbe/agent/lib => ../../lib` directive in `task/executor/go.mod` MUST be preserved — it's how this sub-module references the local lib. Do not remove that replace.
</context>

<requirements>
1. **All work is in `task/executor/` subdirectory.** Do NOT modify files outside `task/executor/`.

2. **Create `task/executor/tools.env`** with this exact content:

   ```
   # Canonical tool versions for all bborbe Go projects.
   # Each repo should keep its tools.env in sync with the canonical file.
   # COUNTERFEITER_VERSION must also match all `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@<ver>` directives.

   ADDLICENSE_VERSION         ?= v1.2.0
   COUNTERFEITER_VERSION      ?= v6.12.2
   ERRCHECK_VERSION           ?= v1.10.0
   GINKGO_VERSION             ?= v2.28.3
   GOIMPORTS_REVISER_VERSION  ?= v3.12.6
   GOLANGCI_LINT_VERSION      ?= v2.11.4
   GOLINES_VERSION            ?= v0.13.0
   GO_MODTOOL_VERSION         ?= v0.7.1
   GOSEC_VERSION              ?= v2.26.1
   GOVULNCHECK_VERSION        ?= v1.3.0
   OSV_SCANNER_VERSION        ?= v2.3.1
   ```

3. **Update `task/executor/Makefile`.** Add `include tools.env` near the top. Replace every `go run -mod=mod pkg` invoking a third-party tool with `go run pkg@$(VERSION_VAR)`. Standard mappings (go-modtool, goimports-reviser, golines, golangci-lint, errcheck, govulncheck, osv-scanner, gosec, addlicense). Leave `go vet -mod=mod`, `go test -mod=mod`, etc. unchanged.

4. **Update every `//go:generate` counterfeiter directive in `task/executor/`.** Replace `//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate` with `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate`.

5. **Delete `task/executor/tools.go`** entirely.

6. **Bump bborbe deps in `task/executor/go.mod` to migrated versions** (executed inside `task/executor/`):

   ```
   go get github.com/bborbe/errors@v1.5.11 \\
   go get github.com/bborbe/cqrs@v0.5.1 \\
   go get github.com/bborbe/http@v1.26.11 \\
   go get github.com/bborbe/k8s@v1.14.1 \\
   go get github.com/bborbe/kafka@v1.22.12 \\
   go get github.com/bborbe/log@v1.6.12 \\
   go get github.com/bborbe/metrics@v0.5.2
   ```

7. **Reset `task/executor/go.mod` to a minimal known-good state, then tidy.**
   - Identify real direct deps from non-tools.go, non-vendor `.go` files in `task/executor/`.
   - Rewrite `task/executor/go.mod` keeping ONLY: `module github.com/bborbe/task/executor`, `go 1.x`, the existing `replace github.com/bborbe/agent/lib => ../../lib` directive (DO NOT remove this), and a single `require (...)` block of direct deps. Drop ALL other `replace (...)` entries. Drop the `// indirect` requires block.
   - Run `go mod tidy` from `task/executor/`. Verify shrinkage.

8. **Clean up stale CVE suppressions** in `task/executor/` (`.osv-scanner.toml`, Makefile `vulncheck` jq filter — drop dead entries).

9. **Run `make precommit` from `task/executor/`.** Must pass end-to-end.

10. **Verify mocks regeneration works.** `make generate` from `task/executor/` should run successfully.

11. **Commit + tag.** Standard release workflow.
</requirements>

<verification>
- `task/executor/tools.env` exists
- `task/executor/tools.go` does NOT exist
- `task/executor/Makefile` includes `tools.env`, has zero `go run -mod=mod ` for third-party tools
- All `//go:generate` directives use `@v6.12.2` for counterfeiter
- `task/executor/go.mod` no longer contains the four obsolete replaces (cellbuf, go-header, go-diskfs, ginkgolinter)
- `task/executor/go.mod` STILL contains `replace github.com/bborbe/agent/lib => ../../lib`
- `task/executor/go.mod` shrunk significantly
- `make precommit` from `task/executor/` passes
- NO files outside `task/executor/` modified
</verification>

<constraints>
- Do NOT touch any file outside `task/executor/`
- Do NOT remove the `replace github.com/bborbe/agent/lib => ../../lib` directive
- Don't bump deps beyond the explicit list in step 6
- Don't change Go language version
- Don't replace `trivy fs ...` with `go run`
- Don't invent CVE suppressions
</constraints>
