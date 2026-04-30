---
status: committing
summary: 'Migrated lib/ to tools.env + Makefile @version pattern: deleted tools.go, created tools.env with 11 version vars, rewrote Makefile with @version invocations, updated 3 counterfeiter directives to @v6.12.2, bumped bborbe deps to migrated versions, and reset go.mod from 502 to 99 lines via go mod tidy — make precommit passed end-to-end.'
container: agent-084-migrate-lib-tools-go
dark-factory-version: dev
created: "2026-04-30T22:31:38Z"
queued: "2026-04-30T22:31:38Z"
started: "2026-04-30T22:31:39Z"
---

# Migrate agent/lib from tools.go to tools.env + Makefile @version pattern

<summary>
- This is sub-module #1 of 6 in the multi-module `bborbe/agent` repo. The migration applies ONLY to the `lib/` subdirectory.
- `lib/` has its own `go.mod`, `tools.go`, `Makefile` — they're independent of the rest of the repo.
- `lib/` depends on multiple bborbe libraries that have ALL been migrated already (errors, collection, cqrs, kafka, log, time, validation, vault-cli). Bumping these to migrated versions is part of the migration.
- After this prompt completes, the 5 other sub-modules (`agent/gemini`, `agent/claude`, `agent/code`, `task/controller`, `task/executor`) can migrate in parallel — they all reference `lib/` via local replace.
- All work is scoped to the `lib/` subdirectory: do NOT touch the repo root, the other sub-modules, or shared `prompts/` / `docs/` / `Makefile` at root.
</summary>

<objective>
Apply the canonical `tools.env` + Makefile `@version` pattern to the `lib/` sub-module of the agent repo. After completion: `lib/tools.go` is gone, `lib/tools.env` exists, `lib/Makefile` uses `@version`, `lib/go.mod` is dramatically smaller and references migrated bborbe deps.
</objective>

<context>
The agent repo has 6 Go modules:
- `lib/` (this prompt)
- `agent/gemini/`, `agent/claude/`, `agent/code/`
- `task/controller/`, `task/executor/`

Each is its own module with its own `go.mod`, `tools.go`, `Makefile`. The 5 non-lib modules use `replace github.com/bborbe/agent/lib => ../../lib` to reference lib locally; a published `agent/lib` version is also referenced via the regular require directive.

Key concepts (from `~/Documents/workspaces/coding/docs/go-tools-versioning-guide.md`):
- `tools.go` imports CLI tools under build tag `tools`. This pollutes go.mod with hundreds of indirect deps.
- `tools.env` declares versions as Make variables. Makefile `include`s it.
- Each Makefile tool invocation uses `go run pkg@$(VERSION)` instead of `go run -mod=mod pkg`.
- `//go:generate` counterfeiter directives use hardcoded `@v6.12.2`.
- After deleting `tools.go`, write a minimal known-good `go.mod`, then `go mod tidy` repopulates legitimate indirects.
- `osv-scanner` must be pinned to `@v2.3.1` (upstream v2.3.2+ broken).

`lib/`'s direct bborbe deps (current versions in lib/go.mod) and target migrated versions:
- `github.com/bborbe/collection` → v1.20.11
- `github.com/bborbe/cqrs` → v0.5.1
- `github.com/bborbe/errors` → v1.5.11
- `github.com/bborbe/kafka` → v1.22.12
- `github.com/bborbe/log` → v1.6.12
- `github.com/bborbe/time` → v1.25.10
- `github.com/bborbe/validation` → v1.4.12
- `github.com/bborbe/vault-cli` → v0.58.1

All other bborbe deps (run, math, parse, sentry, etc.) flow in transitively from these and will resolve to migrated versions automatically when `go mod tidy` runs.

Pilot evidence: 24 bborbe libraries migrated using this pattern; go.mod typically shrinks 5x to 50x. See `bborbe/errors v1.5.11` for the canonical first example.
</context>

<requirements>
1. **All work is in `lib/` subdirectory.** Do NOT modify files at the repo root or in any other sub-module (`agent/`, `task/`, `docs/`, `hack/`, root `Makefile`, root `CHANGELOG.md`).

2. **Create `lib/tools.env`** with this exact content:

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

3. **Update `lib/Makefile`.** Add `include tools.env` near the top. Replace every `go run -mod=mod pkg` invoking a third-party tool with `go run pkg@$(VERSION_VAR)` using the variable from `tools.env`. Standard mappings:
   - `go-modtool` → `@$(GO_MODTOOL_VERSION)`
   - `goimports-reviser/v3` → `@$(GOIMPORTS_REVISER_VERSION)`
   - `segmentio/golines` → `@$(GOLINES_VERSION)`
   - `golangci-lint/v2/cmd/golangci-lint` → `@$(GOLANGCI_LINT_VERSION)` (note `/v2/` path)
   - `kisielk/errcheck` → `@$(ERRCHECK_VERSION)`
   - `golang.org/x/vuln/cmd/govulncheck` → `@$(GOVULNCHECK_VERSION)`
   - `osv-scanner/v2/cmd/osv-scanner` → `@$(OSV_SCANNER_VERSION)`
   - `gosec/v2/cmd/gosec` → `@$(GOSEC_VERSION)`
   - `google/addlicense` → `@$(ADDLICENSE_VERSION)`

   Leave unchanged: `go vet -mod=mod`, `go test -mod=mod`, `go list -mod=mod`, `go generate -mod=mod`, and any `trivy fs ...` invocation.

