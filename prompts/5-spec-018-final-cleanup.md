---
status: draft
spec: [018-use-git-rest-for-vault-writes]
created: "2026-05-02T19:50:00Z"
branch: dark-factory/use-git-rest-for-vault-writes
---

<summary>
- `task/controller/pkg/gitclient/` directory deleted entirely — no remaining imports in the codebase
- `task/controller/pkg/conflict/` directory deleted entirely — Gemini conflict resolver was only used by gitclient
- `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY`, `GIT_SSH_COMMAND` env vars removed from the StatefulSet; corresponding struct fields removed from `main.go`
- `git-ssh-key` removal already done in prompt 4; `gemini-api-key` key removed from Secret in this prompt
- `vaultLocalPath` const stays in `main.go` (still needed as the logical base path for gitrestclient's `Path()`)
- `docs/controller-design.md` updated: `## Git Operation Serialization`, `## Push Retry with Rebase`, `## LLM Conflict Resolution` sections removed; new `## Vault Writes via git-rest` section added
- `make precommit` passes in `task/controller/`

**HUMAN-ONLY GATE — read before approving this prompt:** the conditions below cannot be verified by an automated agent (no cluster access, no clock for "24 h"). DO NOT approve this prompt until the human operator has confirmed:
1. `USE_GIT_REST=true` has been running in dev for at least 24 h
2. Scenario `scenarios/use-git-rest-for-vault-writes.md` has been manually executed and passed
3. Real Kafka commands (CreateTask, UpdateFrontmatter, WriteResult) have been observed in controller logs without errors
4. `prod` namespace `vault-obsidian-openclaw` StatefulSet is running (spec prerequisite)

The agent itself does NOT check these. Approval is the gate.
</summary>

<objective>
Remove the now-unused `pkg/gitclient/` and `pkg/conflict/` packages, drop the corresponding flags from `main.go` and manifests, and update `docs/controller-design.md` to reflect the post-migration architecture. This is the final cutover prompt for spec-018, executed only after dev burn-in confirms the git-rest path is stable.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

**PREREQUISITE CHECK — run first, stop if any fails:**
```bash
# Verify gitclient is no longer needed (only gitrestclient should be used in main.go)
grep -n "gitclient\." task/controller/main.go
# Should only show gitclient.GitClient as a type (the interface); if it shows NewGitClient calls, gitclient is still in use

# Verify gitrestclient adapter implements gitclient.GitClient
grep -n "gitclient.GitClient" task/controller/pkg/gitrestclient/git_client_adapter.go
# Must show: NewGitClient returns gitclient.GitClient

# Verify no remaining callers of pkg/gitclient concrete functions
grep -rn "gitclient.NewGitClient" task/controller/
# Must return no matches (NewGitClient is now in gitrestclient; the gitclient package's NewGitClient should not be called)
```

If `gitclient.NewGitClient` is still called anywhere (from an unflagged non-gitrest code path), STOP. The `USE_GIT_REST=false` path still needs gitclient; do not delete it until the flag is removed.

**Key files to read in full before editing:**

- `task/controller/main.go` — to understand which fields and imports must be removed. The `GitURL`, `GitBranch`, `GeminiAPIKey` struct fields are removed. The `conflict.NewGeminiConflictResolver`, `gitclient.NewGitClient`, `EnsureCloned` from the `else` branch are removed. The `if a.UseGitRest { ... } else { ... }` block collapses to just the `if` body. `GeminiAPIKey`, `GitURL`, `GitBranch` struct tags go away.

- `task/controller/pkg/gitclient/git_client.go` and all files in `task/controller/pkg/gitclient/` — to confirm the package will be safe to delete. After deletion, `gitclient.GitClient` must still be accessible via an import from SOMEWHERE (either the interface moves or callers stop using it as a named type). 

  **CRITICAL:** The `gitclient.GitClient` INTERFACE is used throughout the codebase as the type for dependency injection (`scanner.NewVaultScanner`, factory functions, command constructors). Deleting `pkg/gitclient/` removes this interface definition. Two options:
  
  a. Move the interface to `pkg/gitrestclient/` and update all import paths. (Recommended — clean final state.)
  b. Keep a stub `pkg/gitclient/git_client.go` with ONLY the interface definition and no implementation. (Simpler but leaves a vestigial package.)

  **Recommended: move `GitClient` interface to `pkg/gitrestclient/git_client.go`** (the file that currently defines `GitRestClient`). All callers update their import from `gitclient "github.com/bborbe/agent/task/controller/pkg/gitclient"` to `gitrestclient "github.com/bborbe/agent/task/controller/pkg/gitrestclient"`.

  After the move, regenerate mocks (`make generate`) because the counterfeiter annotation changes package.

- `docs/controller-design.md` — read the full file to identify which sections to rewrite.

Run before editing:
```bash
grep -rn "\"github.com/bborbe/agent/task/controller/pkg/gitclient\"" task/controller/
grep -rn "\"github.com/bborbe/agent/task/controller/pkg/conflict\"" task/controller/
wc -l docs/controller-design.md
```
</context>

<requirements>

1. **Move `GitClient` interface to `pkg/gitrestclient/`**

   a. Copy the `GitClient` interface definition (all 10 methods after prompt 2) from `pkg/gitclient/git_client.go` into `task/controller/pkg/gitrestclient/git_rest_client.go` (append it after the `GitRestClient` interface).

   b. Update the counterfeiter annotation for the `GitClient` interface:
   ```go
   //counterfeiter:generate -o ../../mocks/git_client.go --fake-name FakeGitClient . GitClient
   ```
   This annotation moves from `pkg/gitclient/git_client.go` to `pkg/gitrestclient/git_rest_client.go`.

   c. Update `git_client_adapter.go` return type annotation — `NewGitClient` already returns `GitClient` but now `GitClient` is in the same package, so the return type becomes just `GitClient` (no package prefix). Adjust if needed.

2. **Update all import paths from `gitclient` to `gitrestclient` — including ALL `_test.go` files**

   Find every Go file (production AND test) importing `pkg/gitclient`:
   ```bash
   grep -rln "task/controller/pkg/gitclient" task/controller/
   grep -rln "pkg/gitclient" task/controller/ --include='*_test.go'
   ```

   For each file (covering BOTH production and test files):
   - Change `gitclient "github.com/bborbe/agent/task/controller/pkg/gitclient"` → use the alias `gitclient` for the new package: `gitclient "github.com/bborbe/agent/task/controller/pkg/gitrestclient"` — this minimizes the diff because every occurrence of `gitclient.GitClient` in command executors, scanner, result_writer, factory, main.go and tests stays unchanged.
   - Re-run the grep above after the change. Both commands MUST return zero matches before continuing.

3. **Regenerate mocks after moving the interface**

   ```bash
   cd task/controller && make generate
   ```

   The `mocks/git_client.go` will be regenerated from the annotation's new location in `pkg/gitrestclient/`. Verify it still compiles with all existing tests:
   ```bash
   cd task/controller && make test
   ```

4. **Delete `task/controller/pkg/gitclient/`**

   ```bash
   rm -rf task/controller/pkg/gitclient/
   ```

   Verify no remaining imports:
   ```bash
   grep -rn "pkg/gitclient" task/controller/
   # Must return no matches
   ```

5. **Delete `task/controller/pkg/conflict/`**

   ```bash
   rm -rf task/controller/pkg/conflict/
   ```

   Verify no remaining imports:
   ```bash
   grep -rn "pkg/conflict" task/controller/
   # Must return no matches
   ```

6. **Remove old flags from `task/controller/main.go`**

   Remove from the `application` struct:
   ```go
   GitURL       string `required:"true"  arg:"git-url"        env:"GIT_URL"        ...`
   GitBranch    string `required:"false" arg:"git-branch"      env:"GIT_BRANCH"     ...`
   GeminiAPIKey string `required:"true"  arg:"gemini-api-key"  env:"GEMINI_API_KEY"  ...`
   ```

   Remove the `if a.UseGitRest { ... } else { ... }` block, keeping only the gitrestclient construction:
   ```go
   if a.GitRestURL == "" {
       return errors.Errorf(ctx, "GIT_REST_URL is required")
   }
   restClient := gitrestclient.NewGitRestClient(a.GitRestURL)
   gitClient := gitrestclient.NewGitClient(restClient, vaultLocalPath)
   if err := gitClient.EnsureCloned(ctx); err != nil {
       return errors.Wrapf(ctx, err, "probe git-rest readiness")
   }
   ```

   Remove the `UseGitRest bool` field (now always true) and the `USE_GIT_REST` env / flag.

   Remove the import of `"github.com/bborbe/agent/task/controller/pkg/conflict"` (no longer needed).

   The `vaultLocalPath = "/data/vault"` const MUST stay — gitrestclient's `NewGitClient` still uses it as the logical basePath for `Path()` and relative path computations.

   Also remove the scanner dual-path — keep only the gitrestclient scanner path:
   ```go
   trigger := make(chan struct{}, 1)
   restScanner := scanner.NewGitRestVaultScanner(gitClient, a.TaskDir, a.PollInterval, trigger)
   syncLoop := pkgsync.NewSyncLoop(
       restScanner,
       publisher.NewTaskPublisher(eventObjectSender, lib.TaskV1SchemaID, currentDateTime),
       trigger,
   )
   ```

   **Resolve `factory.CreateSyncLoop` cleanly.** The current code in main.go calls `factory.CreateSyncLoop`; replacing that call with the inline construction above leaves `factory.CreateSyncLoop` as dead code that golangci-lint's `unused` will flag. Pick exactly one of:
   - **(preferred)** Delete `factory.CreateSyncLoop` AND any factory tests referencing it; remove its imports — `make precommit` then passes cleanly.
   - **(only if external callers exist)** Run `grep -rn "factory.CreateSyncLoop" task/controller/` first; if no external callers, do not keep it.

   Do not leave `factory.CreateSyncLoop` defined-but-unused.

7. **Remove old env vars from `task/controller/k8s/agent-task-controller-sts.yaml`**

   Remove:
   - `GIT_URL` env var
   - `GIT_BRANCH` env var
   - `GEMINI_API_KEY` (the secretKeyRef block)

   Remove `USE_GIT_REST` (was `"true"` but the flag is gone now; the behavior is always gitrest).

   **Do NOT remove `NO_SYNC`** — it is a BoltDB option (controls fsync on the offset DB at `/data/bolt`), unrelated to the git migration. Out of scope for this prompt.

   Verify the targeted env vars are gone (NO_SYNC should still be present):
   ```bash
   grep -n "GIT_URL\|GIT_BRANCH\|GEMINI_API_KEY\|GIT_SSH\|ssh-key\|USE_GIT_REST" task/controller/k8s/agent-task-controller-sts.yaml
   # Must return no matches
   grep -n "NO_SYNC" task/controller/k8s/agent-task-controller-sts.yaml
   # SHOULD still be present (BoltDB option, untouched)
   ```

8. **Update `task/controller/k8s/agent-task-controller-secret.yaml`**

   Remove `gemini-api-key` data key (Gemini API key no longer needed by controller).

   Final secret contains only `sentry-dsn`.

9. **Update `docs/controller-design.md`**

   a. **Remove or rewrite** these sections (they describe the old git plumbing that no longer exists):
   - `## Git Operation Serialization` (or equivalent section about mutex-based git access)
   - `## Push Retry with Rebase`
   - `## LLM Conflict Resolution`

   b. **Add new section** `## Vault Writes via git-rest` after `## Command Processing (Kafka → git)`:
   ```markdown
   ## Vault Writes via git-rest

   The controller holds no local git clone. All vault file operations flow through the
   `vault-obsidian-openclaw` git-rest StatefulSet via HTTP:

   | Operation | HTTP call | Who commits |
   |-----------|-----------|-------------|
   | Read file | `GET /api/v1/files/{relPath}` | N/A |
   | Write file | `POST /api/v1/files/{relPath}` | git-rest (auto-commit) |
   | Delete file | `DELETE /api/v1/files/{relPath}` | git-rest (auto-commit) |
   | List files | `GET /api/v1/files/?glob={pattern}` | N/A |

   git-rest ensures one commit per write. The controller's `/readiness` endpoint reflects
   git-rest readiness: if git-rest returns 503 (push stuck), the controller reports 503
   and the Kafka consumer goroutine blocks inside the write retry loop until git-rest
   recovers. Kafka offsets are not advanced during this block.

   BoltDB (at `/data/bolt` on the `datadir` PVC) continues to track Kafka consumer
   offsets — unchanged from the pre-migration architecture.
   ```

   c. **Update `## Change Detection (git → Kafka)` section** to note that `Pull()` is a no-op (git-rest handles pulls internally) and file enumeration uses `gitClient.ListFiles`.

10. **Update `CHANGELOG.md` at repo root**

    Append bullets to `## Unreleased` (insert the heading under `# Changelog` if absent):

    ```markdown
    - feat(task/controller): delete `pkg/gitclient/` and `pkg/conflict/` — all vault I/O now flows through git-rest HTTP API
    - feat(task/controller): remove `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY` flags and manifests — git-rest holds the SSH key
    - docs: update `docs/controller-design.md` — rewrite vault write sections to reflect git-rest architecture
    ```

11. **Run final verification:**

    ```bash
    cd task/controller && make precommit
    ```
    Must exit 0. (`make precommit` already runs the test target; do not run `make test` separately.)

</requirements>

<constraints>
- Move the `GitClient` INTERFACE to `pkg/gitrestclient/` — do not delete it. Import alias `gitclient` in all callers to minimize diff.
- `vaultLocalPath = "/data/vault"` const must NOT be deleted — used by gitrestclient as the logical base path
- `pkg/gitclient/` and `pkg/conflict/` MUST be fully deleted — no remaining Go files
- `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY` removed from both the Go struct and the StatefulSet YAML
- `USE_GIT_REST` removed from Go struct and YAML (behavior is now always gitrest)
- `factory.CreateSyncLoop` may remain in `pkg/factory/factory.go` for now (it's a utility; removal is deferred)
- Error wrapping via `github.com/bborbe/errors` — never `fmt.Errorf`
- All Go files (production + `_test.go`) that import `pkg/gitclient` must update to import `pkg/gitrestclient` with the alias `gitclient` to keep the diff minimal
- The dev-burn-in gate is a HUMAN approval gate (see `<summary>`). The agent does not check cluster state.
- `factory.CreateSyncLoop` MUST NOT be left as defined-but-unused — either delete it (preferred) or leave only if still imported elsewhere; lint will fail otherwise.
- `NO_SYNC` env var is a BoltDB option and is OUT OF SCOPE for this prompt — do NOT remove it.
- Do NOT commit — dark-factory handles git
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>
```bash
# Verify gitclient package deleted
ls task/controller/pkg/gitclient/ 2>&1
# Must say: no such file or directory

# Verify conflict package deleted
ls task/controller/pkg/conflict/ 2>&1
# Must say: no such file or directory

# Verify no remaining gitclient imports
grep -rn "task/controller/pkg/gitclient" task/controller/
# Must return no matches

# Verify no remaining conflict imports
grep -rn "task/controller/pkg/conflict" task/controller/
# Must return no matches

# Verify GitClient interface moved to gitrestclient
grep -n "type GitClient interface" task/controller/pkg/gitrestclient/git_rest_client.go
# Must show the interface

# Verify old flags removed from main.go
grep -n "GitURL\|GitBranch\|GeminiAPIKey\|UseGitRest" task/controller/main.go
# Must return no matches

# Verify StatefulSet cleaned
grep -n "GIT_URL\|GIT_BRANCH\|GEMINI_API_KEY\|ssh-key\|USE_GIT_REST" task/controller/k8s/agent-task-controller-sts.yaml
# Must return no matches

# Verify controller-design.md updated
grep -n "git-rest\|Vault Writes via git-rest" docs/controller-design.md
# Must show the new section

# Verify Push Retry and LLM sections gone
grep -n "Push Retry\|LLM Conflict" docs/controller-design.md
# Must return no matches (or only in a historical note if you choose to keep one)

# Run all tests
cd task/controller && make test
# Must exit 0

cd task/controller && make precommit
# Must exit 0

grep -n "delete \`pkg/gitclient\|pkg/conflict\|remove \`GIT_URL\|git-rest" CHANGELOG.md
# Must show Unreleased entries (matches the bullets added in step 10)
```
</verification>
