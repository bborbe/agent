---
status: committing
summary: Bumped Go toolchain from 1.26.2 to 1.26.3 in all 6 go.mod files and 5 Dockerfiles; added CHANGELOG entry; all six service make precommit runs passed.
container: agent-102-bump-go-1-26-3
dark-factory-version: v0.154.0
created: "2026-05-07T21:10:00Z"
queued: "2026-05-07T21:47:15Z"
started: "2026-05-07T21:47:24Z"
branch: dark-factory/bump-go-1.26.3
---

<summary>
- Every `go.mod` in the repo bumps from `go 1.26.2` to `go 1.26.3` (Go stdlib CVE fix release)
- Any `Dockerfile` using `FROM golang:1.26.2` bumps to `FROM golang:1.26.3`
- Any CI config (e.g. `.github/workflows/*.yml`) referencing the Go version bumps to `1.26.3`
- All services build and `make precommit` clean per service
- `go version` reports `go1.26.3` in the test container
</summary>

<objective>
Bump every Go toolchain reference in the repo from `1.26.2` to `1.26.3` to absorb upstream stdlib CVE fixes (`GO-2026-4918`, `GO-2026-4971`). Pure version bump — no code changes, no API changes, no behavioral changes. Cross-module consistency is the win: `make precommit` must succeed in every service after the bump.
</objective>

<context>
Read `CLAUDE.md` for project conventions (multi-module mono-repo, per-service `make precommit`, errors lib).

Current state (verify before editing):
```bash
find . -name "go.mod" -not -path "*/vendor/*" -exec grep -H "^go " {} \;
```
Expected output: 6 modules, all at `go 1.26.2`:
- `lib/go.mod`
- `task/controller/go.mod`
- `task/executor/go.mod`
- `agent/claude/go.mod`
- `agent/code/go.mod`
- `agent/gemini/go.mod`

Also check Dockerfiles and CI configs:
```bash
grep -rn "1\.26\.2\|FROM golang" --include="Dockerfile" --include="*.yml" --include="*.yaml" --include="*.mod" .
```

No coding-guide references needed — this is a pure toolchain bump, no Go source changes.
</context>

<requirements>

1. **Update every `go.mod` toolchain directive** — change `go 1.26.2` to `go 1.26.3` in all 6 `go.mod` files:
   - `lib/go.mod`
   - `task/controller/go.mod`
   - `task/executor/go.mod`
   - `agent/claude/go.mod`
   - `agent/code/go.mod`
   - `agent/gemini/go.mod`

   Bulk update via:
   ```bash
   find . -name "go.mod" -not -path "*/vendor/*" -type f -exec perl -pi -e "s/^go 1\.26\.2$/go 1.26.3/" {} +
   ```

   Verify zero `1.26.2` references remain in `go.mod` files:
   ```bash
   grep -rn "go 1\.26\.2" --include="go.mod" .
   ```
   Expected: zero matches.

2. **Update Dockerfiles** — actual line shape is `FROM ${DOCKER_REGISTRY}/golang:1.26.2 AS build` (registry-prefixed, NOT bare `FROM golang:`). Match on `golang:1.26.2` directly to handle both forms:
   ```bash
   find . -name "Dockerfile" -not -path "*/vendor/*" -type f -exec perl -pi -e "s/golang:1\.26\.2/golang:1.26.3/g" {} +
   ```
   Affects 5 Dockerfiles: `task/controller/`, `task/executor/`, `agent/claude/`, `agent/code/`, `agent/gemini/`.

3. **Update CI configs** — check `.github/workflows/*.yml` (and similar) for any `go-version: 1.26.2` or `1.26.2` pin:
   ```bash
   grep -rn "1\.26\.2" --include="*.yml" --include="*.yaml" .github/ 2>/dev/null
   ```
   If matches exist, replace `1.26.2` → `1.26.3` in those files. If none, no-op.

4. **Verify `go version`** — confirm the toolchain available in the build environment is `go1.26.3` (or higher):
   ```bash
   go version
   ```
   Expected: `go version go1.26.3 ...`. If `go1.26.2` or older, the host/container Go install needs upgrade — STOP and report (this prompt assumes `go1.26.3` is available).

5. **Run `make precommit` in every service** to confirm the bump compiles + tests + lints + licenses cleanly. Use a robust loop instead of chained `cd`:
   ```bash
   for d in lib task/controller task/executor agent/claude agent/code agent/gemini; do
       (cd "$d" && make precommit) || { echo "precommit failed in $d"; exit 1; }
   done
   ```
   All six must exit 0. If any fails, STOP and report which service + the exact error.

6. **Add a CHANGELOG entry** at the repo root `CHANGELOG.md`. Per repo convention, prepend a new `## Unreleased` section above the latest version header (release tooling renames it on next release):

   ```markdown
   # Changelog

   ## Unreleased

   - chore: bump Go toolchain 1.26.2 → 1.26.3 across all modules (stdlib CVE fixes GO-2026-4918, GO-2026-4971)

   ## v0.58.0
   ...
   ```

   If a `## Unreleased` section already exists, append the entry there.

</requirements>

<constraints>
- Pure version bump — do NOT modify any `.go` source files
- Do NOT run `go mod tidy` or `go get -u` — those introduce dependency upgrades unrelated to the toolchain bump
- Do NOT delete or rename any file
- Do NOT touch `vendor/` directories
- Errors via `github.com/bborbe/errors` (project convention; not relevant for a version bump but stated for completeness)
- Run `make precommit` from each service subdir, never from repo root
- If any service's `make precommit` fails, STOP and report which service + the exact error
</constraints>

<verification>

Verify all `go.mod` files at 1.26.3:
```bash
find . -name "go.mod" -not -path "*/vendor/*" -exec grep -H "^go " {} \;
```
Expected: every line ends `go 1.26.3`.

Verify zero `1.26.2` references remain anywhere:
```bash
grep -rn "1\.26\.2" --include="go.mod" --include="Dockerfile" --include="*.yml" --include="*.yaml" .
```
Expected: zero matches.

Verify `go version`:
```bash
go version
```
Expected: `go version go1.26.3 ...`.

Verify CHANGELOG entry:
```bash
grep -A1 "## Unreleased" CHANGELOG.md | head -5
```
Expected: entry mentioning `1.26.2 → 1.26.3` and the CVE IDs.

Run all precommits (already in step 5 above; restated here for the verifier):
```bash
for d in lib task/controller task/executor agent/claude agent/code agent/gemini; do
    (cd "$d" && make precommit) || { echo "precommit failed in $d"; exit 1; }
done
```
Expected: all six exit 0.

</verification>