4. **Update every `//go:generate` counterfeiter directive in `lib/`.** Find every file containing `//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate` and replace with `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate`.

5. **Delete `lib/tools.go`** entirely.

6. **Bump bborbe deps in `lib/go.mod` to migrated versions** (executed inside `lib/`):

   ```
   go get \
     github.com/bborbe/errors@v1.5.11 \
     github.com/bborbe/collection@v1.20.11 \
     github.com/bborbe/cqrs@v0.5.1 \
     github.com/bborbe/kafka@v1.22.12 \
     github.com/bborbe/log@v1.6.12 \
     github.com/bborbe/time@v1.25.10 \
     github.com/bborbe/validation@v1.4.12 \
     github.com/bborbe/vault-cli@v0.58.1
   ```

7. **Reset `lib/go.mod` to a minimal known-good state, then tidy.**

   a. Identify the real direct deps of `lib/`. Read every non-`tools.go`, non-`vendor/` `.go` file in `lib/`, extract `import` blocks, filter out stdlib and `golang.org/x/...` (if golang.org/x is only transitive). Remaining external imports = direct deps.
   b. Manually rewrite `lib/go.mod` as: `module github.com/bborbe/agent/lib`, `go 1.26.2`, then a single `require (...)` block listing only direct deps (with versions from current go.mod or the bumped versions from step 6). Drop the entire `replace (...)` block. Drop the `// indirect` requires block — `go mod tidy` will repopulate it.
   c. Run `go mod tidy` from `lib/`. Verify the new `lib/go.mod` is dramatically smaller (target: under 50 lines).
   d. Verify `lib/go.sum` was regenerated.

8. **Clean up stale CVE suppressions** in `lib/`:
   - Open `lib/.osv-scanner.toml` if it exists. Remove entries pinned to deps no longer present in the slimmed graph (typically docker, etcd, bbolt, aws-sdk transitives that came in via tools.go). Re-run `make osv-scanner` after removal; if scanner still passes, the entry was dead and stays removed.
   - Inspect the `make vulncheck` target in `lib/Makefile`. If it has a `jq -e 'select(... .finding.osv != "GO-..." ...)'` filter, attempt to drop each ID one at a time and re-run `make vulncheck`. Drop IDs no longer triggered.

9. **Run `make precommit` from `lib/`.** Must pass end-to-end. Real CVEs in actual production deps (post-cleanup) should be left visible — surface in commit message, do NOT invent suppressions.

10. **Verify `lib/mocks/` regeneration works.** `make generate` from `lib/` should run successfully.

11. **Commit + tag.** Use the existing release workflow on master. The CHANGELOG entry (in `lib/CHANGELOG.md` or root CHANGELOG, whichever the repo uses for lib releases) should describe: "Migrate to tools.env + Makefile @version pattern; remove tools.go and obsolete replace block. lib/go.mod reduced from <N> to <M> lines."
</requirements>

<verification>
- `lib/tools.env` exists with the 11 version variables
- `lib/tools.go` does NOT exist
- `lib/Makefile` includes `tools.env`, contains zero `go run -mod=mod ` invocations for third-party tools
- All `//go:generate` directives in `lib/` use `@v6.12.2` syntax for counterfeiter
- `lib/go.mod` does not contain a `replace (` block with the four obsolete entries (cellbuf, go-header, go-diskfs, ginkgolinter)
- `lib/go.mod` line count dramatically reduced (target: under 50 lines for this lib)
- `lib/go.mod` references migrated bborbe versions (errors v1.5.11+, collection v1.20.11+, etc.)
- `make precommit` from `lib/` passes end-to-end
- `git status` shows: added `lib/tools.env`, deleted `lib/tools.go`, modified `lib/Makefile` and `lib/go.mod` and `lib/go.sum`
- NO files outside `lib/` are modified
</verification>

<constraints>
- Do NOT touch any file outside `lib/` (no root Makefile, no other sub-module, no shared docs)
- Don't bump dependency versions beyond the explicit list in step 6 (no `go get -u`)
- Don't refactor production code or test code logic
- Don't touch `vendor/` (gitignored in canonical layout)
- Don't add new linters or remove existing ones from `make check`
- Don't change Go language version in `lib/go.mod`
- Don't replace `trivy fs ...` with `go run` — trivy is a system binary
- Don't invent `.osv-scanner.toml` suppressions
- The `replace github.com/bborbe/agent/lib => ../../lib` directives in OTHER sub-modules are NOT touched here — they're handled when those sub-modules migrate
</constraints>
